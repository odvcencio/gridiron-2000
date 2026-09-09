package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// teamTransferTargetSnapshot and teamTransferBenchSnapshot deliberately read
// the rendered identity attributes, not row text. Text can change when the
// server adds a notice or projection label; source/target IDs are the stable
// contract the transfer action submits and the server persists.
type teamTransferTargetSnapshot struct {
	Slot        string `json:"slot"`
	Source      string `json:"source"`
	EligibleFor string `json:"eligibleFor"`
	Locked      string `json:"locked"`
	Eligible    string `json:"eligible"`
	Disabled    string `json:"disabled"`
	HasHandle   bool   `json:"hasHandle"`
}

type teamTransferBenchSnapshot struct {
	ID        string `json:"id"`
	Disabled  string `json:"disabled"`
	HasHandle bool   `json:"hasHandle"`
}

type teamTransferDOMSnapshot struct {
	Targets        []teamTransferTargetSnapshot `json:"targets"`
	Bench          []teamTransferBenchSnapshot  `json:"bench"`
	Path           string                       `json:"path"`
	Marker         int                          `json:"marker"`
	SubmitCount    int                          `json:"submitCount"`
	PointerDowns   int                          `json:"pointerDowns"`
	PointerUps     int                          `json:"pointerUps"`
	PointerCancels int                          `json:"pointerCancels"`
	PointerMoves   int                          `json:"pointerMoves"`
	LastMoveTarget string                       `json:"lastMoveTarget"`
	TransferErrors int                          `json:"transferErrors"`
	TransferResult int                          `json:"transferResults"`
	NavigationType string                       `json:"navigationType"`
}

const teamTransferSnapshotScript = `(function(){
	var root = document.querySelector('.team-lineup-region[data-gosx-transfer]');
	if (!root) throw new Error('Team transfer root is missing');
	function attr(e, name) { return e.getAttribute(name) || ''; }
	return {
			targets: Array.from(root.querySelectorAll('[data-gosx-transfer-target]')).map(function(e) {
				return {
					slot: attr(e, 'data-gosx-transfer-target'),
					source: attr(e, 'data-player-id') || attr(e, 'data-gosx-transfer-source'),
				eligibleFor: attr(e, 'data-gosx-transfer-eligible-for'),
				locked: attr(e, 'data-gosx-transfer-locked'),
				eligible: attr(e, 'data-gosx-transfer-eligible'),
				disabled: attr(e, 'data-gosx-transfer-disabled'),
				hasHandle: !!e.querySelector('[data-gosx-transfer-handle]')
			};
		}),
		bench: Array.from(root.querySelectorAll('.roster-list .roster-row[data-gosx-transfer-source]')).map(function(e) {
			return {
				id: attr(e, 'data-gosx-transfer-source'),
				disabled: attr(e, 'data-gosx-transfer-disabled'),
				hasHandle: !!e.querySelector('[data-gosx-transfer-handle]')
			};
		}),
		path: location.pathname + location.search,
		marker: Number(window.__teamTransferBrowserMarker || 0),
		submitCount: Number(window.__teamTransferBrowserSubmits || 0),
		pointerDowns: Number(window.__teamTransferBrowserPointerDowns || 0),
		pointerUps: Number(window.__teamTransferBrowserPointerUps || 0),
		pointerCancels: Number(window.__teamTransferBrowserPointerCancels || 0),
		pointerMoves: Number(window.__teamTransferBrowserPointerMoves || 0),
		lastMoveTarget: String(window.__teamTransferBrowserLastMoveTarget || ''),
		transferErrors: Number(window.__teamTransferBrowserErrors || 0),
		transferResults: Number(window.__teamTransferBrowserResults || 0),
		navigationType: (performance.getEntriesByType('navigation')[0] || {}).type || ''
	};
})()`

const teamTransferInstrumentationScript = `(function(){
	window.__teamTransferBrowserSubmits = 0;
	window.__teamTransferBrowserPointerDowns = 0;
	window.__teamTransferBrowserPointerUps = 0;
	window.__teamTransferBrowserPointerCancels = 0;
	window.__teamTransferBrowserPointerMoves = 0;
	window.__teamTransferBrowserLastMoveTarget = '';
	window.__teamTransferBrowserErrors = 0;
	window.__teamTransferBrowserResults = 0;
	document.addEventListener('pointerdown', function(){ window.__teamTransferBrowserPointerDowns += 1; }, true);
	document.addEventListener('pointerup', function(){ window.__teamTransferBrowserPointerUps += 1; }, true);
	document.addEventListener('pointercancel', function(){ window.__teamTransferBrowserPointerCancels += 1; }, true);
	document.addEventListener('pointermove', function(event){
		window.__teamTransferBrowserPointerMoves += 1;
		var target = event.target && event.target.closest ? event.target.closest('[data-gosx-transfer-target]') : null;
		window.__teamTransferBrowserLastMoveTarget = target ? (target.getAttribute('data-gosx-transfer-target') || '') : '';
	}, true);
	document.addEventListener('gosx:transfer:submit', function(){
		window.__teamTransferBrowserSubmits += 1;
	});
	document.addEventListener('gosx:transfer:error', function(){ window.__teamTransferBrowserErrors += 1; });
	document.addEventListener('gosx:transfer:result', function(){ window.__teamTransferBrowserResults += 1; });
})()`

// startTeamTransferFixtureDraftedChild deliberately uses a small explicit
// roster. It gives the browser scenario an occupied single-position target
// with no outgoing starter move, while leaving same-position bench candidates
// in the deterministic offline pool so the test catches a target incorrectly
// disabled by source eligibility. This is an actual app fixture, not an
// accept-all test server.
func startTeamTransferFixtureDraftedChild(t *testing.T) (*simChild, *simLeague) {
	t.Helper()
	root := teamTransferBrowserAppRoot(t)
	leagueFile := writeTeamTransferFixtureLeague(t, root)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "LEAGUE_FILE="+leagueFile)
	league := seatLeague(t, child)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start transfer fixture draft: %v", err)
	}
	for picks := 0; ; picks++ {
		state, err := league.commish.State()
		if err != nil {
			t.Fatalf("read transfer fixture draft state: %v", err)
		}
		if state.Complete {
			break
		}
		if maxPicks := len(simTeamNames)*state.Rounds + 40; state.Rounds > 0 && picks > maxPicks {
			t.Fatalf("transfer fixture draft did not complete within %d picks", maxPicks)
		}
		league.pickOnClock(t)
	}
	return child, league
}

// teamTransferBrowserAppRoot lets the coordinator run this test binary from
// an authoring worktree while pinning the child to a separately built v0.56
// runtime snapshot. The override is checked for the same manifest that
// browserAppRoot requires; an absent override keeps the normal local skip/
// fail behavior intact.
func teamTransferBrowserAppRoot(t *testing.T) string {
	t.Helper()
	if override := strings.TrimSpace(os.Getenv("GOSX_APP_ROOT")); override != "" {
		if _, err := os.Stat(filepath.Join(override, "dist", "build.json")); err != nil {
			t.Fatalf("GOSX_APP_ROOT %s has no built client runtime: %v", override, err)
		}
		return override
	}
	return browserAppRoot(t)
}

func writeTeamTransferFixtureLeague(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "config", "league.json.example"))
	if err != nil {
		t.Fatalf("read transfer fixture league example: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode transfer fixture league example: %v", err)
	}
	draftBlock, ok := document["draft"].(map[string]any)
	if !ok {
		t.Fatalf("transfer fixture league example has no draft block")
	}
	draftBlock["rounds"] = 7
	// Keep the harness's normal eight seats, but use the same names that
	// seatLeague claims. The IDs remain those from the canonical example.
	teams, ok := document["teams"].([]any)
	if !ok || len(teams) != len(simTeamNames) {
		t.Fatalf("transfer fixture league has %d teams, want %d", len(teams), len(simTeamNames))
	}
	for index, rawTeam := range teams {
		team, ok := rawTeam.(map[string]any)
		if !ok {
			t.Fatalf("transfer fixture team %d is not an object", index)
		}
		team["name"] = simTeamNames[index]
	}
	document["roster"] = map[string]any{
		"slots": map[string]int{"QB": 1, "RB": 2, "WR": 1, "TE": 1},
		"bench": 2,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode transfer fixture league: %v", err)
	}
	path := filepath.Join(t.TempDir(), "team-transfer-league.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write transfer fixture league: %v", err)
	}
	return path
}

// startTeamTransferFlagshipDraftedChild is the hermetic acceptance fixture
// for the shipped gridiron-house shape: 11 starters (including SUPERFLEX and
// P) plus 6 bench players. It loads the same embedded fantasy pool the app
// uses, adding the twelve P rows the embedded pool intentionally omits and
// depth for K/DST so this browser proof does not conflate transfer behavior
// with the draft scarcity guard; no route or test server substitutes lineup
// validation.
func startTeamTransferFlagshipDraftedChild(t *testing.T) (*simChild, *simLeague) {
	t.Helper()
	root := teamTransferBrowserAppRoot(t)
	leagueFile := writeTeamTransferFlagshipLeague(t, root)
	fantasyRoot := writeTeamTransferFantasyCache(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "LEAGUE_FILE="+leagueFile, "FANTASY_ROOT="+fantasyRoot, "GRIDIRON_TEST_POOL=")
	league := seatLeague(t, child)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start flagship transfer fixture draft: %v", err)
	}
	for picks := 0; ; picks++ {
		state, err := league.commish.State()
		if err != nil {
			t.Fatalf("read flagship transfer fixture draft state: %v", err)
		}
		if state.Complete {
			break
		}
		if maxPicks := len(simTeamNames)*state.Rounds + 40; state.Rounds > 0 && picks > maxPicks {
			t.Fatalf("flagship transfer fixture draft did not complete within %d picks", maxPicks)
		}
		league.pickOnClock(t)
	}
	return child, league
}

func writeTeamTransferFlagshipLeague(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "config", "league.json.example"))
	if err != nil {
		t.Fatalf("read flagship transfer league example: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode flagship transfer league example: %v", err)
	}
	draftBlock, ok := document["draft"].(map[string]any)
	if !ok {
		t.Fatalf("flagship transfer league example has no draft block")
	}
	draftBlock["rounds"] = 17
	teams, ok := document["teams"].([]any)
	if !ok || len(teams) != len(simTeamNames) {
		t.Fatalf("flagship transfer league has %d teams, want %d", len(teams), len(simTeamNames))
	}
	for index, rawTeam := range teams {
		team, ok := rawTeam.(map[string]any)
		if !ok {
			t.Fatalf("flagship transfer team %d is not an object", index)
		}
		team["name"] = simTeamNames[index]
	}
	document["roster"] = map[string]any{"preset": "gridiron-house"}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode flagship transfer league: %v", err)
	}
	path := filepath.Join(t.TempDir(), "team-transfer-flagship-league.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write flagship transfer league: %v", err)
	}
	return path
}

func writeTeamTransferFantasyCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	players := fantasy.OfflinePool()
	for index := 1; index <= 12; index++ {
		players = append(players, fantasy.Player{
			ID:         fmt.Sprintf("fixture-punter-%02d", index),
			Name:       fmt.Sprintf("Fixture Punter %02d", index),
			Position:   "P",
			NFLTeam:    "TST",
			ADP:        float64(500 + index),
			ADPRank:    500 + index,
			Projection: 7.0 - float64(index)*0.05,
		})
	}
	for _, position := range []string{"K", "DST"} {
		for index := 1; index <= 12; index++ {
			players = append(players, fantasy.Player{
				ID:         fmt.Sprintf("fixture-%s-%02d", strings.ToLower(position), index),
				Name:       fmt.Sprintf("Fixture %s %02d", position, index),
				Position:   position,
				NFLTeam:    "TST",
				ADP:        float64(600 + index),
				ADPRank:    600 + index,
				Projection: 6.0 - float64(index)*0.05,
			})
		}
	}
	cache := struct {
		SchemaVersion  int              `json:"schemaVersion"`
		Provider       string           `json:"provider"`
		Scoring        string           `json:"scoring"`
		SyncedAt       time.Time        `json:"syncedAt"`
		Players        []fantasy.Player `json:"players"`
		ProjectionWeek int              `json:"projectionWeek,omitempty"`
	}{
		SchemaVersion:  fantasy.SchemaVersion,
		Provider:       "tank01",
		Scoring:        "half_ppr",
		SyncedAt:       time.Now().UTC(),
		Players:        players,
		ProjectionWeek: 1,
	}
	encoded, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("encode flagship fantasy cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "players.json"), encoded, 0o600); err != nil {
		t.Fatalf("write flagship fantasy cache: %v", err)
	}
	return root
}

func readTeamTransferDOM(t *testing.T, ctx context.Context) teamTransferDOMSnapshot {
	t.Helper()
	var snapshot teamTransferDOMSnapshot
	if err := chromedp.Run(ctx, chromedp.Evaluate(teamTransferSnapshotScript, &snapshot)); err != nil {
		t.Fatalf("read Team transfer DOM snapshot: %v", err)
	}
	if len(snapshot.Targets) == 0 {
		t.Fatalf("Team transfer DOM snapshot has no starter targets")
	}
	return snapshot
}

func transferAttrTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func transferAllowsSource(raw, sourceID string) bool {
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		if token == "*" || token == sourceID {
			return true
		}
	}
	// GoSX treats an omitted allowlist as unrestricted. The Team view normally
	// renders a non-empty authoritative list, but matching the runtime here
	// keeps the browser fixture honest if a target has no policy attribute.
	return strings.TrimSpace(raw) == ""
}

func chooseBenchPromotion(snapshot teamTransferDOMSnapshot) (teamTransferBenchSnapshot, teamTransferTargetSnapshot, bool) {
	var preferredBench teamTransferBenchSnapshot
	var preferredTarget teamTransferTargetSnapshot
	preferred := false
	for _, bench := range snapshot.Bench {
		if bench.ID == "" || !bench.HasHandle || transferAttrTruthy(bench.Disabled) {
			continue
		}
		for _, target := range snapshot.Targets {
			if target.Source == "" || transferAttrTruthy(target.Locked) || !transferAttrTruthy(target.Eligible) || !transferAllowsSource(target.EligibleFor, bench.ID) {
				continue
			}
			// Prefer a fixed target with no outgoing handle. It is the
			// incoming-only case that must remain usable even when the
			// source's outgoing starter-move set is empty.
			candidate := !target.HasHandle
			if !preferred || candidate {
				preferredBench = bench
				preferredTarget = target
				preferred = true
				if candidate {
					break
				}
			}
		}
		if preferred && !preferredTarget.HasHandle {
			break
		}
	}
	return preferredBench, preferredTarget, preferred
}

func chooseFlagshipVisiblePromotion(snapshot teamTransferDOMSnapshot) (teamTransferBenchSnapshot, teamTransferTargetSnapshot, bool) {
	// The 17-player house view is intentionally a phone touch smoke test, not
	// an edge-scroll test. Prefer the deepest flexible starter so a legal
	// bench promotion can be staged in one viewport; the sparse fixture above
	// separately proves the incoming-only target with no outgoing handle.
	for _, slotID := range []string{"FLEX", "SUPERFLEX"} {
		for _, bench := range snapshot.Bench {
			if bench.ID == "" || !bench.HasHandle || transferAttrTruthy(bench.Disabled) {
				continue
			}
			for _, target := range snapshot.Targets {
				if target.Slot != slotID || target.Source == "" || transferAttrTruthy(target.Locked) || transferAttrTruthy(target.Disabled) || !transferAttrTruthy(target.Eligible) || !transferAllowsSource(target.EligibleFor, bench.ID) {
					continue
				}
				return bench, target, true
			}
		}
	}
	return chooseBenchPromotion(snapshot)
}

func chooseStarterTransfer(snapshot teamTransferDOMSnapshot) (teamTransferTargetSnapshot, teamTransferTargetSnapshot, bool) {
	for _, source := range snapshot.Targets {
		if source.Source == "" || !source.HasHandle || transferAttrTruthy(source.Locked) || transferAttrTruthy(source.Disabled) {
			continue
		}
		for _, target := range snapshot.Targets {
			if target.Slot == source.Slot || transferAttrTruthy(target.Locked) || transferAttrTruthy(target.Disabled) || !transferAttrTruthy(target.Eligible) || !transferAllowsSource(target.EligibleFor, source.Source) {
				continue
			}
			return source, target, true
		}
	}
	return teamTransferTargetSnapshot{}, teamTransferTargetSnapshot{}, false
}

func chooseIneligibleTarget(snapshot teamTransferDOMSnapshot) (teamTransferTargetSnapshot, teamTransferTargetSnapshot, bool) {
	for _, source := range snapshot.Targets {
		if source.Source == "" || !source.HasHandle || transferAttrTruthy(source.Locked) || transferAttrTruthy(source.Disabled) {
			continue
		}
		for _, target := range snapshot.Targets {
			if target.Slot == source.Slot || transferAttrTruthy(target.Locked) || !transferAttrTruthy(target.Eligible) || strings.TrimSpace(target.EligibleFor) == "" {
				continue
			}
			if !transferAllowsSource(target.EligibleFor, source.Source) {
				return source, target, true
			}
		}
	}
	return teamTransferTargetSnapshot{}, teamTransferTargetSnapshot{}, false
}

func transferSnapshotIdentity(snapshot teamTransferDOMSnapshot) (map[string]string, map[string]bool) {
	targets := make(map[string]string, len(snapshot.Targets))
	for _, target := range snapshot.Targets {
		targets[target.Slot] = target.Source
	}
	bench := make(map[string]bool, len(snapshot.Bench))
	for _, player := range snapshot.Bench {
		bench[player.ID] = true
	}
	return targets, bench
}

func assertFlagshipRosterShape(t *testing.T, snapshot teamTransferDOMSnapshot) {
	t.Helper()
	if len(snapshot.Targets) != 11 || len(snapshot.Bench) != 6 {
		t.Fatalf("flagship lineup shape = %d starters + %d bench, want 11 + 6: %+v", len(snapshot.Targets), len(snapshot.Bench), snapshot)
	}
	seen := make(map[string]bool, len(snapshot.Targets)+len(snapshot.Bench))
	for _, target := range snapshot.Targets {
		if target.Source == "" {
			t.Fatalf("flagship starter %s has no stable player identity", target.Slot)
		}
		if seen[target.Source] {
			t.Fatalf("flagship lineup repeats player identity %q", target.Source)
		}
		seen[target.Source] = true
	}
	for _, bench := range snapshot.Bench {
		if bench.ID == "" {
			t.Fatal("flagship bench row has no stable player identity")
		}
		if seen[bench.ID] {
			t.Fatalf("flagship lineup repeats player identity %q", bench.ID)
		}
		seen[bench.ID] = true
	}
	if len(seen) != 17 {
		t.Fatalf("flagship lineup has %d unique players, want 17", len(seen))
	}
}

func assertBenchPromotion(t *testing.T, before, after teamTransferDOMSnapshot, bench teamTransferBenchSnapshot, target teamTransferTargetSnapshot) {
	t.Helper()
	beforeTargets, beforeBench := transferSnapshotIdentity(before)
	afterTargets, afterBench := transferSnapshotIdentity(after)
	if afterTargets[target.Slot] != bench.ID {
		t.Fatalf("%s after bench transfer = %q, want selected bench player %q", target.Slot, afterTargets[target.Slot], bench.ID)
	}
	if target.Source == "" {
		t.Fatalf("bench transfer target %s had no displaced starter", target.Slot)
	}
	if !afterBench[target.Source] {
		t.Fatalf("displaced starter %q is not on the bench after transfer", target.Source)
	}
	if afterBench[bench.ID] {
		t.Fatalf("promoted bench player %q remained on the bench", bench.ID)
	}
	for slot, source := range beforeTargets {
		if slot == target.Slot {
			continue
		}
		if afterTargets[slot] != source {
			t.Fatalf("unrelated starter %s changed from %q to %q", slot, source, afterTargets[slot])
		}
	}
	for playerID := range beforeBench {
		if playerID == bench.ID {
			continue
		}
		if playerID == target.Source {
			continue
		}
		if !afterBench[playerID] {
			t.Fatalf("unrelated bench player %q disappeared after promotion", playerID)
		}
	}
}

func assertStarterSwap(t *testing.T, before, after teamTransferDOMSnapshot, source, target teamTransferTargetSnapshot) {
	t.Helper()
	afterTargets, _ := transferSnapshotIdentity(after)
	if afterTargets[target.Slot] != source.Source {
		t.Fatalf("keyboard transfer target %s = %q, want source %q", target.Slot, afterTargets[target.Slot], source.Source)
	}
	if afterTargets[source.Slot] != target.Source {
		t.Fatalf("keyboard transfer source slot %s = %q, want displaced %q", source.Slot, afterTargets[source.Slot], target.Source)
	}
	beforeTargets, beforeBench := transferSnapshotIdentity(before)
	for slot, sourceID := range beforeTargets {
		if slot == source.Slot || slot == target.Slot {
			continue
		}
		if afterTargets[slot] != sourceID {
			t.Fatalf("unrelated starter %s changed from %q to %q", slot, sourceID, afterTargets[slot])
		}
	}
	_, afterBench := transferSnapshotIdentity(after)
	if len(beforeBench) != len(afterBench) {
		t.Fatalf("starter swap changed bench size from %d to %d", len(beforeBench), len(afterBench))
	}
	for playerID := range beforeBench {
		if !afterBench[playerID] {
			t.Fatalf("starter swap changed unrelated bench identity %q", playerID)
		}
	}
}

func assertNoHardNavigation(t *testing.T, before, after teamTransferDOMSnapshot, description string) {
	t.Helper()
	beforeRoute := strings.SplitN(before.Path, "?", 2)[0]
	afterRoute := strings.SplitN(after.Path, "?", 2)[0]
	if afterRoute != beforeRoute || after.Marker != before.Marker {
		t.Fatalf("%s changed document identity/path: before=%+v after=%+v", description, before, after)
	}
}

func assertTransferSubmitted(t *testing.T, before, after teamTransferDOMSnapshot, description string) {
	t.Helper()
	if after.SubmitCount <= before.SubmitCount {
		t.Fatalf("%s emitted no managed transfer request: before submits=%d after submits=%d", description, before.SubmitCount, after.SubmitCount)
	}
}

func assertTransferNotSubmitted(t *testing.T, before, after teamTransferDOMSnapshot, description string) {
	t.Helper()
	if after.SubmitCount != before.SubmitCount {
		t.Fatalf("%s emitted a managed transfer request: before submits=%d after submits=%d", description, before.SubmitCount, after.SubmitCount)
	}
}

func freshTeamTransferSnapshot(t *testing.T, child *simChild, bot *draft.Bot, width, height int64) teamTransferDOMSnapshot {
	t.Helper()
	ctx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, ctx, child, bot, "/team", width, height)
	return readTeamTransferDOM(t, ctx)
}

// dragTeamTransferMouse sends the same pointer sequence a fine mouse emits
// at the phone viewport. The explicit scroll between pointerdown and
// pointermove keeps a distant bench row and starter target usable without
// introducing app-specific auto-scroll behavior; GoSX retains pointer
// capture on the handle and still resolves the fixed target by its rectangle.
func dragTeamTransferMouse(t *testing.T, ctx context.Context, sourceSelector, targetSelector string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.ScrollIntoView(sourceSelector, chromedp.ByQuery)); err != nil {
		t.Fatalf("scroll transfer source %s into view: %v", sourceSelector, err)
	}
	sourceRect := elementBoundingRect(t, ctx, sourceSelector)
	fromX := sourceRect.Left + sourceRect.Width/2
	fromY := sourceRect.Top + sourceRect.Height/2
	if err := chromedp.Run(ctx, chromedp.MouseEvent(input.MousePressed, fromX, fromY, chromedp.Button("left"), heldLeftButton)); err != nil {
		t.Fatalf("press transfer source %s: %v", sourceSelector, err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate("(function(){var e=document.querySelector("+fmt.Sprintf("%q", targetSelector)+");if(!e)throw new Error('transfer target missing');e.scrollIntoView({block:'center',inline:'nearest'});})()", nil),
		chromedp.Sleep(80*time.Millisecond),
	); err != nil {
		t.Fatalf("scroll transfer target %s during gesture: %v", targetSelector, err)
	}
	targetRect := elementBoundingRect(t, ctx, targetSelector)
	toX := targetRect.Left + targetRect.Width/2
	toY := targetRect.Top + targetRect.Height/2
	var moves []chromedp.Action
	for step := 1; step <= 5; step++ {
		moves = append(moves, chromedp.MouseEvent(input.MouseMoved, toX, toY, heldLeftButton), chromedp.Sleep(35*time.Millisecond))
	}
	moves = append(moves, chromedp.MouseEvent(input.MouseReleased, toX, toY))
	if err := chromedp.Run(ctx, moves...); err != nil {
		t.Fatalf("release transfer source %s over %s: %v", sourceSelector, targetSelector, err)
	}
}

// dragTeamTransferTouch is the phone gesture's true touch-pointer sibling.
// It positions both fixed nodes in one viewport before the contact starts;
// scrolling a document while a touch contact is held can legitimately cancel
// pointer capture, which would test browser scrolling rather than transfer.
// This is narrow touch-pointer plumbing coverage, not a claim about native
// edge-scroll behavior. Input.dispatchTouchEvent keeps Chromium's pointer
// capture semantics aligned with a real coarse pointer.
func dragTeamTransferTouch(t *testing.T, ctx context.Context, sourceSelector, targetSelector string) {
	t.Helper()
	positionScript := fmt.Sprintf(`(function(){
		var source = document.querySelector(%q);
		var target = document.querySelector(%q);
		if (!source || !target) throw new Error('transfer source or target missing');
		var sourceRect = source.getBoundingClientRect();
		var targetRect = target.getBoundingClientRect();
		var top = Math.min(sourceRect.top, targetRect.top);
		var bottom = Math.max(sourceRect.bottom, targetRect.bottom);
		var scroller = document.scrollingElement || document.documentElement;
		var delta = (top + bottom) / 2 - window.innerHeight / 2;
		// The app enables smooth scrolling globally; a test gesture needs
		// settled hit rectangles before TouchStart, not an in-flight scroll.
		document.documentElement.style.scrollBehavior = 'auto';
		if (scroller) scroller.scrollTop = scroller.scrollTop + delta;
	})()`, sourceSelector, targetSelector)
	if err := chromedp.Run(ctx, chromedp.Evaluate(positionScript, nil), chromedp.Sleep(80*time.Millisecond)); err != nil {
		t.Fatalf("position touch transfer source %s and target %s: %v", sourceSelector, targetSelector, err)
	}
	sourceRect := elementBoundingRect(t, ctx, sourceSelector)
	fromX := sourceRect.Left + sourceRect.Width/2
	fromY := sourceRect.Top + sourceRect.Height/2
	targetRect := elementBoundingRect(t, ctx, targetSelector)
	toX := targetRect.Left + targetRect.Width/2
	toY := targetRect.Top + targetRect.Height/2
	var hitTest struct {
		SourceHit bool `json:"sourceHit"`
		TargetHit bool `json:"targetHit"`
	}
	hitTestScript := fmt.Sprintf(`(function(){
		var source = document.querySelector(%q);
		var target = document.querySelector(%q);
		var sourceHit = document.elementFromPoint(%.3f, %.3f);
		var targetHit = document.elementFromPoint(%.3f, %.3f);
		return {
			sourceHit: !!source && !!sourceHit && (sourceHit === source || source.contains(sourceHit)),
			targetHit: !!target && !!targetHit && (targetHit === target || target.contains(targetHit))
		};
	})()`, sourceSelector, targetSelector, fromX, fromY, toX, toY)
	if err := chromedp.Run(ctx, chromedp.Evaluate(hitTestScript, &hitTest)); err != nil {
		t.Fatalf("check touch transfer hit targets %s -> %s: %v", sourceSelector, targetSelector, err)
	}
	t.Logf("touch transfer hit test source=%t target=%t source=(%.1f,%.1f) target=(%.1f,%.1f)", hitTest.SourceHit, hitTest.TargetHit, fromX, fromY, toX, toY)
	if !hitTest.SourceHit || !hitTest.TargetHit {
		t.Fatalf("touch transfer coordinates are not actionable: sourceHit=%t targetHit=%t source=(%.1f,%.1f) target=(%.1f,%.1f)", hitTest.SourceHit, hitTest.TargetHit, fromX, fromY, toX, toY)
	}
	touchPoint := func(x, y float64) []*input.TouchPoint {
		return []*input.TouchPoint{{X: x, Y: y, ID: 1, Force: 1}}
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchStart, touchPoint(fromX, fromY))); err != nil {
		t.Fatalf("touch press transfer source %s: %v", sourceSelector, err)
	}
	for step := 1; step <= 8; step++ {
		fraction := float64(step) / 8
		x := fromX + (toX-fromX)*fraction
		y := fromY + (toY-fromY)*fraction
		if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchMove, touchPoint(x, y)), chromedp.Sleep(35*time.Millisecond)); err != nil {
			t.Fatalf("touch move step %d from %s over %s: %v", step, sourceSelector, targetSelector, err)
		}
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchEnd, nil)); err != nil {
		t.Fatalf("touch release transfer source %s over %s: %v", sourceSelector, targetSelector, err)
	}
}

// scrollTeamTransferSourceWithWheel uses CDP's native mouse-wheel input to
// bring a distant source into a safe, visible touch position. It deliberately
// does not call scrollIntoView or write scrollTop: the transfer gesture below
// must begin with a real source hit and let GoSX own all scrolling after
// TouchStart.
func scrollTeamTransferSourceWithWheel(t *testing.T, ctx context.Context, sourceSelector string, width, height float64) {
	t.Helper()
	for step := 0; step < 24; step++ {
		rect := elementBoundingRect(t, ctx, sourceSelector)
		if rect.Top >= 48 && rect.Bottom <= height-96 {
			return
		}
		delta := 500.0
		if rect.Bottom < 48 {
			delta = -500
		}
		if err := chromedp.Run(ctx,
			input.DispatchMouseEvent(input.MouseWheel, width/2, height/2).WithDeltaY(delta),
			chromedp.Sleep(55*time.Millisecond),
		); err != nil {
			t.Fatalf("wheel transfer source %s into view: %v", sourceSelector, err)
		}
	}
	rect := elementBoundingRect(t, ctx, sourceSelector)
	if rect.Top < 48 || rect.Bottom > height-96 {
		t.Fatalf("native wheel did not expose transfer source %s safely: rect=%+v viewport=%.0fx%.0f", sourceSelector, rect, width, height)
	}
}

type teamTransferEdgeObservation struct {
	ScrollY      float64 `json:"scrollY"`
	TargetTop    float64 `json:"targetTop"`
	TargetBottom float64 `json:"targetBottom"`
	Highlighted  bool    `json:"highlighted"`
}

func readTeamTransferEdgeObservation(t *testing.T, ctx context.Context, targetSelector string) teamTransferEdgeObservation {
	t.Helper()
	var observation teamTransferEdgeObservation
	expression := fmt.Sprintf(`(function(){
		var target = document.querySelector(%q);
		if (!target) throw new Error('transfer target missing');
		var rect = target.getBoundingClientRect();
		return {
			scrollY: Number(window.scrollY || 0),
			targetTop: rect.top,
			targetBottom: rect.bottom,
			highlighted: target.classList.contains('gosx-transfer-target--over')
		};
	})()`, targetSelector)
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &observation)); err != nil {
		t.Fatalf("read held-edge transfer state for %s: %v", targetSelector, err)
	}
	return observation
}

// dragTeamTransferTouchAtViewportEdge exercises GoSX 0.56.1's actual
// fixed-target edge-scroll path. A native wheel positions only the source
// before contact; after TouchStart there is no script-driven scrolling or
// DOM prepositioning. Holding a real touch pointer at the viewport's top
// edge lets the runtime scroll back toward the starter target and re-hit-test
// it, after which the pointer is released on that highlighted fixed target.
func dragTeamTransferTouchAtViewportEdge(t *testing.T, ctx context.Context, sourceSelector, targetSelector string, width, height float64) {
	t.Helper()
	scrollTeamTransferSourceWithWheel(t, ctx, sourceSelector, width, height)
	sourceRect := elementBoundingRect(t, ctx, sourceSelector)
	targetRect := elementBoundingRect(t, ctx, targetSelector)
	fromX := sourceRect.Left + sourceRect.Width/2
	fromY := sourceRect.Top + sourceRect.Height/2
	if fromY < 48 || fromY > height-96 || targetRect.Bottom > 0 {
		t.Fatalf("edge transfer endpoints are not source-visible/target-above after native wheel: source=%+v target=%+v", sourceRect, targetRect)
	}
	var hitTest struct {
		SourceHit bool `json:"sourceHit"`
	}
	hitScript := fmt.Sprintf(`(function(){
		var source = document.querySelector(%q);
		var hit = document.elementFromPoint(%.3f, %.3f);
		return {sourceHit: !!source && !!hit && (hit === source || source.contains(hit))};
	})()`, sourceSelector, fromX, fromY)
	if err := chromedp.Run(ctx, chromedp.Evaluate(hitScript, &hitTest)); err != nil {
		t.Fatalf("check held-edge source hit %s: %v", sourceSelector, err)
	}
	if !hitTest.SourceHit {
		t.Fatalf("edge transfer source is not actionable at (%.1f,%.1f): %s", fromX, fromY, sourceSelector)
	}
	touchPoint := func(x, y float64) []*input.TouchPoint {
		return []*input.TouchPoint{{X: x, Y: y, ID: 1, Force: 1}}
	}
	if err := chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchStart, touchPoint(fromX, fromY))); err != nil {
		t.Fatalf("edge transfer touch start %s: %v", sourceSelector, err)
	}
	var handleState struct {
		Grabbed     string `json:"grabbed"`
		TouchAction string `json:"touchAction"`
	}
	handleScript := fmt.Sprintf(`(function(){
		var handle = document.querySelector(%q);
		return {
			grabbed: handle ? handle.getAttribute('aria-grabbed') || '' : '',
			touchAction: handle ? getComputedStyle(handle).touchAction || '' : ''
		};
	})()`, sourceSelector)
	if err := chromedp.Run(ctx, chromedp.Evaluate(handleScript, &handleState)); err != nil {
		t.Fatalf("read held-edge handle state %s: %v", sourceSelector, err)
	}
	if handleState.Grabbed != "true" {
		_ = chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchEnd, nil))
		t.Fatalf("held-edge transfer did not start on %s: grabbed=%q touchAction=%q", sourceSelector, handleState.Grabbed, handleState.TouchAction)
	}
	start := readTeamTransferEdgeObservation(t, ctx, targetSelector)
	const edgeY = 3.0
	highlighted := false
	var latest teamTransferEdgeObservation
	for step := 0; step < 150; step++ {
		if err := chromedp.Run(ctx,
			input.DispatchTouchEvent(input.TouchMove, touchPoint(fromX, edgeY)),
			chromedp.Sleep(38*time.Millisecond),
		); err != nil {
			t.Fatalf("held-edge transfer move %d %s: %v", step, sourceSelector, err)
		}
		latest = readTeamTransferEdgeObservation(t, ctx, targetSelector)
		if latest.Highlighted && latest.TargetTop <= height && latest.TargetBottom >= 0 {
			highlighted = true
			break
		}
	}
	if !highlighted {
		_ = chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchEnd, nil))
		t.Fatalf("held-edge transfer never highlighted target %s: start=%+v latest=%+v", targetSelector, start, latest)
	}
	if latest.ScrollY >= start.ScrollY {
		_ = chromedp.Run(ctx, input.DispatchTouchEvent(input.TouchEnd, nil))
		t.Fatalf("held-edge transfer did not scroll toward target: start scrollY=%.1f latest=%.1f", start.ScrollY, latest.ScrollY)
	}
	t.Logf("held-edge transfer scrolled viewport %.1f -> %.1f and highlighted %s at rect top=%.1f bottom=%.1f", start.ScrollY, latest.ScrollY, targetSelector, latest.TargetTop, latest.TargetBottom)
	// The target is already under the held edge point when the runtime
	// highlights it. Release at that same point, as a real edge drop would;
	// an extra move to the target's center can race the final scroll tick and
	// clear the fixed target before TouchEnd.
	if err := chromedp.Run(ctx,
		chromedp.Sleep(60*time.Millisecond),
		input.DispatchTouchEvent(input.TouchEnd, nil),
	); err != nil {
		t.Fatalf("edge transfer release %s over %s: %v", sourceSelector, targetSelector, err)
	}
}

func waitForTeamTransfer(t *testing.T, ctx context.Context, check func(teamTransferDOMSnapshot) bool, description string) teamTransferDOMSnapshot {
	t.Helper()
	var latest teamTransferDOMSnapshot
	var lastErr error
	if !pollUntil(ctx, 12*time.Second, func() bool {
		var snapshot teamTransferDOMSnapshot
		if err := chromedp.Run(ctx, chromedp.Evaluate(teamTransferSnapshotScript, &snapshot)); err != nil {
			lastErr = err
			return false
		}
		latest = snapshot
		return check(snapshot)
	}) {
		t.Fatalf("%s did not settle (last snapshot=%+v, last error=%v)", description, latest, lastErr)
	}
	return latest
}

// TestBrowserTeamLineupFixedTargetTransfer exercises the real Team page's
// stable source/target transfer contract. The 390px path is deliberately
// narrow touch-pointer plumbing (CDP touch events plus explicit JS scroll),
// not a claim of native edge-scroll coverage: it promotes a bench player
// into an occupied eligible starter slot, including an incoming-only target
// with no outgoing starter handle. The desktop path uses Space/Enter on a
// starter handle to swap two legal starter slots. Each accepted action is
// checked against a fresh server-rendered page so displaced and unrelated
// identities are persisted, while the original page also proves no hard
// navigation. The phone path observes managed transfer submissions and
// attempts a source-ineligible target, requiring no request or assignment
// change.
func TestBrowserTeamLineupFixedTargetTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league := startTeamTransferFixtureDraftedChild(t)
	bot := league.bots[1]

	phoneCtx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, phoneCtx, child, bot, "/team", 390, 844)
	if err := chromedp.Run(phoneCtx,
		chromedp.Evaluate(`window.__teamTransferBrowserMarker = (window.__teamTransferBrowserMarker || 0) + 1`, nil),
		chromedp.Evaluate(teamTransferInstrumentationScript, nil),
	); err != nil {
		t.Fatalf("instrument phone transfer document: %v", err)
	}
	phoneBefore := readTeamTransferDOM(t, phoneCtx)
	bench, promotionTarget, ok := chooseBenchPromotion(phoneBefore)
	if !ok {
		t.Fatalf("transfer fixture has no occupied eligible incoming-only starter target for an unlocked bench source: %+v", phoneBefore)
	}
	t.Logf("touch transfer selected bench %s -> %s (target disabled=%q, has outgoing handle=%t)", bench.ID, promotionTarget.Slot, promotionTarget.Disabled, promotionTarget.HasHandle)
	benchSelector := "#bench-" + bench.ID + " [data-gosx-transfer-handle]"
	targetSelector := "#slot-" + promotionTarget.Slot
	dragTeamTransferTouch(t, phoneCtx, benchSelector, targetSelector)
	phoneAfter := waitForTeamTransfer(t, phoneCtx, func(snapshot teamTransferDOMSnapshot) bool {
		for _, target := range snapshot.Targets {
			if target.Slot == promotionTarget.Slot && target.Source == bench.ID {
				return true
			}
		}
		return false
	}, "bench-to-starter transfer")
	assertBenchPromotion(t, phoneBefore, phoneAfter, bench, promotionTarget)
	assertNoHardNavigation(t, phoneBefore, phoneAfter, "bench transfer")
	assertTransferSubmitted(t, phoneBefore, phoneAfter, "bench transfer")
	phoneFresh := freshTeamTransferSnapshot(t, child, bot, 390, 844)
	assertBenchPromotion(t, phoneBefore, phoneFresh, bench, promotionTarget)

	invalidSource, invalidTarget, ok := chooseIneligibleTarget(phoneFresh)
	if !ok {
		t.Fatalf("transfer fixture has no unlocked source/ineligible target pair: %+v", phoneFresh)
	}
	invalidBefore := readTeamTransferDOM(t, phoneCtx)
	dragTeamTransferTouch(t, phoneCtx, "#slot-"+invalidSource.Slot+" [data-gosx-transfer-handle]", "#slot-"+invalidTarget.Slot)
	invalidAfter := readTeamTransferDOM(t, phoneCtx)
	beforeTargets, beforeBench := transferSnapshotIdentity(invalidBefore)
	afterTargets, afterBench := transferSnapshotIdentity(invalidAfter)
	if fmt.Sprint(beforeTargets) != fmt.Sprint(afterTargets) || fmt.Sprint(beforeBench) != fmt.Sprint(afterBench) {
		t.Fatalf("ineligible target changed the lineup: before targets=%v bench=%v after targets=%v bench=%v", beforeTargets, beforeBench, afterTargets, afterBench)
	}
	if invalidAfter.Marker != invalidBefore.Marker || invalidAfter.Path != invalidBefore.Path {
		t.Fatalf("ineligible target changed document identity/path: before=%+v after=%+v", invalidBefore, invalidAfter)
	}
	assertTransferNotSubmitted(t, invalidBefore, invalidAfter, "ineligible target")
	invalidFresh := freshTeamTransferSnapshot(t, child, bot, 390, 844)
	freshTargets, freshBench := transferSnapshotIdentity(invalidFresh)
	if fmt.Sprint(beforeTargets) != fmt.Sprint(freshTargets) || fmt.Sprint(beforeBench) != fmt.Sprint(freshBench) {
		t.Fatalf("ineligible target changed persisted lineup: before targets=%v bench=%v fresh targets=%v bench=%v", beforeTargets, beforeBench, freshTargets, freshBench)
	}

	desktopCtx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, desktopCtx, child, bot, "/team", 1280, 900)
	if err := chromedp.Run(desktopCtx,
		chromedp.Evaluate(`window.__teamTransferBrowserMarker = (window.__teamTransferBrowserMarker || 0) + 1`, nil),
		chromedp.Evaluate(teamTransferInstrumentationScript, nil),
	); err != nil {
		t.Fatalf("instrument desktop transfer document: %v", err)
	}
	desktopBefore := readTeamTransferDOM(t, desktopCtx)
	source, target, ok := chooseStarterTransfer(desktopBefore)
	if !ok {
		t.Fatalf("transfer fixture has no legal starter transfer pair: %+v", desktopBefore)
	}
	handleSelector := "#slot-" + source.Slot + " [data-gosx-transfer-handle]"
	if err := chromedp.Run(desktopCtx,
		chromedp.Focus(handleSelector, chromedp.ByQuery),
		chromedp.KeyEvent(" "),
		chromedp.KeyEvent("\r"),
	); err != nil {
		t.Fatalf("keyboard transfer from %s to the first eligible target: %v", source.Slot, err)
	}
	desktopAfter := waitForTeamTransfer(t, desktopCtx, func(snapshot teamTransferDOMSnapshot) bool {
		for _, candidate := range snapshot.Targets {
			if candidate.Slot == target.Slot && candidate.Source == source.Source {
				return true
			}
		}
		return false
	}, "keyboard starter transfer")
	assertStarterSwap(t, desktopBefore, desktopAfter, source, target)
	assertNoHardNavigation(t, desktopBefore, desktopAfter, "keyboard transfer")
	assertTransferSubmitted(t, desktopBefore, desktopAfter, "keyboard transfer")
	desktopFresh := freshTeamTransferSnapshot(t, child, bot, 1280, 900)
	assertStarterSwap(t, desktopBefore, desktopFresh, source, target)
}

// TestBrowserTeamLineupFixedTargetTransferFlagship proves the same managed
// transfer against the configured 17-player gridiron-house roster. Unlike
// the small source/target-disabled probe above, this acceptance path checks
// the shipped 11-starter shape (including SUPERFLEX and P), all six bench
// identities, and persisted bench promotion on a fresh server render.
func TestBrowserTeamLineupFixedTargetTransferFlagship(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league := startTeamTransferFlagshipDraftedChild(t)
	bot := league.bots[1]
	ctx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, ctx, child, bot, "/team", 390, 844)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__teamTransferBrowserMarker = (window.__teamTransferBrowserMarker || 0) + 1`, nil),
		chromedp.Evaluate(teamTransferInstrumentationScript, nil),
	); err != nil {
		t.Fatalf("instrument flagship transfer document: %v", err)
	}
	before := readTeamTransferDOM(t, ctx)
	assertFlagshipRosterShape(t, before)
	bench, target, ok := chooseFlagshipVisiblePromotion(before)
	if !ok {
		t.Fatalf("flagship fixture has no occupied eligible incoming-only starter target: %+v", before)
	}
	t.Logf("flagship touch transfer selected bench %s -> %s (target disabled=%q, has outgoing handle=%t)", bench.ID, target.Slot, target.Disabled, target.HasHandle)
	dragTeamTransferTouch(t, ctx, "#bench-"+bench.ID+" [data-gosx-transfer-handle]", "#slot-"+target.Slot)
	after := waitForTeamTransfer(t, ctx, func(snapshot teamTransferDOMSnapshot) bool {
		for _, candidate := range snapshot.Targets {
			if candidate.Slot == target.Slot && candidate.Source == bench.ID {
				return true
			}
		}
		return false
	}, "flagship bench-to-starter transfer")
	assertFlagshipRosterShape(t, after)
	assertBenchPromotion(t, before, after, bench, target)
	assertNoHardNavigation(t, before, after, "flagship bench transfer")
	assertTransferSubmitted(t, before, after, "flagship bench transfer")
	fresh := freshTeamTransferSnapshot(t, child, bot, 390, 844)
	assertFlagshipRosterShape(t, fresh)
	assertBenchPromotion(t, before, fresh, bench, target)
}

// TestBrowserTeamLineupFixedTargetTransferFlagshipEdgeScroll proves the
// flagship 17-player shape through the real 390x844 coarse-pointer path:
// the bench source is made visible only with native wheel input, the starter
// destination remains more than one viewport away, and GoSX's held edge
// contact scrolls/highlights it before one managed submission. Persistence is
// checked on a fresh server-rendered page; the existing flagship smoke test
// remains separate because its explicit JS positioning is not edge-scroll
// evidence.
func TestBrowserTeamLineupFixedTargetTransferFlagshipEdgeScroll(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league := startTeamTransferFlagshipDraftedChild(t)
	bot := league.bots[1]
	ctx := newCoarsePointerBrowserContext(t, chromePath(t))
	const width, height = 390.0, 844.0
	signInBrowserSeat(t, ctx, child, bot, "/team", int64(width), int64(height))
	var coarsePointer bool
	if err := chromedp.Run(ctx,
		emulation.SetTouchEmulationEnabled(true),
		chromedp.Evaluate(`matchMedia('(pointer: coarse)').matches`, &coarsePointer),
		chromedp.Evaluate(`window.__teamTransferBrowserMarker = (window.__teamTransferBrowserMarker || 0) + 1`, nil),
		chromedp.Evaluate(teamTransferInstrumentationScript, nil),
	); err != nil {
		t.Fatalf("instrument flagship edge transfer document: %v", err)
	}
	if !coarsePointer {
		t.Fatal("flagship edge transfer browser did not report a coarse primary pointer")
	}
	before := readTeamTransferDOM(t, ctx)
	assertFlagshipRosterShape(t, before)
	bench, target, ok := chooseBenchPromotion(before)
	if !ok {
		t.Fatalf("flagship fixture has no legal bench promotion for edge transfer: %+v", before)
	}
	benchSelector := "#bench-" + bench.ID + " [data-gosx-transfer-handle]"
	targetSelector := "#slot-" + target.Slot
	initialSource := elementBoundingRect(t, ctx, benchSelector)
	initialTarget := elementBoundingRect(t, ctx, targetSelector)
	if initialSource.Top-initialTarget.Top < height && initialTarget.Top-initialSource.Top < height {
		t.Fatalf("edge transfer endpoints are not more than one viewport apart: source=%+v target=%+v", initialSource, initialTarget)
	}
	t.Logf("flagship held-edge transfer selected bench %s -> %s; initial source=%+v target=%+v", bench.ID, target.Slot, initialSource, initialTarget)
	dragTeamTransferTouchAtViewportEdge(t, ctx, benchSelector, targetSelector, width, height)
	after := waitForTeamTransfer(t, ctx, func(snapshot teamTransferDOMSnapshot) bool {
		for _, candidate := range snapshot.Targets {
			if candidate.Slot == target.Slot && candidate.Source == bench.ID {
				return true
			}
		}
		return false
	}, "flagship held-edge bench-to-starter transfer")
	assertFlagshipRosterShape(t, after)
	assertBenchPromotion(t, before, after, bench, target)
	assertNoHardNavigation(t, before, after, "flagship held-edge transfer")
	assertTransferSubmitted(t, before, after, "flagship held-edge transfer")
	fresh := freshTeamTransferSnapshot(t, child, bot, int64(width), int64(height))
	assertFlagshipRosterShape(t, fresh)
	assertBenchPromotion(t, before, fresh, bench, target)
}

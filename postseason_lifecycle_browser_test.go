package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	_ "modernc.org/sqlite"
)

// The real schedule is deliberately small and deterministic. Four closed
// league weeks leave room for a two-round, four-team bracket whose rounds use
// weeks 3 and 4 as their authoritative scoring ledgers.
const ps1BrowserGamesCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score\n" +
	"ps1-reg-2026-w1,2026,REG,1,2026-09-10,17:00,BUF,17,MIA,10\n" +
	"ps1-reg-2026-w2,2026,REG,2,2026-09-11,17:00,KC,24,DEN,14\n" +
	"ps1-reg-2026-w3,2026,REG,3,2026-09-12,17:00,PHI,21,DAL,17\n" +
	"ps1-reg-2026-w4,2026,REG,4,2026-09-13,17:00,SF,27,SEA,20\n"

const ps1BrowserStatsHeader = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,passing_yards,passing_tds,passing_interceptions,rushing_yards,rushing_tds,receptions,receiving_yards,receiving_tds,rushing_fumbles_lost,receiving_fumbles_lost,sack_fumbles_lost,fantasy_points,fantasy_points_ppr,fg_made,fg_missed,pat_made\n"

var (
	ps1BrowserSetupClock     = time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	ps1BrowserCloseClock     = time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	ps1BrowserFreshStatsAt   = time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC)
	ps1BrowserWaitingStatsAt = time.Date(2026, time.September, 9, 20, 0, 0, 0, time.UTC)
)

// writePS1BrowserConfig keeps the checked-in example's complete roster,
// scoring, auth, and league metadata while changing only the season dates and
// the bounded four-team playoff rules used by this hermetic scenario.
func writePS1BrowserConfig(t *testing.T, root string) string {
	t.Helper()
	example, err := os.ReadFile(filepath.Join(root, "config", "league.json.example"))
	if err != nil {
		t.Fatalf("read league config example: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(example, &document); err != nil {
		t.Fatalf("decode league config example: %v", err)
	}
	draftConfig, ok := document["draft"].(map[string]any)
	if !ok {
		t.Fatal("league config example has no draft object")
	}
	draftConfig["at"] = "2026-08-01T00:00:00Z"
	document["season_start_at"] = "2026-08-01T00:00:00Z"
	postseason, ok := document["postseason"].(map[string]any)
	if !ok {
		t.Fatal("league config example has no postseason object")
	}
	postseason["teamCount"] = 4
	postseason["startWeek"] = 3
	postseason["roundLengthWeeks"] = 1
	postseason["qualification"] = "top-record"
	postseason["tiebreakOrder"] = []any{"record", "head-to-head", "points-for", "pickem", "seeded-draw"}
	postseason["byes"] = 0
	postseason["divisionWinnersFirst"] = false
	postseason["reseed"] = true
	postseason["consolation"] = false
	postseason["toiletBowl"] = false
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode PS-1 browser config: %v", err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join(t.TempDir(), "league.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write PS-1 browser config: %v", err)
	}
	return path
}

func writePS1BrowserGames(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create PS-1 browser stats root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "games.csv"), []byte(ps1BrowserGamesCSV), 0o600); err != nil {
		t.Fatalf("write PS-1 browser games: %v", err)
	}
}

func writePS1BrowserManifest(t *testing.T, root, statsState string, statsRows int, updatedAt time.Time) {
	t.Helper()
	stamp := updatedAt.UTC().Format(time.RFC3339)
	manifest := map[string]any{
		"schema_version": 1,
		"season":         2026,
		"schedules": map[string]any{
			"name": "schedules", "state": "ready", "license": "CC-BY-4.0", "rows": 4,
			"last_checked": stamp, "last_updated": stamp,
		},
		"player_stats": map[string]any{
			"name": "player_stats", "state": statsState, "license": "CC-BY-4.0", "rows": statsRows,
			"last_checked": stamp, "last_updated": stamp,
		},
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("encode PS-1 browser manifest: %v", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), encoded, 0o600); err != nil {
		t.Fatalf("write PS-1 browser manifest: %v", err)
	}
}

// writePS1BrowserStats supplies an actual stats_player_week ledger for every
// drafted player and every regular week. The production adapter still joins
// these rows by player identity and computes the score from the configured
// scoring rules; fantasy_points columns are present only because they are
// part of the mirror format and are not trusted by the scorer.
func writePS1BrowserStats(t *testing.T, root string, state draft.DraftState) int {
	t.Helper()
	players := make(map[string]fantasy.Player)
	for _, player := range fantasy.OfflinePool() {
		players[player.ID] = player
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(strings.Split(strings.TrimSuffix(ps1BrowserStatsHeader, "\n"), ",")); err != nil {
		t.Fatalf("write PS-1 browser stats header: %v", err)
	}
	rows := 0
	for week := 1; week <= 4; week++ {
		for _, pick := range state.Picks {
			id := draft.PickPlayerID(pick)
			player, ok := players[id]
			if !ok {
				t.Fatalf("drafted player %q is absent from the offline stats pool", id)
			}
			passingYards, passingTDs, rushingYards, rushingTDs := "0", "0", "0", "0"
			receptions, receivingYards, receivingTDs := "0", "0", "0"
			fgMade, patMade := "0", "0"
			switch player.Position {
			case "QB":
				passingYards, passingTDs = "250", "2"
			case "RB":
				rushingYards, rushingTDs = "80", "1"
			case "WR", "TE":
				receptions, receivingYards, receivingTDs = "2", "30", "1"
			case "K":
				fgMade, patMade = "2", "2"
			}
			if err := writer.Write([]string{
				player.ID, player.Name, player.Position, "2026", fmt.Sprintf("%d", week), "REG",
				fmt.Sprintf("ps1-reg-2026-w%d", week), player.NFLTeam, "",
				passingYards, passingTDs, "0", rushingYards, rushingTDs, receptions, receivingYards, receivingTDs,
				"0", "0", "0", "12.0", "14.0", fgMade, "0", patMade,
			}); err != nil {
				t.Fatalf("write PS-1 browser stats row for %s Week %d: %v", player.Name, week, err)
			}
			rows++
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatalf("flush PS-1 browser stats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "stats_player_week_2026.csv"), buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write PS-1 browser stats: %v", err)
	}
	return rows
}

func seedPS1BrowserLineups(t *testing.T, l *simLeague, state draft.DraftState) {
	t.Helper()
	players := make(map[string]fantasy.Player)
	for _, player := range fantasy.OfflinePool() {
		players[player.ID] = player
	}
	qbByTeam := make(map[string]string, len(l.bots))
	for _, pick := range state.Picks {
		teamID := draft.PickTeamID(pick)
		if _, already := qbByTeam[teamID]; already {
			continue
		}
		player, ok := players[draft.PickPlayerID(pick)]
		if ok && player.Position == "QB" {
			qbByTeam[teamID] = player.ID
		}
	}
	for _, bot := range l.bots {
		playerID := qbByTeam[bot.TeamID]
		if playerID == "" {
			t.Fatalf("team %s has no drafted QB for the PS-1 scoring ledger", bot.TeamID)
		}
		for week := 1; week <= 4; week++ {
			if err := bot.SetLineup(week, "QB", playerID); err != nil {
				t.Fatalf("seed PS-1 Week %d QB %s for %s: %v", week, playerID, bot.TeamID, err)
			}
		}
	}
}

// startPS1BrowserFixture reaches a normal, completed draft through the real
// simulated endpoints, generates four regular weeks, seeds actual lineups,
// and restarts the same persisted DATA_FILE after installing the local final
// stats ledger. No Store advancement or force-close is used as lifecycle
// proof by the browser test.
func startPS1BrowserFixture(t *testing.T, root string) (*simChild, *simLeague, string, string, string) {
	t.Helper()
	configPath := writePS1BrowserConfig(t, root)
	statsRoot := t.TempDir()
	writePS1BrowserGames(t, statsRoot)
	writePS1BrowserManifest(t, statsRoot, "waiting", 0, ps1BrowserWaitingStatsAt)
	env := []string{
		"GOSX_APP_ROOT=" + root,
		"LEAGUE_FILE=" + configPath,
		"OPEN_STATS_ROOT=" + statsRoot,
		"NFL_SEASON=2026",
	}
	child := startSimChild(t, "", env...)
	setClockAbsolute(t, child.URL, ps1BrowserSetupClock)
	l := seatLeagueWith(t, child, true)
	if err := l.commish.StartDraft(); err != nil {
		t.Fatalf("start PS-1 browser fixture draft: %v", err)
	}
	completeSimDraft(t, l)
	if err := l.commish.GenerateSchedule(4, 1, 42); err != nil {
		t.Fatalf("generate PS-1 four-week schedule: %v", err)
	}
	state, err := l.commish.State()
	if err != nil {
		t.Fatalf("read PS-1 completed draft: %v", err)
	}
	seedPS1BrowserLineups(t, l, state)
	statsRows := writePS1BrowserStats(t, statsRoot, state)
	writePS1BrowserManifest(t, statsRoot, "ready", statsRows, ps1BrowserFreshStatsAt)
	dataFile := child.DataFile
	child.Stop()
	child = startSimChild(t, dataFile, env...)
	l.repoint(t, child)
	setClockAbsolute(t, child.URL, ps1BrowserSetupClock)

	// A separate source root is reserved for the partial/degraded refusal. It
	// intentionally has no stats CSV and advertises a waiting player ledger.
	degradedRoot := t.TempDir()
	writePS1BrowserGames(t, degradedRoot)
	writePS1BrowserManifest(t, degradedRoot, "waiting", 0, ps1BrowserWaitingStatsAt)
	return child, l, statsRoot, degradedRoot, configPath
}

func restartPS1BrowserChild(t *testing.T, child *simChild, l *simLeague, root, configPath, statsRoot string) *simChild {
	t.Helper()
	dataFile := child.DataFile
	child.Stop()
	next := startSimChild(t, dataFile,
		"GOSX_APP_ROOT="+root,
		"LEAGUE_FILE="+configPath,
		"OPEN_STATS_ROOT="+statsRoot,
		"NFL_SEASON=2026",
	)
	l.repoint(t, next)
	setClockAbsolute(t, next.URL, ps1BrowserCloseClock)
	return next
}

func readPS1BrowserState(t *testing.T, path string) league.PersistedState {
	t.Helper()
	dbPath := filepath.Join(filepath.Dir(path), "league.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open PS-1 persisted database %s: %v", dbPath, err)
	}
	defer db.Close()
	var state league.PersistedState
	if err := db.QueryRow(`SELECT value FROM kv WHERE key = 'phase'`).Scan(&state.Phase); err != nil {
		t.Fatalf("read PS-1 persisted phase %s: %v", dbPath, err)
	}
	var data string
	if err := db.QueryRow(`SELECT data FROM playoffs WHERE id = 1`).Scan(&data); err != nil {
		if err == sql.ErrNoRows {
			return state
		}
		t.Fatalf("read PS-1 persisted playoff state %s: %v", dbPath, err)
	}
	var playoffs league.PlayoffState
	if err := json.Unmarshal([]byte(data), &playoffs); err != nil {
		t.Fatalf("decode PS-1 persisted playoff state %s: %v", dbPath, err)
	}
	state.Playoffs = &playoffs
	return state
}

func ps1BrowserReadBody(t *testing.T, ctx context.Context) string {
	t.Helper()
	var body string
	if err := chromedp.Run(ctx, chromedp.Evaluate("document.body.innerText || ''", &body)); err != nil {
		t.Fatalf("read PS-1 browser body: %v", err)
	}
	return strings.TrimSpace(body)
}

func waitPS1BrowserBody(t *testing.T, ctx context.Context, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var body string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Evaluate("document.body.innerText || ''", &body)); err == nil && strings.Contains(body, want) {
			return body
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("PS-1 browser body did not contain %q within %s: %q", want, timeout, body)
	return body
}

func ps1BrowserHeading(t *testing.T, ctx context.Context, selector string) string {
	t.Helper()
	var heading string
	if err := chromedp.Run(ctx, chromedp.Text(selector, &heading, chromedp.ByQuery)); err != nil {
		t.Fatalf("read PS-1 truth heading %s: %v", selector, err)
	}
	return strings.TrimSpace(heading)
}

// ps1BrowserFillInput updates a managed form field through the same bubbling
// input/change events a real keyboard edit produces. This matters after a
// validation response: GoSX has reconciled the form fragment, so a direct
// value write must notify the managed action before the next submit.
func ps1BrowserFillInput(t *testing.T, ctx context.Context, selector, value string) {
	t.Helper()
	var inputType string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){var e=document.querySelector(`+fmt.Sprintf("%q", selector)+`);return e ? (e.type || "text") : ""})()`, &inputType)); err != nil {
		t.Fatalf("inspect PS-1 input %s: %v", selector, err)
	}
	if inputType == "" {
		t.Fatalf("no PS-1 input matched %s", selector)
	}
	// SendKeys produces trusted keyboard/input events for visible text
	// controls. The app's current runtime keeps the form node under a
	// reconciled fragment that chromedp.SetValue cannot mutate reliably, so
	// clear/focus through the DOM and then send real key input. Hidden fields
	// still receive their value through the DOM because they cannot receive
	// keyboard focus.
	if inputType == "hidden" {
		selectorJSON, err := json.Marshal(selector)
		if err != nil {
			t.Fatalf("encode PS-1 hidden selector %q: %v", selector, err)
		}
		valueJSON, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encode PS-1 hidden value: %v", err)
		}
		script := `(function(){var e=document.querySelector(` + string(selectorJSON) + `);if(!e)throw new Error('no PS-1 input matched');e.value=` + string(valueJSON) + `;return e.value})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, nil)); err != nil {
			t.Fatalf("fill hidden PS-1 input %s: %v", selector, err)
		}
	} else {
		selectorJSON, err := json.Marshal(selector)
		if err != nil {
			t.Fatalf("encode PS-1 visible selector %q: %v", selector, err)
		}
		clearScript := `(function(){var e=document.querySelector(` + string(selectorJSON) + `);if(!e)throw new Error('no PS-1 input matched');e.focus();e.value='';return true})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(clearScript, nil), chromedp.SendKeys(selector, value, chromedp.ByQuery)); err != nil {
			t.Fatalf("fill PS-1 input %s: %v", selector, err)
		}
	}
	var got string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){var e=document.querySelector(`+fmt.Sprintf("%q", selector)+`);return e ? e.value : ""})()`, &got)); err != nil {
		t.Fatalf("read PS-1 input %s after fill: %v", selector, err)
	}
	if got != value {
		t.Fatalf("PS-1 input %s rendered %q after filling, want %q", selector, got, value)
	}
}

func ps1BrowserTruthCardText(t *testing.T, ctx context.Context, selector string) string {
	t.Helper()
	var card string
	expression := `(function(){var e=document.querySelector('` + selector + `');if(!e)return '';var c=e.closest('.playoff-truth-card')||e.parentElement;return (c&&c.innerText)||''})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &card)); err != nil {
		t.Fatalf("read PS-1 truth card %s: %v", selector, err)
	}
	return strings.TrimSpace(card)
}

func assertPS1PublishedConsumer(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot, path, selector string, width, height int64) {
	t.Helper()
	signInBrowserSeat(t, ctx, child, bot, path, width, height)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(selector, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for published PS-1 consumer %s at %s: %v", selector, path, err)
	}
	body := ps1BrowserReadBody(t, ctx)
	if selector == ".commissioner-hq__detail" {
		if !strings.Contains(body, "PLAYOFF TRUTH · YEAR ONE PLAYOFFS") || !strings.Contains(body, "PUBLISHED") {
			t.Fatalf("%s did not render published HQ playoff truth: %q", path, body)
		}
	} else if selector == "#admin-playoffs" {
		if !strings.Contains(body, "STATUS:") || !strings.Contains(body, "PUBLISHED") {
			t.Fatalf("%s did not render published admin playoff truth: %q", path, body)
		}
	} else {
		card := ps1BrowserTruthCardText(t, ctx, selector)
		if !strings.Contains(card, "PLAYOFF BRACKET PUBLISHED") {
			t.Fatalf("%s did not render published playoff truth: %q", path, card)
		}
		if strings.Contains(card, "PLAYOFF BRACKET PREVIEW") || strings.Contains(card, "PLAYOFF BRACKET WAITING") {
			t.Fatalf("%s rendered non-published playoff truth: %q", path, card)
		}
	}
	if scrollWidth, innerWidth := documentOverflowPx(t, ctx); scrollWidth > innerWidth {
		t.Fatalf("%s overflows at %dpx: scrollWidth=%d innerWidth=%d", path, width, scrollWidth, innerWidth)
	}
}

func TestBrowserPS1PostseasonLifecycleUsesPublishedTruth(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	root := browserAppRoot(t)
	chrome := chromePath(t)
	child, l, statsRoot, degradedRoot, configPath := startPS1BrowserFixture(t, root)
	phone := newBrowserContext(t, chrome)
	desktop := newBrowserContext(t, chrome)
	manager := newBrowserContext(t, chrome)

	// The app is now backed by four real final games, the complete player
	// stats CSV, and a controlled post-slate clock. Close all regular weeks
	// through the ordinary ready action in a managed 390px browser; the force
	// close action is never clicked. This bounded fixture intentionally closes
	// all four regular weeks before preview; weeks 3 and 4 are then reused as
	// round-ledger sources. It does not claim real-time week-by-week chronology
	// or browser coverage of tie, bye, or reseeding variants.
	setClockAbsolute(t, child.URL, ps1BrowserCloseClock)
	signInBrowserSeat(t, phone, child, l.commish, "/admin#week-close", 390, 844)
	if err := chromedp.Run(phone, chromedp.WaitVisible("#admin-week-close", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 Week close panel: %v", err)
	}
	var weekCloseSection string
	if err := chromedp.Run(phone, chromedp.Text("#admin-week-close", &weekCloseSection, chromedp.ByQuery)); err != nil {
		t.Fatalf("read PS-1 Week close panel: %v", err)
	}
	if !strings.Contains(strings.ToUpper(weekCloseSection), "READY") {
		t.Fatalf("PS-1 normal Week close did not render READY: %q", weekCloseSection)
	}
	if scrollWidth, innerWidth := documentOverflowPx(t, phone); scrollWidth > innerWidth {
		t.Fatalf("PS-1 phone Week close overflows: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
	for week := 1; week <= 4; week++ {
		var disabled bool
		if err := chromedp.Run(phone, chromedp.Evaluate(`(function(){var e=document.querySelector('form[action*="close-week-ready"] button[type="submit"]');return !e || e.disabled;})()`, &disabled)); err != nil {
			t.Fatalf("read PS-1 ready close button for Week %d: %v", week, err)
		}
		if disabled {
			t.Fatalf("PS-1 normal close button disabled for ready Week %d", week)
		}
		if err := chromedp.Run(phone, chromedp.Click(`form[action*="close-week-ready"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
			t.Fatalf("click PS-1 normal close Week %d: %v", week, err)
		}
		waitPS1BrowserBody(t, phone, fmt.Sprintf("Week %d closed with", week), 8*time.Second)
	}
	state := readPS1BrowserState(t, child.DataFile)
	if state.Phase != league.PhasePlayoffs {
		t.Fatalf("normal browser closes left phase %q, want playoffs", state.Phase)
	}
	if state.Playoffs != nil {
		t.Fatalf("regular-season close unexpectedly persisted playoff state before preview: %+v", state.Playoffs)
	}

	// Build the commissioner-only preview from the final regular-season
	// standings. This is a distinct managed browser action and its preview is
	// intentionally hidden from the manager consumer below.
	signInBrowserSeat(t, phone, child, l.commish, "/admin?section=playoffs", 390, 844)
	if err := chromedp.Run(phone, chromedp.WaitVisible("#admin-playoffs", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 Playoffs panel before preview: %v", err)
	}
	if heading := ps1BrowserHeading(t, phone, "#admin-playoffs .position-chip"); heading != "WAITING" {
		t.Fatalf("PS-1 pre-preview admin status = %q, want WAITING", heading)
	}
	if err := chromedp.Run(phone, chromedp.Click(`form[action*="playoff-preview"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click managed PS-1 preview: %v", err)
	}
	waitPS1BrowserBody(t, phone, "is ready for commissioner review; it is not published", 8*time.Second)
	if err := chromedp.Run(phone, chromedp.WaitVisible("#admin-playoff-preview-id", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 preview id: %v", err)
	}
	var previewID string
	if err := chromedp.Run(phone, chromedp.Evaluate(`document.querySelector('#admin-playoff-preview-id').value`, &previewID)); err != nil {
		t.Fatalf("read PS-1 preview id: %v", err)
	}
	previewID = strings.TrimSpace(previewID)
	if previewID == "" {
		t.Fatal("PS-1 preview id is empty")
	}
	state = readPS1BrowserState(t, child.DataFile)
	if state.Playoffs == nil || state.Playoffs.Status != league.PlayoffStatusPreview || len(state.Playoffs.Matchups) != 2 || state.Playoffs.Revision != 1 {
		t.Fatalf("unexpected persisted PS-1 preview: %+v", state.Playoffs)
	}
	previewBody := ps1BrowserReadBody(t, phone)
	if !strings.Contains(previewBody, "PREVIEW") || !strings.Contains(previewBody, "regular-season-final") {
		t.Fatalf("commissioner preview page did not expose preview status/provenance: %q", previewBody)
	}

	// A manager sees one waiting card while the commissioner sees the preview;
	// no seeds or round rows leak before publication.
	signInBrowserSeat(t, manager, child, l.bots[0], "/matchups?week=1", 390, 844)
	if err := chromedp.Run(manager, chromedp.WaitVisible("#matchups-playoff-truth-heading", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for manager pre-publish truth card: %v", err)
	}
	if heading := ps1BrowserHeading(t, manager, "#matchups-playoff-truth-heading"); heading != "PLAYOFF BRACKET WAITING" {
		t.Fatalf("manager saw preview heading %q, want waiting", heading)
	}
	managerCard := ps1BrowserTruthCardText(t, manager, "#matchups-playoff-truth-heading")
	if strings.Contains(managerCard, "PLAYOFF BRACKET PREVIEW") || strings.Contains(managerCard, "ROUND 1") {
		t.Fatalf("manager preview card leaked commissioner-only bracket data: %q", managerCard)
	}

	// Keep a genuine desktop/native form loaded before publication. The
	// managed phone action first proves wrong typed confirmation refusal without
	// mutating persisted state. Re-navigate the managed phone seat after that
	// refusal so GoSX rebuilds the action model before the exact confirmation;
	// the prior validation fragment can otherwise retain its rejected value.
	if err := chromedp.Run(desktop, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable PS-1 desktop JavaScript for native publish retry: %v", err)
	}
	signInBrowserSeat(t, desktop, child, l.commish, "/admin?section=playoffs", 1440, 900)
	if err := chromedp.Run(desktop, chromedp.WaitVisible("#admin-playoff-publish-confirm", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 native stale publish form: %v", err)
	}
	// The preview ID is rendered from the persisted preview and is already
	// present in this page's form; only the explicit confirmation needs a
	// browser edit here.
	ps1BrowserFillInput(t, phone, "#admin-playoff-publish-confirm", "WRONG")
	if err := chromedp.Run(phone, chromedp.Click(`form[action*="playoff-publish"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("submit wrong PS-1 publish confirmation: %v", err)
	}
	waitPS1BrowserBody(t, phone, "type PUBLISH PLAYOFF BRACKET to confirm", 8*time.Second)
	stateAfterWrongPublish := readPS1BrowserState(t, child.DataFile)
	if stateAfterWrongPublish.Playoffs == nil || stateAfterWrongPublish.Playoffs.Status != league.PlayoffStatusPreview || stateAfterWrongPublish.Playoffs.Revision != 1 {
		t.Fatalf("wrong publish confirmation mutated bracket: %+v", stateAfterWrongPublish.Playoffs)
	}
	signInBrowserSeat(t, phone, child, l.commish, "/admin?section=playoffs", 390, 844)
	if err := chromedp.Run(phone, chromedp.WaitVisible("#admin-playoff-publish-confirm", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 managed publish form after refusal: %v", err)
	}
	ps1BrowserFillInput(t, phone, "#admin-playoff-publish-confirm", league.PlayoffPublishConfirmation)
	if err := chromedp.Run(phone, chromedp.Click(`form[action*="playoff-publish"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("submit correct PS-1 publish confirmation: %v", err)
	}
	waitPS1BrowserBody(t, phone, "is published as the one authoritative bracket truth", 8*time.Second)
	state = readPS1BrowserState(t, child.DataFile)
	if state.Playoffs == nil || state.Playoffs.Status != league.PlayoffStatusPublished || state.Playoffs.Revision != 2 || len(state.Playoffs.Audit) != 2 {
		t.Fatalf("unexpected persisted PS-1 published bracket: %+v", state.Playoffs)
	}

	// The native desktop form was rendered against the same preview ID before
	// the managed publish. Posting it now proves same-preview duplicate
	// publication is idempotent and does not create another bracket revision.
	if err := chromedp.Run(desktop, chromedp.SetValue("#admin-playoff-publish-confirm", league.PlayoffPublishConfirmation, chromedp.ByQuery)); err != nil {
		t.Fatalf("fill PS-1 native duplicate confirmation: %v", err)
	}
	if err := chromedp.Run(desktop, chromedp.Click(`form[action*="playoff-publish"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click PS-1 native duplicate publish: %v", err)
	}
	waitPS1BrowserBody(t, desktop, "is published as the one authoritative bracket truth", 8*time.Second)
	stateAfterDuplicate := readPS1BrowserState(t, child.DataFile)
	if stateAfterDuplicate.Playoffs == nil || stateAfterDuplicate.Playoffs.Revision != 2 || len(stateAfterDuplicate.Playoffs.Audit) != 2 {
		t.Fatalf("duplicate publish changed authoritative bracket: %+v", stateAfterDuplicate.Playoffs)
	}

	// Swap the child to a separate partial/degraded stats root and exercise the
	// real ledger action. It must refuse before Store mutation; the persisted
	// published bracket is compared by value before/after.
	beforeDegraded := readPS1BrowserState(t, child.DataFile)
	child = restartPS1BrowserChild(t, child, l, root, configPath, degradedRoot)
	signInBrowserSeat(t, phone, child, l.commish, "/admin?section=playoffs", 390, 844)
	if err := chromedp.Run(phone, chromedp.WaitVisible(`form[action*="playoff-advance"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 degraded advance form: %v", err)
	}
	if err := chromedp.Run(phone, chromedp.Click(`form[action*="playoff-advance"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click PS-1 degraded advance: %v", err)
	}
	degradedBody := waitPS1BrowserBody(t, phone, "only authoritative final results may advance", 8*time.Second)
	if !strings.Contains(strings.ToLower(degradedBody), "waiting") && !strings.Contains(strings.ToLower(degradedBody), "unavailable") && !strings.Contains(strings.ToLower(degradedBody), "empty") {
		t.Fatalf("degraded PS-1 refusal did not identify partial source: %q", degradedBody)
	}
	afterDegraded := readPS1BrowserState(t, child.DataFile)
	if !reflect.DeepEqual(beforeDegraded.Playoffs, afterDegraded.Playoffs) || beforeDegraded.Phase != afterDegraded.Phase {
		t.Fatalf("degraded PS-1 advance mutated persisted state: before phase=%q after phase=%q before=%+v after=%+v", beforeDegraded.Phase, afterDegraded.Phase, beforeDegraded.Playoffs, afterDegraded.Playoffs)
	}

	// Restore the final source root, then apply both authoritative rounds. The
	// first managed action creates the persisted round-two matchup; the second
	// action uses the native desktop browser and seals the season champion.
	child = restartPS1BrowserChild(t, child, l, root, configPath, statsRoot)
	signInBrowserSeat(t, phone, child, l.commish, "/admin?section=playoffs", 390, 844)
	if err := chromedp.Run(phone, chromedp.Click(`form[action*="playoff-advance"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click PS-1 first authoritative advance: %v", err)
	}
	waitPS1BrowserBody(t, phone, "Authoritative playoff ledger applied at bracket revision 3", 8*time.Second)
	state = readPS1BrowserState(t, child.DataFile)
	if state.Playoffs == nil || state.Playoffs.Status != league.PlayoffStatusPublished || state.Playoffs.Revision != 3 || state.Playoffs.ChampionTeamID != "" {
		t.Fatalf("first PS-1 advance did not open round two: %+v", state.Playoffs)
	}
	var roundTwoID string
	for _, matchup := range state.Playoffs.Matchups {
		if matchup.Bracket == "championship" && matchup.Round == 2 && !matchup.Final {
			roundTwoID = matchup.ID
		}
	}
	if roundTwoID == "" {
		t.Fatalf("first PS-1 advance persisted no active round-two matchup: %+v", state.Playoffs.Matchups)
	}

	if err := chromedp.Run(desktop, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("keep PS-1 desktop JavaScript disabled for native advance: %v", err)
	}
	signInBrowserSeat(t, desktop, child, l.commish, "/admin?section=playoffs", 1440, 900)
	if err := chromedp.Run(desktop, chromedp.WaitVisible(`form[action*="playoff-advance"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 native second advance: %v", err)
	}
	if err := chromedp.Run(desktop, chromedp.Click(`form[action*="playoff-advance"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click PS-1 native second authoritative advance: %v", err)
	}
	waitPS1BrowserBody(t, desktop, "Authoritative playoff ledger applied at bracket revision 4", 8*time.Second)
	state = readPS1BrowserState(t, child.DataFile)
	if state.Playoffs == nil || state.Phase != league.PhaseSeasonComplete || state.Playoffs.Revision != 4 || state.Playoffs.ChampionTeamID == "" {
		t.Fatalf("second PS-1 advance did not seal champion: phase=%q playoffs=%+v", state.Phase, state.Playoffs)
	}
	var final *league.PlayoffMatchup
	for index := range state.Playoffs.Matchups {
		matchup := &state.Playoffs.Matchups[index]
		if matchup.Bracket == "championship" && matchup.Round == 2 && matchup.Final {
			copy := *matchup
			final = &copy
		}
	}
	if final == nil {
		t.Fatalf("season-complete PS-1 bracket has no final championship matchup")
	}

	// Correct the terminal championship in the managed 390px admin form. The
	// selected winner is intentionally changed and accompanied by scores,
	// proving the explicit audit path rather than a no-op correction.
	winner := final.AwayTeamID
	homeScore, awayScore := "90", "120"
	if final.WinnerTeamID == final.AwayTeamID {
		winner = final.HomeTeamID
		homeScore, awayScore = "120", "90"
	}
	signInBrowserSeat(t, phone, child, l.commish, "/admin?section=playoffs", 390, 844)
	if err := chromedp.Run(phone, chromedp.WaitVisible(`form[action*="playoff-correct"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for PS-1 correction form: %v", err)
	}
	ps1BrowserFillInput(t, phone, `form[action*="playoff-correct"] input[name="matchup_id"]`, final.ID)
	ps1BrowserFillInput(t, phone, `form[action*="playoff-correct"] input[name="winner_team_id"]`, winner)
	ps1BrowserFillInput(t, phone, `form[action*="playoff-correct"] input[name="home_score"]`, homeScore)
	ps1BrowserFillInput(t, phone, `form[action*="playoff-correct"] input[name="away_score"]`, awayScore)
	ps1BrowserFillInput(t, phone, `form[action*="playoff-correct"] input[name="reason"]`, "verified stat adjustment")
	ps1BrowserFillInput(t, phone, `form[action*="playoff-correct"] input[name="confirm"]`, league.PlayoffCorrectionConfirmation)
	if err := chromedp.Run(phone, chromedp.Click(`form[action*="playoff-correct"] button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("submit PS-1 terminal correction: %v", err)
	}
	waitPS1BrowserBody(t, phone, "Playoff correction recorded at bracket revision 5", 8*time.Second)
	state = readPS1BrowserState(t, child.DataFile)
	if state.Playoffs == nil || state.Playoffs.Revision != 5 || state.Playoffs.ChampionTeamID != winner || len(state.Playoffs.Audit) != 5 {
		t.Fatalf("PS-1 correction was not audited/persisted: %+v", state.Playoffs)
	}
	if state.Playoffs.Matchups[len(state.Playoffs.Matchups)-1].ResultProvenance == nil {
		t.Fatalf("PS-1 correction did not retain result provenance")
	}
	if got := state.Playoffs.Matchups[len(state.Playoffs.Matchups)-1].ResultProvenance.Source; got != "commissioner-correction" {
		t.Fatalf("PS-1 correction source = %q, want commissioner-correction", got)
	}

	// Finally restart the same DATA_FILE on the good source and verify every
	// exposed truth consumer reads the corrected published bracket. The HQ
	// surface is included alongside Admin, Matchups, Home, Team, and Activity.
	child = restartPS1BrowserChild(t, child, l, root, configPath, statsRoot)
	restarted := readPS1BrowserState(t, child.DataFile)
	if restarted.Playoffs == nil || restarted.Playoffs.Revision != 5 || restarted.Playoffs.ChampionTeamID != winner || restarted.Phase != league.PhaseSeasonComplete {
		t.Fatalf("PS-1 correction did not survive restart: phase=%q playoffs=%+v", restarted.Phase, restarted.Playoffs)
	}
	assertPS1PublishedConsumer(t, manager, child, l.bots[0], "/", "#home-playoff-truth-heading", 390, 844)
	assertPS1PublishedConsumer(t, manager, child, l.bots[0], "/matchups?week=1", "#matchups-playoff-truth-heading", 390, 844)
	matchupsCard := ps1BrowserTruthCardText(t, manager, "#matchups-playoff-truth-heading")
	if !strings.Contains(matchupsCard, "SOURCE regular-season-final") || !strings.Contains(matchupsCard, "ROUND 2") {
		t.Fatalf("published Matchups card omitted persisted provenance/round: %q", matchupsCard)
	}
	assertPS1PublishedConsumer(t, manager, child, l.bots[0], "/team?week=1", "#team-playoff-truth-heading", 390, 844)
	assertPS1PublishedConsumer(t, manager, child, l.bots[0], "/activity", "#activity-playoff-truth-heading", 390, 844)
	assertPS1PublishedConsumer(t, desktop, child, l.commish, "/commissioner", ".commissioner-hq__detail", 1440, 900)
	hqBody := ps1BrowserReadBody(t, desktop)
	if !strings.Contains(hqBody, "PLAYOFF TRUTH · YEAR ONE PLAYOFFS") || !strings.Contains(hqBody, "PUBLISHED") || strings.Contains(hqBody, "PLAYOFF BRACKET PREVIEW") {
		t.Fatalf("HQ did not expose the published bracket truth: %q", hqBody)
	}
	assertPS1PublishedConsumer(t, desktop, child, l.commish, "/admin?section=playoffs", "#admin-playoffs", 1440, 900)
}

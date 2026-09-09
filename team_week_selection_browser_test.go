package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// teamWeekQASeededLeague reuses the flagship draft fixture, then reopens the
// same persisted league with a local schedule snapshot. The first child is
// needed only to discover the deterministic team-1 draft and stamp one
// explicit Week 1 starter; the reopened child is the browser's real app
// server. Week 1 and Week 2 games for that player's NFL team are deliberately
// in the past, so the inherited Week 2 assignment is rendered LOCKED while
// positions from teams absent from this tiny schedule remain editable.
func teamWeekQASeededLeague(t *testing.T) (*simChild, *simLeague, *draft.Bot, string) {
	t.Helper()
	root := teamTransferBrowserAppRoot(t)
	leagueFile := writeTeamTransferFlagshipLeague(t, root)
	fantasyRoot := writeTeamTransferFantasyCache(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "LEAGUE_FILE="+leagueFile, "FANTASY_ROOT="+fantasyRoot, "GRIDIRON_TEST_POOL=")
	league := seatLeague(t, child)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start Week 1/2 QA fixture draft: %v", err)
	}
	completeSimDraft(t, league)
	bot := league.bots[0]

	seedID, seedTeam := teamWeekQASeedPlayer(t, league, bot)
	if err := bot.SetLineup(1, "QB", seedID); err != nil {
		t.Fatalf("seed explicit Week 1 QB %s: %v", seedID, err)
	}
	if err := league.commish.GenerateSchedule(2, 1, 42); err != nil {
		t.Fatalf("publish two-week fantasy schedule: %v", err)
	}

	statsRoot := writeTeamWeekQASchedule(t, seedTeam)
	dataFile := child.DataFile
	child.Stop()
	next := startSimChild(t, dataFile,
		"GOSX_APP_ROOT="+root,
		"LEAGUE_FILE="+leagueFile,
		"FANTASY_ROOT="+fantasyRoot,
		"GRIDIRON_TEST_POOL=",
		"OPEN_STATS_ROOT="+statsRoot,
		"NFL_SEASON=2026",
	)
	setClockAbsolute(t, next.URL, time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	league.repoint(t, next)
	teamWeekQAForceCloseWeekOne(t, next, league.commish)
	return next, league, bot, seedID
}

// teamWeekQAForceCloseWeekOne makes the persisted fantasy schedule's Week 1
// final before the browser starts. MatchupsData derives the current fantasy
// week from that persisted schedule (not from the open-stats snapshot), so
// this is what makes /matchups?week=1 a real historical selection while the
// local Week 2 stats still drive the Team lock/projection assertions.
func teamWeekQAForceCloseWeekOne(t *testing.T, child *simChild, commish *draft.Bot) {
	t.Helper()
	ctx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, ctx, child, commish, "/admin#week-close", 390, 844)
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible("#admin-close-force-week", chromedp.ByQuery),
		chromedp.SetValue("#admin-close-force-week", "1", chromedp.ByQuery),
		chromedp.SetValue("#admin-close-week-confirm", "CLOSE WEEK 1", chromedp.ByQuery),
		chromedp.Click(`form[action*="close-week-force"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(".gosx-toast, .flash-message", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("force-close synthetic fantasy Week 1: %v", err)
	}
	var notice string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.innerText`, &notice)); err != nil {
		t.Fatalf("read forced Week 1 close notice: %v", err)
	}
	if !strings.Contains(strings.ToLower(notice), "week 1") {
		t.Fatalf("forced Week 1 close returned no Week 1 confirmation: %q", notice)
	}
}

func teamWeekQASeedPlayer(t *testing.T, league *simLeague, bot *draft.Bot) (string, string) {
	t.Helper()
	state, err := league.commish.State()
	if err != nil {
		t.Fatalf("read completed draft for Week 1 seed: %v", err)
	}
	players := make(map[string]fantasy.Player)
	for _, player := range fantasy.OfflinePool() {
		players[player.ID] = player
	}
	for _, pick := range state.Picks {
		if draft.PickTeamID(pick) != bot.TeamID {
			continue
		}
		id := draft.PickPlayerID(pick)
		player, ok := players[id]
		if ok && player.Position == "QB" && player.NFLTeam != "" {
			return id, player.NFLTeam
		}
	}
	t.Fatalf("completed team %s has no drafted QB in the offline pool", bot.TeamID)
	return "", ""
}

func writeTeamWeekQASchedule(t *testing.T, nflTeam string) string {
	t.Helper()
	if strings.TrimSpace(nflTeam) == "" {
		t.Fatal("cannot write a lock schedule without an NFL team")
	}
	away := "MIA"
	if strings.EqualFold(away, nflTeam) {
		away = "BUF"
	}
	// Both rows are safely behind the 2026-09 test clock. With no upcoming
	// row, pickemWeekAt resolves Week 2 as current, making /matchups?week=1
	// a genuinely non-current selection and the inherited Week 2 QB locked.
	const header = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score\n"
	body := header + fmt.Sprintf("qa_week1,2026,REG,1,2026-08-20,17:00,%s,,%s,\n", away, nflTeam) +
		fmt.Sprintf("qa_week2,2026,REG,2,2026-08-27,17:00,%s,,%s,\n", away, nflTeam)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "games.csv"), []byte(body), 0o600); err != nil {
		t.Fatalf("write Week 1/2 schedule snapshot: %v", err)
	}
	return root
}

type teamWeekQASaveCandidate struct {
	Slot   string `json:"slot"`
	Option string `json:"option"`
}

func teamWeekQASaveCandidateFor(t *testing.T, ctx context.Context, excludedSlot string) teamWeekQASaveCandidate {
	t.Helper()
	var candidate teamWeekQASaveCandidate
	script := fmt.Sprintf(`(function(){
		var excluded=%q;
		for (var row of document.querySelectorAll('.lineup-slot[data-gosx-transfer-target]')) {
			var slot=row.getAttribute('data-gosx-transfer-target') || '';
			if (!slot || slot === excluded || row.getAttribute('data-gosx-transfer-locked') === 'true') continue;
			var select=row.querySelector('form[action*="lineup-set"] select');
			if (!select) continue;
			for (var option of select.options) {
				if (option.value && !option.disabled && !option.selected) return {slot:slot, option:option.value};
			}
		}
		return {slot:'', option:''};
	})()`, excludedSlot)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &candidate)); err != nil {
		t.Fatalf("find editable Week 2 lineup-set candidate: %v", err)
	}
	if candidate.Slot == "" || candidate.Option == "" {
		t.Fatalf("Week 2 fixture has no editable lineup-set candidate outside locked slot %q: %+v", excludedSlot, candidate)
	}
	return candidate
}

func prepareTeamWeekQASave(t *testing.T, ctx context.Context, candidate teamWeekQASaveCandidate) string {
	t.Helper()
	rowSelector := "#slot-" + candidate.Slot
	openScript := fmt.Sprintf(`(function(){
		var row=document.querySelector(%q);
		if (!row) throw new Error('lineup row missing');
		var form=row.querySelector('form[action*="lineup-set"]');
		if (!form) throw new Error('lineup-set form missing');
		var disclosure=form.closest('details');
		if (!disclosure) throw new Error('lineup-set disclosure missing');
		disclosure.open=true;
	})()`, rowSelector)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(openScript, nil),
		chromedp.WaitVisible(rowSelector+` form[action*="lineup-set"] select`, chromedp.ByQuery),
		chromedp.SetValue(rowSelector+` form[action*="lineup-set"] select`, candidate.Option, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("prepare Week 2 lineup-set %s -> %s: %v", candidate.Slot, candidate.Option, err)
	}
	return rowSelector + ` form[action*="lineup-set"] button[type="submit"]`
}

func assertTeamWeekQALockedSeed(t *testing.T, snapshot teamTransferDOMSnapshot, seedID string) string {
	t.Helper()
	for _, target := range snapshot.Targets {
		if target.Source == seedID && transferAttrTruthy(target.Locked) {
			return target.Slot
		}
	}
	t.Fatalf("inherited Week 1 seed %q was not rendered as a locked Week 2 starter: %+v", seedID, snapshot.Targets)
	return ""
}

func assertTeamWeekQAProjectionTruth(t *testing.T, ctx context.Context) {
	t.Helper()
	var strip string
	if err := chromedp.Run(ctx, chromedp.Text(".team-command-strip", &strip, chromedp.ByQuery)); err != nil {
		t.Fatalf("read Team projection strip: %v", err)
	}
	semantic := strings.ToLower(strip)
	if !strings.Contains(semantic, "starting projection") || !strings.Contains(semantic, "bench projection") {
		t.Fatalf("projection strip does not expose separate starter/bench labels: %q", strip)
	}
	if !strings.Contains(strip, "Forecast unavailable") {
		t.Errorf("Week 2 source-mismatched bench forecast is not marked unavailable: %q", strip)
	}
	if strings.Contains(strip, "Complete forecast") {
		t.Errorf("Week 2 source-mismatched bench forecast was presented as complete: %q", strip)
	}
	if !strings.Contains(semantic, "not included in team score") {
		t.Errorf("bench projection strip does not state score exclusion: %q", strip)
	}
	if !strings.Contains(semantic, "latest source snapshot is week 1") {
		t.Errorf("projection strip lacks the Week 1 source provenance explanation: %q", strip)
	}
}

func assertTeamWeekQAControlsFit(t *testing.T, ctx context.Context, width int64, label string) {
	t.Helper()
	scrollWidth, innerWidth := documentOverflowPx(t, ctx)
	if scrollWidth > innerWidth {
		t.Errorf("%s: document overflows horizontally: scrollWidth=%d innerWidth=%d", label, scrollWidth, innerWidth)
	}
	var clipped []string
	script := `Array.from(document.querySelectorAll('a[href],button,select,input[type="submit"]')).filter(function(e){
		var r=e.getBoundingClientRect(), s=getComputedStyle(e);
		var formControl=e.tagName === 'BUTTON' || e.tagName === 'SELECT' || e.tagName === 'INPUT';
		return s.display !== 'none' && s.visibility !== 'hidden' && r.width > 0 && r.height > 0 &&
			(r.left < -1 || r.right > window.innerWidth + 1 || (formControl && e.scrollWidth > e.clientWidth + 1));
	}).map(function(e){return (e.getAttribute('aria-label') || e.innerText || e.name || e.tagName).trim();})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &clipped)); err != nil {
		t.Fatalf("%s: inspect visible control geometry: %v", label, err)
	}
	if len(clipped) > 0 {
		t.Errorf("%s: visible controls are clipped or outside the %dpx viewport: %v", label, width, clipped)
	}
	assertLineupFooterClearance(t, ctx, label)
}

// TestBrowserTeamWeekNavigationProjectionAndSaveQA is the focused cross-page
// weekly QA lane. Each viewport gets a fresh local league and exercises the
// complete Matchups Week 1 -> Team Week 2 journey, source-week-mismatch
// projection truth, no-JS native and GoSX-managed lineup-set saves, changed-row
// anchors, and fresh authenticated persistence reads. The fixed-target gesture
// tests own drag/touch semantics, and the unit service suite owns exhaustive
// lock/eligibility matrices.
func TestBrowserTeamWeekNavigationProjectionAndSaveQA(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{name: "phone", width: 390, height: 844},
		{name: "landscape-phone", width: 844, height: 390},
		{name: "desktop", width: 1440, height: 900},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			runTeamWeekQAJourney(t, viewport.width, viewport.height, viewport.name)
		})
	}
}

func runTeamWeekQAJourney(t *testing.T, width, height int64, viewportName string) {
	t.Helper()
	child, _, bot, seedID := teamWeekQASeededLeague(t)

	matchupsCtx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, matchupsCtx, child, bot, "/matchups?week=1", width, height)
	if err := chromedp.Run(matchupsCtx, chromedp.WaitVisible(".my-matchup", chromedp.ByQuery)); err != nil {
		t.Fatalf("non-current Matchups Week 1 has no featured matchup: %v", err)
	}
	var selectedWeek string
	if err := chromedp.Run(matchupsCtx, chromedp.Evaluate(`document.querySelector('#matchups-week-select').value`, &selectedWeek)); err != nil {
		t.Fatalf("read Matchups selected week: %v", err)
	}
	if selectedWeek != "1" {
		t.Fatalf("Matchups selected week = %q, want explicitly viewed Week 1", selectedWeek)
	}
	var backToCurrent bool
	if err := chromedp.Run(matchupsCtx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.pickem-weeknav a')).some(function(a){return a.textContent.indexOf('Back to current week') >= 0;})`, &backToCurrent)); err != nil {
		t.Fatalf("check non-current Matchups affordance: %v", err)
	}
	if !backToCurrent {
		t.Fatal("Matchups Week 1 did not expose its non-current Back to current week affordance")
	}
	var nextLineupHref string
	if err := chromedp.Run(matchupsCtx, chromedp.Evaluate(`(function(){var a=document.querySelector('.my-matchup__foot a[href*="/team?"]');return a?a.getAttribute('href'):'';})()`, &nextLineupHref)); err != nil {
		t.Fatalf("read Matchups next-lineup CTA: %v", err)
	}
	if nextLineupHref != "/team?week=2#lineup" {
		t.Fatalf("Matchups next-lineup CTA = %q, want /team?week=2#lineup", nextLineupHref)
	}
	if err := chromedp.Run(matchupsCtx, chromedp.Click(`.my-matchup__foot a[href="/team?week=2#lineup"]`, chromedp.ByQuery), chromedp.WaitVisible(".team-lineup-region", chromedp.ByQuery)); err != nil {
		t.Fatalf("follow Matchups Week 1 CTA to Team Week 2: %v", err)
	}
	var location string
	if err := chromedp.Run(matchupsCtx, chromedp.Location(&location)); err != nil {
		t.Fatalf("read Team location after Matchups CTA: %v", err)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Team location %q: %v", location, err)
	}
	if parsed.Path != "/team" || parsed.Query().Get("week") != "2" || parsed.Fragment != "lineup" {
		t.Fatalf("Matchups CTA landed at %q, want /team?week=2#lineup", location)
	}
	waitLineupTextLayoutSettled(t, matchupsCtx)
	assertTeamWeekQAProjectionTruth(t, matchupsCtx)
	assertTeamWeekQAControlsFit(t, matchupsCtx, width, viewportName+" Team after Matchups CTA")
	before := readTeamTransferDOM(t, matchupsCtx)
	seedSlot := assertTeamWeekQALockedSeed(t, before, seedID)

	// Native fallback: prepare the real lineup-set form with scripts active,
	// then disable page script execution before the click. The browser must
	// follow the ordinary POST/redirect path and land on the changed row.
	nativeCtx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, nativeCtx, child, bot, "/team?week=2#lineup", width, height)
	waitLineupTextLayoutSettled(t, nativeCtx)
	nativeCandidate := teamWeekQASaveCandidateFor(t, nativeCtx, seedSlot)
	nativeButton := prepareTeamWeekQASave(t, nativeCtx, nativeCandidate)
	if err := chromedp.Run(nativeCtx, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable GoSX for native Week 2 save: %v", err)
	}
	if err := chromedp.Run(nativeCtx, chromedp.Click(nativeButton, chromedp.NodeVisible), chromedp.WaitVisible("#lineup-save-result", chromedp.ByQuery)); err != nil {
		t.Fatalf("submit native Week 2 lineup-set: %v", err)
	}
	if err := chromedp.Run(nativeCtx, chromedp.Location(&location)); err != nil {
		t.Fatalf("read native Week 2 save location: %v", err)
	}
	parsed, err = url.Parse(location)
	if err != nil {
		t.Fatalf("parse native Week 2 save location %q: %v", location, err)
	}
	if parsed.Fragment != "slot-"+nativeCandidate.Slot {
		t.Fatalf("native Week 2 save landed at %q, want #slot-%s", location, nativeCandidate.Slot)
	}
	nativeAfter := readTeamTransferDOM(t, nativeCtx)
	if got := teamTransferSourceForSlot(nativeAfter, nativeCandidate.Slot); got != nativeCandidate.Option {
		t.Fatalf("native Week 2 save rendered slot %s as %q, want %q", nativeCandidate.Slot, got, nativeCandidate.Option)
	}
	assertTeamWeekQALockedSeed(t, nativeAfter, seedID)
	nativeFreshCtx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, nativeFreshCtx, child, bot, "/team?week=2#slot-"+nativeCandidate.Slot, width, height)
	waitLineupTextLayoutSettled(t, nativeFreshCtx)
	nativeFresh := readTeamTransferDOM(t, nativeFreshCtx)
	if got := teamTransferSourceForSlot(nativeFresh, nativeCandidate.Slot); got != nativeCandidate.Option {
		t.Fatalf("fresh native Week 2 render slot %s as %q, want persisted %q", nativeCandidate.Slot, got, nativeCandidate.Option)
	}
	assertTeamWeekQALockedSeed(t, nativeFresh, seedID)

	// Managed save: use a second editable slot on a fresh GoSX document, then
	// assert the managed redirect keeps its changed-row fragment and a fresh
	// server render keeps both the new assignment and the locked inheritance.
	managedCtx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, managedCtx, child, bot, "/team?week=2#lineup", width, height)
	waitLineupTextLayoutSettled(t, managedCtx)
	managedCandidate := teamWeekQASaveCandidateFor(t, managedCtx, nativeCandidate.Slot)
	managedButton := prepareTeamWeekQASave(t, managedCtx, managedCandidate)
	if err := chromedp.Run(managedCtx, chromedp.Click(managedButton, chromedp.NodeVisible), chromedp.WaitVisible(".gosx-toast, #lineup-save-result", chromedp.ByQuery)); err != nil {
		t.Fatalf("submit GoSX-managed Week 2 lineup-set: %v", err)
	}
	if err := chromedp.Run(managedCtx, chromedp.Location(&location)); err != nil {
		t.Fatalf("read managed Week 2 save location: %v", err)
	}
	parsed, err = url.Parse(location)
	if err != nil {
		t.Fatalf("parse managed Week 2 save location %q: %v", location, err)
	}
	if parsed.Fragment != "slot-"+managedCandidate.Slot {
		t.Fatalf("managed Week 2 save landed at %q, want #slot-%s", location, managedCandidate.Slot)
	}
	managedAfter := readTeamTransferDOM(t, managedCtx)
	if got := teamTransferSourceForSlot(managedAfter, managedCandidate.Slot); got != managedCandidate.Option {
		t.Fatalf("managed Week 2 save rendered slot %s as %q, want %q", managedCandidate.Slot, got, managedCandidate.Option)
	}
	assertTeamWeekQALockedSeed(t, managedAfter, seedID)
	assertTeamWeekQAControlsFit(t, managedCtx, width, viewportName+" Team after managed save")
	managedFreshCtx := newBrowserContext(t, chromePath(t))
	signInBrowserSeat(t, managedFreshCtx, child, bot, "/team?week=2#slot-"+managedCandidate.Slot, width, height)
	waitLineupTextLayoutSettled(t, managedFreshCtx)
	managedFresh := readTeamTransferDOM(t, managedFreshCtx)
	if got := teamTransferSourceForSlot(managedFresh, managedCandidate.Slot); got != managedCandidate.Option {
		t.Fatalf("fresh managed Week 2 render slot %s as %q, want persisted %q", managedCandidate.Slot, got, managedCandidate.Option)
	}
	assertTeamWeekQALockedSeed(t, managedFresh, seedID)
	assertTeamWeekQAProjectionTruth(t, managedFreshCtx)
	assertTeamWeekQAControlsFit(t, managedFreshCtx, width, viewportName+" Team fresh persistence")
}

func teamTransferSourceForSlot(snapshot teamTransferDOMSnapshot, slot string) string {
	for _, target := range snapshot.Targets {
		if target.Slot == slot {
			return target.Source
		}
	}
	return ""
}

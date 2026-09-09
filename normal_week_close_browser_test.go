package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// The game is final in both browser phases. Only the open-stats freshness
// stamp changes, so the first page proves the normal close is blocked for the
// advertised 24-hour reason and the second page proves the same persisted
// league becomes ready without a force-close shortcut.
const normalWeekCloseQAGamesCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score\n" +
	"normal-close-2026-w1,2026,REG,1,2026-09-10,17:00,BUF,17,MIA,10\n"

var (
	normalWeekCloseQAStaleStatsAt = time.Date(2026, time.September, 11, 20, 0, 0, 0, time.UTC)
	normalWeekCloseQAFreshStatsAt = time.Date(2026, time.September, 12, 0, 0, 0, 0, time.UTC)
	normalWeekCloseQASetupClock   = time.Date(2026, time.September, 10, 16, 0, 0, 0, time.UTC)
	normalWeekCloseQAClock        = time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
)

func writeNormalWeekCloseQAManifest(t *testing.T, root string, statsRows int, statsUpdatedAt time.Time) {
	t.Helper()
	stamp := statsUpdatedAt.UTC().Format(time.RFC3339)
	manifest := fmt.Sprintf("{\n"+
		"  \"schema_version\": 1,\n"+
		"  \"season\": 2026,\n"+
		"  \"schedules\": {\n"+
		"    \"name\": \"schedules\",\n"+
		"    \"state\": \"ready\",\n"+
		"    \"license\": \"CC-BY-4.0\",\n"+
		"    \"rows\": 1,\n"+
		"    \"last_checked\": \"2026-09-10T21:00:00Z\",\n"+
		"    \"last_updated\": \"2026-09-10T21:00:00Z\"\n"+
		"  },\n"+
		"  \"player_stats\": {\n"+
		"    \"name\": \"player_stats\",\n"+
		"    \"state\": \"ready\",\n"+
		"    \"license\": \"CC-BY-4.0\",\n"+
		"    \"rows\": %d,\n"+
		"    \"last_checked\": \"%s\",\n"+
		"    \"last_updated\": \"%s\"\n"+
		"  }\n"+
		"}\n", statsRows, stamp, stamp)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write normal-close manifest: %v", err)
	}
}

// writeNormalWeekCloseQAStats emits one real ledger row for every drafted
// player. The scorer therefore follows its normal name+position join and the
// close notice can truthfully say every starter matched a stat line.
func writeNormalWeekCloseQAStats(t *testing.T, root string, state draft.DraftState) int {
	t.Helper()
	players := make(map[string]fantasy.Player)
	for _, player := range fantasy.OfflinePool() {
		players[player.ID] = player
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{
		"player_id", "player_display_name", "position", "season", "week", "season_type", "game_id", "team", "opponent_team",
		"passing_yards", "passing_tds", "passing_interceptions", "rushing_yards", "rushing_tds", "receptions", "receiving_yards",
		"receiving_tds", "rushing_fumbles_lost", "receiving_fumbles_lost", "sack_fumbles_lost", "fantasy_points", "fantasy_points_ppr",
	}); err != nil {
		t.Fatalf("write normal-close stats header: %v", err)
	}
	rows := 0
	for _, pick := range state.Picks {
		id := draft.PickPlayerID(pick)
		player, ok := players[id]
		if !ok {
			t.Fatalf("drafted player %q is absent from the offline stats pool", id)
		}
		if err := writer.Write([]string{
			player.ID, player.Name, player.Position, "2026", "1", "REG", "normal-close-2026-w1", player.NFLTeam, "",
			"0", "0", "0", "0", "0", "1", "10", "0", "0", "0", "0", "2.0", "3.0",
		}); err != nil {
			t.Fatalf("write normal-close stats row for %s: %v", player.Name, err)
		}
		rows++
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatalf("flush normal-close stats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "stats_player_week_2026.csv"), buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write normal-close player stats: %v", err)
	}
	return rows
}

func seedNormalWeekCloseQALineups(t *testing.T, league *simLeague, state draft.DraftState) {
	t.Helper()
	players := make(map[string]fantasy.Player)
	for _, player := range fantasy.OfflinePool() {
		players[player.ID] = player
	}
	qbByTeam := make(map[string]string, len(league.bots))
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
	for _, bot := range league.bots {
		playerID := qbByTeam[bot.TeamID]
		if playerID == "" {
			t.Fatalf("team %s has no drafted QB for the normal-close score receipt", bot.TeamID)
		}
		if err := bot.SetLineup(1, "QB", playerID); err != nil {
			t.Fatalf("seed Week 1 QB %s for %s: %v", playerID, bot.TeamID, err)
		}
	}
}

// startNormalWeekCloseQA first uses the real draft endpoints to create the
// persisted one-week league. It then restarts that same DATA_FILE after
// installing the local open-stats ledger, which avoids any direct Store.Close
// call and makes the browser exercise the production admin action.
func startNormalWeekCloseQA(t *testing.T, root string) (*simChild, *simLeague, string) {
	t.Helper()
	statsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(statsRoot, "games.csv"), []byte(normalWeekCloseQAGamesCSV), 0o600); err != nil {
		t.Fatalf("write normal-close schedule: %v", err)
	}
	writeNormalWeekCloseQAManifest(t, statsRoot, 0, normalWeekCloseQAStaleStatsAt)
	env := []string{
		"GOSX_APP_ROOT=" + root,
		"OPEN_STATS_ROOT=" + statsRoot,
		"NFL_SEASON=2026",
	}
	child := startSimChild(t, "", env...)
	setClockAbsolute(t, child.URL, normalWeekCloseQASetupClock)
	league := seatLeagueWith(t, child, true)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start normal-close fixture draft: %v", err)
	}
	completeSimDraft(t, league)
	if err := league.commish.GenerateSchedule(1, 1, 42); err != nil {
		t.Fatalf("generate one-week normal-close schedule: %v", err)
	}
	state, err := league.commish.State()
	if err != nil {
		t.Fatalf("read completed normal-close draft: %v", err)
	}
	seedNormalWeekCloseQALineups(t, league, state)
	statsRows := writeNormalWeekCloseQAStats(t, statsRoot, state)
	writeNormalWeekCloseQAManifest(t, statsRoot, statsRows, normalWeekCloseQAStaleStatsAt)
	dataFile := child.DataFile
	child.Stop()
	child = startSimChild(t, dataFile, env...)
	league.repoint(t, child)
	setClockAbsolute(t, child.URL, normalWeekCloseQAClock)
	return child, league, statsRoot
}

func refreshNormalWeekCloseQA(t *testing.T, child *simChild, league *simLeague, root, statsRoot string) *simChild {
	t.Helper()
	writeNormalWeekCloseQAManifest(t, statsRoot, normalWeekCloseQAStatsRows(t, statsRoot), normalWeekCloseQAFreshStatsAt)
	dataFile := child.DataFile
	child.Stop()
	next := startSimChild(t, dataFile,
		"GOSX_APP_ROOT="+root,
		"OPEN_STATS_ROOT="+statsRoot,
		"NFL_SEASON=2026",
	)
	league.repoint(t, next)
	setClockAbsolute(t, next.URL, normalWeekCloseQAClock)
	return next
}

func normalWeekCloseQAStatsRows(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "stats_player_week_2026.csv"))
	if err != nil {
		t.Fatalf("read normal-close stats fixture: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("normal-close stats fixture has %d CSV lines, want a header and rows", len(lines))
	}
	return len(lines) - 1
}

func normalWeekCloseQAReadBody(t *testing.T, ctx context.Context) string {
	t.Helper()
	var body string
	if err := chromedp.Run(ctx, chromedp.Evaluate("document.body.innerText || ''", &body)); err != nil {
		t.Fatalf("read browser body text: %v", err)
	}
	return strings.TrimSpace(body)
}

func normalWeekCloseQAPageStamp(t *testing.T, ctx context.Context) float64 {
	t.Helper()
	var stamp float64
	if err := chromedp.Run(ctx, chromedp.Evaluate("performance.timeOrigin", &stamp)); err != nil {
		t.Fatalf("read Activity document stamp: %v", err)
	}
	return stamp
}

func waitNormalWeekCloseQABody(t *testing.T, ctx context.Context, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var body string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Evaluate("document.body.innerText || ''", &body)); err == nil && strings.Contains(body, want) {
			return body
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("browser body did not contain %q within %s: %q", want, timeout, body)
	return body
}

func assertNormalWeekCloseQAStatus(t *testing.T, ctx context.Context, ready bool) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.WaitVisible("#admin-week-close", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for Week close panel: %v", err)
	}
	var section string
	if err := chromedp.Run(ctx, chromedp.Text("#admin-week-close", &section, chromedp.ByQuery)); err != nil {
		t.Fatalf("read Week close panel: %v", err)
	}
	upper := strings.ToUpper(section)
	var disabled bool
	if err := chromedp.Run(ctx, chromedp.Evaluate("(function(){var e=document.querySelector('form[action*=\"close-week-ready\"] button[type=\"submit\"]'); return !!e && e.disabled;})()", &disabled)); err != nil {
		t.Fatalf("read normal-close button state: %v", err)
	}
	if ready {
		if strings.Contains(upper, "NOT READY") || disabled || !strings.Contains(upper, "READY") || !strings.Contains(upper, "CLOSE READY WEEK 1") {
			t.Fatalf("Week close did not render ready state: disabled=%v panel=%q", disabled, section)
		}
		return
	}
	if !disabled {
		t.Fatalf("normal close button is enabled while stats are stale: %q", section)
	}
	for _, want := range []string{"1/1 FINAL", "NOT READY", "player stats are not yet 24 hours past the final kickoff"} {
		if !strings.Contains(strings.ToLower(section), strings.ToLower(want)) {
			t.Fatalf("Week close blocked panel missing %q: %q", want, section)
		}
	}
}

func normalWeekCloseQAFormCount(t *testing.T, ctx context.Context) int {
	t.Helper()
	var count int
	if err := chromedp.Run(ctx, chromedp.Evaluate("document.querySelectorAll('form[action*=\"close-week-ready\"]').length", &count)); err != nil {
		t.Fatalf("count normal-close forms: %v", err)
	}
	return count
}

type normalWeekCloseQAActivityState struct {
	Count int
	Text  string
}

func readNormalWeekCloseQAActivity(t *testing.T, ctx context.Context) normalWeekCloseQAActivityState {
	t.Helper()
	var state normalWeekCloseQAActivityState
	const script = "(function(){\n" +
		"  var items=Array.from(document.querySelectorAll('#activity-feed-region .activity-item'));\n" +
		"  return {count:items.length,text:items.map(function(e){return e.innerText || e.textContent || '';}).join('\\\\n')};\n" +
		"})()"
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &state)); err != nil {
		t.Fatalf("read filtered activity feed: %v", err)
	}
	return state
}

func waitNormalWeekCloseQAActivity(t *testing.T, ctx context.Context, timeout time.Duration) normalWeekCloseQAActivityState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var state normalWeekCloseQAActivityState
	for time.Now().Before(deadline) {
		state = readNormalWeekCloseQAActivity(t, ctx)
		lower := strings.ToLower(state.Text)
		if state.Count == 1 && strings.Contains(lower, "closed week 1") && !strings.Contains(lower, "force-closed") {
			return state
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("filtered activity did not converge to one normal close within %s: count=%d text=%q", timeout, state.Count, state.Text)
	return state
}

func TestBrowserCommissionerNormalWeekCloseAndRepeatIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	root := browserAppRoot(t)
	chrome := chromePath(t)
	child, league, statsRoot := startNormalWeekCloseQA(t, root)
	phone := newBrowserContext(t, chrome)
	desktop := newBrowserContext(t, chrome)
	activity := newBrowserContext(t, chrome)

	// Before the freshness stamp is eligible, both real viewport renders show
	// final games but keep the normal action disabled with the exact reason.
	signInBrowserSeat(t, phone, child, league.commish, "/admin#week-close", 390, 844)
	assertNormalWeekCloseQAStatus(t, phone, false)
	if scrollWidth, innerWidth := documentOverflowPx(t, phone); scrollWidth > innerWidth {
		t.Fatalf("phone Week close overflows: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
	signInBrowserSeat(t, desktop, child, league.commish, "/admin#week-close", 1440, 900)
	assertNormalWeekCloseQAStatus(t, desktop, false)

	child = refreshNormalWeekCloseQA(t, child, league, root, statsRoot)
	signInBrowserSeat(t, phone, child, league.commish, "/admin#week-close", 390, 844)
	assertNormalWeekCloseQAStatus(t, phone, true)
	signInBrowserSeat(t, desktop, child, league.commish, "/admin#week-close", 1440, 900)
	assertNormalWeekCloseQAStatus(t, desktop, true)
	if err := chromedp.Run(desktop, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable JavaScript for stale normal-close form: %v", err)
	}

	// Load the filtered feed before the write. Its one live region should add
	// exactly one normal-close event after the action, not a force-close row.
	signInBrowserSeat(t, activity, child, league.commish, "/activity?team=COMMISSIONER&type=commissioner&q=closed+week+1", 390, 844)
	if err := chromedp.Run(activity, chromedp.WaitVisible("#activity-feed-region", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for filtered activity feed: %v", err)
	}
	if state := readNormalWeekCloseQAActivity(t, activity); state.Count != 0 {
		t.Fatalf("filtered activity already contains close events: %+v", state)
	}
	activityStamp := normalWeekCloseQAPageStamp(t, activity)
	if scrollWidth, innerWidth := documentOverflowPx(t, activity); scrollWidth > innerWidth {
		t.Fatalf("phone activity overflows: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}

	// Context A keeps GoSX enabled and clicks the production managed form.
	if err := chromedp.Run(phone,
		chromedp.Click("form[action*=\"close-week-ready\"] button[type=\"submit\"]", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click managed normal close: %v", err)
	}
	body := waitNormalWeekCloseQABody(t, phone, "Week 1 closed with 1 of 1 games scored", 8*time.Second)
	if !strings.Contains(body, "Every starter matched a stat line.") {
		t.Fatalf("managed normal-close notice does not confirm matched starters: %q", body)
	}
	if !strings.Contains(body, "ALREADY FINAL") || normalWeekCloseQAFormCount(t, phone) != 0 {
		t.Fatalf("managed close did not render final panel/no normal form: %q", body)
	}

	// Context B retains the genuine ready form from before A's write, with
	// JavaScript already disabled so this is a native browser POST of the
	// stale form.
	if err := chromedp.Run(desktop, chromedp.Click("form[action*=\"close-week-ready\"] button[type=\"submit\"]", chromedp.ByQuery)); err != nil {
		t.Fatalf("click stale native normal close: %v", err)
	}
	body = waitNormalWeekCloseQABody(t, desktop, "Week 1 was already final; no scoring or lineup changes were made.", 8*time.Second)
	if !strings.Contains(body, "ALREADY FINAL") || normalWeekCloseQAFormCount(t, desktop) != 0 {
		t.Fatalf("native repeat did not render the idempotent final panel/no form: %q", body)
	}

	waitNormalWeekCloseQAActivity(t, activity, 8*time.Second)
	if state := readNormalWeekCloseQAActivity(t, activity); state.Count != 1 {
		t.Fatalf("repeat close duplicated filtered activity: %+v", state)
	}
	if after := normalWeekCloseQAPageStamp(t, activity); after != activityStamp {
		t.Fatalf("filtered Activity document reloaded during live convergence: before=%s after=%s", strconv.FormatFloat(activityStamp, 'f', 0, 64), strconv.FormatFloat(after, 'f', 0, 64))
	}

	// The fresh manager view is a real post-close read from the restarted
	// child. The final masthead and score line are authoritative; the visible
	// live-state chip is intentionally not asserted because feed.go maps a
	// posted final card to LEDGER.
	signInBrowserSeat(t, phone, child, league.bots[0], "/matchups?week=1", 390, 844)
	if err := chromedp.Run(phone, chromedp.WaitVisible(".matchup-status-line", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for final Matchups status: %v", err)
	}
	body = normalWeekCloseQAReadBody(t, phone)
	lowerBody := strings.ToLower(body)
	for _, want := range []string{"week 1 results are final", "1 of 1 games final"} {
		if !strings.Contains(lowerBody, want) {
			t.Fatalf("final Matchups page missing %q: %q", want, body)
		}
	}
	if strings.Contains(strings.ToUpper(body), "CLOSED EARLY") {
		t.Fatalf("normal close was mislabeled CLOSED EARLY: %q", body)
	}
	var scoreValues []float64
	if err := chromedp.Run(phone, chromedp.Evaluate("Array.from(document.querySelectorAll('[data-score-team]')).map(function(e){return Number((e.textContent || '').trim());}).filter(function(value){return Number.isFinite(value);})", &scoreValues)); err != nil {
		t.Fatalf("read final Matchups score values: %v", err)
	}
	if len(scoreValues) == 0 {
		t.Fatalf("final Matchups page rendered no numeric score values: %q", body)
	}
	hasNonZeroScore := false
	for _, value := range scoreValues {
		if value > 0 {
			hasNonZeroScore = true
			break
		}
	}
	if !hasNonZeroScore {
		t.Fatalf("final Matchups score values are all zero: %v", scoreValues)
	}
	if scrollWidth, innerWidth := documentOverflowPx(t, phone); scrollWidth > innerWidth {
		t.Fatalf("phone final Matchups overflows: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}

	// Desktop is also checked after the completed workflow so this gate covers
	// both requested form widths without relying on a desktop-only render.
	signInBrowserSeat(t, desktop, child, league.bots[0], "/matchups?week=1", 1440, 900)
	if err := chromedp.Run(desktop, chromedp.WaitVisible(".matchup-status-line", chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for desktop final Matchups status: %v", err)
	}
	if scrollWidth, innerWidth := documentOverflowPx(t, desktop); scrollWidth > innerWidth {
		t.Fatalf("desktop final Matchups overflows: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
}

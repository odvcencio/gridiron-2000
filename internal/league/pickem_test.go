package league

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// pickemFixture returns four games across two weeks, anchored on now:
//   - g-final:     week 1, kicked off long ago, final, BUF beats MIA 24-17.
//   - g-locked:    week 1, kicked off 30 minutes ago, still in progress.
//   - g-open:      week 1, kicks off in 3 hours, not yet locked.
//   - g-nextweek:  week 2, kicks off in 7 days.
func pickemFixture(now time.Time) []GameInfo {
	return []GameInfo{
		{ID: "g-final", Week: 1, Kickoff: now.Add(-72 * time.Hour), Away: "BUF", Home: "MIA", AwayScore: 24, HomeScore: 17, Final: true, ScoresPresent: true, SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
		{ID: "g-locked", Week: 1, Kickoff: now.Add(-30 * time.Minute), Away: "DAL", Home: "PHI", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
		{ID: "g-open", Week: 1, Kickoff: now.Add(3 * time.Hour), Away: "KC", Home: "DEN", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
		{ID: "g-nextweek", Week: 2, Kickoff: now.Add(7 * 24 * time.Hour), Away: "SF", Home: "SEA", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
	}
}

func frozenPickemMarkets(games []GameInfo) map[string]PickemMarket {
	markets := make(map[string]PickemMarket, len(games))
	for _, game := range games {
		markets[game.ID] = PickemMarket{LinePresent: true, Frozen: true, LockAt: game.Kickoff}
	}
	return markets
}

func TestPickemWeekSelection(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)

	if got := service.pickemWeek(games, now); got != 1 {
		t.Fatalf("mid-season week = %d, want 1", got)
	}

	allPast := now.Add(30 * 24 * time.Hour)
	if got := service.pickemWeek(games, allPast); got != 2 {
		t.Fatalf("all-past week = %d, want the largest week (2)", got)
	}

	if got := service.pickemWeek(nil, now); got != 1 {
		t.Fatalf("empty schedule week = %d, want 1", got)
	}
}

func TestPickemWeekSelectionUsesPublishedStartWeekAndNormalizesUnavailableQueries(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	schedule, err := GenerateSchedule(ScheduleParams{
		Season:    2026,
		TeamIDs:   teamIDList(service.teams),
		StartWeek: 3,
		Weeks:     2,
		Seed:      73,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	games := []GameInfo{
		{ID: "week-3", Week: 3, Kickoff: now.Add(time.Hour), Away: "BUF", Home: "MIA", SpreadLinePresent: true},
		{ID: "week-4", Week: 4, Kickoff: now.Add(8 * 24 * time.Hour), Away: "KC", Home: "DEN", SpreadLinePresent: true},
	}
	service.SetScheduleSource(func() []GameInfo { return games })

	if got := service.seasonStartWeek(); got != 3 {
		t.Fatalf("seasonStartWeek = %d, want published schedule start week 3", got)
	}
	for _, test := range []struct {
		name       string
		target     string
		wantWeek   int
		wantNotice bool
	}{
		{name: "missing query", target: "/pickem", wantWeek: 3},
		{name: "hostile query", target: "/pickem?week=not-a-week", wantWeek: 3, wantNotice: true},
		{name: "nonexistent query", target: "/pickem?week=999", wantWeek: 3, wantNotice: true},
		{name: "real future week", target: "/pickem?week=4", wantWeek: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, test.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			data := service.PickemData(request)
			if got := data["week"]; got != test.wantWeek {
				t.Fatalf("week = %v, want %d", got, test.wantWeek)
			}
			if got := data["has_week_notice"]; got != test.wantNotice {
				t.Fatalf("has_week_notice = %v, want %v; notice=%v", got, test.wantNotice, data["week_notice"])
			}
		})
	}
	if got := service.PickemRedirectTarget("999"); got != "/pickem?week=3" {
		t.Fatalf("invalid action week redirect = %q, want /pickem?week=3", got)
	}
	if got := service.PickemRedirectTarget("4"); got != "/pickem?week=4" {
		t.Fatalf("valid action week redirect = %q, want /pickem?week=4", got)
	}
	if got := service.PickemRedirectTarget("not-a-week"); got != "/pickem?week=3" {
		t.Fatalf("hostile action week redirect = %q, want /pickem?week=3", got)
	}
}

// TestPickemDataLimitsWeekSelectorToPublishedSeasonSchedule is item 12's
// own regression test (2026-08-31 post-wave audit, coordinator-added):
// /pickem's week selector must offer only the weeks THIS league's own
// published season schedule carries (14, in this fixture), not every
// week the raw NFL schedule mirror carries (18) — the same /matchups-vs-
// /team gap item 6 already closed for the Team terminal, applied here to
// /pickem's own week list (pickemData, pickem.go).
func TestPickemDataLimitsWeekSelectorToPublishedSeasonSchedule(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	schedule, err := GenerateSchedule(ScheduleParams{
		Season:    2026,
		TeamIDs:   teamIDList(service.teams),
		StartWeek: 1,
		Weeks:     14,
		Seed:      73,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	// The raw NFL mirror spans the full 18-week regular season.
	games := make([]GameInfo, 0, 18)
	for week := 1; week <= 18; week++ {
		games = append(games, GameInfo{ID: fmt.Sprintf("g-week%d", week), Week: week, Kickoff: now.Add(time.Duration(week) * 7 * 24 * time.Hour), Away: "BUF", Home: "MIA", SpreadLinePresent: true})
	}
	service.SetScheduleSource(func() []GameInfo { return games })

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	weekOptions, ok := data["week_options"].([]map[string]any)
	if !ok {
		t.Fatalf("week_options = %#v, want []map[string]any", data["week_options"])
	}
	if len(weekOptions) != 14 {
		t.Fatalf("week_options carries %d entries, want exactly 14 (the published schedule length), not 18 (the raw NFL mirror)", len(weekOptions))
	}
	for _, option := range weekOptions {
		value, _ := option["value"].(string)
		n, atoiErr := strconv.Atoi(value)
		if atoiErr != nil || n > 14 {
			t.Errorf("week_options carries %q, want only published weeks 1-14", value)
		}
	}

	// Week 15 is a real NFL week (inside the 18-week mirror) but past this
	// league's own published 14-week season: it must read a notice, not
	// silently render as if valid.
	request, _ = http.NewRequest(http.MethodGet, "/pickem?week=15", nil)
	data = service.PickemData(request)
	if data["has_week_notice"] != true {
		t.Fatal("week=15 (inside the NFL calendar, past the published 14-week season) produced no notice")
	}
	if data["week"] != 1 {
		t.Fatalf("week=15 fell back to %v, want the published season's first week (1)", data["week"])
	}
}

func TestPickemDataShape(t *testing.T) {
	service := newTestService(t, true) // demo mode: viewer key is "demo-guest"
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	if err := service.store.SetPickem("demo-guest", "g-final", "BUF", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)

	if data["week"] != 1 {
		t.Fatalf("week = %v, want 1", data["week"])
	}
	if data["can_pick"] != true {
		t.Error("can_pick should be true for a demo-mode viewer")
	}
	if data["games_empty"] != false {
		t.Error("games_empty should be false")
	}
	if data["picked_count"] != 1 {
		t.Fatalf("picked_count = %v, want 1", data["picked_count"])
	}

	gamesOut, ok := data["games"].([]PickemGameRow)
	if !ok || len(gamesOut) != 3 {
		t.Fatalf("games = %+v, want 3 week-1 entries", data["games"])
	}
	if gamesOut[0].ID != "g-final" || gamesOut[1].ID != "g-locked" || gamesOut[2].ID != "g-open" {
		t.Fatalf("games must sort by kickoff: %v %v %v", gamesOut[0].ID, gamesOut[1].ID, gamesOut[2].ID)
	}

	final := gamesOut[0]
	if final.Final != true || final.Winner != "BUF" {
		t.Fatalf("final game shape wrong: %+v", final)
	}
	if final.Pick != "BUF" || final.Correct != true || final.Wrong != false {
		t.Fatalf("correct pick not reflected: %+v", final)
	}
	if final.Locked != true {
		t.Error("a final game must also read as locked")
	}
	if final.Label != "BUF @ MIA" {
		t.Fatalf("label = %v, want BUF @ MIA", final.Label)
	}

	locked := gamesOut[1]
	if locked.Locked != true || locked.Final != false {
		t.Fatalf("past-kickoff, not-yet-final game must be locked and not final: %+v", locked)
	}

	open := gamesOut[2]
	if open.Locked != false {
		t.Fatalf("future game must not be locked: %+v", open)
	}
}

func TestPickemDataWrongPickFlag(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })
	if err := service.store.SetPickem("demo-guest", "g-final", "MIA", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	gamesOut, _ := data["games"].([]PickemGameRow)
	final := gamesOut[0]
	if final.Pick != "MIA" || final.Correct != false || final.Wrong != true {
		t.Fatalf("wrong pick not reflected: %+v", final)
	}
}

func TestPickemDataEmptySchedule(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	if data["week"] != 1 || data["games_empty"] != true {
		t.Fatalf("empty-schedule shape wrong: week=%v games_empty=%v", data["week"], data["games_empty"])
	}
	if data["leaderboard_empty"] != true {
		t.Error("leaderboard_empty should be true with no picks")
	}
}

func TestPickemDataCanPickFalseWithoutViewer(t *testing.T) {
	service := newTestService(t, false)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })
	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	if data["can_pick"] != false {
		t.Error("can_pick should be false when nobody is signed in outside demo mode")
	}
}

func TestPickemLeaderboardRanking(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	if _, _, err := service.store.AssignMember("a@example.com", "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("b@example.com", "Bob"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("c@example.com", "Cara"); err != nil {
		t.Fatal(err)
	}

	// Alice picks the final game correctly and also picks an open game, which
	// must not count toward her total.
	if err := service.store.SetPickem("a@example.com", "g-final", "BUF", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("a@example.com", "g-open", "KC", now); err != nil {
		t.Fatal(err)
	}
	// Bob picks the final game wrong.
	if err := service.store.SetPickem("b@example.com", "g-final", "MIA", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Cara has only a pick on a non-final game, so she must not rank.
	if err := service.store.SetPickem("c@example.com", "g-open", "KC", now); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	board, ok := data["leaderboard"].([]PickemLeaderboardEntry)
	if !ok || len(board) != 3 {
		t.Fatalf("leaderboard = %+v, want all 3 weekly participants", data["leaderboard"])
	}
	if data["leaderboard_empty"] != false {
		t.Error("leaderboard_empty should be false")
	}

	alice := board[0]
	teamAbbr := service.teamAbbreviation("team-1")
	if alice.Name != "Alice" || alice.Wins != 1 || alice.Losses != 1 || alice.Total != 2 || alice.Rank != "01" {
		t.Fatalf("alice entry wrong: %+v", alice)
	}
	if alice.Team != teamAbbr {
		t.Fatalf("alice team = %v, want %v", alice.Team, teamAbbr)
	}

	bob := board[1]
	if bob.Name != "Bob" || bob.Wins != 0 || bob.Losses != 2 || bob.Total != 2 || bob.Rank != "02" {
		t.Fatalf("bob entry wrong: %+v", bob)
	}

	if cara := board[2]; cara.Name != "Cara" || cara.Wins != 0 || cara.Losses != 0 || cara.Total != 0 {
		t.Fatalf("Cara's first pick must not create retroactive losses: %+v", cara)
	}
}

func TestPickemSetGuards(t *testing.T) {
	service := newTestService(t, true) // demo mode: viewer key is "demo-guest"
	now := time.Now()
	clockCalls := 0
	service.now = func() time.Time {
		clockCalls++
		return now
	}
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })
	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)

	if _, err := service.PickemSet(request, "no-such-game", "BUF"); err == nil {
		t.Error("unknown game accepted")
	}
	if _, err := service.PickemSet(request, "g-open", "NYJ"); err == nil {
		t.Error("a team that is not in the game must be rejected")
	}
	if _, err := service.PickemSet(request, "g-locked", "DAL"); err == nil {
		t.Error("a game that has already kicked off must reject new picks")
	}
	if enteredAt := service.store.Snapshot().PickemEnteredAt["demo-guest"]; !enteredAt.IsZero() {
		t.Fatalf("invalid or locked mutations established entry at %v", enteredAt)
	}

	clockCalls = 0
	game, err := service.PickemSet(request, "g-open", "KC")
	if err != nil {
		t.Fatal(err)
	}
	if game.ID != "g-open" {
		t.Fatalf("returned game = %+v, want g-open", game)
	}
	if got := service.store.Snapshot().Pickems["demo-guest"]["g-open"]; got != "KC" {
		t.Fatalf("pick not stored: %q, want KC", got)
	}
	if clockCalls != 1 {
		t.Fatalf("successful PickemSet read the clock %d times, want exactly 1", clockCalls)
	}
	if enteredAt := service.store.Snapshot().PickemEnteredAt["demo-guest"]; !enteredAt.Equal(now) {
		t.Fatalf("entry time = %v, want request clock %v", enteredAt, now)
	}
}

func TestPickemSetRejectsDurableUnavailableMarketBeforeLegacyBackfill(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "legacy-entry", Week: 1, Kickoff: now.Add(-2 * time.Hour), Away: "LEG", Home: "ACY", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
		{ID: "void-game", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "AAA", Home: "BBB", SourceObservedAt: now},
		{ID: "no-line-game", Week: 1, Kickoff: now.Add(3 * time.Hour), Away: "CCC", Home: "DDD", SourceObservedAt: now},
		{ID: "neighbor", Week: 1, Kickoff: now.Add(4 * time.Hour), Away: "EEE", Home: "FFF", SpreadLinePresent: true, SpreadLineTenths: 25, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	if err := service.store.SetPickem("demo-guest", "legacy-entry", "LEG", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Model a pre-v6 owner whose valid pick exists but whose immutable entry
	// timestamp still needs the legacy backfill. Both durable unavailable
	// market shapes must be rejected before that migration can run.
	service.store.mu.Lock()
	delete(service.store.state.PickemEnteredAt, "demo-guest")
	service.store.state.PickemMarkets["void-game"] = PickemMarket{
		Week: 1, Kickoff: games[1].Kickoff, Away: "AAA", Home: "BBB", LockAt: now.Add(time.Hour),
		Void: true, VoidReason: "no eligible market line before lock",
	}
	service.store.state.PickemMarkets["no-line-game"] = PickemMarket{
		Week: 1, Kickoff: games[2].Kickoff, Away: "CCC", Home: "DDD", LockAt: now.Add(2 * time.Hour),
	}
	service.store.mu.Unlock()

	request, _ := http.NewRequest(http.MethodPost, "/pickem", nil)
	for _, test := range []struct {
		gameID string
		team   string
	}{
		{gameID: "void-game", team: "AAA"},
		{gameID: "no-line-game", team: "CCC"},
	} {
		_, err := service.PickemSet(request, test.gameID, test.team)
		if err == nil || !strings.Contains(err.Error(), "no eligible market line") {
			t.Fatalf("PickemSet(%s) error = %v, want unavailable-market rejection", test.gameID, err)
		}
	}
	state := service.store.Snapshot()
	if got := state.Pickems["demo-guest"]["void-game"]; got != "" {
		t.Fatalf("void game pick mutated to %q", got)
	}
	if got := state.Pickems["demo-guest"]["no-line-game"]; got != "" {
		t.Fatalf("no-line game pick mutated to %q", got)
	}
	if enteredAt := state.PickemEnteredAt["demo-guest"]; !enteredAt.IsZero() {
		t.Fatalf("unavailable submission ran legacy backfill at %v", enteredAt)
	}
	if got := state.Pickems["demo-guest"]["legacy-entry"]; got != "LEG" {
		t.Fatalf("legacy pick changed during unavailable submission: %q", got)
	}
}

func TestPickemSetKeepsValidNeighborPickableWhenAnotherMarketIsVoid(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "void-game", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "AAA", Home: "BBB", SourceObservedAt: now},
		{ID: "neighbor", Week: 1, Kickoff: now.Add(4 * time.Hour), Away: "EEE", Home: "FFF", SpreadLinePresent: true, SpreadLineTenths: 25, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	service.store.mu.Lock()
	service.store.state.PickemMarkets["void-game"] = PickemMarket{
		Week: 1, Kickoff: games[0].Kickoff, Away: "AAA", Home: "BBB", LockAt: now.Add(time.Hour), Void: true,
	}
	service.store.mu.Unlock()

	request, _ := http.NewRequest(http.MethodPost, "/pickem", nil)
	if _, err := service.PickemSet(request, "neighbor", "EEE"); err != nil {
		t.Fatalf("valid neighboring game rejected: %v", err)
	}
	state := service.store.Snapshot()
	if got := state.Pickems["demo-guest"]["neighbor"]; got != "EEE" {
		t.Fatalf("neighbor pick = %q, want EEE", got)
	}
	if enteredAt := state.PickemEnteredAt["demo-guest"]; !enteredAt.Equal(now) {
		t.Fatalf("neighbor entry time = %v, want request time %v", enteredAt, now)
	}
}

func TestPickemDataMarksFutureUnavailableMarketWithoutClosingNeighbor(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "void-game", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "AAA", Home: "BBB", SourceObservedAt: now},
		{ID: "neighbor", Week: 1, Kickoff: now.Add(4 * time.Hour), Away: "EEE", Home: "FFF", SpreadLinePresent: true, SpreadLineTenths: 25, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	service.store.mu.Lock()
	service.store.state.PickemMarkets["void-game"] = PickemMarket{
		Week: 1, Kickoff: games[0].Kickoff, Away: "AAA", Home: "BBB", LockAt: now.Add(time.Hour), Void: true,
	}
	service.store.mu.Unlock()

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	rows := data["games"].([]PickemGameRow)
	byID := make(map[string]PickemGameRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	voidRow := byID["void-game"]
	if !voidRow.MarketUnavailable || !voidRow.Void || voidRow.Locked || voidRow.Picked {
		t.Fatalf("future void row = %+v, want unavailable/unlocked/unpicked", voidRow)
	}
	if voidRow.ResultLabel != "NO PICK · MARKET VOID" {
		t.Fatalf("future void result label = %q, want explicit no-pick state", voidRow.ResultLabel)
	}
	if data["picked_count"] != 0 || data["unpicked_count"] != 1 {
		t.Fatalf("Pick'em counters = picked:%v unpicked:%v, want void excluded and neighbor actionable", data["picked_count"], data["unpicked_count"])
	}
	neighbor := byID["neighbor"]
	if neighbor.MarketUnavailable || neighbor.Locked || neighbor.AwayLine != "EEE +2.5" || neighbor.HomeLine != "FFF -2.5" {
		t.Fatalf("valid neighbor row = %+v, want open frozen-line controls", neighbor)
	}
	summary := service.pickemHomeSummaryFromSnapshot(request, service.store.Snapshot(), now)
	if summary["game_count"] != 1 || summary["open_unpicked_count"] != 1 || summary["locked_unpicked_count"] != 0 || summary["unpicked_count"] != 1 {
		t.Fatalf("home Pick'em summary = %+v, want only the valid neighbor in actionable counts", summary)
	}
}

func TestPickemSetRequiresAuthOutsideDemoMode(t *testing.T) {
	service := newTestService(t, false)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })
	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)

	if _, err := service.PickemSet(request, "g-open", "KC"); err == nil {
		t.Error("an unauthenticated pick outside demo mode must be rejected")
	}
}

// ---------------------------------------------------------------------
// Seatless membership (pick'em HQ task, build item 1)
// ---------------------------------------------------------------------

// TestSeatlessMemberPicksAppearsOnLeaderboardNoSeatAssigned is the task's
// named acceptance test: a seatless member (EnsureMember, never
// AssignMember) picks, appears on the leaderboard with the right name and
// no team abbreviation, and still holds no seat afterward. The pick is
// written the same way PickemSet writes it — Store.SetPickem, keyed by
// email only — since forging a signed-in, non-demo identity is not
// reachable from this test package (see TestCommissionerForceAutopick's
// doc comment); PickemData's own leaderboard and PickemSet's own guards
// are both viewer-independent, so this test still exercises the exact
// code paths build item 1 is about.
func TestSeatlessMemberPicksAppearsOnLeaderboardNoSeatAssigned(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	member, err := service.EnsureMember("seatless@example.com", "Sea Tless")
	if err != nil {
		t.Fatal(err)
	}
	if member.TeamID != "" {
		t.Fatalf("EnsureMember assigned a team seat: %+v", member)
	}

	if err := service.store.SetPickem("seatless@example.com", "g-final", "BUF", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	board, ok := data["leaderboard"].([]PickemLeaderboardEntry)
	if !ok || len(board) != 1 {
		t.Fatalf("leaderboard = %+v, want the seatless member's one entry", data["leaderboard"])
	}
	entry := board[0]
	if entry.Name != "Sea Tless" || entry.Wins != 1 || entry.Losses != 1 || entry.Total != 2 {
		t.Fatalf("seatless leaderboard entry wrong: %+v", entry)
	}
	if entry.Team != "" {
		t.Fatalf("a seatless member must show no team abbreviation: %+v", entry)
	}

	after, ok := service.store.MemberByEmail("seatless@example.com")
	if !ok || after.TeamID != "" {
		t.Fatalf("picking must never assign a team seat: %+v", after)
	}
}

// ---------------------------------------------------------------------
// Consensus (build item 2): hidden before lock, correct after
// ---------------------------------------------------------------------

func TestPickemConsensusHiddenBeforeLockVisibleAfter(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	// g-open has not kicked off yet: picks here must never surface a split.
	if err := service.store.SetPickem("a@example.com", "g-open", "KC", now); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("b@example.com", "g-open", "DEN", now); err != nil {
		t.Fatal(err)
	}
	// g-locked already kicked off: 2 of 3 picked DAL (away), 1 picked PHI.
	if err := service.store.SetPickem("a@example.com", "g-locked", "DAL", games[1].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("b@example.com", "g-locked", "DAL", games[1].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("c@example.com", "g-locked", "PHI", games[1].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	gamesOut, _ := data["games"].([]PickemGameRow)

	var open, locked *PickemGameRow
	for index := range gamesOut {
		switch gamesOut[index].ID {
		case "g-open":
			open = &gamesOut[index]
		case "g-locked":
			locked = &gamesOut[index]
		}
	}
	if open == nil || locked == nil {
		t.Fatalf("fixture games missing from output: %+v", gamesOut)
	}

	openConsensus := open.Consensus
	if openConsensus.HasPicks != false || openConsensus.Total != 0 {
		t.Fatalf("an unlocked game must never expose a pick split: %+v", openConsensus)
	}

	lockedConsensus := locked.Consensus
	if lockedConsensus.HasPicks != true {
		t.Fatalf("a locked game with picks must expose a split: %+v", lockedConsensus)
	}
	if lockedConsensus.Total != 3 {
		t.Fatalf("consensus total = %v, want 3", lockedConsensus.Total)
	}
	if lockedConsensus.AwayPct != 67 || lockedConsensus.HomePct != 33 {
		t.Fatalf("consensus split = %+v, want away 67 / home 33", lockedConsensus)
	}
}

// ---------------------------------------------------------------------
// Streak (build item 2): consecutive correct on final games, backward
// ---------------------------------------------------------------------

func TestPickemStreakMostRecentBackward(t *testing.T) {
	now := time.Now()
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(-96 * time.Hour), Away: "AAA", Home: "BBB", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true}, // winner BBB
		{ID: "g2", Week: 1, Kickoff: now.Add(-72 * time.Hour), Away: "CCC", Home: "DDD", AwayScore: 20, HomeScore: 10, Final: true, ScoresPresent: true}, // winner CCC
		{ID: "g3", Week: 1, Kickoff: now.Add(-48 * time.Hour), Away: "EEE", Home: "FFF", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true}, // winner FFF
		{ID: "g4", Week: 1, Kickoff: now.Add(-24 * time.Hour), Away: "GGG", Home: "HHH", AwayScore: 20, HomeScore: 10, Final: true, ScoresPresent: true}, // winner GGG
	}
	picks := map[string]string{
		"g1": "AAA", // wrong (winner BBB) — breaks the streak here
		"g2": "CCC", // correct
		"g3": "FFF", // correct
		"g4": "GGG", // correct — most recent
	}
	if got := pickemStreak(games, frozenPickemMarkets(games), picks, games[0].Kickoff.Add(-time.Hour), now); got != 3 {
		t.Fatalf("streak = %d, want 3 (g4, g3, g2 correct; g1 breaks it)", got)
	}
}

// TestPickemStreakLabeledBreakStopsAtLossOrMissedLoss pins pickemStreak's
// loop-exit behavior after replacing the switch's own ineffective break
// (it exited only the switch, never the loop) with a labeled break on the
// enclosing loop. The streak result was already correct either way — a
// redundant check after the switch also stopped the loop — but this test
// locks in that a wrong pick (loss) and an unpicked final game (missed
// loss) both still stop the count at the first one hit, walking
// most-recent-first.
func TestPickemStreakLabeledBreakStopsAtLossOrMissedLoss(t *testing.T) {
	now := time.Now()
	lossGames := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(-48 * time.Hour), Away: "AAA", Home: "BBB", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true}, // winner BBB
		{ID: "g2", Week: 1, Kickoff: now.Add(-24 * time.Hour), Away: "CCC", Home: "DDD", AwayScore: 20, HomeScore: 10, Final: true, ScoresPresent: true}, // winner CCC
	}
	lossPicks := map[string]string{
		"g1": "AAA", // wrong — loss, breaks the streak
		"g2": "CCC", // correct, most recent
	}
	if got := pickemStreak(lossGames, frozenPickemMarkets(lossGames), lossPicks, lossGames[0].Kickoff.Add(-time.Hour), now); got != 1 {
		t.Fatalf("streak with trailing loss = %d, want 1 (g2 correct; g1 loss breaks it)", got)
	}

	missedGames := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(-48 * time.Hour), Away: "AAA", Home: "BBB", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true},
		{ID: "g2", Week: 1, Kickoff: now.Add(-24 * time.Hour), Away: "CCC", Home: "DDD", AwayScore: 20, HomeScore: 10, Final: true, ScoresPresent: true},
	}
	missedPicks := map[string]string{
		"g2": "CCC", // correct, most recent; g1 left unpicked — missed loss
	}
	if got := pickemStreak(missedGames, frozenPickemMarkets(missedGames), missedPicks, missedGames[0].Kickoff.Add(-time.Hour), now); got != 1 {
		t.Fatalf("streak with trailing missed pick = %d, want 1 (g2 correct; g1 missed-loss breaks it)", got)
	}
}

// TestPickemStreakNoFinalsYet pins the explicit edge case: with no final
// games at all, the streak is 0, not an error or a panic on empty input.
func TestPickemStreakNoFinalsYet(t *testing.T) {
	now := time.Now()
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(3 * time.Hour), Away: "AAA", Home: "BBB"},
	}
	picks := map[string]string{"g1": "AAA"}
	if got := pickemStreak(games, frozenPickemMarkets(games), picks, now, now); got != 0 {
		t.Fatalf("streak with no finals yet = %d, want 0", got)
	}
	if got := pickemStreak(nil, nil, nil, time.Time{}, now); got != 0 {
		t.Fatalf("streak over an empty schedule = %d, want 0", got)
	}
}

// TestPickemStreakDeterministicOnSimultaneousKickoffs pins a real-world
// edge case a manual QA pass against a live NFL slate surfaced: several
// games sharing one exact kickoff instant (a full Sunday 1:00 PM ET slate)
// have no natural "most recent" order among themselves, so the streak
// must still be deterministic — the same stable ID tie-break
// sortGamesByKickoff already uses, not whatever order sort.Slice happens
// to leave equal keys in.
func TestPickemStreakDeterministicOnSimultaneousKickoffs(t *testing.T) {
	kickoff := time.Now().Add(-48 * time.Hour)
	games := []GameInfo{
		{ID: "2025_18_AAA_BBB", Week: 18, Kickoff: kickoff, Away: "AAA", Home: "BBB", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true}, // winner BBB
		{ID: "2025_18_CCC_DDD", Week: 18, Kickoff: kickoff, Away: "CCC", Home: "DDD", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true}, // winner DDD
		{ID: "2025_18_EEE_FFF", Week: 18, Kickoff: kickoff, Away: "EEE", Home: "FFF", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true}, // winner FFF
	}
	picks := map[string]string{
		"2025_18_AAA_BBB": "BBB", // correct
		"2025_18_CCC_DDD": "DDD", // correct
		"2025_18_EEE_FFF": "EEE", // wrong — winner is FFF
	}
	// ID tie-break is descending, so among three equal kickoffs the walk
	// order is EEE_FFF, DDD_CCC... i.e. by ID string, highest first:
	// "2025_18_EEE_FFF" > "2025_18_CCC_DDD" > "2025_18_AAA_BBB". The wrong
	// pick (EEE_FFF) is walked first and breaks the streak immediately.
	for i := 0; i < 20; i++ {
		if got := pickemStreak(games, frozenPickemMarkets(games), picks, kickoff.Add(-time.Hour), time.Now()); got != 0 {
			t.Fatalf("streak on run %d = %d, want 0 (deterministic, not flaky)", i, got)
		}
	}
}

// TestPickemStreakSkipsUnpickedFinalGame verifies the season-entry rule: once
// entered, an unpicked gradeable kickoff is a missed loss and breaks the
// streak before the latest correct pick, leaving only that latest run counted.
func TestPickemStreakSkipsUnpickedFinalGame(t *testing.T) {
	now := time.Now()
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(-48 * time.Hour), Away: "AAA", Home: "BBB", AwayScore: 20, HomeScore: 10, Final: true, ScoresPresent: true}, // winner AAA
		{ID: "g2", Week: 1, Kickoff: now.Add(-24 * time.Hour), Away: "CCC", Home: "DDD", AwayScore: 20, HomeScore: 10, Final: true, ScoresPresent: true}, // winner CCC, unpicked
		{ID: "g3", Week: 1, Kickoff: now.Add(-1 * time.Hour), Away: "EEE", Home: "FFF", AwayScore: 20, HomeScore: 10, Final: true, ScoresPresent: true},  // winner EEE
	}
	picks := map[string]string{"g1": "AAA", "g3": "EEE"} // g2 never picked
	if got := pickemStreak(games, frozenPickemMarkets(games), picks, games[0].Kickoff.Add(-time.Hour), now); got != 1 {
		t.Fatalf("streak after a missed game = %d, want 1 latest win after missed_loss break", got)
	}
}

// ---------------------------------------------------------------------
// Weekly leaderboard and shared-rank ties (build item 2)
// ---------------------------------------------------------------------

func TestPickemWeeklyLeaderboardScopedToViewedWeek(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	if _, _, err := service.store.AssignMember("a@example.com", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("a@example.com", "g-final", "BUF", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	week2, _ := http.NewRequest(http.MethodGet, "/pickem?week=2", nil)
	data2 := service.PickemData(week2)
	if data2["week"] != 2 {
		t.Fatalf("week = %v, want 2 (from the query param)", data2["week"])
	}
	weekBoard2, _ := data2["week_leaderboard"].([]PickemLeaderboardEntry)
	if len(weekBoard2) != 1 || weekBoard2[0].Name != "Alice" {
		t.Fatalf("week-2 leaderboard must retain an established season entrant: %+v", weekBoard2)
	}
	if weekBoard2[0].Losses != 0 || weekBoard2[0].Total != 0 {
		t.Fatalf("future week must not manufacture losses before kickoff: %+v", weekBoard2[0])
	}
	if data2["week_leaderboard_empty"] != false {
		t.Error("week_leaderboard_empty should be false for an established entrant")
	}

	week1, _ := http.NewRequest(http.MethodGet, "/pickem?week=1", nil)
	data1 := service.PickemData(week1)
	weekBoard1, _ := data1["week_leaderboard"].([]PickemLeaderboardEntry)
	if len(weekBoard1) != 1 || weekBoard1[0].Name != "Alice" {
		t.Fatalf("week-1 leaderboard = %+v, want Alice's one entry", weekBoard1)
	}
	// The season leaderboard must still carry Alice regardless of the
	// viewed week — it is never scoped to it.
	seasonBoard, _ := data2["leaderboard"].([]PickemLeaderboardEntry)
	if len(seasonBoard) != 1 {
		t.Fatalf("season leaderboard while viewing week 2 = %+v, want Alice's entry still present", seasonBoard)
	}
}

func TestPickemLeaderboardSharedRankOnTie(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	if _, _, err := service.store.AssignMember("a@example.com", "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("b@example.com", "Bob"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("c@example.com", "Cara"); err != nil {
		t.Fatal(err)
	}

	if err := service.store.SetPickem("a@example.com", "g-final", "BUF", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("b@example.com", "g-final", "BUF", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("c@example.com", "g-final", "MIA", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	board, _ := data["leaderboard"].([]PickemLeaderboardEntry)
	if len(board) != 3 {
		t.Fatalf("leaderboard = %+v, want 3 entries", board)
	}
	if board[0].Name != "Alice" || board[0].Rank != "01" {
		t.Fatalf("board[0] = %+v, want Alice at rank 01", board[0])
	}
	if board[1].Name != "Bob" || board[1].Rank != "01" {
		t.Fatalf("board[1] = %+v, want Bob sharing rank 01", board[1])
	}
	if board[2].Name != "Cara" || board[2].Rank != "03" {
		t.Fatalf("board[2] = %+v, want Cara at rank 03 (competition ranking skips 02)", board[2])
	}
}

// ---------------------------------------------------------------------
// Week navigation and honest empty states (build item 2)
// ---------------------------------------------------------------------

func TestPickemDataWeekNavigationAcrossSchedule(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	request, _ := http.NewRequest(http.MethodGet, "/pickem?week=2", nil)
	data := service.PickemData(request)
	if data["week"] != 2 || data["current_week"] != 1 || data["is_current_week"] != false {
		t.Fatalf("week nav shape wrong: week=%v current_week=%v is_current_week=%v",
			data["week"], data["current_week"], data["is_current_week"])
	}
	gamesOut, _ := data["games"].([]PickemGameRow)
	if len(gamesOut) != 1 || gamesOut[0].ID != "g-nextweek" {
		t.Fatalf("week-2 games = %+v, want just g-nextweek", gamesOut)
	}
	if data["has_next_week"] != false {
		t.Error("has_next_week should be false at the schedule's last week")
	}
	if data["has_prev_week"] != true {
		t.Error("has_prev_week should be true")
	}
}

func TestPickemDataEmptyScheduleRecordAndWeekLeaderboardHonestState(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)

	record, ok := data["record"].(map[string]any)
	if !ok {
		t.Fatalf("record missing from an empty-schedule response: %+v", data)
	}
	if record["has_record"] != false || record["has_streak"] != false || record["season_total"] != 0 {
		t.Fatalf("empty-schedule record must be honestly empty: %+v", record)
	}
	if data["week_leaderboard_empty"] != true {
		t.Error("week_leaderboard_empty should be true with no games")
	}
	if got := len(data["week_options"].([]map[string]any)); got != 0 {
		t.Errorf("week_options with no schedule = %d entries, want 0", got)
	}
}

func TestGradePickemAgainstFrozenSpread(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	base := GameInfo{ID: "ats", Week: 1, Kickoff: now.Add(-time.Hour), Away: "BUF", Home: "MIA", AwayScore: 21, HomeScore: 24, Final: true, ScoresPresent: true}
	tests := []struct {
		name string
		line int
		pick string
		want PickemOutcome
	}{
		{name: "home favorite covers", line: 25, pick: "MIA", want: pickemWin},
		{name: "home favorite fails", line: 35, pick: "BUF", want: pickemWin},
		{name: "away favorite home covers", line: -25, pick: "MIA", want: pickemWin},
		{name: "pick em home wins", line: 0, pick: "MIA", want: pickemWin},
		{name: "exact push", line: 30, pick: "BUF", want: pickemPush},
		{name: "wrong side", line: 25, pick: "BUF", want: pickemLoss},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			market := PickemMarket{Frozen: true, LinePresent: true, LineTenths: tt.line}
			if got := gradePickemAt(base, market, tt.pick, base.Kickoff.Add(-time.Hour), now).Outcome; got != tt.want {
				t.Fatalf("outcome = %s, want %s", got, tt.want)
			}
		})
	}
	withoutScores := base
	withoutScores.ScoresPresent = false
	if got := gradePickemAt(withoutScores, PickemMarket{Frozen: true, LinePresent: true}, "MIA", base.Kickoff.Add(-time.Hour), now).Outcome; got != pickemPending {
		t.Fatalf("result without scores = %s, want pending", got)
	}
}

func TestPickemFirstThursdaySubmissionForMondayMakesSundayAnObligation(t *testing.T) {
	service := newTestService(t, true)
	enteredAt := time.Date(2026, 9, 17, 16, 0, 0, 0, time.UTC) // Thursday
	now := enteredAt
	service.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "before-entry", Week: 1, Kickoff: enteredAt.Add(-24 * time.Hour), Away: "A", Home: "B", SpreadLinePresent: true, SourceObservedAt: enteredAt.Add(-48 * time.Hour)},
		{ID: "sunday", Week: 2, Kickoff: time.Date(2026, 9, 20, 17, 0, 0, 0, time.UTC), Away: "C", Home: "D", SpreadLinePresent: true, SourceObservedAt: enteredAt.Add(-48 * time.Hour)},
		{ID: "monday", Week: 2, Kickoff: time.Date(2026, 9, 22, 0, 15, 0, 0, time.UTC), Away: "E", Home: "F", SpreadLinePresent: true, SourceObservedAt: enteredAt.Add(-48 * time.Hour)},
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	request, _ := http.NewRequest(http.MethodGet, "/pickem?week=2", nil)

	if _, err := service.PickemSet(request, "monday", "E"); err != nil {
		t.Fatalf("Thursday pick for Monday: %v", err)
	}
	if got := service.store.Snapshot().PickemEnteredAt["demo-guest"]; !got.Equal(enteredAt) {
		t.Fatalf("persisted entry = %v, want first submission %v", got, enteredAt)
	}

	now = games[1].Kickoff.Add(time.Hour)
	data := service.PickemData(request)
	rows := data["games"].([]PickemGameRow)
	byID := make(map[string]PickemGameRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	if byID["sunday"].Outcome != string(pickemMissedLoss) {
		t.Fatalf("unpicked Sunday outcome = %s, want missed_loss", byID["sunday"].Outcome)
	}
	if byID["monday"].Outcome != string(pickemPending) || byID["monday"].Pick != "E" || byID["monday"].Locked {
		t.Fatalf("later Monday pick must remain open and pending: %+v", byID["monday"])
	}
	record := data["record"].(map[string]any)
	if record["week_losses"] != 1 || record["season_losses"] != 1 || record["season_total"] != 1 {
		t.Fatalf("record after Sunday kickoff = %+v, want one missed loss", record)
	}
}

func TestPickemDataKeepsLegacyScoringTruthWhenEntryBackfillCannotPersist(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "legacy-entry", Week: 2, Kickoff: now.Add(-2 * time.Hour), Away: "A", Home: "B", AwayScore: 24, HomeScore: 17, Final: true, ScoresPresent: true, SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
		{ID: "legacy-missed", Week: 2, Kickoff: now.Add(-time.Hour), Away: "C", Home: "D", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	if err := service.store.ReconcilePickemMarkets(now, games, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("demo-guest", "legacy-entry", "A", games[0].Kickoff); err != nil {
		t.Fatal(err)
	}

	// Reproduce an imported pre-v6 owner: picks exist, the timestamp does not.
	service.store.mu.Lock()
	delete(service.store.state.PickemEnteredAt, "demo-guest")
	service.store.mu.Unlock()
	failThisStorePersist(service.store)

	request, _ := http.NewRequest(http.MethodGet, "/pickem?week=2", nil)
	data := service.PickemData(request)
	if got := service.store.Snapshot().PickemEnteredAt["demo-guest"]; !got.IsZero() {
		t.Fatalf("failed backfill unexpectedly remained in memory at %v", got)
	}
	record := data["record"].(map[string]any)
	if record["season_wins"] != 1 || record["season_losses"] != 1 || record["season_total"] != 2 || record["streak"] != 0 {
		t.Fatalf("legacy fallback record = %+v, want one win, one missed loss, streak zero", record)
	}
	weekBoard := data["week_leaderboard"].([]PickemLeaderboardEntry)
	if len(weekBoard) != 1 || weekBoard[0].Wins != 1 || weekBoard[0].Losses != 1 {
		t.Fatalf("legacy fallback weekly leaderboard = %+v", weekBoard)
	}
}

func TestPickemSeasonEntryPenalizesEveryLaterStartedGameAndPreservesFuture(t *testing.T) {
	now := time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC)
	enteredAt := now.Add(-3 * time.Hour)
	games := []GameInfo{
		{ID: "prior-week", Week: 1, Kickoff: now.Add(-72 * time.Hour), Away: "A", Home: "B", Final: true, ScoresPresent: true},
		{ID: "same-week-before-entry", Week: 2, Kickoff: now.Add(-4 * time.Hour), Away: "C", Home: "D", Final: true, ScoresPresent: true},
		{ID: "entry", Week: 2, Kickoff: enteredAt, Away: "E", Home: "F"},
		{ID: "later-week-picked", Week: 3, Kickoff: now.Add(-2 * time.Hour), Away: "G", Home: "H", HomeScore: 24, AwayScore: 17, Final: true, ScoresPresent: true},
		{ID: "later-week-missed", Week: 3, Kickoff: now.Add(-time.Hour), Away: "I", Home: "J", Final: true, ScoresPresent: true},
		{ID: "later-week-void", Week: 3, Kickoff: now.Add(-30 * time.Minute), Away: "K", Home: "L", Final: true, ScoresPresent: true},
		{ID: "future", Week: 4, Kickoff: now.Add(2 * time.Hour), Away: "M", Home: "N"},
	}
	markets := frozenPickemMarkets(games)
	voidMarket := markets["later-week-void"]
	voidMarket.Frozen = false
	voidMarket.LinePresent = false
	voidMarket.Void = true
	markets["later-week-void"] = voidMarket
	picks := map[string]string{
		"entry":             "E",
		"later-week-picked": "H",
	}

	record := tallyPicks(games, markets, picks, enteredAt, now)
	if record.Wins != 1 || record.Losses != 1 || record.Pushes != 0 || !record.Participated {
		t.Fatalf("season record = %+v, want one later win and one missed loss", record)
	}

	if got := gradePickemAt(games[0], markets[games[0].ID], "", enteredAt, now).Outcome; got != pickemPending {
		t.Fatalf("pre-entry week outcome = %s, want pending/no retroactive loss", got)
	}
	if got := gradePickemAt(games[1], markets[games[1].ID], "", enteredAt, now).Outcome; got != pickemPending {
		t.Fatalf("pre-entry same-week outcome = %s, want pending/no retroactive loss", got)
	}
	if got := gradePickemAt(games[4], markets[games[4].ID], "", enteredAt, now).Outcome; got != pickemMissedLoss {
		t.Fatalf("later skipped game outcome = %s, want missed_loss", got)
	}
	if got := gradePickemAt(games[5], markets[games[5].ID], "", enteredAt, now).Outcome; got != pickemVoid {
		t.Fatalf("void later game outcome = %s, want void", got)
	}
	if got := gradePickemAt(games[6], markets[games[6].ID], "", enteredAt, now).Outcome; got != pickemPending {
		t.Fatalf("future game outcome = %s, want pending/pickable", got)
	}
	if got := gradePickemAt(GameInfo{ID: "equal", Kickoff: enteredAt, Away: "O", Home: "P"}, PickemMarket{Frozen: true, LinePresent: true}, "", enteredAt, now).Outcome; got != pickemMissedLoss {
		t.Fatalf("kickoff exactly at entry = %s, want deterministic missed_loss", got)
	}
	nonParticipant := tallyPicks(games, markets, nil, time.Time{}, now)
	if nonParticipant.Participated || nonParticipant.Losses != 0 {
		t.Fatalf("never-participant record = %+v, want no artificial losses", nonParticipant)
	}
}

func TestPickemWeeklyLeaderboardKeepsEntrantForSkippedLaterWeek(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "entry", Week: 1, Kickoff: now.Add(-3 * time.Hour), Away: "A", Home: "B", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
		{ID: "week2-a", Week: 2, Kickoff: now.Add(-2 * time.Hour), Away: "C", Home: "D", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
		{ID: "week2-b", Week: 2, Kickoff: now.Add(-time.Hour), Away: "E", Home: "F", SpreadLinePresent: true, SourceObservedAt: now.Add(-14 * 24 * time.Hour)},
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	if _, _, err := service.store.AssignMember("entrant@example.com", "Entrant"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("entrant@example.com", "entry", "A", games[0].Kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem?week=2", nil)
	data := service.PickemData(request)
	board, ok := data["week_leaderboard"].([]PickemLeaderboardEntry)
	if !ok || len(board) != 1 {
		t.Fatalf("week leaderboard = %+v, want established entrant", data["week_leaderboard"])
	}
	if board[0].Name != "Entrant" || board[0].Wins != 0 || board[0].Losses != 2 || board[0].Total != 2 {
		t.Fatalf("skipped week leaderboard entry = %+v, want two missed losses", board[0])
	}
}

func TestPickemSetAndDataUseInjectedPerGameClock(t *testing.T) {
	svc := newTestService(t, true)
	kickoff := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	now := kickoff.Add(-time.Second)
	svc.now = func() time.Time { return now }
	preLockObservation := kickoff.Add(-14 * 24 * time.Hour)
	games := []GameInfo{{ID: "early", Week: 2, Kickoff: kickoff, Away: "AAA", Home: "BBB", SpreadLinePresent: true, SourceObservedAt: preLockObservation}, {ID: "late", Week: 2, Kickoff: kickoff.Add(time.Hour), Away: "CCC", Home: "DDD", SpreadLinePresent: true, SourceObservedAt: preLockObservation}}
	svc.SetScheduleSource(func() []GameInfo { return games })
	req, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	if _, err := svc.PickemSet(req, "early", "AAA"); err != nil {
		t.Fatalf("pick one second before kickoff: %v", err)
	}
	now = kickoff
	if _, err := svc.PickemSet(req, "early", "BBB"); err == nil {
		t.Fatal("pick at exact kickoff was accepted")
	}
	if _, err := svc.PickemSet(req, "late", "CCC"); err != nil {
		t.Fatalf("later game should remain pickable: %v", err)
	}
	data := svc.PickemData(req)
	rows := data["games"].([]PickemGameRow)
	if !rows[0].Locked || rows[1].Locked {
		t.Fatalf("per-game locks = early:%v late:%v", rows[0].Locked, rows[1].Locked)
	}
}

func TestPickemStreakMissBreaksPushAndVoidNeutral(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	games := []GameInfo{
		{ID: "win-old", Week: 1, Kickoff: now.Add(-4 * time.Hour), Away: "A", Home: "B", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true},
		{ID: "miss", Week: 1, Kickoff: now.Add(-3 * time.Hour), Away: "C", Home: "D"},
		{ID: "push", Week: 1, Kickoff: now.Add(-2 * time.Hour), Away: "E", Home: "F", AwayScore: 17, HomeScore: 20, Final: true, ScoresPresent: true},
		{ID: "win-new", Week: 1, Kickoff: now.Add(-time.Hour), Away: "G", Home: "H", AwayScore: 10, HomeScore: 20, Final: true, ScoresPresent: true},
	}
	markets := frozenPickemMarkets(games)
	push := markets["push"]
	push.LineTenths = 30
	markets["push"] = push
	picks := map[string]string{"win-old": "B", "push": "E", "win-new": "H"}
	if got := pickemStreak(games, markets, picks, games[0].Kickoff.Add(-time.Hour), now); got != 1 {
		t.Fatalf("streak = %d, want latest win then neutral push then missed-loss break", got)
	}
	void := markets["miss"]
	void.Void, void.Frozen, void.LinePresent = true, false, false
	markets["miss"] = void
	if got := pickemStreak(games, markets, picks, games[0].Kickoff.Add(-time.Hour), now); got != 2 {
		t.Fatalf("streak with void miss = %d, want two wins across neutral push/void", got)
	}
}

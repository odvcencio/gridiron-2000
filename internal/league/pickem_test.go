package league

import (
	"net/http"
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
		{ID: "g-final", Week: 1, Kickoff: now.Add(-72 * time.Hour), Away: "BUF", Home: "MIA", AwayScore: 24, HomeScore: 17, Final: true},
		{ID: "g-locked", Week: 1, Kickoff: now.Add(-30 * time.Minute), Away: "DAL", Home: "PHI"},
		{ID: "g-open", Week: 1, Kickoff: now.Add(3 * time.Hour), Away: "KC", Home: "DEN"},
		{ID: "g-nextweek", Week: 2, Kickoff: now.Add(7 * 24 * time.Hour), Away: "SF", Home: "SEA"},
	}
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

func TestPickemDataShape(t *testing.T) {
	service := newTestService(t, true) // demo mode: viewer key is "demo-guest"
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	if err := service.store.SetPickem("demo-guest", "g-final", "BUF"); err != nil {
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
	if err := service.store.SetPickem("demo-guest", "g-final", "MIA"); err != nil {
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
	if err := service.store.SetPickem("a@example.com", "g-final", "BUF"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("a@example.com", "g-open", "KC"); err != nil {
		t.Fatal(err)
	}
	// Bob picks the final game wrong.
	if err := service.store.SetPickem("b@example.com", "g-final", "MIA"); err != nil {
		t.Fatal(err)
	}
	// Cara has only a pick on a non-final game, so she must not rank.
	if err := service.store.SetPickem("c@example.com", "g-open", "KC"); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	board, ok := data["leaderboard"].([]PickemLeaderboardEntry)
	if !ok || len(board) != 2 {
		t.Fatalf("leaderboard = %+v, want 2 entries", data["leaderboard"])
	}
	if data["leaderboard_empty"] != false {
		t.Error("leaderboard_empty should be false")
	}

	alice := board[0]
	teamAbbr := service.teamAbbreviation("team-1")
	if alice.Name != "Alice" || alice.Correct != 1 || alice.Total != 1 || alice.Rank != "01" {
		t.Fatalf("alice entry wrong: %+v", alice)
	}
	if alice.Team != teamAbbr {
		t.Fatalf("alice team = %v, want %v", alice.Team, teamAbbr)
	}

	bob := board[1]
	if bob.Name != "Bob" || bob.Correct != 0 || bob.Total != 1 || bob.Rank != "02" {
		t.Fatalf("bob entry wrong: %+v", bob)
	}

	for _, entry := range board {
		if entry.Name == "Cara" {
			t.Fatalf("a member with no final-game pick must not rank: %+v", entry)
		}
	}
}

func TestPickemSetGuards(t *testing.T) {
	service := newTestService(t, true) // demo mode: viewer key is "demo-guest"
	now := time.Now()
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

	if err := service.store.SetPickem("seatless@example.com", "g-final", "BUF"); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/pickem", nil)
	data := service.PickemData(request)
	board, ok := data["leaderboard"].([]PickemLeaderboardEntry)
	if !ok || len(board) != 1 {
		t.Fatalf("leaderboard = %+v, want the seatless member's one entry", data["leaderboard"])
	}
	entry := board[0]
	if entry.Name != "Sea Tless" || entry.Correct != 1 || entry.Total != 1 {
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
	if err := service.store.SetPickem("a@example.com", "g-open", "KC"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("b@example.com", "g-open", "DEN"); err != nil {
		t.Fatal(err)
	}
	// g-locked already kicked off: 2 of 3 picked DAL (away), 1 picked PHI.
	if err := service.store.SetPickem("a@example.com", "g-locked", "DAL"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("b@example.com", "g-locked", "DAL"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("c@example.com", "g-locked", "PHI"); err != nil {
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
		{ID: "g1", Week: 1, Kickoff: now.Add(-96 * time.Hour), Away: "AAA", Home: "BBB", AwayScore: 10, HomeScore: 20, Final: true}, // winner BBB
		{ID: "g2", Week: 1, Kickoff: now.Add(-72 * time.Hour), Away: "CCC", Home: "DDD", AwayScore: 20, HomeScore: 10, Final: true}, // winner CCC
		{ID: "g3", Week: 1, Kickoff: now.Add(-48 * time.Hour), Away: "EEE", Home: "FFF", AwayScore: 10, HomeScore: 20, Final: true}, // winner FFF
		{ID: "g4", Week: 1, Kickoff: now.Add(-24 * time.Hour), Away: "GGG", Home: "HHH", AwayScore: 20, HomeScore: 10, Final: true}, // winner GGG
	}
	picks := map[string]string{
		"g1": "AAA", // wrong (winner BBB) — breaks the streak here
		"g2": "CCC", // correct
		"g3": "FFF", // correct
		"g4": "GGG", // correct — most recent
	}
	if got := pickemStreak(games, picks); got != 3 {
		t.Fatalf("streak = %d, want 3 (g4, g3, g2 correct; g1 breaks it)", got)
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
	if got := pickemStreak(games, picks); got != 0 {
		t.Fatalf("streak with no finals yet = %d, want 0", got)
	}
	if got := pickemStreak(nil, nil); got != 0 {
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
		{ID: "2025_18_AAA_BBB", Week: 18, Kickoff: kickoff, Away: "AAA", Home: "BBB", AwayScore: 10, HomeScore: 20, Final: true}, // winner BBB
		{ID: "2025_18_CCC_DDD", Week: 18, Kickoff: kickoff, Away: "CCC", Home: "DDD", AwayScore: 10, HomeScore: 20, Final: true}, // winner DDD
		{ID: "2025_18_EEE_FFF", Week: 18, Kickoff: kickoff, Away: "EEE", Home: "FFF", AwayScore: 10, HomeScore: 20, Final: true}, // winner FFF
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
		if got := pickemStreak(games, picks); got != 0 {
			t.Fatalf("streak on run %d = %d, want 0 (deterministic, not flaky)", i, got)
		}
	}
}

// TestPickemStreakSkipsUnpickedFinalGame documents the design choice: a
// final game the viewer never picked does not break the streak (it is
// invisible, like an ungraded tie), matching tallyPicks's own total.
func TestPickemStreakSkipsUnpickedFinalGame(t *testing.T) {
	now := time.Now()
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(-48 * time.Hour), Away: "AAA", Home: "BBB", AwayScore: 20, HomeScore: 10, Final: true}, // winner AAA
		{ID: "g2", Week: 1, Kickoff: now.Add(-24 * time.Hour), Away: "CCC", Home: "DDD", AwayScore: 20, HomeScore: 10, Final: true}, // winner CCC, unpicked
		{ID: "g3", Week: 1, Kickoff: now.Add(-1 * time.Hour), Away: "EEE", Home: "FFF", AwayScore: 20, HomeScore: 10, Final: true},  // winner EEE
	}
	picks := map[string]string{"g1": "AAA", "g3": "EEE"} // g2 never picked
	if got := pickemStreak(games, picks); got != 2 {
		t.Fatalf("streak skipping an unpicked final game = %d, want 2", got)
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
	if err := service.store.SetPickem("a@example.com", "g-final", "BUF"); err != nil {
		t.Fatal(err)
	}

	week2, _ := http.NewRequest(http.MethodGet, "/pickem?week=2", nil)
	data2 := service.PickemData(week2)
	if data2["week"] != 2 {
		t.Fatalf("week = %v, want 2 (from the query param)", data2["week"])
	}
	weekBoard2, _ := data2["week_leaderboard"].([]PickemLeaderboardEntry)
	if len(weekBoard2) != 0 {
		t.Fatalf("week-2 leaderboard must be empty (Alice's only pick is week 1): %+v", weekBoard2)
	}
	if data2["week_leaderboard_empty"] != true {
		t.Error("week_leaderboard_empty should be true for week 2")
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

	if err := service.store.SetPickem("a@example.com", "g-final", "BUF"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("b@example.com", "g-final", "BUF"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetPickem("c@example.com", "g-final", "MIA"); err != nil {
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

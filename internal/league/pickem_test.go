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

	gamesOut, ok := data["games"].([]map[string]any)
	if !ok || len(gamesOut) != 3 {
		t.Fatalf("games = %+v, want 3 week-1 entries", data["games"])
	}
	if gamesOut[0]["id"] != "g-final" || gamesOut[1]["id"] != "g-locked" || gamesOut[2]["id"] != "g-open" {
		t.Fatalf("games must sort by kickoff: %v %v %v", gamesOut[0]["id"], gamesOut[1]["id"], gamesOut[2]["id"])
	}

	final := gamesOut[0]
	if final["final"] != true || final["winner"] != "BUF" {
		t.Fatalf("final game shape wrong: %+v", final)
	}
	if final["pick"] != "BUF" || final["correct"] != true || final["wrong"] != false {
		t.Fatalf("correct pick not reflected: %+v", final)
	}
	if final["locked"] != true {
		t.Error("a final game must also read as locked")
	}
	if final["label"] != "BUF @ MIA" {
		t.Fatalf("label = %v, want BUF @ MIA", final["label"])
	}

	locked := gamesOut[1]
	if locked["locked"] != true || locked["final"] != false {
		t.Fatalf("past-kickoff, not-yet-final game must be locked and not final: %+v", locked)
	}

	open := gamesOut[2]
	if open["locked"] != false {
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
	gamesOut, _ := data["games"].([]map[string]any)
	final := gamesOut[0]
	if final["pick"] != "MIA" || final["correct"] != false || final["wrong"] != true {
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

	if _, err := service.store.AssignMember("a@example.com", "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.AssignMember("b@example.com", "Bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.AssignMember("c@example.com", "Cara"); err != nil {
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
	board, ok := data["leaderboard"].([]map[string]any)
	if !ok || len(board) != 2 {
		t.Fatalf("leaderboard = %+v, want 2 entries", data["leaderboard"])
	}
	if data["leaderboard_empty"] != false {
		t.Error("leaderboard_empty should be false")
	}

	alice := board[0]
	teamAbbr := service.teamAbbreviation("team-1")
	if alice["name"] != "Alice" || alice["correct"] != 1 || alice["total"] != 1 || alice["rank"] != "01" {
		t.Fatalf("alice entry wrong: %+v", alice)
	}
	if alice["team"] != teamAbbr {
		t.Fatalf("alice team = %v, want %v", alice["team"], teamAbbr)
	}

	bob := board[1]
	if bob["name"] != "Bob" || bob["correct"] != 0 || bob["total"] != 1 || bob["rank"] != "02" {
		t.Fatalf("bob entry wrong: %+v", bob)
	}

	for _, entry := range board {
		if entry["name"] == "Cara" {
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

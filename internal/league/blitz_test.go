package league

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// blitzFixturePool covers the eligibility and validation matrix: three
// players on KC (for the team-cap test, V8), one each on DEN/BUF/MIA/DAL
// (all in open pre2 games, for filling a 5-player entry), one each on
// SF/NYJ (kicked-off and final games, for V9), one on CIN (not in any
// slate game, for V5), and a synthesized DST entry (for V4).
func blitzFixturePool() []Player {
	return []Player{
		{ID: "p-kc-1", Name: "KC One", Position: "QB", NFLTeam: "KC"},
		{ID: "p-kc-2", Name: "KC Two", Position: "RB", NFLTeam: "KC"},
		{ID: "p-kc-3", Name: "KC Three", Position: "WR", NFLTeam: "KC"},
		{ID: "p-den-1", Name: "DEN One", Position: "WR", NFLTeam: "DEN"},
		{ID: "p-buf-1", Name: "BUF One", Position: "TE", NFLTeam: "BUF"},
		{ID: "p-mia-1", Name: "MIA One", Position: "K", NFLTeam: "MIA"},
		{ID: "p-dal-1", Name: "DAL One", Position: "RB", NFLTeam: "DAL"},
		{ID: "p-phi-1", Name: "PHI One", Position: "WR", NFLTeam: "PHI"},
		{ID: "p-sf-1", Name: "SF One", Position: "QB", NFLTeam: "SF"},
		{ID: "p-nyj-1", Name: "NYJ One", Position: "WR", NFLTeam: "NYJ"},
		{ID: "p-cin-1", Name: "CIN One", Position: "WR", NFLTeam: "CIN"},
		{ID: "DST-KC", Name: "Kansas City", Position: "DST", NFLTeam: "KC"},
	}
}

// blitzFixtureGames returns a pre2 slate with three open games (KC@DEN,
// BUF@MIA, DAL@PHI), one locked-but-not-final game (SF@SEA, kicked off 30
// minutes ago), and one final game (NYJ@NYG) — so pre2 itself is not
// closed. pre3 holds a single, already-final game, so pre3 is closed.
func blitzFixtureGames(now time.Time) []BlitzGame {
	return []BlitzGame{
		{ID: "g-open-1", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(3 * time.Hour)},
		{ID: "g-open-2", Slate: "pre2", Away: "BUF", Home: "MIA", Kickoff: now.Add(4 * time.Hour)},
		{ID: "g-open-3", Slate: "pre2", Away: "DAL", Home: "PHI", Kickoff: now.Add(5 * time.Hour)},
		{ID: "g-locked", Slate: "pre2", Away: "SF", Home: "SEA", Kickoff: now.Add(-30 * time.Minute)},
		{ID: "g-final", Slate: "pre2", Away: "NYJ", Home: "NYG", Kickoff: now.Add(-6 * time.Hour), Final: true},
		{ID: "g3-final", Slate: "pre3", Away: "LAR", Home: "ARI", Kickoff: now.Add(-6 * time.Hour), Final: true},
	}
}

// blitzTestService wires a demo-mode service (viewer key "demo-guest") with
// the fixture pool and a fixed clock, so BlitzAdd/BlitzRemove exercise the
// same lock math the page renders.
func blitzTestService(t *testing.T, now time.Time, games []BlitzGame) *Service {
	t.Helper()
	service := newTestService(t, true)
	service.now = func() time.Time { return now }
	service.SetPlayerSource(func() ([]Player, int64, string) { return blitzFixturePool(), 1, "live" })
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{Version: 1, Games: games, Stats: map[string]map[string]map[string]float64{}}
	})
	return service
}

// TestBlitzValidationMessages is T9: every section 7 message asserted
// verbatim, each triggered in isolation.
func TestBlitzValidationMessages(t *testing.T) {
	now := time.Now()
	games := blitzFixtureGames(now)
	request, _ := http.NewRequest(http.MethodGet, "/blitz", nil)

	t.Run("V1 sign in to enter the blitz", func(t *testing.T) {
		service := newTestService(t, false) // no demo mode, no forged auth
		service.now = func() time.Time { return now }
		service.SetBlitzSource(func() BlitzSnapshot { return BlitzSnapshot{Games: games} })
		if err := service.BlitzAdd(request, "pre2", "p-kc-1"); err == nil || err.Error() != "sign in to enter the blitz" {
			t.Fatalf("BlitzAdd error = %v, want the sign-in message", err)
		}
		if err := service.BlitzRemove(request, "pre2", "p-kc-1"); err == nil || err.Error() != "sign in to enter the blitz" {
			t.Fatalf("BlitzRemove error = %v, want the sign-in message", err)
		}
	})

	t.Run("V2 unknown slate", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre9", "p-kc-1"); err == nil || err.Error() != "unknown slate" {
			t.Fatalf("BlitzAdd error = %v, want unknown slate", err)
		}
		if err := service.BlitzRemove(request, "pre9", "p-kc-1"); err == nil || err.Error() != "unknown slate" {
			t.Fatalf("BlitzRemove error = %v, want unknown slate", err)
		}
	})

	t.Run("V10 this slate is closed", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre3", "p-kc-1"); err == nil || err.Error() != "this slate is closed" {
			t.Fatalf("BlitzAdd error = %v, want this slate is closed", err)
		}
		if err := service.BlitzRemove(request, "pre3", "p-kc-1"); err == nil || err.Error() != "this slate is closed" {
			t.Fatalf("BlitzRemove error = %v, want this slate is closed", err)
		}
	})

	t.Run("V3 choose a player from the pool", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "no-such-player"); err == nil || err.Error() != "choose a player from the pool" {
			t.Fatalf("BlitzAdd error = %v, want choose a player from the pool", err)
		}
	})

	t.Run("V4 defenses are not eligible in the blitz", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "DST-KC"); err == nil || err.Error() != "defenses are not eligible in the blitz" {
			t.Fatalf("BlitzAdd error = %v, want the defense message", err)
		}
	})

	t.Run("V5 that player's team does not play in this slate", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-cin-1"); err == nil || err.Error() != "that player's team does not play in this slate" {
			t.Fatalf("BlitzAdd error = %v, want the no-game message", err)
		}
	})

	t.Run("V9 that player's game has already kicked off", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-sf-1"); err == nil || err.Error() != "that player's game has already kicked off" {
			t.Fatalf("BlitzAdd error = %v, want the kicked-off message", err)
		}
		// Also reachable on remove: enter a player, fast-forward past
		// kickoff, then try to remove.
		early := blitzTestService(t, now.Add(-time.Hour), blitzFixtureGames(now.Add(-time.Hour)))
		if err := early.BlitzAdd(request, "pre2", "p-kc-1"); err != nil {
			t.Fatalf("setup add failed: %v", err)
		}
		early.now = func() time.Time { return now.Add(4 * time.Hour) } // now g-open-1 has kicked off
		if err := early.BlitzRemove(request, "pre2", "p-kc-1"); err == nil || err.Error() != "that player's game has already kicked off" {
			t.Fatalf("BlitzRemove error = %v, want the kicked-off message", err)
		}
	})

	t.Run("V7 that player is already in your entry", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-kc-1"); err != nil {
			t.Fatalf("setup add failed: %v", err)
		}
		if err := service.BlitzAdd(request, "pre2", "p-kc-1"); err == nil || err.Error() != "that player is already in your entry" {
			t.Fatalf("BlitzAdd error = %v, want the already-entered message", err)
		}
	})

	t.Run("V6 an entry holds at most 5 players", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		for _, id := range []string{"p-kc-1", "p-den-1", "p-buf-1", "p-mia-1", "p-dal-1"} {
			if err := service.BlitzAdd(request, "pre2", id); err != nil {
				t.Fatalf("setup add %s failed: %v", id, err)
			}
		}
		if err := service.BlitzAdd(request, "pre2", "p-phi-1"); err == nil || err.Error() != "an entry holds at most 5 players" {
			t.Fatalf("BlitzAdd error = %v, want the entry-full message", err)
		}
	})

	t.Run("V8 at most 2 players from one NFL team", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-kc-1"); err != nil {
			t.Fatalf("setup add failed: %v", err)
		}
		if err := service.BlitzAdd(request, "pre2", "p-kc-2"); err != nil {
			t.Fatalf("setup add failed: %v", err)
		}
		if err := service.BlitzAdd(request, "pre2", "p-kc-3"); err == nil || err.Error() != "at most 2 players from one NFL team" {
			t.Fatalf("BlitzAdd error = %v, want the team-cap message", err)
		}
	})

	t.Run("V11 that player is not in your entry", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzRemove(request, "pre2", "p-kc-1"); err == nil || err.Error() != "that player is not in your entry" {
			t.Fatalf("BlitzRemove error = %v, want the not-in-entry message", err)
		}
	})
}

// TestBlitzLockMatrix is T1: {before kickoff, after kickoff, game final,
// slate closed} x {add, remove} -> allow/deny per section 7.
func TestBlitzLockMatrix(t *testing.T) {
	now := time.Now()
	games := blitzFixtureGames(now)
	request, _ := http.NewRequest(http.MethodGet, "/blitz", nil)

	t.Run("add before kickoff succeeds", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-den-1"); err != nil {
			t.Fatalf("add before kickoff must succeed: %v", err)
		}
	})

	t.Run("add after kickoff (not yet final) is denied", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-sf-1"); err == nil {
			t.Fatal("add on a kicked-off, non-final game must be denied")
		}
	})

	t.Run("add on a final game is denied", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-nyj-1"); err == nil {
			t.Fatal("add on a final game must be denied")
		}
	})

	t.Run("add on a closed slate is denied even for an open-looking team", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre3", "p-kc-1"); err == nil {
			t.Fatal("add on a closed slate must be denied")
		}
	})

	t.Run("remove before kickoff succeeds", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzAdd(request, "pre2", "p-den-1"); err != nil {
			t.Fatal(err)
		}
		if err := service.BlitzRemove(request, "pre2", "p-den-1"); err != nil {
			t.Fatalf("remove before kickoff must succeed: %v", err)
		}
	})

	t.Run("remove after kickoff is denied", func(t *testing.T) {
		early := now.Add(-time.Hour)
		service := blitzTestService(t, early, blitzFixtureGames(early))
		if err := service.BlitzAdd(request, "pre2", "p-kc-1"); err != nil {
			t.Fatal(err)
		}
		service.now = func() time.Time { return now.Add(4 * time.Hour) }
		if err := service.BlitzRemove(request, "pre2", "p-kc-1"); err == nil {
			t.Fatal("remove after kickoff must be denied")
		}
	})

	t.Run("remove on a closed slate is denied", func(t *testing.T) {
		service := blitzTestService(t, now, games)
		if err := service.BlitzRemove(request, "pre3", "p-kc-1"); err == nil {
			t.Fatal("remove on a closed slate must be denied")
		}
	})
}

// TestBlitzEligibilityEdges is T6: a box-score playerID absent from the
// pool is ignored (no error) by the leaderboard and slot renderer; a pool
// player on a non-slate team is rejected with V5; a DST- pool ID is
// rejected with V4; and a 3-player entry scores exactly those 3 players.
func TestBlitzEligibilityEdges(t *testing.T) {
	now := time.Now()
	games := blitzFixtureGames(now)
	request, _ := http.NewRequest(http.MethodGet, "/blitz", nil)
	service := blitzTestService(t, now, games)

	if err := service.BlitzAdd(request, "pre2", "p-kc-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.BlitzAdd(request, "pre2", "p-den-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.BlitzAdd(request, "pre2", "p-buf-1"); err != nil {
		t.Fatal(err)
	}

	// A box-score playerID absent from the pool ("ghost-player") must not
	// error the leaderboard or the slot renderer; it simply cannot be
	// entered (V3) and cannot appear in anyone's stats to begin with.
	state := service.store.Snapshot()
	pool := service.pool()
	stats := map[string]map[string]float64{"ghost-player": {"rushYds": 40}}
	board := service.blitzLeaderboard(state, "pre2", games, stats, breakdownDefaultValues(), pool, now)
	if len(board) != 1 {
		t.Fatalf("board = %+v, want 1 entry", board)
	}
	if board[0]["total"] != "0.0" {
		t.Fatalf("a 3-player entry with no locked games must score 0.0 until kickoff, got %v", board[0]["total"])
	}
	players, _ := board[0]["players"].([]map[string]any)
	if len(players) != 3 {
		t.Fatalf("players = %+v, want exactly the 3 entered players", players)
	}

	if err := service.BlitzAdd(request, "pre2", "p-cin-1"); err == nil || err.Error() != "that player's team does not play in this slate" {
		t.Fatalf("a non-slate-team player must be rejected with V5, got %v", err)
	}
	if err := service.BlitzAdd(request, "pre2", "DST-KC"); err == nil || err.Error() != "defenses are not eligible in the blitz" {
		t.Fatalf("a DST- pool ID must be rejected with V4, got %v", err)
	}
}

// TestBlitzLeaderboardDeterminism is T4: two entries with equal totals
// share a rank (competition ranking: the next distinct total skips to
// index+1, section 4.5), tie order is earlier UpdatedAt then email, and
// repeated renders are byte-identical.
func TestBlitzLeaderboardDeterminism(t *testing.T) {
	now := time.Now()
	games := []BlitzGame{
		{ID: "g1", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(-2 * time.Hour), Final: true},
	}
	pool := []Player{
		{ID: "p-kc-1", Name: "KC One", Position: "QB", NFLTeam: "KC"},
		{ID: "p-kc-2", Name: "KC Two", Position: "QB", NFLTeam: "KC"},
		{ID: "p-den-1", Name: "DEN One", Position: "WR", NFLTeam: "DEN"},
	}
	stats := map[string]map[string]float64{
		"p-kc-1":  {"passYds": 100}, // 100 x 0.04 = 4.0
		"p-den-1": {"recYds": 40},   // 40 x 0.1 = 4.0 (tie with p-kc-1)
		"p-kc-2":  {"passYds": 50},  // 50 x 0.04 = 2.0 (distinct, lower)
	}
	service := newTestService(t, true)
	service.now = func() time.Time { return now }
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	state := PersistedState{
		Members: map[string]Member{
			"b@example.com": {Name: "Bravo", Email: "b@example.com"},
			"a@example.com": {Name: "Alpha", Email: "a@example.com"},
			"z@example.com": {Name: "Zulu", Email: "z@example.com"},
		},
		BlitzEntries: map[string]map[string]BlitzEntry{
			"b@example.com": {"pre2": {Players: []string{"p-kc-1"}, UpdatedAt: now.Add(-time.Hour)}},
			"a@example.com": {"pre2": {Players: []string{"p-den-1"}, UpdatedAt: now.Add(-time.Minute)}},
			"z@example.com": {"pre2": {Players: []string{"p-kc-2"}, UpdatedAt: now}},
		},
	}
	values := breakdownDefaultValues()
	poolView := service.pool()
	board := service.blitzLeaderboard(state, "pre2", games, stats, values, poolView, now)
	if len(board) != 3 {
		t.Fatalf("board = %+v, want 3 entries", board)
	}
	if board[0]["name"] != "Bravo" || board[1]["name"] != "Alpha" {
		t.Fatalf("tie order must be earlier UpdatedAt first (Bravo, then Alpha): %+v", board[:2])
	}
	if board[0]["rank"] != "01" || board[1]["rank"] != "01" {
		t.Fatalf("tied entries must share rank 01: %+v %+v", board[0], board[1])
	}
	if board[2]["name"] != "Zulu" || board[2]["rank"] != "03" {
		t.Fatalf("the next distinct total must be Zulu at rank 03: %+v", board[2])
	}

	board2 := service.blitzLeaderboard(state, "pre2", games, stats, values, poolView, now)
	encoded1, _ := json.Marshal(board)
	encoded2, _ := json.Marshal(board2)
	if string(encoded1) != string(encoded2) {
		t.Fatal("repeated renders must be byte-identical")
	}
}

// blitzWorkedExamplePool and blitzWorkedExampleGames mirror the design
// spec's section 6 worked example against the verified sample.
func blitzWorkedExamplePool() []Player {
	return []Player{
		{ID: "4038524", Name: "Gardner Minshew II", Position: "QB", NFLTeam: "ARI"},
		{ID: "4363538", Name: "Chad Ryland", Position: "K", NFLTeam: "ARI"},
		{ID: "4431431", Name: "Corey Kiner", Position: "RB", NFLTeam: "ARI"},
		{ID: "4603186", Name: "Jack Bech", Position: "WR", NFLTeam: "LV"},
		{ID: "4429086", Name: "Michael Mayer", Position: "TE", NFLTeam: "LV"},
		{ID: "p-hou-e", Name: "Player E", Position: "WR", NFLTeam: "HOU"},
	}
}

// blitzWorkedExampleGames anchors both games on kickoff, the ARI@LV
// kickoff instant: the design spec's fixture-now (section 6, "slots 1-4
// are locked ... slot 5 is editable") sits after this kickoff but before
// LV@HOU's.
func blitzWorkedExampleGames(kickoff time.Time) []BlitzGame {
	return []BlitzGame{
		{ID: "20260813_ARI@LV", Slate: "pre2", Away: "ARI", Home: "LV", Kickoff: kickoff, Final: true},
		{ID: "20260820_LV@HOU", Slate: "pre2", Away: "LV", Home: "HOU", Kickoff: kickoff.Add(7 * 24 * time.Hour)},
	}
}

// blitzWorkedExampleStats mirrors parsePreseasonBoxScore's output against
// internal/fantasy/testdata/preseason-boxscore-sample.json (pinned by
// TestParsePreseasonBoxScoreStatMaps in the fantasy package). It is
// duplicated here as literal values because internal/league must not
// import internal/fantasy (F15's seam boundary).
func blitzWorkedExampleStats() map[string]map[string]float64 {
	return map[string]map[string]float64{
		"4038524": {"passYds": 101, "passTD": 2, "passAttempts": 16, "passCompletions": 14},
		"4363538": {"fgMade": 2, "fgMissed": 1, "xpMade": 3},
		"4603186": {"receptions": 3, "recYds": 25, "recTD": 1, "targets": 3},
		"4429086": {"receptions": 2, "recYds": 19, "recTD": 1, "targets": 2},
	}
}

// TestBlitzWorkedExampleTeamCap demonstrates section 6's team-cap example
// on its own, uncrowded entry: with only Minshew and Ryland (both ARI)
// entered, adding a third ARI player (Kiner) hits V8, not V6 — the entry
// still has room. (The full 5-player entry in
// TestBlitzWorkedExampleScoring is already at capacity, so a further add
// there hits V6 first per the stated validation order, section 7: V6
// before V8.)
func TestBlitzWorkedExampleTeamCap(t *testing.T) {
	kickoff := time.Now().Add(-time.Hour) // still pre-kickoff for this entry
	games := blitzWorkedExampleGames(kickoff.Add(time.Hour))
	service := newTestService(t, true)
	service.now = func() time.Time { return kickoff }
	service.SetPlayerSource(func() ([]Player, int64, string) { return blitzWorkedExamplePool(), 1, "live" })
	service.SetBlitzSource(func() BlitzSnapshot { return BlitzSnapshot{Version: 1, Games: games} })

	request, _ := http.NewRequest(http.MethodGet, "/blitz?slate=pre2", nil)
	if err := service.BlitzAdd(request, "pre2", "4038524"); err != nil { // Minshew, ARI
		t.Fatal(err)
	}
	if err := service.BlitzAdd(request, "pre2", "4363538"); err != nil { // Ryland, ARI
		t.Fatal(err)
	}
	if err := service.BlitzAdd(request, "pre2", "4431431"); err == nil { // Kiner, ARI
		t.Fatal("a third ARI player (Kiner) must be rejected")
	} else if err.Error() != "at most 2 players from one NFL team" {
		t.Fatalf("wrong message for the third-ARI attempt: %q", err.Error())
	}
}

// TestBlitzWorkedExampleScoring is T2's league-level half: entry total
// 38.94 -> displayed 38.9, and per-player totals 12.0/8.0/10.0/8.9/0.0
// (section 6).
func TestBlitzWorkedExampleScoring(t *testing.T) {
	kickoff := time.Now()
	games := blitzWorkedExampleGames(kickoff)
	// clock moves in two phases: entries go in before kickoff (kickoff-1h,
	// exactly as a member would have entered before the games began), then
	// the view phase reads the fixture-now the spec anchors on — after
	// ARI@LV's kickoff, before LV@HOU's — so slots 1-4 read locked and slot
	// 5 reads editable, matching section 6.
	clock := kickoff.Add(-time.Hour)
	service := newTestService(t, true)
	service.now = func() time.Time { return clock }
	service.SetPlayerSource(func() ([]Player, int64, string) { return blitzWorkedExamplePool(), 1, "live" })
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{
			Version: 1,
			Games:   games,
			Stats:   map[string]map[string]map[string]float64{"pre2": blitzWorkedExampleStats()},
		}
	})

	request, _ := http.NewRequest(http.MethodGet, "/blitz?slate=pre2", nil)
	for _, playerID := range []string{"4038524", "4363538", "4603186", "4429086", "p-hou-e"} {
		if err := service.BlitzAdd(request, "pre2", playerID); err != nil {
			t.Fatalf("adding %s: %v", playerID, err)
		}
	}

	clock = kickoff.Add(time.Hour) // fixture-now: ARI@LV has kicked off, LV@HOU has not
	data := service.BlitzData(request)
	board, ok := data["leaderboard"].([]map[string]any)
	if !ok || len(board) != 1 {
		t.Fatalf("leaderboard = %+v, want 1 entry", data["leaderboard"])
	}
	if got := board[0]["total"]; got != "38.9" {
		t.Fatalf("entry total = %v, want 38.9", got)
	}

	players, ok := board[0]["players"].([]map[string]any)
	if !ok || len(players) != 5 {
		t.Fatalf("players = %+v, want 5", board[0]["players"])
	}
	want := map[string]string{
		"4038524": "12.0", "4363538": "8.0", "4603186": "10.0", "4429086": "8.9", "p-hou-e": "0.0",
	}
	for _, player := range players {
		id, _ := player["id"].(string)
		if got := player["points"]; got != want[id] {
			t.Errorf("%s points = %v, want %v", id, got, want[id])
		}
		if id == "p-hou-e" && player["revealed"] != false {
			t.Errorf("Player E's game has not kicked off; the slot must not be revealed: %+v", player)
		}
	}
}

func TestBlitzScoringFallsBackFromNonFiniteRuleValues(t *testing.T) {
	kickoff := time.Now().Add(-time.Hour)
	service := newTestService(t, true)
	pool := service.buildPool(blitzWorkedExamplePool(), 1, "fixture")
	entry := BlitzEntry{Players: []string{"4038524"}}
	games := blitzWorkedExampleGames(kickoff)
	stats := map[string]map[string]float64{
		"4038524": {"passYds": 101, "passTD": 2},
	}
	for _, points := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		values := breakdownDefaultValues()
		values["passTD"] = points
		slots := service.blitzSlotMaps(entry, games, stats, values, pool, kickoff.Add(time.Hour))
		if len(slots) != 1 {
			t.Fatalf("slots = %+v, want one locked player", slots)
		}
		if got := slots[0]["points"]; got != "12.0" {
			t.Errorf("rule value %v contaminated Blitz points: got %v, want 12.0", points, got)
		}
	}
}

// TestStateFingerprintBlitzSuffix is T8: attaching a blitz source changes
// the fingerprint; bumping the snapshot version changes it again; an
// unchanged snapshot leaves it stable.
func TestStateFingerprintBlitzSuffix(t *testing.T) {
	service := newTestService(t, true)
	base := service.StateFingerprint(0)

	version := int64(1)
	service.SetBlitzSource(func() BlitzSnapshot { return BlitzSnapshot{Version: version} })
	withSource := service.StateFingerprint(0)
	if withSource == base {
		t.Fatal("attaching a blitz source must change the fingerprint")
	}

	version = 2
	bumped := service.StateFingerprint(0)
	if bumped == withSource {
		t.Fatal("bumping the blitz snapshot version must change the fingerprint")
	}

	stable := service.StateFingerprint(0)
	if stable != bumped {
		t.Fatal("an unchanged snapshot must leave the fingerprint stable")
	}
}

// TestBlitzEntriesStateMigration is T7: a version-2 file without
// blitzEntries loads with an empty map; ResetLeague clears entries;
// ResetDraft preserves them; cloneState deep-copies (mutating a snapshot
// never mutates the store).
func TestBlitzEntriesStateMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"schemaVersion": 2,
		"ready": {}, "picks": [], "members": {}, "invites": [], "boards": {},
		"teamNames": {}, "draftOrder": [], "scoring": {}, "pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatalf("a file without blitzEntries must load cleanly: %v", err)
	}
	state := store.Snapshot()
	if state.BlitzEntries == nil {
		t.Fatal("BlitzEntries must decode to an empty map, not nil")
	}
	if len(state.BlitzEntries) != 0 {
		t.Fatalf("BlitzEntries = %+v, want empty", state.BlitzEntries)
	}

	now := time.Now()
	if err := store.BlitzSetEntry("a@example.com", "pre2", []string{"p1", "p2"}, now); err != nil {
		t.Fatal(err)
	}

	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().BlitzEntries["a@example.com"]["pre2"].Players; len(got) != 2 {
		t.Fatalf("ResetDraft must not touch blitz entries (game state, not draft state), got %+v", got)
	}

	snapshot := store.Snapshot()
	snapshot.BlitzEntries["a@example.com"]["pre2"].Players[0] = "mutated"
	if got := store.Snapshot().BlitzEntries["a@example.com"]["pre2"].Players[0]; got != "p1" {
		t.Fatalf("mutating a snapshot's blitz players leaked into the store: %q, want p1", got)
	}

	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().BlitzEntries; len(got) != 0 {
		t.Fatalf("ResetLeague must clear blitz entries, got %+v", got)
	}
}

// TestBreakdownAdditivity is T10: a projection-shaped stat line (no
// fgMade/fgMissed/xpMade/returnTD keys) renders identical rows whether or
// not the four Blitz rows exist in breakdownRows, and the four new rows
// render correctly when their keys are present.
func TestBreakdownAdditivity(t *testing.T) {
	service := newTestService(t, true)
	projectionStats := map[string]float64{
		"passYds": 250, "passTD": 2, "rushYds": 10, "receptions": 3,
	}
	rows, total := service.scoreBreakdown(projectionStats)
	if len(rows) != 4 {
		t.Fatalf("rows = %+v, want 4 (no kicker/return rows for a projection stat line)", rows)
	}
	for _, row := range rows {
		label, _ := row["label"].(string)
		switch label {
		case "FG made", "FG miss", "XP made", "Ret TD":
			t.Fatalf("a projection stat line must never render a Blitz-only row: %+v", row)
		}
	}
	// 250*0.04 + 2*4 + 10*0.1 + 3*0.5 = 10 + 8 + 1 + 1.5 = 20.5
	if total != "20.5" {
		t.Fatalf("total = %q, want 20.5 (unchanged by the four additive rows)", total)
	}

	blitzStats := map[string]float64{"fgMade": 1, "fgMissed": 1, "xpMade": 2, "returnTD": 1}
	blitzRows, blitzTotal := service.scoreBreakdown(blitzStats)
	if len(blitzRows) != 4 {
		t.Fatalf("blitz rows = %+v, want 4", blitzRows)
	}
	wantLabels := []string{"Ret TD", "FG made", "FG miss", "XP made"}
	for index, label := range wantLabels {
		if blitzRows[index]["label"] != label {
			t.Fatalf("row %d label = %v, want %v (order: MISC before KICKING)", index, blitzRows[index]["label"], label)
		}
	}
	// 1*6 (returnTD) + 1*3 (fgMade) + 1*-1 (fgMissed) + 2*1 (xpMade) = 10.0
	if blitzTotal != "10.0" {
		t.Fatalf("blitz total = %q, want 10.0", blitzTotal)
	}
}

// TestPreWeek1StatLine pins the compact stat-line format (owner directive,
// 2026-08-16): grouped Passing/Rushing/Receiving/Kicking/Misc, items
// joined ", " within a group, groups joined " · ", a value of exactly 1
// on a touchdown-style key prints bare. Fixtures are drawn from the
// verified preseason box-score sample (internal/fantasy/testdata/
// preseason-boxscore-sample.json / preseason_test.go).
func TestPreWeek1StatLine(t *testing.T) {
	cases := []struct {
		name  string
		stats map[string]float64
		want  string
	}{
		{"no data", nil, ""},
		{"empty map", map[string]float64{}, ""},
		{
			"rushing only, single TD",
			map[string]float64{"carries": 8, "rushYds": 47, "rushTD": 1},
			"8 att, 47 rush yds, rush TD",
		},
		{
			"rushing plus receiving (Kiner fixture)",
			map[string]float64{"rushYds": 59, "carries": 10, "receptions": 2, "recYds": 13, "targets": 2},
			"10 att, 59 rush yds · 2 rec, 13 rec yds",
		},
		{
			"kicking (Ryland fixture)",
			map[string]float64{"fgMade": 2, "fgMissed": 1, "xpMade": 3},
			"2 FG, 1 FG miss, 3 XP",
		},
		{
			"passing, multi-TD (Minshew fixture)",
			map[string]float64{"passYds": 101, "passTD": 2, "passAttempts": 16, "passCompletions": 14},
			"16 pass att, 101 pass yds, 2 pass TD",
		},
		{
			"unmapped keys only (punting) never crash into a blank-looking line",
			map[string]float64{"puntYards": 40},
			"",
		},
		{
			"zero-valued keys are dropped like everywhere else in this package",
			map[string]float64{"carries": 3, "rushYds": 0},
			"3 att",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := preWeek1StatLine(tc.stats); got != tc.want {
				t.Errorf("preWeek1StatLine(%+v) = %q, want %q", tc.stats, got, tc.want)
			}
		})
	}
}

// TestBlitzRests pins the "likely to rest" tier rule (owner directive,
// 2026-08-16): a rookie never rests regardless of ADPRank; a non-rookie
// rests only inside [1, blitzRestingADPRankMax] — the "strong (low) ADP"
// established-starter band. ADPRank == 0 (mergePool's "not on the live
// ADP list at all" marker) is explicitly not "low."
func TestBlitzRests(t *testing.T) {
	cases := []struct {
		name    string
		player  Player
		resting bool
	}{
		{"rookie at a strong ADPRank never rests", Player{Rookie: true, ADPRank: 5}, false},
		{"no ADP at all is not a resting star", Player{Rookie: false, ADPRank: 0}, false},
		{"ADPRank 1 (the strongest possible) rests", Player{Rookie: false, ADPRank: 1}, true},
		{"ADPRank at the threshold rests", Player{Rookie: false, ADPRank: blitzRestingADPRankMax}, true},
		{"ADPRank just past the threshold does not rest", Player{Rookie: false, ADPRank: blitzRestingADPRankMax + 1}, false},
		{"deep bench ADPRank does not rest", Player{Rookie: false, ADPRank: 250}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := blitzRests(tc.player); got != tc.resting {
				t.Errorf("blitzRests(%+v) = %v, want %v", tc.player, got, tc.resting)
			}
		})
	}
}

// blitzOrderingFixturePool covers the board's three ordering tiers (owner
// directive, 2026-08-16): two pre1 producers (different point totals);
// three zero-pre1 front-tier players — a rookie at a strong ADPRank, a
// no-ADP depth player, and a deep-bench player whose weak (high) ADPRank
// is NOT "established, strong ADP" and so must not earn the resting tag
// either; and one zero-pre1 back-tier player, an established strong-ADP
// starter, the board's only "likely to rest" row.
func blitzOrderingFixturePool() []Player {
	return []Player{
		{ID: "p-producer-b", Name: "Producer Bravo", Position: "WR", NFLTeam: "MIA"},
		{ID: "p-producer-a", Name: "Producer Alpha", Position: "RB", NFLTeam: "DEN"},
		{ID: "p-rookie-depth", Name: "Rookie Depth", Position: "WR", NFLTeam: "BUF", Rookie: true, ADPRank: 5},
		{ID: "p-noadp-depth", Name: "NoADP Depth", Position: "RB", NFLTeam: "KC", ADPRank: 0},
		{ID: "p-established-star", Name: "Established Star", Position: "QB", NFLTeam: "DAL", ADPRank: 10},
		{ID: "p-deep-bench", Name: "Deep Bench", Position: "TE", NFLTeam: "PHI", ADPRank: 250},
	}
}

// blitzOrderingFixtureGames returns three pre2 games that have not yet
// kicked off, so every ordering fixture below tests the board's tiering
// alone. A game needs a real future kickoff here: a zero kickoff instant
// reads as already started (blitzGameLocked), which would lock the whole
// fixture and hide the tiering under the lock rule.
func blitzOrderingFixtureGames(now time.Time) []BlitzGame {
	return []BlitzGame{
		{ID: "g1", Slate: "pre2", Away: "DEN", Home: "KC", Kickoff: now.Add(3 * time.Hour)},
		{ID: "g2", Slate: "pre2", Away: "BUF", Home: "MIA", Kickoff: now.Add(4 * time.Hour)},
		{ID: "g3", Slate: "pre2", Away: "DAL", Home: "PHI", Kickoff: now.Add(5 * time.Hour)},
	}
}

// TestBlitzEligiblePlayersOrdersThreeTiers is the ordering spec itself
// (owner directive, 2026-08-16): pre1 producers first (descending
// points), then the zero-pre1 front tier (rookie/no-ADP depth,
// alphabetical), then the zero-pre1 back tier (established starters,
// "likely to rest," alphabetical). Every position/team-plays-in-slate
// filter stays untouched — this only changes order and labels.
func TestBlitzEligiblePlayersOrdersThreeTiers(t *testing.T) {
	now := time.Now()
	service := newTestService(t, true)
	pool := playerPool{players: blitzOrderingFixturePool()}
	pre1Stats := map[string]map[string]float64{
		// 50*0.1 (rushYards) + 6 (rushTD) = 11.0
		"p-producer-a": {"rushYds": 50, "carries": 10, "rushTD": 1},
		// 20*0.1 (recYards) + 2*0.5 (reception) = 4.0
		"p-producer-b": {"recYds": 20, "receptions": 4},
	}
	rows := service.blitzEligiblePlayers(pool, blitzOrderingFixtureGames(now), nil, pre1Stats, now)

	wantOrder := []string{
		"p-producer-a", "p-producer-b", // pre1 producers, 11.0 then 4.0
		// zero-pre1 front tier — Deep Bench included: a weak (high)
		// ADPRank is not "established, strong ADP" (blitzRests), so it
		// never earns the resting tag even though it does carry an ADP.
		"p-deep-bench", "p-noadp-depth", "p-rookie-depth",
		"p-established-star", // zero-pre1 back tier: the lone resting star
	}
	if len(rows) != len(wantOrder) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(wantOrder), rows)
	}
	for index, want := range wantOrder {
		if got := rows[index]["id"]; got != want {
			t.Errorf("row %d id = %v, want %v (full order: %v)", index, got, want, idsOf(rows))
		}
	}

	// Only the true back-tier row carries the resting tag; the deep-bench
	// player (weak ADP) must render like any other zero-pre1 depth player.
	for _, row := range rows {
		wantResting := row["id"] == "p-established-star"
		if got := row["resting"]; got != wantResting {
			t.Errorf("row %v resting = %v, want %v", row["id"], got, wantResting)
		}
	}

	// Rookie flag threads through to the row regardless of tier.
	rookieRow := rowByID(rows, "p-rookie-depth")
	if rookieRow["is_rookie"] != true {
		t.Errorf("rookie row is_rookie = %v, want true", rookieRow["is_rookie"])
	}
	nonRookieRow := rowByID(rows, "p-noadp-depth")
	if nonRookieRow["is_rookie"] != false {
		t.Errorf("non-rookie row is_rookie = %v, want false", nonRookieRow["is_rookie"])
	}
}

// TestBlitzEligiblePlayersRowEvidence checks each row's own pre1 evidence
// text (owner directive, 2026-08-16): a producer's row carries "PRE1
// {points} — {stat line}"; a zero-pre1 row with no box-score data at all
// carries the honest "no pre1 snaps" copy instead of a blank field.
func TestBlitzEligiblePlayersRowEvidence(t *testing.T) {
	now := time.Now()
	service := newTestService(t, true)
	pool := playerPool{players: blitzOrderingFixturePool()}
	pre1Stats := map[string]map[string]float64{
		"p-producer-a": {"rushYds": 50, "carries": 10, "rushTD": 1},
	}
	rows := service.blitzEligiblePlayers(pool, blitzOrderingFixtureGames(now), nil, pre1Stats, now)

	producer := rowByID(rows, "p-producer-a")
	if producer["has_pre1"] != true {
		t.Errorf("producer has_pre1 = %v, want true", producer["has_pre1"])
	}
	wantSummary := "PRE1 11.0 — 10 att, 50 rush yds, rush TD"
	if producer["pre1_summary"] != wantSummary {
		t.Errorf("producer pre1_summary = %q, want %q", producer["pre1_summary"], wantSummary)
	}

	depth := rowByID(rows, "p-noadp-depth")
	if depth["has_pre1"] != false {
		t.Errorf("depth has_pre1 = %v, want false", depth["has_pre1"])
	}
	if depth["pre1_summary"] != "no pre1 snaps" {
		t.Errorf("depth pre1_summary = %q, want %q", depth["pre1_summary"], "no pre1 snaps")
	}
}

// TestBlitzEligiblePlayersNilPre1FallsBackHonestly is the "Tank01
// unavailable" degrade path (owner directive, 2026-08-16): a nil pre1Stats
// map — exactly what blitzPre1Stats() returns when SetBlitzPre1Source was
// never called — must not crash, and every player falls into the
// zero-pre1 group, still correctly tiered by the rookie/ADP rules.
func TestBlitzEligiblePlayersNilPre1FallsBackHonestly(t *testing.T) {
	now := time.Now()
	service := newTestService(t, true)
	pool := playerPool{players: blitzOrderingFixturePool()}
	rows := service.blitzEligiblePlayers(pool, blitzOrderingFixtureGames(now), nil, nil, now)
	if len(rows) != len(blitzOrderingFixturePool()) {
		t.Fatalf("got %d rows, want %d — nil pre1Stats must not drop or crash on any player", len(rows), len(blitzOrderingFixturePool()))
	}
	// The established star still sinks to the very last row even with no
	// pre1 data anywhere: the fallback tiering is not "no ordering at
	// all," it is "ordering driven by rookie/ADP alone."
	if got := rows[len(rows)-1]["id"]; got != "p-established-star" {
		t.Errorf("last row id = %v, want p-established-star", got)
	}
	for _, row := range rows {
		if row["has_pre1"] != false || row["pre1_summary"] != "no pre1 snaps" {
			t.Errorf("row %v must render the honest no-data copy when pre1Stats is nil: %+v", row["id"], row)
		}
	}
}

// TestBlitzDataFallsBackWithoutPre1Source is the integration-level twin of
// TestBlitzEligiblePlayersNilPre1FallsBackHonestly: BlitzData itself must
// not panic or error when SetBlitzPre1Source is never called (the honest
// "no TANK01 key" / "pre1 fetch failed" state).
func TestBlitzDataFallsBackWithoutPre1Source(t *testing.T) {
	now := time.Now()
	games := blitzOrderingFixtureGames(now)
	service := newTestService(t, true)
	service.now = func() time.Time { return now }
	service.SetPlayerSource(func() ([]Player, int64, string) { return blitzOrderingFixturePool(), 1, "live" })
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{Version: 1, Games: games, Stats: map[string]map[string]map[string]float64{}}
	})
	// SetBlitzPre1Source is deliberately never called.

	request, _ := http.NewRequest(http.MethodGet, "/blitz?slate=pre2", nil)
	data := service.BlitzData(request)
	eligible, ok := data["eligible"].([]map[string]any)
	if !ok || len(eligible) == 0 {
		t.Fatalf("BlitzData without a pre1 source must still render the eligible list, got %+v", data["eligible"])
	}
	for _, row := range eligible {
		if row["has_pre1"] != false {
			t.Errorf("row %v has_pre1 = %v, want false with no pre1 source attached", row["id"], row["has_pre1"])
		}
	}
}

// TestBlitzEligiblePlayersLocksAtKickoff is the board half of V9 (owner
// directive, 2026-08-22: "when a player has already played, they are no
// longer capable of adding, they should lock as soon as their game
// starts"). BlitzAdd already refuses the add (TestBlitzLockMatrix); this
// asserts the board says so before the click instead of after it: the row
// carries locked, and it sinks below every player still addable — even
// when its pre1 production would otherwise sort it first.
func TestBlitzEligiblePlayersLocksAtKickoff(t *testing.T) {
	now := time.Now()
	service := newTestService(t, true)
	pool := playerPool{players: blitzOrderingFixturePool()}
	games := []BlitzGame{
		// DEN@KC has kicked off: Producer Alpha (DEN) and NoADP Depth (KC)
		// are both locked, and Producer Alpha is the board's top scorer.
		{ID: "g1", Slate: "pre2", Away: "DEN", Home: "KC", Kickoff: now.Add(-30 * time.Minute)},
		{ID: "g2", Slate: "pre2", Away: "BUF", Home: "MIA", Kickoff: now.Add(3 * time.Hour)},
		{ID: "g3", Slate: "pre2", Away: "DAL", Home: "PHI", Kickoff: now.Add(4 * time.Hour)},
	}
	pre1Stats := map[string]map[string]float64{
		"p-producer-a": {"rushYds": 50, "carries": 10, "rushTD": 1},
		"p-producer-b": {"recYds": 20, "receptions": 4},
	}
	rows := service.blitzEligiblePlayers(pool, games, nil, pre1Stats, now)

	wantLocked := map[string]bool{"p-producer-a": true, "p-noadp-depth": true}
	for _, row := range rows {
		id, _ := row["id"].(string)
		if got := row["locked"]; got != wantLocked[id] {
			t.Errorf("row %s locked = %v, want %v", id, got, wantLocked[id])
		}
	}

	seenLocked := false
	for _, row := range rows {
		if row["locked"] == true {
			seenLocked = true
			continue
		}
		if seenLocked {
			t.Errorf("row %v is still addable but sorts after a locked row: %v", row["id"], idsOf(rows))
		}
	}
}

// TestBlitzDataEligibleCannotAddAfterKickoff is the page-level twin: the
// eligible list BlitzData ships must mark a kicked-off player and a
// final-game player as unaddable, while every player whose game is still
// upcoming stays addable. The fixture's SF@SEA kicked off 30 minutes ago
// and NYJ@NYG is already final, so both cases appear on one open slate.
func TestBlitzDataEligibleCannotAddAfterKickoff(t *testing.T) {
	now := time.Now()
	service := blitzTestService(t, now, blitzFixtureGames(now))
	request, _ := http.NewRequest(http.MethodGet, "/blitz?slate=pre2", nil)

	eligible, ok := service.BlitzData(request)["eligible"].([]map[string]any)
	if !ok {
		t.Fatalf("BlitzData eligible is not a row list")
	}
	want := map[string]bool{
		"p-kc-1":  true,
		"p-den-1": true,
		"p-buf-1": true,
		"p-sf-1":  false, // SF@SEA kicked off 30 minutes ago
		"p-nyj-1": false, // NYJ@NYG is final
	}
	for id, canAdd := range want {
		row := rowByID(eligible, id)
		if row == nil {
			t.Fatalf("eligible list is missing %s: %v", id, idsOf(eligible))
		}
		if got := row["can_add"]; got != canAdd {
			t.Errorf("row %s can_add = %v, want %v", id, got, canAdd)
		}
	}
}

// TestStateFingerprintMovesAtBlitzKickoff covers the freshness half of
// the lock: a board open before kickoff soft-refreshes on the same 4s
// /api/league/version poll everything else uses, so the Add button turns
// itself off at kickoff. Nothing else moves here — no persisted state
// changes and the snapshot version is fixed — so only a lock-boundary
// term in the fingerprint can make it change.
func TestStateFingerprintMovesAtBlitzKickoff(t *testing.T) {
	kickoff := time.Now().Add(time.Hour)
	games := []BlitzGame{{ID: "g1", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: kickoff}}
	service := newTestService(t, true)
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{Version: 1, Games: games, Stats: map[string]map[string]map[string]float64{}}
	})

	service.now = func() time.Time { return kickoff.Add(-time.Minute) }
	before := service.StateFingerprint(1)
	stable := service.StateFingerprint(1)
	if stable != before {
		t.Fatalf("fingerprint moved without a lock transition: %s then %s", before, stable)
	}

	service.now = func() time.Time { return kickoff }
	if after := service.StateFingerprint(1); after == before {
		t.Fatalf("fingerprint did not move across kickoff (%s); an open board keeps offering a locked player", after)
	}
}

// idsOf collects a row list's "id" values in order, for a readable
// t.Errorf on an ordering mismatch.
func idsOf(rows []map[string]any) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// rowByID finds one row by its "id" field; the caller's fixtures always
// carry the ID being asked for, so a missing ID returns a nil map rather
// than panicking (a nil map's subscript reads as a friendlier failure than
// an index-out-of-range in test output).
func rowByID(rows []map[string]any, id string) map[string]any {
	for _, row := range rows {
		if row["id"] == id {
			return row
		}
	}
	return nil
}

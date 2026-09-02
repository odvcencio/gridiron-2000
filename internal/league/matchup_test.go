package league

import (
	"strings"
	"testing"
)

func TestOpponentInWeekResolvesHomeAndAway(t *testing.T) {
	games := []GameInfo{
		{Week: 1, Home: "DEN", Away: "KC"},
		{Week: 1, Home: "SF", Away: "SEA"},
		{Week: 2, Home: "KC", Away: "DEN"},
	}
	opp, home, ok := opponentInWeek(games, "kc", 1)
	if !ok || opp != "DEN" || home {
		t.Fatalf("KC week 1 = (%q, home=%v, ok=%v), want (DEN, false, true)", opp, home, ok)
	}
	opp, home, ok = opponentInWeek(games, "DEN", 1)
	if !ok || opp != "KC" || !home {
		t.Fatalf("DEN week 1 = (%q, home=%v, ok=%v), want (KC, true, true)", opp, home, ok)
	}
}

// TestOpponentInWeekByeReturnsNotOK checks the missing-data path (design
// point 5): a team with no game in the requested week -- a bye -- must
// report ok=false, never a fabricated opponent.
func TestOpponentInWeekByeReturnsNotOK(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "DEN", Away: "KC"}}
	if _, _, ok := opponentInWeek(games, "NE", 1); ok {
		t.Fatalf("NE has no week-1 game in the fixture; ok should be false")
	}
	if _, _, ok := opponentInWeek(games, "DEN", 5); ok {
		t.Fatalf("DEN has no week-5 game in the fixture; ok should be false")
	}
	if _, _, ok := opponentInWeek(nil, "DEN", 1); ok {
		t.Fatalf("an empty schedule mirror should never resolve an opponent")
	}
}

// TestOpponentInWeekNormalizesTank01Abbreviations is item 2's own
// regression test (2026-08-31 post-wave audit): a Tank01-sourced "LAR"
// player must resolve against an nflverse-normalized "LA" schedule entry
// — teamHasGame/playerLockAt's own fix, applied here too — so /team,
// /players, and /board render that player's opponent, kickoff, and
// matchup-difficulty chip instead of a bare name with none of the three.
func TestOpponentInWeekNormalizesTank01Abbreviations(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "LA", Away: "SF"}} // nflverse-normalized
	opp, home, ok := opponentInWeek(games, "LAR", 1)       // Tank01-style
	if !ok || opp != "SF" || !home {
		t.Fatalf("LAR week 1 = (%q, home=%v, ok=%v), want (SF, true, true)", opp, home, ok)
	}
	opp, home, ok = opponentInWeek(games, "lar", 1) // case-insensitive too
	if !ok || opp != "SF" || !home {
		t.Fatalf("lar week 1 = (%q, home=%v, ok=%v), want (SF, true, true)", opp, home, ok)
	}
}

func TestOpponentInSlateResolvesHomeAndAway(t *testing.T) {
	games := []BlitzGame{{Home: "LV", Away: "HOU"}}
	opp, home, ok := opponentInSlate(games, "hou")
	if !ok || opp != "LV" || home {
		t.Fatalf("HOU = (%q, home=%v, ok=%v), want (LV, false, true)", opp, home, ok)
	}
}

func fixtureMatchupSource(t *testing.T) MatchupSource {
	t.Helper()
	return func(opponent, position string) (TeamMatchup, bool) {
		switch {
		case opponent == "DEN" && position == "WR":
			return TeamMatchup{Rank: 31, Total: 32, Tier: "favorable", SourceLabel: "2025 season"}, true
		case opponent == "SF" && position == "WR":
			return TeamMatchup{Rank: 3, Total: 32, Tier: "difficult", SourceLabel: "2025 season"}, true
		case opponent == "LV" && position == "DST":
			return TeamMatchup{Rank: 28, Total: 32, Tier: "favorable", SourceLabel: "2025 season"}, true
		default:
			return TeamMatchup{}, false
		}
	}
}

// TestMatchupIndexFieldsOffensivePlayer checks the always-visible chip
// copy for an offensive position with a wired rank: the opponent string,
// the explicit "31st-toughest" phrasing, the favorable tier class, and a
// detail line that names both the position and the season source.
func TestMatchupIndexFieldsOffensivePlayer(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "MIN", Away: "DEN"}}
	idx := matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInWeek(games, nflTeam, 1) },
		source:  fixtureMatchupSource(t),
	}
	player := Player{NFLTeam: "MIN", Position: "WR"}
	fields := idx.fields(player)
	if fields["has_opponent"] != true || fields["opponent"] != "vs DEN" {
		t.Fatalf("opponent fields = %+v, want has_opponent=true opponent=\"vs DEN\"", fields)
	}
	if fields["has_matchup"] != true {
		t.Fatalf("has_matchup = %v, want true", fields["has_matchup"])
	}
	if fields["matchup_chip"] != "31st-toughest" {
		t.Fatalf("matchup_chip = %q, want %q", fields["matchup_chip"], "31st-toughest")
	}
	if fields["matchup_tier"] != "favorable" {
		t.Fatalf("matchup_tier = %q, want favorable", fields["matchup_tier"])
	}
	detail, _ := fields["matchup_detail"].(string)
	if detail == "" || !containsAll(detail, "31st-toughest", "vs WR", "2025 season", "soft") {
		t.Fatalf("matchup_detail = %q, missing an expected substring", detail)
	}
}

// TestMatchupIndexFieldsDST checks a DST row's copy: "offense" phrasing
// rather than "vs POS", since a DST's rank describes the opponent's
// offense, not a defense facing that position.
func TestMatchupIndexFieldsDST(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "LV", Away: "LAC"}}
	idx := matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInWeek(games, nflTeam, 1) },
		source:  fixtureMatchupSource(t),
	}
	player := Player{NFLTeam: "LAC", Position: "DST"}
	fields := idx.fields(player)
	if fields["opponent"] != "@ LV" {
		t.Fatalf("opponent = %q, want \"@ LV\" (LAC is away)", fields["opponent"])
	}
	if fields["matchup_chip"] != "28th-toughest" {
		t.Fatalf("matchup_chip = %q, want %q", fields["matchup_chip"], "28th-toughest")
	}
	detail, _ := fields["matchup_detail"].(string)
	if !containsAll(detail, "28th-toughest", "offense on the schedule") {
		t.Fatalf("matchup_detail = %q, want the offense-facing phrasing", detail)
	}
}

// TestMatchupIndexFieldsSkipsKAndP checks the owner directive verbatim:
// K and P show the opponent but never a rank, even when a source is
// wired and would otherwise resolve one.
func TestMatchupIndexFieldsSkipsKAndP(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "MIN", Away: "DEN"}}
	source := func(opponent, position string) (TeamMatchup, bool) {
		// Would happily answer for any position — proves the skip is a
		// deliberate position gate, not an absent-data accident.
		return TeamMatchup{Rank: 1, Total: 32, Tier: "difficult", SourceLabel: "2025 season"}, true
	}
	idx := matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInWeek(games, nflTeam, 1) },
		source:  source,
	}
	for _, position := range []string{"K", "P"} {
		fields := idx.fields(Player{NFLTeam: "MIN", Position: position})
		if fields["has_opponent"] != true {
			t.Fatalf("%s: has_opponent = %v, want true (opponent still shows)", position, fields["has_opponent"])
		}
		if fields["has_matchup"] != false {
			t.Fatalf("%s: has_matchup = %v, want false (K/P never rank)", position, fields["has_matchup"])
		}
	}
}

// TestMatchupIndexFieldsByeWeekNoOpponent checks the missing-data path: a
// player whose team has no game in the resolved week (a bye) renders no
// opponent and no matchup, never a zero or a guess.
func TestMatchupIndexFieldsByeWeekNoOpponent(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "MIN", Away: "DEN"}}
	idx := matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInWeek(games, nflTeam, 1) },
		source:  fixtureMatchupSource(t),
	}
	fields := idx.fields(Player{NFLTeam: "NE", Position: "WR"})
	if fields["has_opponent"] != false || fields["opponent"] != "" {
		t.Fatalf("bye-week fields = %+v, want has_opponent=false opponent=\"\"", fields)
	}
	if fields["has_matchup"] != false {
		t.Fatalf("has_matchup = %v, want false on a bye", fields["has_matchup"])
	}
}

// TestMatchupIndexFieldsNoSourceStillShowsOpponent checks main.go's
// documented degrade: a nil ranking source (the cache has not computed
// yet) still resolves the opponent; only the rank half is missing.
func TestMatchupIndexFieldsNoSourceStillShowsOpponent(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "MIN", Away: "DEN"}}
	idx := matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInWeek(games, nflTeam, 1) },
		source:  nil,
	}
	fields := idx.fields(Player{NFLTeam: "MIN", Position: "WR"})
	if fields["has_opponent"] != true || fields["opponent"] != "vs DEN" {
		t.Fatalf("opponent fields = %+v, want has_opponent=true opponent=\"vs DEN\"", fields)
	}
	if fields["has_matchup"] != false {
		t.Fatalf("has_matchup = %v, want false with no source wired", fields["has_matchup"])
	}
}

// TestMatchupIndexFieldsRankMissForTeam checks a wired source that
// simply has no ranked sample for this particular team (early season,
// insufficient games) -- ok=false from the source must degrade the same
// honest way a bye week does.
func TestMatchupIndexFieldsRankMissForTeam(t *testing.T) {
	games := []GameInfo{{Week: 1, Home: "MIN", Away: "NYJ"}}
	idx := matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInWeek(games, nflTeam, 1) },
		source:  fixtureMatchupSource(t), // has no NYJ/WR entry
	}
	fields := idx.fields(Player{NFLTeam: "MIN", Position: "WR"})
	if fields["has_opponent"] != true {
		t.Fatalf("has_opponent = %v, want true — the opponent itself is known", fields["has_opponent"])
	}
	if fields["has_matchup"] != false {
		t.Fatalf("has_matchup = %v, want false — no ranked sample for NYJ/WR", fields["has_matchup"])
	}
}

// TestMatchupIndexFieldsZeroValueIsSafe checks that the zero-value
// matchupIndex (used by call sites with no schedule context, like the
// draft pick tape) never panics and renders the same honest empty state
// a resolve failure would.
func TestMatchupIndexFieldsZeroValueIsSafe(t *testing.T) {
	fields := matchupIndex{}.fields(Player{NFLTeam: "MIN", Position: "WR"})
	if fields["has_opponent"] != false || fields["has_matchup"] != false {
		t.Fatalf("zero-value matchupIndex fields = %+v, want every has_* false", fields)
	}
}

// TestMatchupIndexFieldsFallsBackToStaticOpponent checks the demo/
// offline-pool path: when resolve cannot find a live game (no schedule
// mirror wired) but the player already carries a static Opponent string
// (the demo pool's DemoPlayers literal), that string still renders
// rather than being blanked out — but never gets a rank, since there is
// no raw team code to look one up against.
func TestMatchupIndexFieldsFallsBackToStaticOpponent(t *testing.T) {
	idx := matchupIndex{source: fixtureMatchupSource(t)} // resolve is nil: no live schedule
	fields := idx.fields(Player{NFLTeam: "CIN", Position: "WR", Opponent: "vs CLE"})
	if fields["has_opponent"] != true || fields["opponent"] != "vs CLE" {
		t.Fatalf("opponent fields = %+v, want the static demo Opponent preserved", fields)
	}
	if fields["has_matchup"] != false {
		t.Fatalf("has_matchup = %v, want false — no raw team code to rank against", fields["has_matchup"])
	}
}

func TestOrdinalSuffixes(t *testing.T) {
	cases := map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st", 32: "32nd"}
	for n, want := range cases {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", n, got, want)
		}
	}
}

func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

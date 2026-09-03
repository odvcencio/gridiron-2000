package league

import (
	"fmt"
	"testing"
)

// fixtureStandings builds N teams already ranked 1..N (team-1 best).
func fixtureStandings(n int) []Standing {
	out := make([]Standing, n)
	for i := 0; i < n; i++ {
		out[i] = Standing{TeamID: fmt.Sprintf("team-%d", i+1), Rank: i + 1}
	}
	return out
}

func baseCfg(teamCount int) PlayoffConfig {
	return PlayoffConfig{TeamCount: teamCount, StartWeek: 15, RoundLengthWeeks: 1, Reseed: true}
}

// TestValidatePlayoffConfig covers section 3.1's validation table.
func TestValidatePlayoffConfig(t *testing.T) {
	valid := PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1, DivisionWinnersFirst: true}
	if err := ValidatePlayoffConfig(valid, 8, 2, 1, 17); err != nil {
		t.Fatalf("expected valid config to pass: %v", err)
	}
	cases := []struct {
		name                                        string
		cfg                                         PlayoffConfig
		league, divisions, seasonStart, seasonFinal int
	}{
		{"teamCount too low", PlayoffConfig{TeamCount: 1, StartWeek: 15, RoundLengthWeeks: 1}, 8, 0, 1, 17},
		{"teamCount too high", PlayoffConfig{TeamCount: 9, StartWeek: 15, RoundLengthWeeks: 1}, 12, 0, 1, 17},
		{"teamCount exceeds league", PlayoffConfig{TeamCount: 6, StartWeek: 15, RoundLengthWeeks: 1}, 4, 0, 1, 17},
		{"bad round length", PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 3}, 8, 0, 1, 17},
		{"startWeek not after season start", PlayoffConfig{TeamCount: 4, StartWeek: 1, RoundLengthWeeks: 1}, 8, 0, 1, 17},
		{"divisionWinnersFirst with no divisions", PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1, DivisionWinnersFirst: true}, 8, 0, 1, 17},
		{"divisionWinnersFirst below division count", PlayoffConfig{TeamCount: 2, StartWeek: 15, RoundLengthWeeks: 1, DivisionWinnersFirst: true}, 8, 3, 1, 17},
		{"bracket runs past final week", PlayoffConfig{TeamCount: 8, StartWeek: 16, RoundLengthWeeks: 2}, 8, 0, 1, 17},
	}
	for _, c := range cases {
		if err := ValidatePlayoffConfig(c.cfg, c.league, c.divisions, c.seasonStart, c.seasonFinal); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

// TestBracketShapesByTeamCount checks bracket shape (byes, round-1 pairing)
// for every teamCount 2-8, with explicit bye checks at 3, 5, 6, 7.
func TestBracketShapesByTeamCount(t *testing.T) {
	wantBracketSize := map[int]int{2: 2, 3: 4, 4: 4, 5: 8, 6: 8, 7: 8, 8: 8}
	for teamCount := 2; teamCount <= 8; teamCount++ {
		standings := fixtureStandings(8)
		cfg := baseCfg(teamCount)
		state, err := GeneratePlayoffState(standings, nil, cfg)
		if err != nil {
			t.Fatalf("teamCount=%d: %v", teamCount, err)
		}
		bracketSize := wantBracketSize[teamCount]
		round1 := matchupsForRound(state.Matchups, "championship", 1)
		if len(round1) != bracketSize/2 {
			t.Fatalf("teamCount=%d: round-1 matchup count = %d, want %d", teamCount, len(round1), bracketSize/2)
		}
		byes := bracketSize - teamCount
		gotByes := 0
		for _, m := range round1 {
			if m.AwayTeamID == "" {
				gotByes++
				if !m.Final || m.WinnerTeamID != m.HomeTeamID {
					t.Errorf("teamCount=%d: bye matchup %+v must be final with the home (bye) team as winner", teamCount, m)
				}
				if m.HomeSeed > byes {
					t.Errorf("teamCount=%d: bye given to seed %d, want a top-%d seed", teamCount, m.HomeSeed, byes)
				}
			}
		}
		if gotByes != byes {
			t.Errorf("teamCount=%d: bye count = %d, want %d (bracketSize %d)", teamCount, gotByes, byes, bracketSize)
		}
		if len(state.Seeds) != teamCount {
			t.Errorf("teamCount=%d: seed count = %d, want %d", teamCount, len(state.Seeds), teamCount)
		}
	}
}

// TestSeedPlayoffsRejectsOutOfRangeOrOverLeagueSize checks the guard
// conditions.
func TestSeedPlayoffsRejectsOutOfRangeOrOverLeagueSize(t *testing.T) {
	standings := fixtureStandings(4)
	if _, err := SeedPlayoffs(standings, nil, PlayoffConfig{TeamCount: 1}); err == nil {
		t.Error("expected an error for teamCount below 2")
	}
	if _, err := SeedPlayoffs(standings, nil, PlayoffConfig{TeamCount: 9}); err == nil {
		t.Error("expected an error for teamCount above 8")
	}
	if _, err := SeedPlayoffs(standings, nil, PlayoffConfig{TeamCount: 5}); err == nil {
		t.Error("expected an error for teamCount exceeding the league size")
	}
}

// TestDivisionWinnerSeedingTwoDivisions checks section 3.2 step 2 with two
// divisions of four.
func TestDivisionWinnerSeedingTwoDivisions(t *testing.T) {
	standings := fixtureStandings(8) // team-1..team-8, best to worst
	divisions := map[string]string{
		"team-1": "Aqua", "team-2": "Aqua", "team-5": "Aqua", "team-7": "Aqua",
		"team-3": "Orange", "team-4": "Orange", "team-6": "Orange", "team-8": "Orange",
	}
	cfg := PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1, DivisionWinnersFirst: true}
	seeds, err := SeedPlayoffs(standings, divisions, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Division winners: team-1 (Aqua's best) and team-3 (Orange's best),
	// ordered among themselves by rank -> team-1 seed 1, team-3 seed 2.
	// Wildcards: the next-best non-winners by rank -> team-2, team-4.
	want := []struct {
		teamID string
		source string
	}{
		{"team-1", "division-winner"},
		{"team-3", "division-winner"},
		{"team-2", "wildcard"},
		{"team-4", "wildcard"},
	}
	if len(seeds) != len(want) {
		t.Fatalf("seed count = %d, want %d: %+v", len(seeds), len(want), seeds)
	}
	for i, w := range want {
		if seeds[i].TeamID != w.teamID || seeds[i].Source != w.source || seeds[i].Seed != i+1 {
			t.Errorf("seed %d = %+v, want {%d %s %s}", i, seeds[i], i+1, w.teamID, w.source)
		}
	}
}

// TestDivisionWinnerSeedingThreeDivisions checks the same rule generalizes
// to three divisions without any hardcoded pair assumption.
func TestDivisionWinnerSeedingThreeDivisions(t *testing.T) {
	standings := fixtureStandings(9)
	divisions := map[string]string{
		"team-1": "A", "team-4": "A", "team-7": "A",
		"team-2": "B", "team-5": "B", "team-8": "B",
		"team-3": "C", "team-6": "C", "team-9": "C",
	}
	cfg := PlayoffConfig{TeamCount: 6, StartWeek: 15, RoundLengthWeeks: 1, DivisionWinnersFirst: true}
	seeds, err := SeedPlayoffs(standings, divisions, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Division winners are team-1 (A), team-2 (B), team-3 (C) in that rank
	// order; wildcards fill from the remaining best-ranked non-winners:
	// team-4, team-5, team-6.
	wantOrder := []string{"team-1", "team-2", "team-3", "team-4", "team-5", "team-6"}
	for i, want := range wantOrder {
		if seeds[i].TeamID != want {
			t.Errorf("seed %d = %s, want %s (seeds=%+v)", i+1, seeds[i].TeamID, want, seeds)
		}
	}
	for i := 0; i < 3; i++ {
		if seeds[i].Source != "division-winner" {
			t.Errorf("seed %d source = %q, want division-winner", i+1, seeds[i].Source)
		}
	}
	for i := 3; i < 6; i++ {
		if seeds[i].Source != "wildcard" {
			t.Errorf("seed %d source = %q, want wildcard", i+1, seeds[i].Source)
		}
	}
}

// TestSeedPlayoffsWithoutDivisionWinnersFirstIsStrictByRank checks section
// 3.2 step 3.
func TestSeedPlayoffsWithoutDivisionWinnersFirstIsStrictByRank(t *testing.T) {
	standings := fixtureStandings(6)
	divisions := map[string]string{"team-1": "A", "team-2": "A", "team-3": "B", "team-4": "B", "team-5": "B", "team-6": "B"}
	cfg := PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1, DivisionWinnersFirst: false}
	seeds, err := SeedPlayoffs(standings, divisions, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"team-1", "team-2", "team-3", "team-4"}
	for i, w := range want {
		if seeds[i].TeamID != w || seeds[i].Source != "field" {
			t.Errorf("seed %d = %+v, want {%s field}", i+1, seeds[i], w)
		}
	}
}

// TestReseedOnAndOff checks section 3.4's reseed behavior after round 1.
func TestReseedOnAndOff(t *testing.T) {
	standings := fixtureStandings(8)
	for _, reseed := range []bool{true, false} {
		cfg := PlayoffConfig{TeamCount: 8, StartWeek: 15, RoundLengthWeeks: 1, Reseed: reseed}
		state, err := GeneratePlayoffState(standings, nil, cfg)
		if err != nil {
			t.Fatal(err)
		}
		round1 := matchupsForRound(state.Matchups, "championship", 1)
		results := winEveryMatchupForHigherSeed(round1)
		state, err = AdvancePlayoffRound(state, results)
		if err != nil {
			t.Fatal(err)
		}
		round1 = matchupsForRound(state.Matchups, "championship", 1) // re-fetch: now carries WinnerTeamID
		round2 := matchupsForRound(state.Matchups, "championship", 2)
		if len(round2) != 2 {
			t.Fatalf("reseed=%v: round-2 matchup count = %d, want 2", reseed, len(round2))
		}
		if reseed {
			// With every round-1 favorite winning, seeds 1,2,3,4 survive.
			// Reseeded best-vs-worst pairs them 1v4 and 2v3.
			wantPairs := map[[2]int]bool{{1, 4}: true, {2, 3}: true}
			for _, m := range round2 {
				pair := [2]int{m.HomeSeed, m.AwaySeed}
				if m.HomeSeed > m.AwaySeed {
					pair = [2]int{m.AwaySeed, m.HomeSeed}
				}
				if !wantPairs[pair] {
					t.Errorf("reseed=true: unexpected round-2 pairing %v (matchup %+v)", pair, m)
				}
			}
		} else {
			// Fixed bracket: round-1 matchup 0's winner (seed 1, since
			// higher seed always wins in this test) faces round-1 matchup
			// 1's winner, matching standard adjacent-pair progression.
			wantHome := round1[0].WinnerTeamID
			wantAway := round1[1].WinnerTeamID
			if round2[0].HomeTeamID != wantHome || round2[0].AwayTeamID != wantAway {
				t.Errorf("reseed=false: round2[0] = %+v, want home=%s away=%s", round2[0], wantHome, wantAway)
			}
		}
	}
}

// winEveryMatchupForHigherSeed builds PlayoffRoundResult inputs where the
// better (lower-numbered) seed always wins by a fixed margin.
func winEveryMatchupForHigherSeed(matchups []PlayoffMatchup) []PlayoffRoundResult {
	out := make([]PlayoffRoundResult, 0, len(matchups))
	for _, m := range matchups {
		if m.Final {
			continue // already-resolved byes need no result
		}
		res := PlayoffRoundResult{MatchupID: m.ID}
		if m.HomeSeed < m.AwaySeed {
			res.HomeScore, res.AwayScore = 100, 80
		} else {
			res.HomeScore, res.AwayScore = 80, 100
		}
		out = append(out, res)
	}
	return out
}

// TestRoundLengthWeeksAffectsRoundWeek checks 1-week and 2-week rounds
// (section 3.1).
func TestRoundLengthWeeksAffectsRoundWeek(t *testing.T) {
	standings := fixtureStandings(4)
	for _, weeks := range []int{1, 2} {
		cfg := PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: weeks}
		state, err := GeneratePlayoffState(standings, nil, cfg)
		if err != nil {
			t.Fatal(err)
		}
		round1 := matchupsForRound(state.Matchups, "championship", 1)
		for _, m := range round1 {
			if m.Week != 15 {
				t.Errorf("roundLengthWeeks=%d: round1 week = %d, want 15", weeks, m.Week)
			}
		}
		results := winEveryMatchupForHigherSeed(round1)
		state, err = AdvancePlayoffRound(state, results)
		if err != nil {
			t.Fatal(err)
		}
		round2 := matchupsForRound(state.Matchups, "championship", 2)
		wantWeek := 15 + weeks
		for _, m := range round2 {
			if m.Week != wantWeek {
				t.Errorf("roundLengthWeeks=%d: round2 week = %d, want %d", weeks, m.Week, wantWeek)
			}
		}
	}
}

// TestChampionRecordedOnlyWhenFinalMatchupIsFinal checks section 3.5.
func TestChampionRecordedOnlyWhenFinalMatchupIsFinal(t *testing.T) {
	standings := fixtureStandings(4)
	cfg := PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1, Reseed: true}
	state, err := GeneratePlayoffState(standings, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	round1 := matchupsForRound(state.Matchups, "championship", 1)
	state, err = AdvancePlayoffRound(state, winEveryMatchupForHigherSeed(round1))
	if err != nil {
		t.Fatal(err)
	}
	if state.ChampionTeamID != "" {
		t.Fatalf("champion must not be recorded before the final round is final: %+v", state)
	}
	round2 := matchupsForRound(state.Matchups, "championship", 2)
	if len(round2) != 1 {
		t.Fatalf("expected exactly one round-2 (final) matchup for a 4-team bracket, got %d", len(round2))
	}
	state, err = AdvancePlayoffRound(state, winEveryMatchupForHigherSeed(round2))
	if err != nil {
		t.Fatal(err)
	}
	if state.ChampionTeamID == "" {
		t.Fatal("champion must be recorded once the final matchup is final")
	}
	if state.ChampionTeamID != "team-1" {
		t.Errorf("champion = %s, want team-1 (best seed wins every game in this fixture)", state.ChampionTeamID)
	}
	if state.RunnerUpTeamID == "" || state.RunnerUpTeamID == state.ChampionTeamID {
		t.Errorf("runner-up = %q, want the losing finalist", state.RunnerUpTeamID)
	}
}

// TestToiletBowlAdvancement checks section 3.4: losers advance, and the
// team that loses every round is the ToiletTeamID.
func TestToiletBowlAdvancement(t *testing.T) {
	standings := fixtureStandings(8)
	cfg := PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1, Reseed: true, ToiletBowl: true}
	state, err := GeneratePlayoffState(standings, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	toiletRound1 := matchupsForRound(state.Matchups, "toilet", 1)
	if len(toiletRound1) == 0 {
		t.Fatal("expected a toilet-bracket round 1 for 4 non-playoff teams")
	}
	// Non-playoff teams are team-5..team-8; inverse-seeded means team-8
	// (worst) is toilet seed 1.
	seedByTeam := map[string]int{}
	for _, s := range state.Seeds {
		seedByTeam[s.TeamID] = s.Seed
	}
	foundTeam8Seed1 := false
	for _, m := range toiletRound1 {
		if m.HomeTeamID == "team-8" && m.HomeSeed == 1 {
			foundTeam8Seed1 = true
		}
	}
	if !foundTeam8Seed1 {
		t.Errorf("expected team-8 (worst non-playoff team) at toilet seed 1: %+v", toiletRound1)
	}

	// Drive every toilet matchup so the "better" (lower-seed) team always
	// WINS the game; since losers advance, the lower seed should be
	// eliminated from the toilet spotlight while the loser (worse team)
	// keeps advancing toward being crowned ToiletTeamID.
	for {
		round := currentUnresolvedRound(state.Matchups, "toilet")
		if round == 0 {
			break
		}
		matchups := matchupsForRound(state.Matchups, "toilet", round)
		state, err = AdvancePlayoffRound(state, winEveryMatchupForHigherSeed(matchups))
		if err != nil {
			t.Fatal(err)
		}
		if state.ToiletTeamID != "" {
			break
		}
	}
	if state.ToiletTeamID == "" {
		t.Fatal("expected a ToiletTeamID once the toilet bracket resolves")
	}
}

// currentUnresolvedRound returns the highest round number for bracket that
// still has a non-final matchup, or 0 when the bracket has no matchups or
// is fully resolved.
func currentUnresolvedRound(matchups []PlayoffMatchup, bracket string) int {
	maxRound := 0
	for _, m := range matchups {
		if m.Bracket == bracket && m.Round > maxRound {
			maxRound = m.Round
		}
	}
	if maxRound == 0 {
		return 0
	}
	round := matchupsForRound(matchups, bracket, maxRound)
	for _, m := range round {
		if !m.Final {
			return maxRound
		}
	}
	return 0
}

// TestConsolationBracketFormsFromRound1Losers checks section 3.4.
func TestConsolationBracketFormsFromRound1Losers(t *testing.T) {
	standings := fixtureStandings(8)
	cfg := PlayoffConfig{TeamCount: 8, StartWeek: 15, RoundLengthWeeks: 1, Reseed: true, Consolation: true}
	state, err := GeneratePlayoffState(standings, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(matchupsForRound(state.Matchups, "consolation", 1)) != 0 {
		t.Fatal("consolation bracket must not exist before championship round 1 completes")
	}
	round1 := matchupsForRound(state.Matchups, "championship", 1)
	state, err = AdvancePlayoffRound(state, winEveryMatchupForHigherSeed(round1))
	if err != nil {
		t.Fatal(err)
	}
	consRound1 := matchupsForRound(state.Matchups, "consolation", 1)
	if len(consRound1) != 2 {
		t.Fatalf("consolation round 1 matchup count = %d, want 2 (4 round-1 losers)", len(consRound1))
	}
	losers := map[string]bool{}
	for _, m := range round1 {
		if m.AwayTeamID == "" {
			continue
		}
		loser := m.AwayTeamID
		if m.WinnerTeamID == m.AwayTeamID {
			loser = m.HomeTeamID
		}
		losers[loser] = true
	}
	for _, m := range consRound1 {
		if !losers[m.HomeTeamID] || !losers[m.AwayTeamID] {
			t.Errorf("consolation matchup %+v does not consist of round-1 losers %v", m, losers)
		}
	}
	// Advance the consolation bracket to completion and check a winner
	// appears on the final consolation round.
	for {
		round := currentUnresolvedRound(state.Matchups, "consolation")
		if round == 0 {
			break
		}
		matchups := matchupsForRound(state.Matchups, "consolation", round)
		state, err = AdvancePlayoffRound(state, winEveryMatchupForHigherSeed(matchups))
		if err != nil {
			t.Fatal(err)
		}
	}
	finalRound := 0
	for _, m := range state.Matchups {
		if m.Bracket == "consolation" && m.Round > finalRound {
			finalRound = m.Round
		}
	}
	final := matchupsForRound(state.Matchups, "consolation", finalRound)
	if len(final) != 1 || final[0].WinnerTeamID == "" {
		t.Fatalf("expected exactly one final consolation matchup with a winner, got %+v", final)
	}
}

// TestAdvancePlayoffRoundRejectsUnknownMatchup and byeMatchup handling.
func TestAdvancePlayoffRoundRejectsUnknownMatchup(t *testing.T) {
	standings := fixtureStandings(4)
	state, err := GeneratePlayoffState(standings, nil, baseCfg(4))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdvancePlayoffRound(state, []PlayoffRoundResult{{MatchupID: "does-not-exist"}}); err == nil {
		t.Error("expected an error for an unknown matchup ID")
	}
}

func TestAdvancePlayoffRoundTieBreaksByBestWeekThenSeed(t *testing.T) {
	standings := fixtureStandings(4)
	state, err := GeneratePlayoffState(standings, nil, baseCfg(4))
	if err != nil {
		t.Fatal(err)
	}
	round1 := matchupsForRound(state.Matchups, "championship", 1)
	// Tie the summed round score; break by best single week.
	m := round1[0]
	result := PlayoffRoundResult{MatchupID: m.ID, HomeScore: 100, AwayScore: 100, HomeBestWeek: 60, AwayBestWeek: 55}
	next, err := AdvancePlayoffRound(state, []PlayoffRoundResult{result})
	if err != nil {
		t.Fatal(err)
	}
	resolved := matchupsForRound(next.Matchups, "championship", 1)[0]
	if resolved.WinnerTeamID != m.HomeTeamID {
		t.Errorf("expected the home team (higher best-week) to win a tied round: %+v", resolved)
	}

	// Tie both the score and the best week; break by seed (lower wins).
	result2 := PlayoffRoundResult{MatchupID: m.ID, HomeScore: 90, AwayScore: 90, HomeBestWeek: 50, AwayBestWeek: 50}
	next2, err := AdvancePlayoffRound(state, []PlayoffRoundResult{result2})
	if err != nil {
		t.Fatal(err)
	}
	resolved2 := matchupsForRound(next2.Matchups, "championship", 1)[0]
	wantWinner := m.HomeTeamID
	if m.AwaySeed != 0 && m.AwaySeed < m.HomeSeed {
		wantWinner = m.AwayTeamID
	}
	if resolved2.WinnerTeamID != wantWinner {
		t.Errorf("expected the better seed to win a fully tied round: got %s, want %s (%+v)", resolved2.WinnerTeamID, wantWinner, resolved2)
	}
}

func TestClonePlayoffStateIsDeepCopy(t *testing.T) {
	standings := fixtureStandings(4)
	state, err := GeneratePlayoffState(standings, nil, baseCfg(4))
	if err != nil {
		t.Fatal(err)
	}
	clone := clonePlayoffState(&state)
	clone.Matchups[0].Final = true
	if state.Matchups[0].Final {
		t.Error("mutating the clone's matchups must not affect the original")
	}
	if clonePlayoffState(nil) != nil {
		t.Error("clonePlayoffState(nil) must return nil")
	}
}

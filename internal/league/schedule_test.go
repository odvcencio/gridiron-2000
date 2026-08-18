package league

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
)

func genTeamIDs(n int) []string {
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("team-%d", i+1)
	}
	return ids
}

// referenceDivisions splits the reference 8-team layout into Aqua (the
// first half) and Orange (the second half), matching defaultTeams().
func referenceDivisions(ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	for i, id := range ids {
		if i < len(ids)/2 {
			out[id] = "Aqua"
		} else {
			out[id] = "Orange"
		}
	}
	return out
}

// TestScheduleProperties is the WP1 required property suite: every N in
// 4..14, every week count in 1..17, 32 seeds. Section 8.
func TestScheduleProperties(t *testing.T) {
	for n := 4; n <= 14; n++ {
		teamIDs := genTeamIDs(n)
		for weeks := 1; weeks <= 17; weeks++ {
			for seed := int64(0); seed < 32; seed++ {
				sched, err := GenerateSchedule(ScheduleParams{
					Season: 2026, TeamIDs: teamIDs, StartWeek: 1, Weeks: weeks, Seed: seed,
				})
				if err != nil {
					t.Fatalf("N=%d weeks=%d seed=%d: unexpected error: %v", n, weeks, seed, err)
				}
				checkEveryTeamOncePerWeek(t, sched, teamIDs, n, weeks, seed)
				checkNoSelfPairAndNoByeLeak(t, sched, n, weeks, seed)
				checkFullCyclePairsOnce(t, sched, n, weeks, seed)
				checkByeFairness(t, sched, teamIDs, n, weeks, seed)
				checkHomeAwayAlternates(t, sched, n, weeks, seed)
			}
		}
	}
}

func checkEveryTeamOncePerWeek(t *testing.T, sched SeasonSchedule, teamIDs []string, n, weeks int, seed int64) {
	t.Helper()
	for _, wk := range sched.Weeks {
		seen := map[string]int{}
		for _, m := range wk.Matchups {
			seen[m.HomeTeamID]++
			seen[m.AwayTeamID]++
		}
		if wk.ByeTeamID != "" {
			seen[wk.ByeTeamID]++
		}
		if len(seen) != n {
			t.Fatalf("N=%d weeks=%d seed=%d week=%d: %d distinct teams appeared, want %d (%v)",
				n, weeks, seed, wk.Week, len(seen), n, seen)
		}
		for _, id := range teamIDs {
			if seen[id] != 1 {
				t.Fatalf("N=%d weeks=%d seed=%d week=%d: team %s appeared %d times, want 1",
					n, weeks, seed, wk.Week, id, seen[id])
			}
		}
	}
}

func checkNoSelfPairAndNoByeLeak(t *testing.T, sched SeasonSchedule, n, weeks int, seed int64) {
	t.Helper()
	for _, wk := range sched.Weeks {
		for _, m := range wk.Matchups {
			if m.HomeTeamID == m.AwayTeamID {
				t.Fatalf("N=%d weeks=%d seed=%d week=%d: self-pairing %s vs %s", n, weeks, seed, wk.Week, m.HomeTeamID, m.AwayTeamID)
			}
			if m.HomeTeamID == byeSentinel || m.AwayTeamID == byeSentinel {
				t.Fatalf("N=%d weeks=%d seed=%d week=%d: BYE leaked into a matchup: %+v", n, weeks, seed, wk.Week, m)
			}
		}
	}
}

// checkFullCyclePairsOnce groups weeks into cycles of numRounds weeks and
// verifies every pair meets exactly once inside each full cycle, and that
// a trailing partial cycle repeats no pairing inside itself.
func checkFullCyclePairsOnce(t *testing.T, sched SeasonSchedule, n, weeks int, seed int64) {
	t.Helper()
	numRounds := n - 1
	if n%2 == 1 {
		numRounds = n // BYE-padded ring size is n+1, cycle length (n+1)-1 = n
	}
	for start := 0; start < len(sched.Weeks); start += numRounds {
		end := start + numRounds
		if end > len(sched.Weeks) {
			end = len(sched.Weeks)
		}
		seenPairs := map[string]bool{}
		for _, wk := range sched.Weeks[start:end] {
			for _, m := range wk.Matchups {
				key := pairKey(m.HomeTeamID, m.AwayTeamID)
				if seenPairs[key] {
					t.Fatalf("N=%d weeks=%d seed=%d: pair %s repeated inside cycle weeks [%d,%d)", n, weeks, seed, key, start, end)
				}
				seenPairs[key] = true
			}
		}
	}
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func checkByeFairness(t *testing.T, sched SeasonSchedule, teamIDs []string, n, weeks int, seed int64) {
	t.Helper()
	if n%2 == 0 {
		for _, wk := range sched.Weeks {
			if wk.ByeTeamID != "" {
				t.Fatalf("N=%d weeks=%d seed=%d: even team count produced a bye in week %d", n, weeks, seed, wk.Week)
			}
		}
		return
	}
	counts := make(map[string]int, len(teamIDs))
	for _, id := range teamIDs {
		counts[id] = 0
	}
	for _, wk := range sched.Weeks {
		if wk.ByeTeamID != "" {
			counts[wk.ByeTeamID]++
		}
	}
	minC, maxC := -1, -1
	for _, c := range counts {
		if minC == -1 || c < minC {
			minC = c
		}
		if maxC == -1 || c > maxC {
			maxC = c
		}
	}
	if maxC-minC > 1 {
		t.Fatalf("N=%d weeks=%d seed=%d: bye counts differ by more than 1: %v", n, weeks, seed, counts)
	}
}

func checkHomeAwayAlternates(t *testing.T, sched SeasonSchedule, n, weeks int, seed int64) {
	t.Helper()
	lastHome := map[string]string{} // pairKey -> last home team ID seen
	for _, wk := range sched.Weeks {
		for _, m := range wk.Matchups {
			key := pairKey(m.HomeTeamID, m.AwayTeamID)
			if prev, ok := lastHome[key]; ok && prev == m.HomeTeamID {
				t.Fatalf("N=%d weeks=%d seed=%d week=%d: repeat meeting %s did not alternate venue", n, weeks, seed, wk.Week, key)
			}
			lastHome[key] = m.HomeTeamID
		}
	}
}

// TestScheduleDeterminism checks that identical params yield byte-identical
// JSON, across a spread of team counts, week counts, and seeds.
func TestScheduleDeterminism(t *testing.T) {
	cases := []struct {
		n, weeks int
		seed     int64
	}{
		{8, 14, 42}, {5, 10, 7}, {14, 17, 0}, {4, 1, 99}, {9, 13, 12345},
	}
	for _, c := range cases {
		teamIDs := genTeamIDs(c.n)
		params := ScheduleParams{Season: 2026, TeamIDs: teamIDs, Divisions: referenceDivisions(teamIDs), StartWeek: 1, Weeks: c.weeks, Seed: c.seed}
		first, err := GenerateSchedule(params)
		if err != nil {
			t.Fatalf("n=%d weeks=%d seed=%d: %v", c.n, c.weeks, c.seed, err)
		}
		second, err := GenerateSchedule(params)
		if err != nil {
			t.Fatalf("n=%d weeks=%d seed=%d: %v", c.n, c.weeks, c.seed, err)
		}
		firstJSON, _ := json.Marshal(first)
		secondJSON, _ := json.Marshal(second)
		if string(firstJSON) != string(secondJSON) {
			t.Fatalf("n=%d weeks=%d seed=%d: schedules are not byte-identical", c.n, c.weeks, c.seed)
		}
	}
}

// TestScheduleDivisionalWeighting exercises section 2.2 step 4 against the
// reference 8-team, 2-division layout. It uses 10 regular-season weeks
// rather than the reference league's literal 14: with 8 teams the cycle
// length is 7 rounds, and 14 = 2*7 divides evenly, leaving no partial
// cycle at all to reorder. 10 weeks (10 mod 7 = 3) exercises the actual
// selection path the spec describes. See the final report for this
// documented deviation from the literal "14 weeks" example in section 8.
func TestScheduleDivisionalWeighting(t *testing.T) {
	teamIDs := genTeamIDs(8)
	divisions := referenceDivisions(teamIDs)
	const weeks = 10
	const numRounds = 7 // n-1 for 8 (even) teams
	const seed = 42
	const remainder = weeks % numRounds
	if remainder == 0 {
		t.Fatalf("test setup error: expected a nonzero remainder for weeks=%d, numRounds=%d", weeks, numRounds)
	}

	// Reconstruct the same shuffled ring and round set the generator
	// itself computes for this seed, so the test can inspect exactly
	// which round indices weekRoundSequence selects for the tail weeks.
	ring := append([]string(nil), teamIDs...)
	rng := rand.New(rand.NewPCG(seed, 0))
	rng.Shuffle(len(ring), func(i, j int) { ring[i], ring[j] = ring[j], ring[i] })
	rounds := circleMethodRounds(ring)
	counts := make([]int, numRounds)
	for i, r := range rounds {
		counts[i] = intraDivisionCount(r, divisions)
	}

	seq := weekRoundSequence(weeks, numRounds, rounds, divisions)
	tailIdx := seq[weeks-remainder:]
	selected := map[int]bool{}
	for _, idx := range tailIdx {
		selected[idx] = true
	}
	if len(selected) != remainder {
		t.Fatalf("expected %d distinct selected round indices, got %d (%v)", remainder, len(selected), tailIdx)
	}
	minSelectedCount := -1
	for idx := range selected {
		if minSelectedCount == -1 || counts[idx] < minSelectedCount {
			minSelectedCount = counts[idx]
		}
	}
	maxNonSelectedCount := 0
	for idx := 0; idx < numRounds; idx++ {
		if selected[idx] {
			continue
		}
		if counts[idx] > maxNonSelectedCount {
			maxNonSelectedCount = counts[idx]
		}
	}
	if minSelectedCount < maxNonSelectedCount {
		t.Fatalf("selected round counts (min %d) must be >= every non-selected round's count (max %d); counts=%v selected=%v",
			minSelectedCount, maxNonSelectedCount, counts, tailIdx)
	}

	// Cross-check against the public API: the tail weeks' actual
	// intra-division matchup counts must match the selected rounds'
	// precomputed counts.
	sched, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: teamIDs, Divisions: divisions, StartWeek: 1, Weeks: weeks, Seed: seed,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, wk := range sched.Weeks[weeks-remainder:] {
		got := 0
		for _, m := range wk.Matchups {
			if divisions[m.HomeTeamID] != "" && divisions[m.HomeTeamID] == divisions[m.AwayTeamID] {
				got++
			}
		}
		want := counts[tailIdx[i]]
		if got != want {
			t.Fatalf("tail week %d: intra-division matchups = %d, want %d (round %d)", wk.Week, got, want, tailIdx[i])
		}
	}
}

func TestGenerateScheduleValidation(t *testing.T) {
	base := ScheduleParams{Season: 2026, TeamIDs: genTeamIDs(8), StartWeek: 1, Weeks: 14, Seed: 1}

	tooFew := base
	tooFew.TeamIDs = genTeamIDs(3)
	if _, err := GenerateSchedule(tooFew); err == nil {
		t.Error("expected an error for 3 teams")
	}

	tooMany := base
	tooMany.TeamIDs = genTeamIDs(15)
	if _, err := GenerateSchedule(tooMany); err == nil {
		t.Error("expected an error for 15 teams")
	}

	dup := base
	dup.TeamIDs = []string{"team-1", "team-1", "team-3", "team-4"}
	if _, err := GenerateSchedule(dup); err == nil {
		t.Error("expected an error for duplicate team IDs")
	}

	noWeeks := base
	noWeeks.Weeks = 0
	if _, err := GenerateSchedule(noWeeks); err == nil {
		t.Error("expected an error for zero weeks")
	}

	overrun := base
	overrun.StartWeek = 10
	overrun.Weeks = 10
	if _, err := GenerateSchedule(overrun); err == nil {
		t.Error("expected an error when startWeek+weeks-1 exceeds the final NFL week")
	}

	if _, err := GenerateSchedule(base); err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
}

func TestGenerateScheduleMatchupIDFormat(t *testing.T) {
	sched, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: genTeamIDs(8), StartWeek: 1, Weeks: 1, Seed: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(sched.Weeks) != 1 || len(sched.Weeks[0].Matchups) != 4 {
		t.Fatalf("expected 1 week of 4 matchups, got %+v", sched.Weeks)
	}
	for _, m := range sched.Weeks[0].Matchups {
		want := fmt.Sprintf("2026-w01-%s-%s", m.HomeTeamID, m.AwayTeamID)
		if m.ID != want {
			t.Errorf("matchup ID = %q, want %q", m.ID, want)
		}
	}
}

func TestGenerateScheduleOddCountHasByeEveryWeek(t *testing.T) {
	sched, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: genTeamIDs(9), StartWeek: 1, Weeks: 8, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, wk := range sched.Weeks {
		if wk.ByeTeamID == "" {
			t.Fatalf("week %d: expected a bye team for a 9-team league", wk.Week)
		}
		if len(wk.Matchups) != 4 {
			t.Fatalf("week %d: expected 4 matchups, got %d", wk.Week, len(wk.Matchups))
		}
	}
}

// TestGenerateScheduleEveryCountFourToFourteen proves every team count a
// seat trim could plausibly land the SK instance on (config's engine floor
// through the engine max, minTeams/maxTeams — config.go) produces a valid
// schedule: an odd count carries exactly one bye per week and every team,
// bye included, is scheduled exactly once a week (SK unclaimed-seat spec:
// "odd resulting counts are expected and fine"). An even count carries no
// bye at all.
func TestGenerateScheduleEveryCountFourToFourteen(t *testing.T) {
	for n := minTeams; n <= maxTeams; n++ {
		n := n
		t.Run(fmt.Sprintf("teams-%d", n), func(t *testing.T) {
			ids := genTeamIDs(n)
			sched, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: ids, StartWeek: 1, Weeks: 6, Seed: 11})
			if err != nil {
				t.Fatalf("GenerateSchedule(%d teams) failed: %v", n, err)
			}
			wantBye := n%2 == 1
			for _, wk := range sched.Weeks {
				if wantBye && wk.ByeTeamID == "" {
					t.Fatalf("%d teams, week %d: expected a bye team, got none", n, wk.Week)
				}
				if !wantBye && wk.ByeTeamID != "" {
					t.Fatalf("%d teams, week %d: expected no bye team, got %q", n, wk.Week, wk.ByeTeamID)
				}
				seen := map[string]bool{}
				if wk.ByeTeamID != "" {
					seen[wk.ByeTeamID] = true
				}
				for _, m := range wk.Matchups {
					if seen[m.HomeTeamID] || seen[m.AwayTeamID] {
						t.Fatalf("%d teams, week %d: team scheduled twice in %+v", n, wk.Week, wk)
					}
					seen[m.HomeTeamID] = true
					seen[m.AwayTeamID] = true
				}
				if len(seen) != n {
					t.Fatalf("%d teams, week %d: %d of %d teams scheduled (bye included)", n, wk.Week, len(seen), n)
				}
			}
		})
	}
}

func TestGenerateScheduleGeneratedAtStaysZero(t *testing.T) {
	sched, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: genTeamIDs(8), StartWeek: 1, Weeks: 1, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !sched.GeneratedAt.IsZero() {
		t.Fatalf("GenerateSchedule must not touch the clock; GeneratedAt = %v", sched.GeneratedAt)
	}
}

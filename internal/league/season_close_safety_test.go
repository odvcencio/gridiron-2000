package league

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// This file is the SAFETY proof the audit's PR 1(a) item demands before
// shipping the hundredth-point rounding fix (scoring_rounding_test.go):
// the season is live, so a change to how scores compare must never change
// an already-decided result.
//
// Owner decision (least intervention, live season, 2026-09-23): rather
// than trust an architectural argument about when ComputeStandings might
// be safe to round, the fix does not touch standings.go or recap.go at
// all — TestAlreadyFinalWeeksCompareByteIdenticallyBeforeAndAfter below
// requires ComputeStandings' output to be byte-for-byte identical, for
// any already-final schedule, to preFixComputeStandings (a verbatim copy
// of ComputeStandings as it existed at git HEAD, before this PR). The
// canonical hundredth rounding (audit item 7) lives once, upstream, at
// the single place a fantasy point total is computed — scorePlayerStats /
// scorePlayers — which only ever affects a score computed from here on,
// never an already-stored one.
//
// The rounding this fix does add, at the scorer, still needs its own
// safety property: TestPerMatchupRoundingNeverReversesAWinner and
// TestRoundPointsIsMonotonic below prove that rounding a freshly computed
// score can only ever collapse a decision into a genuine tie (when the
// two raw totals are already within one cent of each other), never
// reverse who is ahead.
//
// rawScorePlayers below is a verbatim, deliberately un-rounded copy of
// the scorer as it existed before this fix (git HEAD, scorer.go's
// scorePlayerStats/scorePlayers) — i.e., exactly what produced every
// score already sitting in production's database. preFixComputeStandings
// is the equivalent verbatim copy of the original ComputeStandings
// accumulation loop (git HEAD, standings.go): raw > and ==, no rounding.
func TestPerMatchupRoundingNeverReversesAWinner(t *testing.T) {
	const trials = 20000
	values := breakdownDefaultValues()
	rng := rand.New(rand.NewSource(20260923))

	collapses := 0
	for trial := 0; trial < trials; trial++ {
		home := rawScorePlayers(randomRealisticRoster(rng, 5, 9), values)
		away := rawScorePlayers(randomRealisticRoster(rng, 5, 9), values)
		rawDiff := home - away
		roundedHome, roundedAway := roundPoints(home), roundPoints(away)
		roundedDiff := roundedHome - roundedAway

		switch {
		case rawDiff > 0 && roundedDiff < 0, rawDiff < 0 && roundedDiff > 0:
			t.Fatalf("trial %d: REVERSAL — raw home-away=%.20f (sign %+d) but rounded=%.20f (sign %+d); home=%.20f away=%.20f",
				trial, rawDiff, sign(rawDiff), roundedDiff, sign(roundedDiff), home, away)
		case rawDiff != 0 && roundedDiff == 0:
			collapses++
			if d := rawDiff; d < -0.01 || d > 0.01 {
				t.Fatalf("trial %d: rounding collapsed a decision into a tie whose raw scores were %.20f apart — more than one cent, not a genuine hundredth tie; home=%.20f away=%.20f",
					trial, d, home, away)
			}
		}
	}
	t.Logf("%d/%d trials: rounding collapsed a decisive-but-raw-float-noise-only result into a genuine tie (every other trial's decision, if any, kept its winner)", collapses, trials)
}

func sign(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

// TestRoundPointsIsMonotonic is the underlying math fact
// TestPerMatchupRoundingNeverReversesAWinner relies on: a <= b implies
// roundPoints(a) <= roundPoints(b), always. math.Round never reverses
// order, and dividing two ordered float64s by the same positive constant
// preserves their order (IEEE 754 division is correctly rounded).
func TestRoundPointsIsMonotonic(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 50000; i++ {
		a := (rng.Float64() - 0.5) * 4000
		b := (rng.Float64() - 0.5) * 4000
		if a > b {
			a, b = b, a
		}
		if roundPoints(a) > roundPoints(b) {
			t.Fatalf("monotonicity violated: a=%.20f b=%.20f round(a)=%.20f round(b)=%.20f", a, b, roundPoints(a), roundPoints(b))
		}
	}
}

// TestAlreadyFinalWeeksCompareByteIdenticallyBeforeAndAfter is the owner's
// explicit safety requirement (2026-09-23, least-intervention decision):
// for any already-final schedule, ComputeStandings must produce output
// that is byte-for-byte identical (reflect.DeepEqual — every float
// included) to preFixComputeStandings, the verbatim pre-fix
// implementation. Not "no reversal", not "close enough" — identical. That
// is only true because this fix does not touch standings.go's comparison
// logic at all; it stays a regression lock against ever adding rounding
// there.
//
// Trials use realistic full seasons (real default scoring rules, integer
// stat counts, a real schedule shape, every matchup already Final) built
// through rawScorePlayers — i.e., scored exactly as production's own
// pre-fix scorer already scored every already-closed week sitting in the
// database today.
func TestAlreadyFinalWeeksCompareByteIdenticallyBeforeAndAfter(t *testing.T) {
	const (
		trials        = 500
		teamsPerTrial = 8
		weeksPerTrial = 14
	)
	values := breakdownDefaultValues()
	rng := rand.New(rand.NewSource(20260923))

	teamIDs := make([]string, teamsPerTrial)
	for i := range teamIDs {
		teamIDs[i] = teamIDName(i)
	}

	for trial := 0; trial < trials; trial++ {
		sch := SeasonSchedule{Season: 2026}
		for week := 1; week <= weeksPerTrial; week++ {
			order := rng.Perm(teamsPerTrial)
			var matchups []LeagueMatchup
			for i := 0; i+1 < teamsPerTrial; i += 2 {
				home, away := teamIDs[order[i]], teamIDs[order[i+1]]
				homeScore := rawScorePlayers(randomRealisticRoster(rng, 5, 9), values)
				awayScore := rawScorePlayers(randomRealisticRoster(rng, 5, 9), values)
				matchups = append(matchups, LeagueMatchup{
					HomeTeamID: home, AwayTeamID: away,
					HomeScore: homeScore, AwayScore: awayScore,
					Final: true,
				})
			}
			sch.Weeks = append(sch.Weeks, ScheduleWeek{Week: week, Matchups: matchups})
		}

		oldOut := preFixComputeStandings(sch, teamIDs, TiebreakInputs{})
		newOut := ComputeStandings(sch, teamIDs, TiebreakInputs{})
		if !reflect.DeepEqual(oldOut, newOut) {
			t.Fatalf("trial %d: ComputeStandings diverged from the pre-fix implementation for an already-final schedule:\nold=%+v\nnew=%+v", trial, oldOut, newOut)
		}
	}
}

// teamIDName is a small stable name generator, kept out of a literal slice
// so teamsPerTrial can change without a matching literal edit.
func teamIDName(i int) string {
	return "safety-team-" + string(rune('A'+i))
}

// rawScorePlayers is a verbatim, deliberately un-rounded reimplementation
// of the pre-fix scorer (git HEAD's scorer.go: scorePlayerStats summed by
// scorePlayers, before this PR's roundPoints calls) — see this file's
// doc comment. players is a slice of rule-keyed stat maps (one per
// starter); this walks them in slice order and sums with +=, exactly as
// the original scorePlayers did, so it reproduces the original's
// summation-order float64 noise faithfully.
func rawScorePlayers(playerStats []map[string]float64, values map[string]float64) float64 {
	total := 0.0
	for _, stats := range playerStats {
		points := 0.0
		for ruleKey, statValue := range stats {
			points += statValue * scoringPoints(values, ruleKey)
		}
		total += points
	}
	return total
}

// randomRealisticRoster returns between min and max starters' worth of
// integer stat lines, each drawing a small, plausible subset of the
// league's real scoring-rule keys with small non-negative integer counts
// — mirroring the shape a real starting lineup's stat lines take (whole
// receptions, whole yards, 0-3 touchdowns, and so on).
func randomRealisticRoster(rng *rand.Rand, min, max int) []map[string]float64 {
	n := min + rng.Intn(max-min+1)
	ruleKeys := []struct {
		key string
		max int
	}{
		{"passYards", 350}, {"passTD", 4}, {"passInt", 3},
		{"rushYards", 140}, {"rushTD", 3},
		{"reception", 12}, {"recYards", 160}, {"recTD", 3},
		{"fumbleLost", 1}, {"twoPt", 1}, {"returnTD", 1},
		{"fgMade", 4}, {"fgMissed", 2}, {"xpMade", 5},
		{"dstSack", 5}, {"dstInt", 3}, {"dstTD", 1},
	}
	out := make([]map[string]float64, n)
	for i := range out {
		stats := map[string]float64{}
		// Two to four categories per player, matching how few stat
		// categories one real player's box score line actually fills.
		picks := 2 + rng.Intn(3)
		for p := 0; p < picks; p++ {
			rule := ruleKeys[rng.Intn(len(ruleKeys))]
			stats[rule.key] = float64(rng.Intn(rule.max + 1))
		}
		out[i] = stats
	}
	return out
}

// preFixComputeStandings is a verbatim reimplementation of
// ComputeStandings as it existed at git HEAD, before this PR's roundPoints
// calls: raw > and == on m.HomeScore/m.AwayScore, and a raw += into
// PointsFor/PointsAgainst. It reuses every other unchanged piece of the
// real tiebreak machinery (resolveTiebreakChain, computeStreak,
// DefaultTiebreakChain) so the only difference from ComputeStandings is
// exactly the rounding this PR adds — never a second, independently
// drifting implementation of the tiebreak logic itself.
func preFixComputeStandings(sch SeasonSchedule, teamIDs []string, tb TiebreakInputs) []Standing {
	standings := make(map[string]*Standing, len(teamIDs))
	for _, id := range teamIDs {
		standings[id] = &Standing{TeamID: id}
	}

	weeks := append([]ScheduleWeek(nil), sch.Weeks...)
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].Week < weeks[j].Week })

	results := make(map[string][]string, len(teamIDs))
	for _, wk := range weeks {
		for _, m := range wk.Matchups {
			if !m.Final {
				continue
			}
			home, homeOK := standings[m.HomeTeamID]
			away, awayOK := standings[m.AwayTeamID]
			if !homeOK || !awayOK {
				continue
			}
			home.PointsFor += m.HomeScore
			home.PointsAgainst += m.AwayScore
			away.PointsFor += m.AwayScore
			away.PointsAgainst += m.HomeScore
			switch {
			case m.HomeScore > m.AwayScore:
				home.Wins++
				away.Losses++
				results[m.HomeTeamID] = append(results[m.HomeTeamID], "W")
				results[m.AwayTeamID] = append(results[m.AwayTeamID], "L")
			case m.AwayScore > m.HomeScore:
				away.Wins++
				home.Losses++
				results[m.AwayTeamID] = append(results[m.AwayTeamID], "W")
				results[m.HomeTeamID] = append(results[m.HomeTeamID], "L")
			default:
				home.Ties++
				away.Ties++
				results[m.HomeTeamID] = append(results[m.HomeTeamID], "T")
				results[m.AwayTeamID] = append(results[m.AwayTeamID], "T")
			}
		}
	}
	for id, s := range standings {
		s.Streak = computeStreak(results[id])
	}

	chain := tb.Chain
	if len(chain) == 0 {
		chain = DefaultTiebreakChain
	}
	order := resolveTiebreakChain(append([]string(nil), teamIDs...), chain, standings, sch, tb)
	out := make([]Standing, 0, len(order))
	for i, id := range order {
		s := *standings[id]
		s.Rank = i + 1
		out = append(out, s)
	}
	return out
}

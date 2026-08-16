package league

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
)

// Standing is one team's computed regular-season standing. Standings are
// derived, never stored (section 2.5): ComputeStandings is a pure function
// over a SeasonSchedule and a tiebreaker configuration.
type Standing struct {
	TeamID        string
	Wins          int
	Losses        int
	Ties          int
	PointsFor     float64
	PointsAgainst float64
	Streak        string
	Rank          int
	// DecidedBy is the key of the tiebreaker rule that separated this team
	// from the team ranked directly below it, or "" when the record alone
	// decided (or this is the last-ranked team). See TiebreakInputs.
	DecidedBy string
}

// PickemRecord is one team's pick'em tally for tiebreaks (awards-and-
// performance spec, WP-P0 amendment to competition-formats section 3.3).
// Correct and Total both count only games that satisfy the amendment's
// through-week and decided-winner rules; building that tally from stored
// picks is a separate, later concern (pickem.go's BuildPickemRecords, not
// part of this work package) — ComputeStandings only consumes an
// already-built map.
type PickemRecord struct {
	Correct int
	Total   int
}

// TiebreakInputs carries everything the tiebreaker chain needs beyond the
// schedule itself (competition-formats spec section 3.3, amended by the
// awards-and-performance spec's WP-P0 to add the "pickem" rule).
type TiebreakInputs struct {
	// SeasonSeed feeds the deterministic "seeded-draw" fallback.
	SeasonSeed int64
	// Chain is the ordered rule list. An empty Chain uses
	// DefaultTiebreakChain. A non-empty Chain must satisfy
	// ValidateTiebreakChain.
	Chain []string
	// Pickem maps teamID to its pick'em record. nil means the "pickem"
	// rule cannot separate anything and is skipped wherever it appears in
	// the chain (WP-P0: "nil = pickem rule cannot separate"). A team
	// absent from a non-nil map is treated as PickemRecord{0, 0}.
	Pickem map[string]PickemRecord
}

// DefaultTiebreakChain is the chain applied when TiebreakInputs.Chain is
// empty: record, head-to-head, points-for, pickem, seeded-draw (WP-P0
// amendment A1's default; pickem sits after points-for and before the
// seeded-draw fallback).
var DefaultTiebreakChain = []string{"record", "head-to-head", "points-for", "pickem", "seeded-draw"}

// validTiebreakRules is the full known rule set (WP-P0 amendment A1).
var validTiebreakRules = map[string]bool{
	"record": true, "head-to-head": true, "points-for": true, "pickem": true, "seeded-draw": true,
}

// ValidateTiebreakChain enforces amendment A1's chain constraints: record
// first, seeded-draw last, no duplicate or unknown keys, at least those
// two entries.
func ValidateTiebreakChain(chain []string) error {
	if len(chain) < 2 {
		return fmt.Errorf("the tiebreaker chain must start with %q and end with %q", "record", "seeded-draw")
	}
	if chain[0] != "record" {
		return fmt.Errorf("the tiebreaker chain must start with %q", "record")
	}
	if chain[len(chain)-1] != "seeded-draw" {
		return fmt.Errorf("the tiebreaker chain must end with %q", "seeded-draw")
	}
	seen := make(map[string]bool, len(chain))
	for _, key := range chain {
		if !validTiebreakRules[key] {
			return fmt.Errorf("unknown tiebreaker rule %q", key)
		}
		if seen[key] {
			return fmt.Errorf("tiebreaker rule %q appears more than once", key)
		}
		seen[key] = true
	}
	return nil
}

// ComputeStandings derives every team's regular-season standing from final
// matchups in sch, ranked by the tiebreaker chain in tb (section 2.5,
// 3.3). teamIDs sets which teams appear; a team with no final matchups
// still gets a zero-value standing.
func ComputeStandings(sch SeasonSchedule, teamIDs []string, tb TiebreakInputs) []Standing {
	standings := make(map[string]*Standing, len(teamIDs))
	for _, id := range teamIDs {
		standings[id] = &Standing{TeamID: id}
	}

	weeks := append([]ScheduleWeek(nil), sch.Weeks...)
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].Week < weeks[j].Week })

	results := make(map[string][]string, len(teamIDs)) // teamID -> ordered "W"/"L"/"T"
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

func computeStreak(results []string) string {
	if len(results) == 0 {
		return "—"
	}
	last := results[len(results)-1]
	count := 0
	for i := len(results) - 1; i >= 0; i-- {
		if results[i] != last {
			break
		}
		count++
	}
	return last + strconv.Itoa(count)
}

// resolveTiebreakChain orders teams by chain, recursively partitioning
// into tie groups at each rule and recursing into the remaining chain
// within each group. It sets standings[id].DecidedBy for every team whose
// separation from the team ranked directly below it was decided by a rule
// after "record" (section 3.3, WP-P0 amendment A2).
func resolveTiebreakChain(teams []string, chain []string, standings map[string]*Standing, sch SeasonSchedule, tb TiebreakInputs) []string {
	if len(teams) <= 1 || len(chain) == 0 {
		return teams
	}
	rule := chain[0]
	rest := chain[1:]
	groups := groupByTiebreakRule(teams, rule, standings, sch, tb)
	out := make([]string, 0, len(teams))
	for gi, group := range groups {
		resolved := resolveTiebreakChain(group, rest, standings, sch, tb)
		out = append(out, resolved...)
		if gi < len(groups)-1 && rule != "record" && len(resolved) > 0 {
			last := resolved[len(resolved)-1]
			if standings[last].DecidedBy == "" {
				standings[last].DecidedBy = rule
			}
		}
	}
	return out
}

// groupByTiebreakRule partitions teams into ordered tie groups (best
// group first) for one rule. A rule that cannot separate the given teams
// (a skip condition, or ties throughout) returns a single group containing
// every team, unchanged in relative order, so the caller recurses straight
// into the rest of the chain.
func groupByTiebreakRule(teams []string, rule string, standings map[string]*Standing, sch SeasonSchedule, tb TiebreakInputs) [][]string {
	switch rule {
	case "record":
		return groupByDescendingFloat(teams, func(id string) float64 { return winPct(standings[id]) })
	case "head-to-head":
		if len(teams) != 2 {
			return [][]string{teams}
		}
		a, b := teams[0], teams[1]
		pctA, pctB, played := headToHeadPct(sch, a, b)
		if !played || pctA == pctB {
			return [][]string{teams}
		}
		if pctA > pctB {
			return [][]string{{a}, {b}}
		}
		return [][]string{{b}, {a}}
	case "points-for":
		return groupByDescendingFloat(teams, func(id string) float64 { return standings[id].PointsFor })
	case "pickem":
		if tb.Pickem == nil {
			return [][]string{teams}
		}
		return groupByPickem(teams, tb.Pickem)
	case "seeded-draw":
		return groupBySeededDraw(teams, tb.SeasonSeed)
	default:
		return [][]string{teams}
	}
}

func winPct(s *Standing) float64 {
	games := s.Wins + s.Losses + s.Ties
	if games == 0 {
		return 0
	}
	return (float64(s.Wins) + 0.5*float64(s.Ties)) / float64(games)
}

// headToHeadPct computes each team's win percentage in games played only
// between a and b. played is false when they never met.
func headToHeadPct(sch SeasonSchedule, a, b string) (pctA, pctB float64, played bool) {
	var winsA, winsB, ties float64
	games := 0
	for _, wk := range sch.Weeks {
		for _, m := range wk.Matchups {
			if !m.Final {
				continue
			}
			var isPair bool
			switch {
			case m.HomeTeamID == a && m.AwayTeamID == b, m.HomeTeamID == b && m.AwayTeamID == a:
				isPair = true
			}
			if !isPair {
				continue
			}
			games++
			if m.HomeScore == m.AwayScore {
				ties++
				continue
			}
			winner := m.HomeTeamID
			if m.AwayScore > m.HomeScore {
				winner = m.AwayTeamID
			}
			if winner == a {
				winsA++
			} else {
				winsB++
			}
		}
	}
	if games == 0 {
		return 0, 0, false
	}
	pctA = (winsA + 0.5*ties) / float64(games)
	pctB = (winsB + 0.5*ties) / float64(games)
	return pctA, pctB, true
}

// groupByDescendingFloat sorts teams by descending value (stable, so ties
// keep their relative input order) and partitions into runs of equal
// value.
func groupByDescendingFloat(teams []string, value func(string) float64) [][]string {
	ordered := append([]string(nil), teams...)
	sort.SliceStable(ordered, func(i, j int) bool { return value(ordered[i]) > value(ordered[j]) })
	return partitionEqual(ordered, func(a, b string) bool { return value(a) == value(b) })
}

// pickemKey is the WP-P0 amendment A1 rule-4 comparator: participation
// first (Total > 0 beats Total == 0), then higher Correct, then higher
// accuracy.
func pickemKey(id string, records map[string]PickemRecord) (participated int, correct int, accuracy float64) {
	rec := records[id]
	if rec.Total > 0 {
		participated = 1
		accuracy = float64(rec.Correct) / float64(rec.Total)
	}
	return participated, rec.Correct, accuracy
}

func groupByPickem(teams []string, records map[string]PickemRecord) [][]string {
	ordered := append([]string(nil), teams...)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, ci, ai := pickemKey(ordered[i], records)
		pj, cj, aj := pickemKey(ordered[j], records)
		if pi != pj {
			return pi > pj
		}
		if ci != cj {
			return ci > cj
		}
		return ai > aj
	})
	return partitionEqual(ordered, func(a, b string) bool {
		pa, ca, aa := pickemKey(a, records)
		pb, cb, ab := pickemKey(b, records)
		return pa == pb && ca == cb && aa == ab
	})
}

// seededDrawHash hashes seasonSeed|teamID (section 3.3 rule 4 / WP-P0 rule
// 5): "lower value ... wins."
func seededDrawHash(seasonSeed int64, teamID string) [32]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("%d|%s", seasonSeed, teamID)))
}

func groupBySeededDraw(teams []string, seasonSeed int64) [][]string {
	ordered := append([]string(nil), teams...)
	sort.SliceStable(ordered, func(i, j int) bool {
		hi := seededDrawHash(seasonSeed, ordered[i])
		hj := seededDrawHash(seasonSeed, ordered[j])
		return bytes.Compare(hi[:], hj[:]) < 0
	})
	return partitionEqual(ordered, func(a, b string) bool {
		ha := seededDrawHash(seasonSeed, a)
		hb := seededDrawHash(seasonSeed, b)
		return ha == hb
	})
}

// partitionEqual groups consecutive elements of an already-sorted slice
// into runs where eq holds pairwise against the run's first element.
func partitionEqual(ordered []string, eq func(a, b string) bool) [][]string {
	if len(ordered) == 0 {
		return nil
	}
	groups := make([][]string, 0, len(ordered))
	start := 0
	for i := 1; i <= len(ordered); i++ {
		if i == len(ordered) || !eq(ordered[start], ordered[i]) {
			groups = append(groups, ordered[start:i])
			start = i
		}
	}
	return groups
}

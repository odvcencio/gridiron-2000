// Package matchup ranks every NFL team's fantasy-matchup difficulty for
// each offensive skill position and for DST, so a projection anywhere in
// the app can show "how tough is this week's opponent" at a glance
// (owner ask: "whenever projections of stats are made, we should see the
// opponent at a glance").
//
// The package is deliberately provider- and scoring-engine-agnostic: it
// consumes WeekRow, a minimal already-scored stat line, rather than
// importing internal/openstats or internal/league directly. The caller
// (main.go's matchup-rank cache) adapts the openstats mirror into
// WeekRow, scoring each row through the league's own scoring engine
// (league.ScoreStatLine) — the same "reuse the scoring math, never fork
// it" discipline the Preseason Blitz pre1 work follows (blitz_pre1.go).
//
// Both rank tables share one convention: Rank 1 is always the toughest
// matchup a fantasy player can draw, and Rank Total is always the
// softest/most generous — for an offensive position that means the
// stingiest defense ranks 1st; for DST that means the most potent
// opposing offense ranks 1st. A chip's color and copy never have to
// special-case which side of the ball a rank describes.
package matchup

import (
	"fmt"
	"sort"
)

// WeekRow is one player's already-scored fantasy output for one NFL week:
// enough to aggregate "how many fantasy points did this position score
// against this defense" (grouped by Opponent) or "how many fantasy
// points did this team's offense score" (grouped by Team). Points must
// already reflect the league's own scoring rules (see league.
// ScoreStatLine) — this package never computes a point value itself.
type WeekRow struct {
	Team     string
	Opponent string
	Position string
	Week     int
	Points   float64
}

// TeamRank is one team's computed matchup difficulty: its position among
// every team with a ranked sample (Total), and a precomputed Tier
// ("difficult" | "neutral" | "favorable") so a caller never has to bucket
// the raw number itself.
type TeamRank struct {
	Rank  int
	Total int
	Tier  string
}

// tierFor buckets rank into thirds of total: the toughest third is
// "difficult", the softest third is "favorable", and the middle is
// "neutral". For the NFL's 32 teams that is a 10/12/10 split — simple,
// deterministic, and symmetric enough that a chip's color always lines
// up with its plain-English reading ("31st-toughest" always lands in the
// softest third).
func tierFor(rank, total int) string {
	if total <= 0 || rank <= 0 {
		return "neutral"
	}
	third := total / 3
	if third < 1 {
		third = 1
	}
	switch {
	case rank <= third:
		return "difficult"
	case rank > total-third:
		return "favorable"
	default:
		return "neutral"
	}
}

// RankDefenseAllowed ranks every team's defense by the average fantasy
// points it allows to position, using only rows whose Position matches
// and whose Opponent is set (a row with no opponent cannot attribute a
// defense). Rank 1 is the stingiest defense (the toughest matchup for
// that position); Rank Total is the most generous. A team with no
// matching rows at all is omitted from the result — never a fabricated
// rank — so a lookup miss is the caller's honest "no rank available"
// signal.
func RankDefenseAllowed(rows []WeekRow, position string) map[string]TeamRank {
	sums := map[string]float64{}
	weeks := map[string]map[int]bool{}
	for _, row := range rows {
		if row.Position != position || row.Opponent == "" {
			continue
		}
		sums[row.Opponent] += row.Points
		if weeks[row.Opponent] == nil {
			weeks[row.Opponent] = map[int]bool{}
		}
		weeks[row.Opponent][row.Week] = true
	}
	return rankTeams(sums, weeks, true)
}

// RankOffenseOutput ranks every team's own offense by the average
// fantasy points it scores, using rows grouped by Team (the caller is
// expected to have already limited rows to the offensive positions it
// wants counted — QB/RB/WR/TE — since this package carries no position
// table of its own). Rank 1 is the most potent offense (the toughest
// matchup for a DST facing it); Rank Total is the weakest (the softest
// DST matchup). A team with no matching rows is omitted.
func RankOffenseOutput(rows []WeekRow) map[string]TeamRank {
	sums := map[string]float64{}
	weeks := map[string]map[int]bool{}
	for _, row := range rows {
		if row.Team == "" {
			continue
		}
		sums[row.Team] += row.Points
		if weeks[row.Team] == nil {
			weeks[row.Team] = map[int]bool{}
		}
		weeks[row.Team][row.Week] = true
	}
	return rankTeams(sums, weeks, false)
}

// rankTeams turns per-team point sums and per-team distinct-week counts
// into ranks: average points per week, sorted ascending when lowTough is
// true (defense-allowed: fewest points allowed is toughest) or
// descending when false (offense-output: most points scored is
// toughest), ties broken by team code for a deterministic order.
func rankTeams(sums map[string]float64, weeks map[string]map[int]bool, lowTough bool) map[string]TeamRank {
	type avgEntry struct {
		team string
		avg  float64
	}
	entries := make([]avgEntry, 0, len(sums))
	for team, weekSet := range weeks {
		if len(weekSet) == 0 {
			continue
		}
		entries = append(entries, avgEntry{team: team, avg: sums[team] / float64(len(weekSet))})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].avg != entries[j].avg {
			if lowTough {
				return entries[i].avg < entries[j].avg
			}
			return entries[i].avg > entries[j].avg
		}
		return entries[i].team < entries[j].team
	})
	total := len(entries)
	out := make(map[string]TeamRank, total)
	for i, e := range entries {
		rank := i + 1
		out[e.team] = TeamRank{Rank: rank, Total: total, Tier: tierFor(rank, total)}
	}
	return out
}

// MinCurrentWeeks is the fewest completed current-season REG weeks of
// data required before ranks switch from the prior completed season to
// the current one (design point 4). Four weeks is the owner's own
// example threshold ("2026 thru wk 4"); it also lines up with the NFL
// schedule itself — byes never start before week 5, so through week 4
// every team has an equal, uninterrupted sample, the cleanest point to
// cut over before uneven bye weeks start affecting sample size.
const MinCurrentWeeks = 4

// SelectSeason decides which season's data to rank from and returns an
// honest, UI-ready source label: the current season once it has at
// least MinCurrentWeeks of REG-season data (currentWeeks counts distinct
// weeks seen), otherwise the most recently completed season. useCurrent
// tells the caller which row set to rank; label must be shown on every
// surface a rank appears (design point 4: "never mistake last year's
// ranks for this year's").
func SelectSeason(currentSeason, currentWeeks, previousSeason int) (season int, label string, useCurrent bool) {
	if currentWeeks >= MinCurrentWeeks {
		return currentSeason, fmt.Sprintf("%d thru wk %d", currentSeason, currentWeeks), true
	}
	return previousSeason, fmt.Sprintf("%d season", previousSeason), false
}

// offensePositions are the skill positions RankOffenseOutput's caller
// should include when building a DST-facing offense-output row set —
// exported so main.go's adapter and this package's own tests share one
// list rather than each hand-rolling it.
var offensePositions = []string{"QB", "RB", "WR", "TE"}

// OffensePositions returns the position set RankDefenseAllowed ranks
// individually and RankOffenseOutput's caller should sum together: a
// copy, so a caller mutating the result cannot corrupt the package's own
// list.
func OffensePositions() []string {
	out := make([]string, len(offensePositions))
	copy(out, offensePositions)
	return out
}

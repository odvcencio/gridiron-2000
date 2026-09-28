package league

import (
	"fmt"
	"sort"
	"time"
)

// Bench display phases. A bench player's number follows the same rule as a
// starter's: projection before kickoff, live actual once the game starts,
// final actual once it ends.
const (
	BenchPhaseProj  = "proj"
	BenchPhaseLive  = "live"
	BenchPhaseFinal = "final"
	BenchPhaseBye   = "bye"
)

// BenchStarter is one starting-lineup player, as ComputeBenchPoints reads
// it. SlotEligible lists the positions the starter's slot accepts ("RB",
// "WR", "TE" for a FLEX slot). Started is false while the starter's game
// has not kicked off; that slot is then left alone, because a bench player
// cannot be said to beat a score that does not exist yet.
type BenchStarter struct {
	PlayerID     string
	Position     string
	SlotEligible []string
	Points       float64
	Started      bool
}

// BenchPlayer is one bench player, as ComputeBenchPoints reads it. Started
// is false before kickoff and on a bye; such a player has no actual points
// and is left out of both the bench total and the swap search.
type BenchPlayer struct {
	PlayerID string
	Position string
	Points   float64
	Started  bool
}

// BenchPoints is ComputeBenchPoints' result.
//
//   - Total is the sum of actual points scored by bench players whose game
//     has started.
//   - PointsLeft is how many more points the team would hold if every
//     started slot had been filled by the best eligible started player
//     (starter or bench). It is never negative. It is the "points left on
//     bench" figure and the input a Bench Blunder trophy should use.
//   - BeatIDs lists the bench players the best lineup would have started.
type BenchPoints struct {
	Total      float64
	PointsLeft float64
	BeatIDs    []string
}

// ComputeBenchPoints is the one place bench points are calculated. The
// matchups page and any trophy that rewards or mocks bench decisions must
// call it rather than repeat the sums, so they can never disagree.
//
// The best lineup is the maximum-weight assignment of started players to
// started slots. Slot eligibility forms a transversal matroid, so taking
// players from highest score to lowest and keeping each one that still
// leaves a valid assignment (Kuhn's augmenting paths) gives the optimum.
// On equal scores a starter ranks before a bench player, so a tie never
// counts as a missed swap.
func ComputeBenchPoints(starters []BenchStarter, bench []BenchPlayer) BenchPoints {
	var out BenchPoints
	for _, b := range bench {
		if b.Started {
			out.Total += b.Points
		}
	}
	type candidate struct {
		id       string
		position string
		points   float64
		isBench  bool
	}
	var slots [][]string
	var candidates []candidate
	actual := 0.0
	for _, st := range starters {
		if !st.Started {
			continue
		}
		slots = append(slots, st.SlotEligible)
		actual += st.Points
		candidates = append(candidates, candidate{id: st.PlayerID, position: st.Position, points: st.Points})
	}
	if len(slots) == 0 {
		return out
	}
	for _, b := range bench {
		if b.Started {
			candidates = append(candidates, candidate{id: b.PlayerID, position: b.Position, points: b.Points, isBench: true})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].points != candidates[j].points {
			return candidates[i].points > candidates[j].points
		}
		return !candidates[i].isBench && candidates[j].isBench
	})
	fits := func(slot int, position string) bool {
		for _, eligible := range slots[slot] {
			if eligible == position {
				return true
			}
		}
		return false
	}
	// owner[slot] is the index into candidates now holding the slot, or -1.
	owner := make([]int, len(slots))
	for i := range owner {
		owner[i] = -1
	}
	var augment func(c int, seen []bool) bool
	augment = func(c int, seen []bool) bool {
		for slot := range slots {
			if seen[slot] || !fits(slot, candidates[c].position) {
				continue
			}
			seen[slot] = true
			if owner[slot] < 0 || augment(owner[slot], seen) {
				owner[slot] = c
				return true
			}
		}
		return false
	}
	for c := range candidates {
		augment(c, make([]bool, len(slots)))
	}
	best := 0.0
	seenBeat := map[string]bool{}
	for _, c := range owner {
		if c < 0 {
			continue
		}
		best += candidates[c].points
		if candidates[c].isBench && !seenBeat[candidates[c].id] {
			seenBeat[candidates[c].id] = true
			out.BeatIDs = append(out.BeatIDs, candidates[c].id)
		}
	}
	if best > actual {
		out.PointsLeft = best - actual
	}
	sort.Strings(out.BeatIDs)
	return out
}

// BenchPlayerLine is one bench row, ready to render.
type BenchPlayerLine struct {
	PlayerID string
	Name     string
	Position string
	NFLTeam  string
	// Phase is BenchPhaseProj, Live, Final, or Bye.
	Phase  string
	Points float64
	// Value is the number to show: the projection before kickoff, the
	// actual once the game has started, "—" or "BYE" otherwise.
	Value string
	// Label names what Value is in words, so the meaning never rests on
	// colour: "PROJ", "LIVE", "FINAL", or "BYE".
	Label string
	// Beat marks a bench player who outscored a started player at an
	// eligible position.
	Beat bool
}

// BenchReport is one team's bench for one week.
type BenchReport struct {
	Players []BenchPlayerLine
	BenchPoints
	// TotalText and PointsLeftText are formatted for display.
	TotalText      string
	PointsLeftText string
	ProjTotal      float64
	ProjTotalText  string
	// Phase is "proj" while no bench player's game has started, "final"
	// once every bench player with a game this week has finished, and
	// "live" in between.
	Phase string
	// HasStartedPlayer reports whether any starter or bench game has begun.
	HasStartedPlayer bool
}

// TeamBenchReport builds teamID's bench report for week from the same
// stat snapshot the starter ledger uses. It is the entry point a trophy or
// any other page should call: bench composition, actual points, phases,
// and the points-left figure all come from here.
func (s *Service) TeamBenchReport(state PersistedState, teamID string, week int, postedFinal bool) BenchReport {
	snapshot := s.matchupStatsSnapshot(week)
	ledger := s.teamWeekLedgerFromSnapshot(state, teamID, week, snapshot)
	return s.benchReport(state, teamID, week, ledger.Rows, snapshot, postedFinal)
}

func (s *Service) benchReport(state PersistedState, teamID string, week int, starterRows []StarterLedgerRow, snapshot matchupStatsSnapshot, postedFinal bool) BenchReport {
	effective := s.effectiveLineupForTeam(state, teamID, week)
	starting := map[string]bool{}
	for _, row := range starterRows {
		if row.PlayerID != "" {
			starting[row.PlayerID] = true
		}
	}
	// A closed week pins its own starters, which can differ from today's
	// effective lineup, so every rostered player the pinned lineup did not
	// start is bench for that week.
	var pool []Player
	for _, slot := range effective.Slots {
		if slot.HasPlayer {
			pool = append(pool, slot.Player)
		}
	}
	pool = append(pool, effective.Bench...)
	return buildBenchReport(pool, starting, starterRows, week, snapshot, s.currentScoringValues(), postedFinal, s.clock())
}

func buildBenchReport(pool []Player, starting map[string]bool, starterRows []StarterLedgerRow, week int, snapshot matchupStatsSnapshot, values map[string]float64, postedFinal bool, now time.Time) BenchReport {
	lineByKey := weekStatLinesByKey(snapshot.lines)
	slotEligible := map[string][]string{}
	for _, slot := range lineupSlots(CurrentRoster()) {
		slotEligible[slot.ID] = slot.Def.Eligible
	}
	var report BenchReport
	var starters []BenchStarter
	for _, row := range starterRows {
		if row.PlayerID == "" {
			continue
		}
		started := postedFinal || row.GameFinal || row.JoinState == "matched" || starterGameStarted(row.NFLTeam, snapshot, now)
		if started {
			report.HasStartedPlayer = true
		}
		starters = append(starters, BenchStarter{
			PlayerID: row.PlayerID, Position: row.Position, SlotEligible: slotEligible[row.Slot],
			Points: row.Points, Started: started,
		})
	}
	var bench []BenchPlayer
	startedBench, unfinishedBench := 0, 0
	projSum := 0.0
	seen := map[string]bool{}
	for _, player := range pool {
		if starting[player.ID] || seen[player.ID] {
			continue
		}
		seen[player.ID] = true
		line := BenchPlayerLine{PlayerID: player.ID, Name: player.Name, Position: player.Position, NFLTeam: player.NFLTeam}
		stat, joined := lineByKey[playerStatKey(player)]
		final := postedFinal || starterGameFinal(player.NFLTeam, snapshot)
		started := joined || final || starterGameStarted(player.NFLTeam, snapshot, now)
		switch {
		case !started && starterOnBye(player, week, snapshot):
			line.Phase, line.Label, line.Value = BenchPhaseBye, "BYE", "—"
		case !started:
			line.Phase, line.Label, line.Value = BenchPhaseProj, "PROJ", playerProjectionText(player)
			unfinishedBench++
			if playerHasProjection(player) {
				projSum += player.Projection
			}
		default:
			if joined {
				line.Points = scorePlayerStats(stat.Stats, values)
			}
			line.Value = fmt.Sprintf("%.1f", line.Points)
			startedBench++
			if final {
				line.Phase, line.Label = BenchPhaseFinal, "FINAL"
			} else {
				line.Phase, line.Label = BenchPhaseLive, "LIVE"
				unfinishedBench++
			}
			report.HasStartedPlayer = true
		}
		report.Players = append(report.Players, line)
		bench = append(bench, BenchPlayer{
			PlayerID: player.ID, Position: player.Position, Points: line.Points,
			Started: line.Phase == BenchPhaseLive || line.Phase == BenchPhaseFinal,
		})
	}
	report.BenchPoints = ComputeBenchPoints(starters, bench)
	beat := map[string]bool{}
	for _, id := range report.BeatIDs {
		beat[id] = true
	}
	for i := range report.Players {
		report.Players[i].Beat = beat[report.Players[i].PlayerID]
	}
	sort.SliceStable(report.Players, func(i, j int) bool {
		return report.Players[i].Points > report.Players[j].Points
	})
	switch {
	case startedBench == 0:
		report.Phase = BenchPhaseProj
	case unfinishedBench == 0:
		report.Phase = BenchPhaseFinal
	default:
		report.Phase = BenchPhaseLive
	}
	report.ProjTotal = projSum
	report.ProjTotalText = fmt.Sprintf("%.1f", report.ProjTotal)
	report.TotalText = fmt.Sprintf("%.1f", report.Total)
	report.PointsLeftText = fmt.Sprintf("%.1f", report.PointsLeft)
	return report
}

// TotalLine is the bench total in words: the label carries the meaning so
// it never depends on colour ("Bench 12.4 pts, final").
func (r BenchReport) TotalLine() string {
	if len(r.Players) == 0 {
		return "No bench players"
	}
	switch r.Phase {
	case BenchPhaseFinal:
		return "Bench total " + r.TotalText + " pts, final"
	case BenchPhaseLive:
		return "Bench total " + r.TotalText + " pts, live"
	}
	return "Bench projected " + r.ProjTotalText + " pts, not started"
}

// LeftLine is the points-left-on-bench sentence, empty until any starter
// or bench game has begun (before that there is nothing to compare).
func (r BenchReport) LeftLine() string {
	if !r.HasStartedPlayer {
		return ""
	}
	suffix := ", final"
	if r.Phase != BenchPhaseFinal {
		suffix = " so far"
	}
	return "Left on bench " + r.PointsLeftText + " pts" + suffix
}

// benchRowMapsFromReport converts a report into the template's row maps.
// liveKey is stable per team and player so the live poll can rewrite each
// row's value and label.
func benchRowMapsFromReport(teamID string, report BenchReport) []map[string]any {
	out := make([]map[string]any, 0, len(report.Players))
	for _, line := range report.Players {
		out = append(out, map[string]any{
			"player_name": line.Name,
			"position":    line.Position,
			"nfl_team":    line.NFLTeam,
			"proj":        line.Value,
			"value":       line.Value,
			"label":       line.Label,
			"phase":       line.Phase,
			"beat":        line.Beat,
			"beat_text":   BenchBeatText(line.Beat),
			"live_key":    BenchLiveKey(teamID, line.PlayerID),
		})
	}
	return out
}

// BenchLiveKey is the flat live-bind key for one bench row.
func BenchLiveKey(teamID, playerID string) string {
	return teamID + "_bench_" + playerID
}

func benchSummaryMap(report BenchReport) map[string]any {
	return map[string]any{
		"total_line": report.TotalLine(),
		"left_line":  report.LeftLine(),
		"has_left":   report.LeftLine() != "",
	}
}

// BenchBeatText is the words shown on a bench row whose player outscored a
// started player at an eligible position, empty otherwise.
func BenchBeatText(beat bool) string {
	if beat {
		return "Outscored a starter"
	}
	return ""
}

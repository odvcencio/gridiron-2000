package league

import (
	"fmt"
	"strings"
	"time"
)

// matchupStatsSnapshot is one week's authoritative read for both the
// scoring ledger and the A5 truthful-state resolution that rides beside
// it: games and live/hasLive let starterGameState and matchupLiveState
// (feed.go) answer "is this starter's game live, final, or not started
// yet" from the same read the points come from, so a page render can
// never show a score from one instant and a game-state label from
// another (round-2 finding 17).
type matchupStatsSnapshot struct {
	lines       []WeekStatLine
	final       bool
	known       bool
	sourceState string
	sourceErr   error
	games       []GameInfo // this week's NFL games (s.schedule() filtered by Week)
	live        LiveStatus
	hasLive     bool
}

func (s *Service) matchupStatsSnapshot(week int) matchupStatsSnapshot {
	lines, final, known, sourceState, sourceErr := weekStatsSnapshot(s.weekStatsSource(), week)
	var games []GameInfo
	for _, game := range s.schedule() {
		if game.Week == week {
			games = append(games, game)
		}
	}
	live, hasLive := s.liveStatus()
	return matchupStatsSnapshot{
		lines: lines, final: final, known: known, sourceState: sourceState, sourceErr: sourceErr,
		games: games, live: live, hasLive: hasLive,
	}
}

// matchupLineup resolves the exact starter set used by matchup scoring and
// keeps the provenance needed by the manager-facing ledger. Closed weeks use
// the materialized pin directly; open weeks use effectiveLineup, including
// the same auto-fill behavior the scorer uses. The closed path intentionally
// resolves players from the whole pool so a later drop or trade cannot change
// a posted week's explanation.
func (s *Service) matchupLineup(state PersistedState, teamID string, week int) (EffectiveLineup, bool) {
	if weekIsFinalInSchedule(state.Schedule, week) {
		pinned := state.Lineups[teamID][week]
		pool := s.pool()
		slots := lineupSlots(CurrentRoster())
		resolved := EffectiveLineup{Week: week, Slots: make([]SlotAssignment, 0, len(slots))}
		for _, slot := range slots {
			a := SlotAssignment{Slot: slot}
			if playerID := pinned[slot.ID]; playerID != "" {
				if player, ok := pool.byID[playerID]; ok {
					a.Player = player
					a.HasPlayer = true
				}
			}
			resolved.Slots = append(resolved.Slots, a)
		}
		return resolved, true
	}

	preset := CurrentRoster()
	roster, _ := s.rosterForTeam(state, teamID)
	general, _, _ := splitRosterZones(state, teamID, roster)
	return effectiveLineup(preset, general, state.Lineups[teamID], week, s.schedule(), s.clock()), false
}

// explicitLineupForWeek returns the stored map that effectiveLineup walks
// back to for an open week. A slot absent from this map was auto-filled (or
// left empty), even when an earlier week's map supplied other slots.
func explicitLineupForWeek(stored map[int]map[string]string, week int) map[string]string {
	for current := week; current >= 1; current-- {
		if explicit, ok := stored[current]; ok {
			return explicit
		}
	}
	return nil
}

func ledgerProvenance(assignment SlotAssignment, explicit map[string]string, pinned bool) string {
	if !assignment.HasPlayer {
		return "empty"
	}
	if pinned {
		return "pinned"
	}
	if assignment.AutoFilled || explicit[assignment.Slot.ID] == "" {
		return "auto-filled"
	}
	return "explicit"
}

func ledgerPlayerDetail(row *StarterLedgerRow) {
	switch row.JoinState {
	case "matched":
		switch row.Source {
		case StatSourceLive:
			row.Detail = "Matched to the live box score; the game is in progress."
		case StatSourceLiveFinal:
			row.Detail = "Matched to the final box score; the weekly ledger is not posted yet."
		default:
			row.Detail = "Matched to the mirrored player-stat ledger."
		}
	case "missing-join":
		row.Detail = "No matching player-stat row for this name and position; 0.0 is an explicit join miss."
	case "stats-unavailable":
		row.Detail = "Player-stat source is unavailable; points are not being treated as an official zero."
	case "stats-empty":
		row.Detail = "No player-stat rows are available for this week; points are not being treated as an official zero."
	case "empty":
		row.Detail = "No player is configured in this starting slot."
	}
}

// starterGameState renders one starter's game clock: "Q3 8:12" while the
// poller sees the game in progress, "FINAL" once final, the kickoff
// ("SUN 4:25 PM") from the schedule before it starts, else "".
func starterGameState(team string, snapshot matchupStatsSnapshot, location *time.Location) string {
	if snapshot.hasLive {
		if game, ok := snapshot.live.Games[team]; ok {
			switch {
			case game.Final:
				return "FINAL"
			case game.InProgress && game.Clock != "":
				return game.Period + " " + game.Clock
			case game.InProgress:
				return game.Period
			}
		}
	}
	for _, game := range snapshot.games {
		if (game.Away == team || game.Home == team) && !game.Kickoff.IsZero() {
			if game.Final {
				return "FINAL"
			}
			return strings.ToUpper(game.Kickoff.In(location).Format("Mon 3:04 PM"))
		}
	}
	return ""
}

// starterGameNotStarted reports whether team's game is affirmatively
// known — from the live poller's status or, before the poller has it in
// its window, from the NFL schedule's kickoff instant — to not have
// kicked off yet. It answers the same question starterGameState renders
// as a label, kept as its own bool so the point-value fallback (rider
// R3: an explicit 0.0 once a starter's game is live even with no line
// for them yet, an honest "—" only before kickoff) never has to parse
// GameState's formatted string back apart. Absent any signal at all
// (no live status wired, no matching scheduled game) it answers false —
// "not known to be unstarted" is not the same claim as "in progress",
// but rendering an explicit 0.0 by default is the safer of the two
// honest options when the starter's game state truly cannot be read.
func starterGameNotStarted(team string, snapshot matchupStatsSnapshot, now time.Time) bool {
	if snapshot.hasLive {
		if game, ok := snapshot.live.Games[team]; ok {
			return !game.Final && !game.InProgress
		}
	}
	for _, game := range snapshot.games {
		if (game.Away == team || game.Home == team) && !game.Kickoff.IsZero() {
			return !game.Final && now.Before(game.Kickoff)
		}
	}
	return false
}

// teamWeekLedger is the canonical scoring/ledger calculation. It calls the
// same scorePlayerPoints helper as MatchupScorer.TeamWeekScore, while adding
// one row per configured slot and explicit source/join states for rendering.
func (s *Service) teamWeekLedger(state PersistedState, teamID string, week int) TeamWeekLedger {
	return s.teamWeekLedgerFromSnapshot(state, teamID, week, s.matchupStatsSnapshot(week))
}

func (s *Service) teamWeekLedgerFromSnapshot(state PersistedState, teamID string, week int, snapshot matchupStatsSnapshot) TeamWeekLedger {
	lines, final, known, sourceState, sourceErr := snapshot.lines, snapshot.final, snapshot.known, snapshot.sourceState, snapshot.sourceErr
	if sourceErr != nil {
		sourceState = "unavailable"
	}
	values := s.currentScoringValues()
	lineByKey := weekStatLinesByKey(lines)
	lineup, pinned := s.matchupLineup(state, teamID, week)
	explicit := explicitLineupForWeek(state.Lineups[teamID], week)
	// Hoisted out of the per-starter loop below: every row reads the same
	// clock instant, and s.clock() is not free (round-2 review of commit
	// 8a4ffea, finding 6).
	now := s.clock()
	rows := make([]StarterLedgerRow, 0, len(lineup.Slots))
	total := 0.0
	complete := true
	for _, assignment := range lineup.Slots {
		row := StarterLedgerRow{
			LiveKey:    teamID + "_" + assignment.Slot.ID,
			Slot:       assignment.Slot.ID,
			Position:   strings.Join(assignment.Slot.Def.Eligible, "/"),
			Provenance: ledgerProvenance(assignment, explicit, pinned),
			JoinState:  sourceState,
		}
		if !assignment.HasPlayer {
			row.PlayerName = "Empty slot"
			row.JoinState = "empty"
			row.Provenance = "empty"
			ledgerPlayerDetail(&row)
			row.PointsText = "0.0"
			rows = append(rows, row)
			continue
		}
		row.PlayerID = assignment.Player.ID
		row.PlayerName = assignment.Player.Name
		row.Position = assignment.Player.Position
		row.NFLTeam = assignment.Player.NFLTeam
		row.GameState = starterGameState(row.NFLTeam, snapshot, s.matchupLocation())
		if sourceErr != nil {
			row.JoinState = "stats-unavailable"
		} else if len(lines) == 0 {
			row.JoinState = "stats-empty"
		} else if line, joined := lineByKey[normalizePlayerKey(assignment.Player.Name, assignment.Player.Position)]; joined {
			row.Points = scorePlayerStats(line.Stats, values)
			row.JoinState = "matched"
			row.Source = line.Source
			if row.Source == "" {
				row.Source = StatSourceLedger
			}
			total += row.Points
		} else {
			row.JoinState = "missing-join"
			complete = false
		}
		row.PointsText = fmt.Sprintf("%.1f", row.Points)
		switch {
		case row.JoinState == "matched":
			// scored above; PointsText already reflects it.
		case snapshot.hasLive && snapshot.live.Degraded:
			// A known live-poller outage is not "unknown": never render an
			// implicit 0.0 while the poller itself reports it cannot see the
			// game right now (round-2 review of commit 8a4ffea, finding 2).
			row.PointsText = "—"
		case starterGameNotStarted(row.NFLTeam, snapshot, now):
			// R3: a starter with no live row yet and no ledger line either
			// only reads as an honest "—" once the game is known not to
			// have started; once it is live (or we cannot tell), the
			// explicit 0.0 above stands — the player is simply scoreless
			// so far, not unaccounted for.
			row.PointsText = "—"
		}
		ledgerPlayerDetail(&row)
		rows = append(rows, row)
	}
	if known && !complete {
		known = false
		sourceState = "partial"
	}
	return TeamWeekLedger{
		TeamID:      teamID,
		Week:        week,
		Total:       total,
		TotalText:   map[bool]string{true: fmt.Sprintf("%.1f", total), false: "—"}[known],
		Known:       known,
		Final:       final,
		SourceState: sourceState,
		Rows:        rows,
	}
}

func scoreTeamFromLedger(team Team, ledger TeamWeekLedger) ScoreTeam {
	return ScoreTeam{
		ID:              team.ID,
		Name:            team.Name,
		Abbreviation:    team.Abbreviation,
		Score:           ledger.Total,
		ScoreText:       ledger.TotalText,
		ScoreKnown:      ledger.Known,
		LedgerTotal:     ledger.Total,
		LedgerTotalText: ledger.TotalText,
		LedgerKnown:     ledger.Known,
		ScoreBasis:      "starter-ledger",
		StarterLedger:   ledger.Rows,
	}
}

// applyPostedFinalScore keeps the persisted fantasy result authoritative after
// close. The current mirrored-stat ledger remains visible for explanation, but
// a later source correction must not silently rewrite a posted result. When
// the two values differ (or the ledger is incomplete), the card receives an
// explicit note instead of pretending the rows sum to the posted total.
func (team *ScoreTeam) applyPostedFinalScore(posted float64) {
	team.Score = posted
	team.ScoreText = fmt.Sprintf("%.1f", posted)
	team.ScoreKnown = true
	team.ScoreBasis = "posted-final"
	if !team.LedgerKnown {
		team.ScoreNote = "Posted final is authoritative; the current starter ledger is incomplete."
		return
	}
	delta := team.LedgerTotal - posted
	if delta == 0 {
		team.ScoreNote = fmt.Sprintf("Posted final %.1f; starter ledger matches.", posted)
		return
	}
	team.ScoreNote = fmt.Sprintf("Posted final %.1f; current starter ledger %.1f (delta %+.1f). Posted total is authoritative.", posted, team.LedgerTotal, delta)
}

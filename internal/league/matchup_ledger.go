package league

import (
	"fmt"
	"math"
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
	return effectiveLineupWithState(preset, general, state, teamID, week, s.schedule(), s.clock()), false
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
		case StatSourceLedgerLive:
			row.Detail = "Matched to the mirrored player-stat ledger, plus a category (for example a return touchdown) only the final box score reported."
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

// ledgerLineupText, ledgerStatsText, and ledgerSourceText are the single
// place StarterLedgerRow's raw Provenance/JoinState/Source tokens turn
// into the manager-facing words the per-starter ledger disclosure shows
// ("Lineup: auto-filled · Stats: none yet") — wave-8 audit item 3. Before
// this, /matchups printed the bare enum tokens themselves, unlabelled and
// unguarded ("auto-filled · stats-empty ·", "empty · empty ·" for an
// empty slot, both with a dangling trailing separator).
//
// Both callers this text feeds — starterLedgerMaps (service.go, the
// initial page render) and LiveScoresView's per-row live-bind loop
// (service.go) — call these three functions and nothing else, so the
// token-to-word mapping itself lives in exactly one place, and every
// live poll re-sends the same already-labelled text a full render would.
// Stats/Source each carry their own leading " · " separator baked into
// the non-empty case and return "" otherwise, so an empty segment (Stats
// on an empty slot, Source before a stat line has ever matched) vanishes
// together with its separator instead of leaving a dangling "· ·" — the
// page concatenates the three segments with no separator of its own
// (page.gsx's StarterCell).
func ledgerLineupText(provenance string) string {
	switch provenance {
	case "empty":
		return "Lineup: no player in this slot"
	case "pinned":
		return "Lineup: locked for the closed week"
	case "auto-filled":
		return "Lineup: auto-filled"
	case "explicit":
		return "Lineup: set by the manager"
	default:
		return "Lineup: " + provenance
	}
}

func ledgerStatsText(joinState string) string {
	switch joinState {
	case "empty":
		// Lineup already said "no player in this slot"; a starter this
		// week never had, has nothing further to say about stats.
		return ""
	case "matched":
		return " · Stats: scored"
	case "missing-join":
		return " · Stats: no stat row yet"
	case "stats-unavailable":
		return " · Stats: source unavailable"
	case "stats-empty":
		return " · Stats: none yet"
	default:
		return " · Stats: " + joinState
	}
}

func ledgerSourceText(source string) string {
	switch source {
	case "":
		return ""
	case StatSourceLive:
		return " · Source: live box score"
	case StatSourceLiveFinal:
		return " · Source: final box score"
	case StatSourceLedgerLive:
		return " · Source: ledger + live box score"
	case StatSourceLedger:
		return " · Source: weekly ledger"
	default:
		return " · Source: " + source
	}
}

// tank01ToNFLverseAbbreviation maps the three Tank01 team abbreviations
// that differ from nflverse's own (LAR, WSH, JAC), the same correction
// internal/livescore.NormalizeTeam applies. This is a small, deliberate
// duplicate — internal/league must not import internal/livescore (no
// import cycle, Tasks 3/5 decision) — used only by teamHasGame below: a
// real pool player can carry a Tank01-sourced NFLTeam like "LAR"
// (defaultPlayers' Puka Nacua), while GameInfo.Away/Home can arrive
// already normalized to nflverse's "LA" (internal/sim/replay's own
// ScheduleSource calls the same normalizer), so an unnormalized
// string-equality check would miss that starter's real game and
// misread them as on bye.
var tank01ToNFLverseAbbreviation = map[string]string{"LAR": "LA", "WSH": "WAS", "JAC": "JAX"}

func normalizeNFLAbbreviation(abbr string) string {
	upper := strings.ToUpper(strings.TrimSpace(abbr))
	if mapped, ok := tank01ToNFLverseAbbreviation[upper]; ok {
		return mapped
	}
	return upper
}

// teamHasGame reports whether team (normalized) is either side of any
// of games.
func teamHasGame(team string, games []GameInfo) bool {
	normalized := normalizeNFLAbbreviation(team)
	for _, game := range games {
		if normalizeNFLAbbreviation(game.Away) == normalized || normalizeNFLAbbreviation(game.Home) == normalized {
			return true
		}
	}
	return false
}

// starterOnBye reports whether player's NFL team has no game at all in
// week's loaded schedule — a true bye — guarded on the schedule actually
// being loaded (len(snapshot.games) > 0). With no schedule wired at all
// (the common unit-test shape, or before the season schedule syncs),
// every team looks like "no game this week"; treating that as a
// league-wide bye would be a much larger, wrong claim than the "cannot
// read this one starter's game" case starterGameKnownZeroSoFar's other
// branches already fall back to (rider on the review of ff2a9b3, item
// 3).
//
// teamHasGame's schedule-presence check is the sole signal (residuals
// N1/N2, review of eb549b6): player.ByeWeek/slotWarnsBye is not
// consulted here at all. Trusting ByeWeek directly had two failure
// modes — a stale ByeWeek that happened to equal week could claim "BYE"
// (and let the bye branch bypass the poller's own Degraded check) even
// while the team's real game sat right there in the loaded schedule
// (N1), and a pool that carries no bye data at all (ByeWeek == 0, the
// offline/fallback pool) could never read a genuine bye correctly no
// matter how plainly the schedule showed the team absent (N2). The
// schedule the render already has is strictly more authoritative than a
// separate, possibly stale or absent field for the same fact.
func starterOnBye(player Player, week int, snapshot matchupStatsSnapshot) bool {
	if len(snapshot.games) == 0 {
		return false
	}
	return !teamHasGame(player.NFLTeam, snapshot.games)
}

// starterFinalLabel renders a finished game as its result, read from one
// starter's own team's side: "W 27-20", "L 20-27", "T 20-20". team must
// already be nflverse-normalized; away and home are the game's two sides
// in that same namespace, with awayPoints/homePoints their scores.
//
// The score is what the owner asked this chip for after week 1's opener
// (2026-09-09): the bare word "FINAL" told a manager their starter's
// game had ended but not how it ended, so the result had to be looked up
// somewhere else. The word itself is not lost — the chip carries
// starterStateClass's own state--final treatment (app/matchups's
// page.server.go), and a settled score beside a W/L/T reads as concluded
// where "Q3 8:12" reads as running. It stays compact on purpose: the
// .slot-row grid gives this chip a 5rem column with white-space: nowrap
// (public/styles.css), which "FINAL W 27-20" overflows and "W 27-20"
// does not.
//
// When the starter's team matches neither side — an unrecognized
// abbreviation on either end of a source drift — it falls back to the
// bare "FINAL" this label used to be. Naming a winner it cannot actually
// locate would be worse than saying less.
func starterFinalLabel(team, away, home string, awayPoints, homePoints float64) string {
	var own, opponent float64
	switch team {
	case normalizeNFLAbbreviation(away):
		own, opponent = awayPoints, homePoints
	case normalizeNFLAbbreviation(home):
		own, opponent = homePoints, awayPoints
	default:
		return "FINAL"
	}
	result := "T"
	switch {
	case own > opponent:
		result = "W"
	case own < opponent:
		result = "L"
	}
	return fmt.Sprintf("%s %d-%d", result, int(math.Round(own)), int(math.Round(opponent)))
}

// starterGameState renders one starter's game clock: "BYE" when the
// starter's team has no game this week, "Q3 8:12" while the poller sees
// the game in progress, the game's own result ("W 27-20", see
// starterFinalLabel) once final, the kickoff ("SUN 4:25 PM") from the
// schedule before it starts, else "".
//
// Item 2 (2026-08-31 post-wave audit): the schedule loop below matches
// through normalizeNFLAbbreviation — teamHasGame's own fix (this file,
// above) and playerLockAt's (lineup.go) — because snapshot.games is
// nflverse-normalized ("LA") while player.NFLTeam can carry a
// Tank01-sourced abbreviation ("LAR"). Before this fix a LAR/WSH/JAC
// starter's raw-string compare here never matched, so /team, /players,
// and /board rendered no kickoff/game-clock text for that starter at
// all (blank where "@ SF" or "SUN 4:25 PM" belongs) even though the
// lock join (playerLockAt) already correctly enforced their kickoff.
//
// 2026-09-09: the live.Games lookup is normalized for exactly the same
// reason, correcting this comment's own former claim that it did not
// need to be. LiveStatus.Games is keyed by nflverse abbreviation, not by
// a Tank01 one — liveStatusFromPoller (live_scoring.go) builds it from
// livescore.GameState.Away/Home, which snapshot.go writes through
// livescore.NormalizeTeam — while player.NFLTeam is the pool's raw
// Tank01 string ("LAR"). So every Rams, Commanders, and Jaguars starter
// missed this join outright: no live clock, no live final, and (through
// starterGameKnownZeroSoFar below, which shares the lookup) a team total,
// projection, and win probability that fell to "—" for the whole game
// whenever one of those three teams' starters had no ledger row yet.
// starterPossessionLabel (possession.go) already normalized this same
// map correctly; these three call sites were the ones left behind. There
// is no collision risk in normalizing: the map's own keys are nflverse
// abbreviations, so LAR/WSH/JAC resolve onto LA/WAS/JAX, which is
// precisely the entry being looked for.
func starterGameState(player Player, week int, snapshot matchupStatsSnapshot, location *time.Location) string {
	if starterOnBye(player, week, snapshot) {
		return "BYE"
	}
	team := normalizeNFLAbbreviation(player.NFLTeam)
	if snapshot.hasLive {
		if game, ok := snapshot.live.Games[team]; ok {
			switch {
			case game.Final:
				return starterFinalLabel(team, game.Away, game.Home, game.AwayPoints, game.HomePoints)
			case game.InProgress && game.Clock != "":
				return game.Period + " " + game.Clock
			case game.InProgress:
				return game.Period
			}
		}
	}
	for _, game := range snapshot.games {
		if (normalizeNFLAbbreviation(game.Away) == team || normalizeNFLAbbreviation(game.Home) == team) && !game.Kickoff.IsZero() {
			if game.Final {
				// ScoresPresent, not the numbers themselves: a blank
				// nflverse score is not an actual 0-0 (openstats'
				// HasFinalScore keeps the same distinction), so a final
				// game the schedule has no score for still reads "FINAL".
				if game.ScoresPresent {
					return starterFinalLabel(team, game.Away, game.Home, float64(game.AwayScore), float64(game.HomeScore))
				}
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
	// Item 2, and the 2026-09-09 live-key correction beside it: both the
	// live lookup and the schedule loop below normalize, so a LAR/WSH/JAC
	// starter resolves in either. starterGameState's own doc comment
	// carries the full explanation.
	normalized := normalizeNFLAbbreviation(team)
	if snapshot.hasLive {
		if game, ok := snapshot.live.Games[normalized]; ok {
			return !game.Final && !game.InProgress
		}
	}
	for _, game := range snapshot.games {
		if (normalizeNFLAbbreviation(game.Away) == normalized || normalizeNFLAbbreviation(game.Home) == normalized) && !game.Kickoff.IsZero() {
			return !game.Final && now.Before(game.Kickoff)
		}
	}
	return false
}

// starterGameStarted reports whether a player's NFL game is
// AFFIRMATIVELY known to have begun — the live poller has it in progress
// or final, or the loaded schedule says its kickoff has passed. It is
// deliberately not the negation of starterGameNotStarted: that function
// answers false both for "the game is running" and for "no signal at
// all", and those two must not render the same way.
//
// 2026-09-09 (owner report: a bench defense that had not played read
// 0.0): an explicit zero is a claim that a player took the field and
// scored nothing. Only this function's positive answer justifies making
// it. With no schedule loaded and no poller entry — the state a fresh
// boot or an unwired stat source produces — the honest render is the
// dash, not a zero every roster row would wear alike.
func starterGameStarted(team string, snapshot matchupStatsSnapshot, now time.Time) bool {
	normalized := normalizeNFLAbbreviation(team)
	if snapshot.hasLive {
		if game, ok := snapshot.live.Games[normalized]; ok {
			return game.InProgress || game.Final
		}
	}
	for _, game := range snapshot.games {
		if normalizeNFLAbbreviation(game.Away) != normalized && normalizeNFLAbbreviation(game.Home) != normalized {
			continue
		}
		if game.Final {
			return true
		}
		return !game.Kickoff.IsZero() && !now.Before(game.Kickoff)
	}
	return false
}

// starterGameKnownZeroSoFar reports whether a missing-join starter's game
// state is affirmatively known — a true bye (starterOnBye), under a
// healthy live poller, or (via starterGameNotStarted's own fallback)
// from the NFL schedule when the poller has no entry yet — to be a bye,
// pre-kickoff, or in progress. In every one of those states the starter's
// box score genuinely has nothing to report: the missing ledger join is
// an honest 0.0, not an unaccounted-for gap, and the team total may count
// it toward a KNOWN sum instead of going UNKNOWN for hours until
// nflverse posts the week (rider on the review of ae1a525, item 1 —
// today one such starter alone forced the whole team total, its
// projection, and its win probability to read "—" for the entire game;
// extended to true byes by the review of ff2a9b3, item 3). A bye is
// known regardless of poller health — there is no game for the poller to
// be degraded about. It answers false — leaving the total UNKNOWN, same
// as before this rider — when the live poller itself reports Degraded,
// when the game located for this team is already Final (a missing
// ledger row for a finished game is a real gap the ledger has not
// closed yet, not an honest zero), or when no signal at all locates the
// game for this starter (no live entry and no scheduled kickoff,
// including an unconfigured/unrecognized NFL team).
func starterGameKnownZeroSoFar(player Player, week int, snapshot matchupStatsSnapshot, now time.Time) bool {
	if starterOnBye(player, week, snapshot) {
		return true
	}
	if snapshot.hasLive && snapshot.live.Degraded {
		return false
	}
	team := normalizeNFLAbbreviation(player.NFLTeam)
	if starterGameNotStarted(team, snapshot, now) {
		return true
	}
	if snapshot.hasLive {
		if game, ok := snapshot.live.Games[team]; ok {
			return game.InProgress
		}
	}
	return false
}

// weeklyPlayerPointsText (J3 F12) is a starting slot's own PointsText
// join/fallback rule (teamWeekLedgerFromSnapshot, below), generalized to
// any player — not only one holding a starting slot this week. /team's
// bench rows (and, ahead of F23's own PROJ/PTS columns landing on
// starter rows, its starter rows too) used to format player.Points
// directly: a field nothing in this codebase ever populates from a real
// source, so it always read the false "0.0", scored week or not. This
// renders the exact same "—" a starter's own row renders before the
// weekly ledger has anything to say about that player, so the two
// surfaces can never disagree about whether a week has posted.
func weeklyPlayerPointsText(player Player, snapshot matchupStatsSnapshot, values map[string]float64, lineByKey map[string]WeekStatLine, now time.Time) string {
	if snapshot.sourceErr != nil {
		return "—"
	}
	if len(snapshot.lines) == 0 {
		return "—"
	}
	if line, joined := lineByKey[playerStatKey(player)]; joined {
		return fmt.Sprintf("%.1f", scorePlayerStats(line.Stats, values))
	}
	if snapshot.hasLive && snapshot.live.Degraded {
		return "—"
	}
	// An explicit 0.0 needs a positive reason: this player's game is
	// known to have started and the ledger simply has nothing for them
	// yet. Every other case — not kicked off, or no signal at all — is
	// the dash (starterGameStarted's own doc comment).
	if !starterGameStarted(player.NFLTeam, snapshot, now) {
		return "—"
	}
	return "0.0"
}

// weeklyPlayerScoredBreakdown explains the number weeklyPlayerPointsText
// renders, rule by rule (ScoreBreakdownText). It is deliberately empty
// whenever that function does not return a real score: an explanation of
// a dash, or of a zero nothing has posted yet, would be an explanation of
// nothing. /team renders it beside the PROJECTION breakdown its stat tip
// already carries, which is a different claim about a different number —
// one is a forecast, this is what actually happened.
func weeklyPlayerScoredBreakdown(player Player, snapshot matchupStatsSnapshot, values map[string]float64, lineByKey map[string]WeekStatLine) string {
	if snapshot.sourceErr != nil || len(snapshot.lines) == 0 {
		return ""
	}
	line, joined := lineByKey[playerStatKey(player)]
	if !joined {
		return ""
	}
	return ScoreBreakdownText(line.Stats, values)
}

// SeasonPointsText sums player's posted weekly ledger points across every
// closed week (J3 F31 residue, wave E). weeklyPlayerPointsText, above,
// reads one week's live/posted score — the trade composer's own
// SeasonPoints field used to read that single-week value under a season-
// shaped name, accurate for judging a multi-week trade only by
// accident. This walks every week the schedule already marks closed
// (scheduleWeekIsFinal — the same "this week is closed" truth
// AdminCloseWeek and the console both read) and sums each one's posted
// score with the exact same per-week reader weeklyPlayerPointsText uses
// (weekStatLinesByKey, scorePlayerStats) — never live or in-progress
// data from a week still open. Returns "—" before any week has closed,
// never a claimed "0.0" for a season with nothing posted yet; once at
// least one week has closed, an unposted player sums as a real "0.0".
func (s *Service) SeasonPointsText(state PersistedState, player Player) string {
	if state.Schedule == nil {
		return "—"
	}
	source := s.weekStatsSource()
	values := s.currentScoringValues()
	key := playerStatKey(player)
	total := 0.0
	closedWeeks := 0
	for _, wk := range state.Schedule.Weeks {
		if !scheduleWeekIsFinal(wk) {
			continue
		}
		closedWeeks++
		if source == nil {
			continue
		}
		byKey := weekStatLinesByKey(source(wk.Week))
		if line, joined := byKey[key]; joined {
			total += scorePlayerStats(line.Stats, values)
		}
	}
	if closedWeeks == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f", total)
}

// applyWeeklyPointsText overwrites each row's playerMap-sourced "points"
// field (see weeklyPlayerPointsText's own doc comment) in place. rows is
// starterRowMaps' or playerMapsWithScoring's own []map[string]any output
// — every row carrying a player also carries "id" (playerMap's own key);
// a row with no "id" (an empty starting slot) is left untouched. players
// backs the id->Player lookup; passing a superset (the whole roster) is
// fine, since only rows whose "id" actually matches are ever touched.
func applyWeeklyPointsText(rows []map[string]any, players []Player, snapshot matchupStatsSnapshot, values map[string]float64, lineByKey map[string]WeekStatLine, now time.Time) {
	if len(rows) == 0 || len(players) == 0 {
		return
	}
	byID := make(map[string]Player, len(players))
	for _, p := range players {
		byID[p.ID] = p
	}
	for _, row := range rows {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		if player, ok := byID[id]; ok {
			row["points"] = weeklyPlayerPointsText(player, snapshot, values, lineByKey, now)
			row["points_breakdown"] = weeklyPlayerScoredBreakdown(player, snapshot, values, lineByKey)
		}
	}
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
		row.GameState = starterGameState(assignment.Player, week, snapshot, s.matchupLocation())
		row.Possession = starterPossessionLabel(assignment.Player, snapshot.live, snapshot.hasLive)
		if sourceErr != nil {
			row.JoinState = "stats-unavailable"
		} else if len(lines) == 0 {
			row.JoinState = "stats-empty"
		} else if line, joined := lineByKey[playerStatKey(assignment.Player)]; joined {
			row.Points = scorePlayerStats(line.Stats, values)
			row.Breakdown = ScoreBreakdownText(line.Stats, values)
			row.JoinState = "matched"
			row.Source = line.Source
			if row.Source == "" {
				row.Source = StatSourceLedger
			}
			total += row.Points
		} else {
			row.JoinState = "missing-join"
			if !starterGameKnownZeroSoFar(assignment.Player, week, snapshot, now) {
				complete = false
			}
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

// lineupHasProjectableStarter reports whether lineup carries at least one
// filled starting slot — the minimum TeamStartersProjectedTotal needs to
// mean anything (mirrors hasProjectableStarters' own gate for a
// []StarterLedgerRow, just against an EffectiveLineup instead).
func lineupHasProjectableStarter(lineup EffectiveLineup) bool {
	for _, slot := range lineup.Slots {
		if slot.HasPlayer {
			return true
		}
	}
	return false
}

// teamCurrentMatchupCard is /team's own scorebug summary (section-B item
// 1), replacing the lone "View matchup" button: opponent identity, both
// sides' starters-only projected total (TeamStartersProjectedTotal, the
// one canonical helper the stat strip, this card, and /matchups all
// agree on for the same team and week — projection.go), A6's win
// probability, and the week's next player lock — reusing deadline, the
// exact same LineupDeadlineView the stat strip's own "Locks ..." line
// renders, so the two facts never drift. A week with no published
// schedule, a bye, or an unpaired team returns has_matchup=false with a
// plain-language schedule_fact instead of a projection nobody can back
// yet.
func (s *Service) teamCurrentMatchupCard(state PersistedState, teamID string, week int, deadline LineupDeadlineView, projectionByID map[string]Player, projectionAvailable bool) map[string]any {
	out := map[string]any{
		"has_matchup":      false,
		"is_bye":           false,
		"opponent":         map[string]any{},
		"proj_mine":        "",
		"proj_theirs":      "",
		"win_prob":         "",
		"has_kickoff":      false,
		"kickoff_exact":    "",
		"kickoff_relative": "",
		"kickoff_timezone": "",
		"href":             matchupWeekHref(week),
		"schedule_fact":    "The schedule has not been published yet.",
	}
	if state.Schedule == nil || len(state.Schedule.Weeks) == 0 {
		return out
	}
	wk, ok := scheduleWeekByNumber(*state.Schedule, week)
	if !ok {
		out["schedule_fact"] = fmt.Sprintf("Week %d is not on the published schedule.", week)
		return out
	}
	if wk.ByeTeamID == teamID {
		out["is_bye"] = true
		out["schedule_fact"] = fmt.Sprintf("Week %d is a bye — no matchup this week.", week)
		return out
	}
	for _, m := range wk.Matchups {
		var opponentID string
		switch {
		case m.HomeTeamID == teamID:
			opponentID = m.AwayTeamID
		case m.AwayTeamID == teamID:
			opponentID = m.HomeTeamID
		default:
			continue
		}
		opponent := s.teamView(state, opponentID)
		mineLineup, _ := s.matchupLineup(state, teamID, week)
		theirsLineup, _ := s.matchupLineup(state, opponentID, week)
		// Keep the scorebug's projected values on the same matching-week
		// source as the Team stat strip. A raw roster snapshot can carry a
		// prior pool forecast after the source week changes; overlaying the
		// already-gated pool and requiring every filled starter to be known
		// prevents a stale or partial card total from contradicting the strip.
		mineLineup = lineupWithProjectionPool(mineLineup, projectionByID)
		theirsLineup = lineupWithProjectionPool(theirsLineup, projectionByID)
		mineProjected := TeamStartersProjectedTotal(mineLineup)
		theirsProjected := TeamStartersProjectedTotal(theirsLineup)
		mineHasProjection := projectionAvailable && TeamStartersProjectionKnown(mineLineup)
		theirsHasProjection := projectionAvailable && TeamStartersProjectionKnown(theirsLineup)
		out["has_matchup"] = true
		out["opponent"] = s.teamMap(opponent)
		// record (F27, J4 console gap-audit): teamMap's default "record"
		// is the static seed placeholder (model.go); currentTeamRecord
		// reads the same standings the standings table itself computes.
		out["opponent"].(map[string]any)["record"] = s.currentTeamRecord(state, opponentID)
		out["proj_mine"] = projectedText(mineProjected, mineHasProjection)
		out["proj_theirs"] = projectedText(theirsProjected, theirsHasProjection)
		winProbText := winProbabilityText(mineProjected, theirsProjected, mineHasProjection, theirsHasProjection)
		out["win_prob"] = winProbText
		out["has_win_prob"] = winProbText != winProbabilityDashText
		if deadline.HasDeadline {
			out["has_kickoff"] = true
			out["kickoff_exact"] = deadline.Exact
			out["kickoff_relative"] = deadline.Relative
			out["kickoff_timezone"] = FriendlyTimezoneLabel(deadline.Timezone)
		}
		return out
	}
	out["schedule_fact"] = fmt.Sprintf("No matchup is published for week %d.", week)
	return out
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

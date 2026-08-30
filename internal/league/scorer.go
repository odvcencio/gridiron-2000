package league

import (
	"fmt"
	"strings"
	"unicode"
)

// MatchupScorer computes one team's fantasy score for one NFL week.
// final=true means the inputs for that week can no longer change.
type MatchupScorer interface {
	TeamWeekScore(teamID string, week int) (points float64, final bool, err error)
}

// StatSource* names the origin of a WeekStatLine, and therefore of any
// StarterLedgerRow joined against it (A2, the live-scoring precedence
// rule): a live row wins while that player's game is in progress; a
// ledger row wins once the game is final, or when live has no data for
// it. livescore.MergeLines (Task 3) picks the winning line per player key
// using the poller's game status; this package only carries the label.
const (
	StatSourceLedger    = "ledger"     // nflverse weekly file
	StatSourceLive      = "live"       // Tank01 box score, game in progress
	StatSourceLiveFinal = "live-final" // Tank01 box score, game final, ledger not posted yet
)

// WeekStatLine is one player's weekly totals, keyed by scoring rule keys
// (passYards, rushTD, reception, ...). Key is the output of
// normalizePlayerKey (player name + position), the same shape
// openstats.NormalizePlayerKey produces — internal/league must not import
// internal/openstats, so main.go's WeekStatsSource adapter builds Key with
// the real function and this package keeps its own copy in lockstep; see
// scorer_test.go's parity test.
type WeekStatLine struct {
	Key    string
	Stats  map[string]float64
	Source string // one of the StatSource* values; "" reads as ledger
}

// WeekStatsSource supplies every player's stat line for one NFL week.
// main.go injects an adapter over internal/openstats, following the
// ScheduleSource / HistoricalSource pattern.
type WeekStatsSource func(week int) []WeekStatLine

// weekStatSourcesByKey indexes each WeekStatLine's Source by its player
// key, defaulting an empty Source to StatSourceLedger so a caller can look
// up a row's origin without re-checking the empty-string case at every
// call site.
func weekStatSourcesByKey(lines []WeekStatLine) map[string]string {
	out := make(map[string]string, len(lines))
	for _, line := range lines {
		if line.Source == "" {
			out[line.Key] = StatSourceLedger
			continue
		}
		out[line.Key] = line.Source
	}
	return out
}

// weekStatsSnapshot reads one weekly ledger for a scoring operation. The
// source is intentionally queried once by callers that render a whole
// matchup, so displayed starter rows and the aggregate total observe the
// same source slice.
func weekStatsSnapshot(source WeekStatsSource, week int) ([]WeekStatLine, bool, bool, string, error) {
	if source == nil {
		return nil, false, false, "unavailable", fmt.Errorf("no week stats source is configured")
	}
	lines := source(week)
	if len(lines) == 0 {
		return lines, false, false, "empty", nil
	}
	return lines, true, true, "available", nil
}

func weekStatsByKey(lines []WeekStatLine) map[string]map[string]float64 {
	byKey := make(map[string]map[string]float64, len(lines))
	for _, line := range lines {
		byKey[line.Key] = line.Stats
	}
	return byKey
}

// scorePlayerPoints is the single player-to-points calculation used by both
// MatchupScorer.TeamWeekScore and the explanatory starter ledger. Keeping the
// join and rule application here prevents a rendered row from drifting away
// from the team's aggregate score.
func scorePlayerPoints(player Player, byKey map[string]map[string]float64, values map[string]float64) (float64, bool) {
	line, ok := byKey[normalizePlayerKey(player.Name, player.Position)]
	if !ok {
		return 0, false
	}
	points := 0.0
	for ruleKey, statValue := range line {
		if !finiteScoringPoints(statValue) {
			continue
		}
		points += statValue * scoringPoints(values, ruleKey)
	}
	return points, true
}

func scorePlayers(players []Player, lines []WeekStatLine, values map[string]float64, week int, teamID string, onMiss func(JoinMiss)) float64 {
	byKey := weekStatsByKey(lines)
	total := 0.0
	for _, player := range players {
		points, joined := scorePlayerPoints(player, byKey, values)
		if !joined {
			if onMiss != nil {
				onMiss(JoinMiss{Week: week, TeamID: teamID, PlayerID: player.ID, PlayerName: player.Name, Position: player.Position})
			}
			continue
		}
		total += points
	}
	return total
}

// SetWeekStatsSource attaches the weekly stats feed. Call it once during
// startup, before the server accepts requests.
func (s *Service) SetWeekStatsSource(fn WeekStatsSource) {
	s.poolMu.Lock()
	s.weekStatsFn = fn
	s.poolMu.Unlock()
}

// weekStatsSource returns the current weekly stats feed, or nil when none
// is attached.
func (s *Service) weekStatsSource() WeekStatsSource {
	s.poolMu.Lock()
	fn := s.weekStatsFn
	s.poolMu.Unlock()
	return fn
}

// JoinMiss is one rostered player whose normalized name+position found no
// matching stat line for a scored week. It always scores zero; season.go's
// week-close path collects these so the commissioner can see them (section
// 2.4: "the week-close report lists misses").
type JoinMiss struct {
	Week       int    `json:"week"`
	TeamID     string `json:"teamId"`
	PlayerID   string `json:"playerId"`
	PlayerName string `json:"playerName"`
	Position   string `json:"position"`
}

// normalizePlayerKey mirrors openstats.NormalizePlayerKey exactly:
// lowercase letters/digits only, then "|", then the upper-cased position.
// internal/league must not import internal/openstats (see WeekStatLine's
// doc comment), so this is a deliberate, tested duplicate — keep it in
// lockstep; scorer_test.go asserts parity directly against the openstats
// implementation on a range of names.
func normalizePlayerKey(name, position string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	b.WriteByte('|')
	b.WriteString(strings.ToUpper(strings.TrimSpace(position)))
	return b.String()
}

// rosterTotalScorer is the v1 MatchupScorer (section 2.4): score the whole
// roster, no lineup — the sum over every rostered player of (weekly stat
// line x league scoring values). A join miss (no stat line for the
// player's normalized key) scores zero and is reported via onMiss, when
// set.
type rosterTotalScorer struct {
	// roster returns teamID's scorable players. In the inaugural season
	// this is the team's draft picks (see Service.rosterForTeam); after a
	// dynasty rollover it becomes the persisted Rosters set (section 5.4,
	// out of scope for this work package).
	roster func(teamID string) []Player
	stats  WeekStatsSource
	values func() map[string]float64
	onMiss func(JoinMiss)
}

// newRosterTotalScorer builds a rosterTotalScorer. onMiss may be nil.
func newRosterTotalScorer(roster func(teamID string) []Player, stats WeekStatsSource, values func() map[string]float64, onMiss func(JoinMiss)) *rosterTotalScorer {
	return &rosterTotalScorer{roster: roster, stats: stats, values: values, onMiss: onMiss}
}

// TeamWeekScore implements MatchupScorer. final reports whether the wired
// WeekStatsSource returned any stat lines at all for week: v1 has no
// per-player in-progress tracking, so "the ledger has data for this week"
// is the finality signal. The two-condition auto-close check
// (season.go's WeekCloseReady) is the authoritative "is this week done"
// gate; this flag is advisory.
func (r *rosterTotalScorer) TeamWeekScore(teamID string, week int) (points float64, final bool, err error) {
	lines, final, _, _, err := weekStatsSnapshot(r.stats, week)
	if err != nil {
		return 0, false, err
	}
	values := map[string]float64{}
	if r.values != nil {
		values = r.values()
	}
	return scorePlayers(r.roster(teamID), lines, values, week, teamID, r.onMiss), final, nil
}

// lineupScorer is the roster-ops spec's lineupScorer (section 4.6):
// TeamWeekScore counts only the effective lineup's starters for the given
// week — a bench player never contributes to the matchup total.
//
// Closed-week short-circuit (WP-R2, section 4.6 step 1). starters resolves
// through Service.lineupStarters, which now honors the materialize-at-close
// pin: once week's matchups are final (season.go's closeWeek sets that flag
// in the same persist that writes the pin), starters reads the frozen
// PersistedState.Lineups[teamID][week] map directly against the whole
// player pool, bypassing effectiveLineup's current-roster filter entirely.
// That bypass is the point of the pin — see pinnedStarters' doc comment for
// why the spec's literal "stored ⇒ closed" reading is not quite right and
// what signal this implementation uses instead. For an open (not yet
// final) week, starters still resolves live via effectiveLineup, exactly
// as before.
type lineupScorer struct {
	// starters returns teamID's resolved starting lineup for week (a
	// service-layer effectiveLineup call, players only, bench excluded).
	starters func(teamID string, week int) []Player
	stats    WeekStatsSource
	values   func() map[string]float64
	onMiss   func(JoinMiss)
}

// newLineupScorer builds a lineupScorer. onMiss may be nil.
func newLineupScorer(starters func(teamID string, week int) []Player, stats WeekStatsSource, values func() map[string]float64, onMiss func(JoinMiss)) *lineupScorer {
	return &lineupScorer{starters: starters, stats: stats, values: values, onMiss: onMiss}
}

// TeamWeekScore implements MatchupScorer, scoped to starters only (roster-
// ops spec section 4.6 step 2): the same WeekStatsSource join
// rosterTotalScorer uses, but over teamID's resolved starting lineup
// instead of its whole roster. Bench players, empty slots, and bye slots
// with no eligible replacement all contribute zero, because they are
// simply absent from starters' result. final keeps rosterTotalScorer's
// advisory semantics (see that type's TeamWeekScore doc comment).
func (l *lineupScorer) TeamWeekScore(teamID string, week int) (points float64, final bool, err error) {
	lines, final, _, _, err := weekStatsSnapshot(l.stats, week)
	if err != nil {
		return 0, false, err
	}
	values := map[string]float64{}
	if l.values != nil {
		values = l.values()
	}
	return scorePlayers(l.starters(teamID, week), lines, values, week, teamID, l.onMiss), final, nil
}

// lineupStarters resolves teamID's effective-lineup starters for week
// against state: every slot's assigned player (explicit or auto-filled),
// bench excluded — the func lineupScorer.starters wires into
// Service.matchupScorer. It defers to the closed-week pin (pinnedStarters)
// first; only an open week falls through to the live effectiveLineup
// resolution.
func (s *Service) lineupStarters(state PersistedState, teamID string, week int) []Player {
	if pinned, ok := s.pinnedStarters(state, teamID, week); ok {
		return pinned
	}
	preset := CurrentRoster()
	roster, _ := s.rosterForTeam(state, teamID)
	// Zone occupants (RESERVE, IR) never score: the SK spec's "scorer
	// exclusion of zone occupants" rule. general is the same
	// splitRosterZones filter TeamData and the lineup actions use.
	general, _, _ := splitRosterZones(state, teamID, roster)
	games := s.schedule()
	now := s.clock()
	lineup := effectiveLineup(preset, general, state.Lineups[teamID], week, games, now)
	starters := make([]Player, 0, len(lineup.Slots))
	for _, assignment := range lineup.Slots {
		if assignment.HasPlayer {
			starters = append(starters, assignment.Player)
		}
	}
	return starters
}

// pinnedStarters resolves teamID's frozen starters for week directly from
// the materialize-at-close pin (state.Lineups[teamID][week]), when week is
// closed. ok is false for an open week, so lineupStarters falls through to
// the live effectiveLineup resolution.
//
// Reconciliation (report at hand-off): section 4.6 step 1's literal text
// reads "Lineups[teamID][week] when stored ... otherwise effectiveLineup"
// — stored presence alone as the closed-week signal. That is not quite
// right: the section 4.7 lineup-auto action (autoFillWeek's doc comment,
// lineup.go) already writes a *complete* stored map for the *current,
// still-open* week too — so "stored" cannot mean "closed" by itself.
// Treating it as if it did would break exactly the case section 6.3
// requires: a trade in the still-open current week must still remove the
// traded player from that week's live lineup. This implementation instead
// gates on weekIsFinalInSchedule — the schedule's own Final flag, set by
// closeWeek in the very same persist that writes the pin (section 4.2's
// "in the same persist that marks the week's matchups final"), which is
// both the literal "existing week-close/finality machinery" this work
// package's brief points at and the one signal that correctly separates a
// frozen week from a live one.
//
// Once gated closed, the pin resolves player identity from the whole
// player pool (Service.pool, keyed by ID), not from currentRosters — that
// bypass is the entire point: a later drop, trade, or roster-shape edit
// removes the player from the *current* roster, and effectiveLineup's
// step-3 "no longer rostered" rule would otherwise silently re-auto-fill
// the slot from the *new* roster, changing a closed week's score after the
// fact (the roster-shape-editor / trade churn this pin exists to survive).
// A player ID that no longer resolves in the pool at all (should not
// happen — the pool only grows) is skipped, scoring zero for that slot,
// the same fail-quiet posture as a plain join miss.
func (s *Service) pinnedStarters(state PersistedState, teamID string, week int) ([]Player, bool) {
	if !weekIsFinalInSchedule(state.Schedule, week) {
		return nil, false
	}
	slots := state.Lineups[teamID][week]
	if len(slots) == 0 {
		return []Player{}, true // week is closed; this team pinned no starters.
	}
	pool := s.pool()
	starters := make([]Player, 0, len(slots))
	for _, playerID := range slots {
		if player, ok := pool.byID[playerID]; ok {
			starters = append(starters, player)
		}
	}
	return starters, true
}

// weekIsFinalInSchedule reports whether week's matchups are already all
// final in sch — the authoritative "this week is closed" signal (season.go's
// closeWeek sets it, in the same persist as the materialize-at-close pin).
// A nil schedule, or a week absent from it, is never closed.
func weekIsFinalInSchedule(sch *SeasonSchedule, week int) bool {
	if sch == nil {
		return false
	}
	for _, wk := range sch.Weeks {
		if wk.Week == week {
			return matchupsAllFinal(wk.Matchups)
		}
	}
	return false
}

// matchupScorer builds the wired v1 MatchupScorer for one call (a week
// close, or one live-feed snapshot): it snapshots store state once, so a
// whole week's worth of TeamWeekScore calls share one consistent roster
// view. misses, when non-nil, is appended to for every join miss found.
//
// This constructs lineupScorer, not rosterTotalScorer — the scorer-scoping
// swap this work package's brief calls for (roster-ops spec section 4.6:
// "the seam change is one constructor call"). rosterTotalScorer stays in
// this file with its own tests; nothing binds it after this swap.
func (s *Service) matchupScorer(misses *[]JoinMiss) MatchupScorer {
	state := s.store.Snapshot()
	var onMiss func(JoinMiss)
	if misses != nil {
		onMiss = func(m JoinMiss) { *misses = append(*misses, m) }
	}
	return newLineupScorer(
		func(teamID string, week int) []Player {
			return s.lineupStarters(state, teamID, week)
		},
		s.weekStatsSource(),
		s.currentScoringValues,
		onMiss,
	)
}

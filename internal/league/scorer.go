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

// WeekStatLine is one player's weekly totals, keyed by scoring rule keys
// (passYards, rushTD, reception, ...). Key is the output of
// normalizePlayerKey (player name + position), the same shape
// openstats.NormalizePlayerKey produces — internal/league must not import
// internal/openstats, so main.go's WeekStatsSource adapter builds Key with
// the real function and this package keeps its own copy in lockstep; see
// scorer_test.go's parity test.
type WeekStatLine struct {
	Key   string
	Stats map[string]float64
}

// WeekStatsSource supplies every player's stat line for one NFL week.
// main.go injects an adapter over internal/openstats, following the
// ScheduleSource / HistoricalSource pattern.
type WeekStatsSource func(week int) []WeekStatLine

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
	if r.stats == nil {
		return 0, false, fmt.Errorf("no week stats source is configured")
	}
	lines := r.stats(week)
	final = len(lines) > 0
	byKey := make(map[string]map[string]float64, len(lines))
	for _, line := range lines {
		byKey[line.Key] = line.Stats
	}
	values := map[string]float64{}
	if r.values != nil {
		values = r.values()
	}
	roster := r.roster(teamID)
	total := 0.0
	for _, player := range roster {
		key := normalizePlayerKey(player.Name, player.Position)
		line, ok := byKey[key]
		if !ok {
			if r.onMiss != nil {
				r.onMiss(JoinMiss{Week: week, TeamID: teamID, PlayerID: player.ID, PlayerName: player.Name, Position: player.Position})
			}
			continue
		}
		for ruleKey, statValue := range line {
			total += statValue * values[ruleKey]
		}
	}
	return total, final, nil
}

// lineupScorer is the roster-ops spec's lineupScorer (section 4.6),
// wired early per this work package's explicit scorer-scoping brief:
// TeamWeekScore counts only the effective lineup's starters for the given
// week — a bench player never contributes to the matchup total. Section
// 4.6's step 1 ("Lineups[teamID][week] when stored ... otherwise
// effectiveLineup") collapses to "always effectiveLineup" here: the
// materialize-at-close pin (spec section 4.2/13, WP-R2) is explicitly out
// of this work package's scope, so nothing ever writes a full closed-week
// snapshot into Lineups for this scorer to prefer — effectiveLineup
// already returns exactly that value (the stored explicit week, gap-filled
// by auto-fill) for both an open and a closed week, so scoring is
// unaffected once the pin lands and starts short-circuiting the same read.
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
	if l.stats == nil {
		return 0, false, fmt.Errorf("no week stats source is configured")
	}
	lines := l.stats(week)
	final = len(lines) > 0
	byKey := make(map[string]map[string]float64, len(lines))
	for _, line := range lines {
		byKey[line.Key] = line.Stats
	}
	values := map[string]float64{}
	if l.values != nil {
		values = l.values()
	}
	starters := l.starters(teamID, week)
	total := 0.0
	for _, player := range starters {
		key := normalizePlayerKey(player.Name, player.Position)
		line, ok := byKey[key]
		if !ok {
			if l.onMiss != nil {
				l.onMiss(JoinMiss{Week: week, TeamID: teamID, PlayerID: player.ID, PlayerName: player.Name, Position: player.Position})
			}
			continue
		}
		for ruleKey, statValue := range line {
			total += statValue * values[ruleKey]
		}
	}
	return total, final, nil
}

// lineupStarters resolves teamID's effective-lineup starters for week
// against state: every slot's assigned player (explicit or auto-filled),
// bench excluded — the func lineupScorer.starters wires into
// Service.matchupScorer.
func (s *Service) lineupStarters(state PersistedState, teamID string, week int) []Player {
	preset := CurrentRoster()
	roster, _ := s.rosterForTeam(state, teamID)
	games := s.schedule()
	now := s.clock()
	lineup := effectiveLineup(preset, roster, state.Lineups[teamID], week, games, now)
	starters := make([]Player, 0, len(lineup.Slots))
	for _, assignment := range lineup.Slots {
		if assignment.HasPlayer {
			starters = append(starters, assignment.Player)
		}
	}
	return starters
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

package league

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"time"
)

// LeagueMatchup is one scheduled fantasy pairing. Scores fill in when the
// week closes (see season.go).
type LeagueMatchup struct {
	ID         string  `json:"id"` // "2026-w03-team-1-team-6"
	Week       int     `json:"week"`
	HomeTeamID string  `json:"homeTeamId"`
	AwayTeamID string  `json:"awayTeamId"`
	HomeScore  float64 `json:"homeScore"`
	AwayScore  float64 `json:"awayScore"`
	Final      bool    `json:"final"`
}

// ScheduleWeek is one league week: its matchups and the bye team, if any.
type ScheduleWeek struct {
	Week      int             `json:"week"`
	Matchups  []LeagueMatchup `json:"matchups"`
	ByeTeamID string          `json:"byeTeamId,omitempty"`
	// ClosedAt is the wall-clock instant season.go's closeWeek actually
	// scored and finalized this week (2026-08-30 review round 2, finding
	// 2): the true settlement instant, not merely the last game's kickoff.
	// closeWeek stamps it once, from its own clock, the moment it commits
	// this week as final; it never changes after that. Zero on a week
	// that has not closed yet, and on a row written before this field
	// existed — waiverPenaltyBoundary treats a zero ClosedAt as a legacy
	// row and falls back to the last-kickoff estimate for it.
	ClosedAt time.Time `json:"closedAt"`
}

// SeasonSchedule is the persisted regular-season plan plus results.
type SeasonSchedule struct {
	Season      int            `json:"season"`
	Seed        int64          `json:"seed"`
	GeneratedAt time.Time      `json:"generatedAt"`
	StartWeek   int            `json:"startWeek"` // NFL week of league week 1
	Weeks       []ScheduleWeek `json:"weeks"`
}

// ScheduleParams configures one call to GenerateSchedule.
type ScheduleParams struct {
	Season    int
	TeamIDs   []string          // config order, 4-14 entries
	Divisions map[string]string // teamID -> division name; empty = none
	StartWeek int               // first NFL week, default 1
	Weeks     int               // regular-season length, >= 1
	Seed      int64
}

// defaultSeasonStartWeek is the honest fallback before a commissioner has
// published a schedule. league.json currently carries the season opening
// instant, not an NFL week number, so an unconfigured or pre-schedule league
// starts from the platform's neutral NFL week-one convention. Once a
// persisted schedule exists, seasonStartWeekForSchedule is authoritative.
const defaultSeasonStartWeek = 1

// seasonStartWeekForSchedule resolves the first NFL week represented by a
// published schedule. StartWeek is the durable header; the first positive
// week is a defensive fallback for older or partially-written state that has
// weeks but no header. A nil/empty schedule deliberately falls back to the
// neutral configured default instead of inventing a week from today's date.
func seasonStartWeekForSchedule(schedule *SeasonSchedule) int {
	if schedule != nil {
		if schedule.StartWeek > 0 {
			return schedule.StartWeek
		}
		start := 0
		for _, week := range schedule.Weeks {
			if week.Week > 0 && (start == 0 || week.Week < start) {
				start = week.Week
			}
		}
		if start > 0 {
			return start
		}
	}
	return defaultSeasonStartWeek
}

// earliestKickoffForWeek returns the earliest real NFL kickoff among
// week's games in the attached schedule source (s.schedule(), the live
// NFL game feed — distinct from the persisted fantasy SeasonSchedule
// above), when at least one game for that week carries a known kickoff.
// Both /matchups' weekly masthead (feed.go's weekState) and /scoring's
// season-start derivation (scheduleWeek1Kickoff, scoring.go) share this
// one lookup, so neither page can compute a different answer for the
// same week (F29, 2026-09-04 UX pass).
func (s *Service) earliestKickoffForWeek(week int) (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, game := range s.schedule() {
		if game.Week != week || game.Kickoff.IsZero() {
			continue
		}
		if !found || game.Kickoff.Before(earliest) {
			earliest = game.Kickoff
			found = true
		}
	}
	return earliest, found
}

// maxNFLWeek is the latest NFL week a schedule may reach (section 7,
// season.finalWeek's validated maximum).
const maxNFLWeek = 18

// byeSentinel is the virtual ring slot an odd team count pairs against. It
// never appears in an emitted LeagueMatchup; see ScheduleWeek.ByeTeamID.
const byeSentinel = "BYE"

// GenerateSchedule builds a deterministic, seeded round-robin schedule: a
// pure function with no store access and no clock access (section 2.2). The
// same params always yield byte-identical output, GeneratedAt included
// (this function never sets it — see AdminGenerateSchedule, which stamps it
// from the caller's clock after generation).
func GenerateSchedule(p ScheduleParams) (SeasonSchedule, error) {
	if err := validateScheduleParams(p); err != nil {
		return SeasonSchedule{}, err
	}
	startWeek := p.StartWeek
	if startWeek == 0 {
		startWeek = 1
	}

	// Step 2: seed a PCG source and shuffle a copy of TeamIDs into the base
	// ring. The stored seed makes the schedule stable across restarts and
	// reproducible in tests.
	ring := append([]string(nil), p.TeamIDs...)
	rng := rand.New(rand.NewPCG(uint64(p.Seed), 0))
	rng.Shuffle(len(ring), func(i, j int) { ring[i], ring[j] = ring[j], ring[i] })

	// Step 3: append the BYE sentinel for an odd team count so the ring size
	// is even, then run the circle method for n-1 rounds (one full cycle).
	if len(ring)%2 == 1 {
		ring = append(ring, byeSentinel)
	}
	n := len(ring)
	numRounds := n - 1
	rounds := circleMethodRounds(ring)

	// Step 4: divisional weighting selects which rounds fill the trailing,
	// less-than-a-full-cycle stretch of weeks, when Divisions is set.
	weekRounds := weekRoundSequence(p.Weeks, numRounds, rounds, p.Divisions)

	sched := SeasonSchedule{
		Season:    p.Season,
		Seed:      p.Seed,
		StartWeek: startWeek,
		Weeks:     make([]ScheduleWeek, 0, p.Weeks),
	}
	// Step 6: track a per-pair meeting count for deterministic home/away
	// alternation across repeat meetings.
	meetingCount := map[string]int{}
	for w := 0; w < p.Weeks; w++ {
		roundIndex := weekRounds[w]
		nflWeek := startWeek + w
		week := ScheduleWeek{Week: nflWeek}
		for _, pair := range rounds[roundIndex] {
			a, b := pair[0], pair[1]
			if a == byeSentinel {
				week.ByeTeamID = b
				continue
			}
			if b == byeSentinel {
				week.ByeTeamID = a
				continue
			}
			home, away := resolveHomeAway(a, b, meetingCount)
			week.Matchups = append(week.Matchups, LeagueMatchup{
				ID:         matchupID(p.Season, nflWeek, home, away),
				Week:       nflWeek,
				HomeTeamID: home,
				AwayTeamID: away,
			})
		}
		sort.Slice(week.Matchups, func(i, j int) bool { return week.Matchups[i].ID < week.Matchups[j].ID })
		sched.Weeks = append(sched.Weeks, week)
	}
	return sched, nil
}

func validateScheduleParams(p ScheduleParams) error {
	if len(p.TeamIDs) < 4 || len(p.TeamIDs) > 14 {
		return fmt.Errorf("a schedule needs 4 to 14 teams, got %d", len(p.TeamIDs))
	}
	seen := make(map[string]bool, len(p.TeamIDs))
	for _, id := range p.TeamIDs {
		if id == "" {
			return fmt.Errorf("team IDs may not be empty")
		}
		if id == byeSentinel {
			return fmt.Errorf("team IDs may not be named %q", byeSentinel)
		}
		if seen[id] {
			return fmt.Errorf("team ID %q appears more than once", id)
		}
		seen[id] = true
	}
	if p.Weeks < 1 {
		return fmt.Errorf("a schedule needs at least one week")
	}
	startWeek := p.StartWeek
	if startWeek == 0 {
		startWeek = 1
	}
	if startWeek < 1 {
		return fmt.Errorf("startWeek must be 1 or greater")
	}
	if startWeek+p.Weeks-1 > maxNFLWeek {
		return fmt.Errorf("startWeek %d plus %d weeks runs past week %d", startWeek, p.Weeks, maxNFLWeek)
	}
	return nil
}

// seasonStartWeek is the one service-level season-opening-week authority used
// by preseason copy, week fallback, and page view models. A published state
// schedule wins; before publication the neutral platform default is explicit.
func (s *Service) seasonStartWeek() int {
	if s == nil || s.store == nil {
		return defaultSeasonStartWeek
	}
	return seasonStartWeekForSchedule(s.store.Snapshot().Schedule)
}

// pairing is one round's matchup: two ring slots, either of which may be
// byeSentinel.
type pairing = [2]string

// circleMethodRounds runs the standard round-robin "circle method" over
// ring (already BYE-padded to an even length): slot 0 stays fixed and the
// remaining slots rotate one position each round, for n-1 rounds. Every
// pair meets exactly once across the returned rounds (section 1.4, 2.2).
func circleMethodRounds(ring []string) [][]pairing {
	n := len(ring)
	arr := append([]string(nil), ring...)
	rounds := make([][]pairing, n-1)
	for round := 0; round < n-1; round++ {
		pairs := make([]pairing, 0, n/2)
		for i := 0; i < n/2; i++ {
			pairs = append(pairs, pairing{arr[i], arr[n-1-i]})
		}
		rounds[round] = pairs
		// Rotate: keep arr[0] fixed, shift arr[1:] one position, wrapping
		// the last element back to index 1.
		last := arr[n-1]
		for i := n - 1; i > 1; i-- {
			arr[i] = arr[i-1]
		}
		arr[1] = last
	}
	return rounds
}

// weekRoundSequence maps each week (0-based) to the round index that fills
// it. Full cycles (numRounds weeks each) always use the raw circle order,
// which preserves the every-pair-once-per-cycle guarantee. A trailing,
// less-than-a-full-cycle stretch of weeks — divisions is non-empty and
// there is a remainder — instead selects the `remainder` rounds with the
// highest intra-division pairing count, ties broken by round index
// ascending (section 2.2 step 4).
func weekRoundSequence(weeks, numRounds int, rounds [][]pairing, divisions map[string]string) []int {
	out := make([]int, weeks)
	remainder := weeks % numRounds
	fullWeeks := weeks - remainder
	for w := 0; w < fullWeeks; w++ {
		out[w] = w % numRounds
	}
	if remainder == 0 {
		return out
	}
	if len(divisions) == 0 {
		for i := 0; i < remainder; i++ {
			out[fullWeeks+i] = i % numRounds
		}
		return out
	}
	type roundRank struct {
		index, count int
	}
	ranks := make([]roundRank, numRounds)
	for i, round := range rounds {
		ranks[i] = roundRank{index: i, count: intraDivisionCount(round, divisions)}
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].count != ranks[j].count {
			return ranks[i].count > ranks[j].count
		}
		return ranks[i].index < ranks[j].index
	})
	for i := 0; i < remainder; i++ {
		out[fullWeeks+i] = ranks[i].index
	}
	return out
}

// intraDivisionCount counts round's pairings where both sides are real
// teams (not the bye) sharing a non-empty division.
func intraDivisionCount(round []pairing, divisions map[string]string) int {
	count := 0
	for _, pair := range round {
		a, b := pair[0], pair[1]
		if a == byeSentinel || b == byeSentinel {
			continue
		}
		da, db := divisions[a], divisions[b]
		if da != "" && da == db {
			count++
		}
	}
	return count
}

// resolveHomeAway assigns home/away for one pairing and records the
// meeting in counts. Section 2.2 step 6: on an even meeting count the
// lexicographically lower team ID is home; on an odd count it is away, so
// repeat meetings alternate venue deterministically.
func resolveHomeAway(a, b string, counts map[string]int) (home, away string) {
	lo, hi := a, b
	if hi < lo {
		lo, hi = hi, lo
	}
	key := lo + "|" + hi
	meeting := counts[key]
	counts[key] = meeting + 1
	if meeting%2 == 0 {
		return lo, hi
	}
	return hi, lo
}

func matchupID(season, week int, home, away string) string {
	return fmt.Sprintf("%d-w%02d-%s-%s", season, week, home, away)
}

// cloneSchedule deep-copies sch (nil-safe), matching the store's
// snapshot/clone discipline: callers must never observe or mutate the
// Store's internal slices through a Snapshot() result.
func cloneSchedule(sch *SeasonSchedule) *SeasonSchedule {
	if sch == nil {
		return nil
	}
	out := *sch
	out.Weeks = make([]ScheduleWeek, len(sch.Weeks))
	for i, wk := range sch.Weeks {
		out.Weeks[i] = wk
		out.Weeks[i].Matchups = append([]LeagueMatchup(nil), wk.Matchups...)
	}
	return &out
}

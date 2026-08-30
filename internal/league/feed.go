package league

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type scoreProvider interface {
	Snapshot(context.Context, time.Time) (LiveSnapshot, error)
}

type liveFeed struct {
	mu       sync.Mutex
	provider scoreProvider
	fallback scoreProvider
	cacheFor time.Duration
	cachedAt time.Time
	cached   LiveSnapshot
	// version and cachedVersion key the cache by the live poller's
	// version (Task 4), on top of cacheFor: a poller tick that changes a
	// score must not wait out the 45 s window before Snapshot notices.
	// nil version means live scoring is not wired, so the cache behaves
	// exactly as before (age only).
	version       func() int64
	cachedVersion int64
}

func newLiveFeed(provider scoreProvider) *liveFeed {
	demo := demoProvider{}
	if provider == nil {
		provider = demo
	}
	return &liveFeed{
		provider: provider,
		fallback: demo,
		cacheFor: 45 * time.Second,
	}
}

// setVersionSource attaches the live poller's version accessor so Snapshot
// can key its cache by version as well as age; see SetLiveVersionSource.
func (f *liveFeed) setVersionSource(fn func() int64) {
	f.mu.Lock()
	f.version = fn
	f.mu.Unlock()
}

func (f *liveFeed) Snapshot(ctx context.Context, now time.Time) LiveSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	current := int64(0)
	if f.version != nil {
		current = f.version()
	}
	if !f.cachedAt.IsZero() && now.Sub(f.cachedAt) < f.cacheFor && f.cachedVersion == current {
		return f.cached
	}
	snapshot, err := f.provider.Snapshot(ctx, now)
	if err != nil {
		snapshot, _ = f.fallback.Snapshot(ctx, now)
		snapshot.Source = "fallback"
		snapshot.SourceLabel = "Backup scoreboard"
		snapshot.State = MatchupStateDegraded
		snapshot.Status = "Matchup data is temporarily unavailable"
		snapshot.Warning = "Live scoring is down. The backup scoreboard is showing."
	}
	snapshot.OK = true
	if snapshot.CheckedAt.IsZero() {
		snapshot.CheckedAt = now.UTC()
	}
	if snapshot.LastUpdated.IsZero() {
		// Keep the legacy field populated for clients that have not migrated to
		// the explicit checked/ledger freshness fields yet.
		snapshot.LastUpdated = snapshot.CheckedAt
	}
	snapshot.RefreshAfterSeconds = int(DefaultRefreshPeriod.Seconds())
	f.cached, f.cachedAt, f.cachedVersion = snapshot, now, current
	return snapshot
}

// scheduleProvider reads the current week's matchups from the persisted
// schedule and fills scores from the wired MatchupScorer (section 2.5).
// Before a schedule exists it defers straight to demoProvider's honest
// preseason snapshot, so the LiveSnapshot contract's meaning never changes
// — only its source does, once a schedule is generated.
type scheduleProvider struct {
	svc *Service
}

func (p scheduleProvider) preseasonProvider() demoProvider {
	startAt := p.svc.cfg.SeasonStartAt
	if startAt.IsZero() {
		startAt, _ = time.Parse(time.RFC3339, DefaultSeasonStartAt)
	}
	return demoProvider{
		startWeek: p.svc.seasonStartWeek(),
		startAt:   startAt,
	}
}

func (p scheduleProvider) Snapshot(ctx context.Context, now time.Time) (LiveSnapshot, error) {
	state := p.svc.store.Snapshot()
	if state.Schedule == nil || len(state.Schedule.Weeks) == 0 {
		return p.preseasonProvider().Snapshot(ctx, now)
	}
	return p.SnapshotWeek(ctx, now, currentScheduleWeek(*state.Schedule))
}

// SnapshotWeek reads one persisted fantasy week while preserving the same
// schedule/scoring state taxonomy as Snapshot. The current-week live feed
// still uses Snapshot through liveFeed; MatchupsData calls this explicit path
// only for a selected historical or future week so a week browser never
// silently falls back to the current week.
func (p scheduleProvider) SnapshotWeek(ctx context.Context, now time.Time, week int) (LiveSnapshot, error) {
	state := p.svc.store.Snapshot()
	if state.Schedule == nil || len(state.Schedule.Weeks) == 0 {
		return p.preseasonProvider().Snapshot(ctx, now)
	}
	wk, ok := scheduleWeekByNumber(*state.Schedule, week)
	if !ok {
		return LiveSnapshot{}, fmt.Errorf("week %d is not in the persisted season schedule", week)
	}
	stateLabel, statusLabel, clockLabel := p.weekState(week, wk.Matchups, now)
	// One page render must use one authoritative weekly stat slice. Taking the
	// snapshot before walking matchups keeps both ledgers, their aggregate
	// scores, and every matchup on the page on the same source read/timestamp.
	weeklyStats := p.svc.matchupStatsSnapshot(week)
	matchups := make([]ScoreMatchup, 0, len(wk.Matchups))
	for _, m := range wk.Matchups {
		homeLedger := p.svc.teamWeekLedgerFromSnapshot(state, m.HomeTeamID, week, weeklyStats)
		awayLedger := p.svc.teamWeekLedgerFromSnapshot(state, m.AwayTeamID, week, weeklyStats)
		homeScore, awayScore := m.HomeScore, m.AwayScore
		matchupState := MatchupStateFinal
		status := "Final"
		clock := "FINAL"
		if !m.Final {
			matchupState = stateLabel
			status = matchupStateCardLabel(stateLabel)
			clock = clockLabel
			if stateLabel == MatchupStateInProgress || stateLabel == MatchupStateDegraded {
				homeScore = homeLedger.Total
				awayScore = awayLedger.Total
			}
		}
		home := p.svc.teamByID(m.HomeTeamID)
		away := p.svc.teamByID(m.AwayTeamID)
		homeScoreTeam := scoreTeamFromLedger(home, homeLedger)
		awayScoreTeam := scoreTeamFromLedger(away, awayLedger)
		// A closed week always keeps its persisted posted total authoritative.
		// The current starter ledger remains visible below it; if a later source
		// correction differs, applyPostedFinalScore labels that delta explicitly.
		if m.Final {
			homeScoreTeam.applyPostedFinalScore(homeScore)
			awayScoreTeam.applyPostedFinalScore(awayScore)
		}
		if !m.Final && !homeLedger.Known {
			homeScoreTeam.Score = homeScore
		}
		if !m.Final && !awayLedger.Known {
			awayScoreTeam.Score = awayScore
		}
		matchups = append(matchups, ScoreMatchup{
			ID:     m.ID,
			Home:   homeScoreTeam,
			Away:   awayScoreTeam,
			State:  matchupState,
			Status: status,
			Clock:  clock,
		})
	}
	statsUpdatedAt := p.svc.statsUpdatedAt()
	lastUpdated := statsUpdatedAt
	if lastUpdated.IsZero() {
		// A missing freshness source cannot be invented. Keep the legacy field
		// useful as a checked instant while the explicit StatsUpdatedAt field
		// remains zero and the UI says "Unavailable".
		lastUpdated = now.UTC()
	}
	warning := ""
	if stateLabel == MatchupStateInProgress && p.svc.weekStatsSource() == nil {
		warning = "Player-stat source is unavailable; live totals are not authoritative."
	}
	return LiveSnapshot{
		Source:         "league-schedule",
		SourceLabel:    "League matchups",
		Week:           week,
		WeekLabel:      fmt.Sprintf("Week %d", week),
		State:          stateLabel,
		Status:         statusLabel,
		LastUpdated:    lastUpdated.UTC(),
		CheckedAt:      now.UTC(),
		StatsUpdatedAt: statsUpdatedAt.UTC(),
		Matchups:       matchups,
		Warning:        warning,
	}, nil
}

func (p scheduleProvider) weekState(week int, matchups []LeagueMatchup, now time.Time) (state, status, clock string) {
	allFinal := len(matchups) > 0
	for _, matchup := range matchups {
		if !matchup.Final {
			allFinal = false
			break
		}
	}
	if allFinal {
		return MatchupStateFinal, fmt.Sprintf("Week %d results are final", week), "FINAL"
	}
	var weekGames []GameInfo
	for _, game := range p.svc.schedule() {
		if game.Week == week && !game.Kickoff.IsZero() {
			weekGames = append(weekGames, game)
		}
	}
	if len(weekGames) == 0 {
		return MatchupStateDegraded, "Schedule loaded; kickoff timing is unavailable", "TIMING UNAVAILABLE"
	}
	earliest := weekGames[0].Kickoff
	weekStarted := false
	allNFLFinal := true
	for _, game := range weekGames {
		if game.Kickoff.Before(earliest) {
			earliest = game.Kickoff
		}
		if !now.Before(game.Kickoff) {
			weekStarted = true
		}
		if !game.Final {
			allNFLFinal = false
		}
	}
	location := p.svc.matchupLocation()
	if !weekStarted {
		kickoff := earliest.In(location).Format("Mon Jan 2 · 3:04 PM MST")
		return MatchupStateScheduled, "Fantasy scoring begins " + kickoff, kickoff
	}
	if allNFLFinal {
		return MatchupStateDegraded, "NFL games are final; fantasy results await week close", "AWAITING CLOSE"
	}
	return MatchupStateInProgress, "Fantasy scoring is in progress", DefaultMatchupClockLabel
}

func matchupStateCardLabel(state string) string {
	switch state {
	case MatchupStateScheduled:
		return "Scheduled"
	case MatchupStateInProgress:
		return "In progress"
	case MatchupStateFinal:
		return "Final"
	case MatchupStateDegraded:
		return "Status pending"
	default:
		return "Preseason"
	}
}

// currentScheduleWeek picks the schedule's current NFL week: the earliest
// week that still has any non-final matchup, or the schedule's last week
// once everything has closed. An empty schedule returns 0 (the caller
// falls back to the preseason snapshot).
func currentScheduleWeek(sch SeasonSchedule) int {
	if len(sch.Weeks) == 0 {
		return 0
	}
	last := sch.Weeks[0].Week
	for _, wk := range sch.Weeks {
		if wk.Week > last {
			last = wk.Week
		}
		for _, m := range wk.Matchups {
			if !m.Final {
				return wk.Week
			}
		}
	}
	return last
}

func scheduleWeekByNumber(sch SeasonSchedule, week int) (ScheduleWeek, bool) {
	for _, wk := range sch.Weeks {
		if wk.Week == week {
			return wk, true
		}
	}
	return ScheduleWeek{}, false
}

type demoProvider struct {
	startWeek int
	startAt   time.Time
}

// Snapshot reports the honest preseason state: no matchups exist until the
// league's real schedule and lineups are in place. The week label derives
// from the active season opening instant and the published schedule's
// opening NFL week, with neutral defaults before either is available.
func (p demoProvider) Snapshot(_ context.Context, now time.Time) (LiveSnapshot, error) {
	startWeek := p.startWeek
	if startWeek <= 0 {
		startWeek = defaultSeasonStartWeek
	}
	startAt := p.startAt
	if startAt.IsZero() {
		startAt, _ = time.Parse(time.RFC3339, DefaultSeasonStartAt)
	}
	return LiveSnapshot{
		Source:      "preseason",
		SourceLabel: "Preseason",
		Week:        startWeek,
		WeekLabel:   fmt.Sprintf("Week %d · Sundays from %s", startWeek, seasonOpenDateLabelAt(startAt)),
		State:       MatchupStatePreseason,
		Status:      "League matchups begin when the season starts",
		LastUpdated: now.UTC(),
		CheckedAt:   now.UTC(),
		Matchups:    []ScoreMatchup{},
	}, nil
}

// seasonOpenDateLabelAt renders a configured opening instant as "Month Day".
// The zero-time fallback is kept explicit for tests and neutral checkouts.
func seasonOpenDateLabelAt(start time.Time) string {
	if start.IsZero() {
		return DefaultSeasonStartAt
	}
	return start.Format("January 2")
}

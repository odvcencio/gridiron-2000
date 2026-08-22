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

func (f *liveFeed) Snapshot(ctx context.Context, now time.Time) LiveSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.cachedAt.IsZero() && now.Sub(f.cachedAt) < f.cacheFor {
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
	snapshot.RefreshAfterSeconds = int(DefaultRefreshPeriod.Seconds())
	f.cached = snapshot
	f.cachedAt = now
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

func (p scheduleProvider) Snapshot(ctx context.Context, now time.Time) (LiveSnapshot, error) {
	state := p.svc.store.Snapshot()
	if state.Schedule == nil || len(state.Schedule.Weeks) == 0 {
		return demoProvider{}.Snapshot(ctx, now)
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
		return demoProvider{}.Snapshot(ctx, now)
	}
	wk, ok := scheduleWeekByNumber(*state.Schedule, week)
	if !ok {
		return LiveSnapshot{}, fmt.Errorf("week %d is not in the persisted season schedule", week)
	}
	scorer := p.svc.matchupScorer(nil)
	stateLabel, statusLabel, clockLabel := p.weekState(week, wk.Matchups, now)
	matchups := make([]ScoreMatchup, 0, len(wk.Matchups))
	for _, m := range wk.Matchups {
		homeScore, awayScore := m.HomeScore, m.AwayScore
		matchupState := MatchupStateFinal
		status := "Final"
		clock := "FINAL"
		if !m.Final {
			matchupState = stateLabel
			status = matchupStateCardLabel(stateLabel)
			clock = clockLabel
			if stateLabel == MatchupStateInProgress || stateLabel == MatchupStateDegraded {
				if s, _, err := scorer.TeamWeekScore(m.HomeTeamID, week); err == nil {
					homeScore = s
				}
				if s, _, err := scorer.TeamWeekScore(m.AwayTeamID, week); err == nil {
					awayScore = s
				}
			}
		}
		home := p.svc.teamByID(m.HomeTeamID)
		away := p.svc.teamByID(m.AwayTeamID)
		matchups = append(matchups, ScoreMatchup{
			ID:     m.ID,
			Home:   ScoreTeam{ID: home.ID, Name: home.Name, Abbreviation: home.Abbreviation, Score: homeScore},
			Away:   ScoreTeam{ID: away.ID, Name: away.Name, Abbreviation: away.Abbreviation, Score: awayScore},
			State:  matchupState,
			Status: status,
			Clock:  clock,
		})
	}
	return LiveSnapshot{
		Source:      "league-schedule",
		SourceLabel: "League matchups",
		Week:        week,
		WeekLabel:   fmt.Sprintf("Week %d", week),
		State:       stateLabel,
		Status:      statusLabel,
		LastUpdated: now.UTC(),
		Matchups:    matchups,
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

type demoProvider struct{}

// Snapshot reports the honest preseason state: no matchups exist until the
// league's real schedule and lineups are in place. The week label derives
// from the config-driven season_start_at (DefaultSeasonStartAt, mutated
// once at boot by applyActiveConfig) instead of a hardcoded September 13.
func (demoProvider) Snapshot(_ context.Context, now time.Time) (LiveSnapshot, error) {
	return LiveSnapshot{
		Source:      "preseason",
		SourceLabel: "Preseason",
		Week:        1,
		WeekLabel:   "Week 1 · Sundays from " + seasonOpenDateLabel(),
		State:       MatchupStatePreseason,
		Status:      "League matchups begin when the season starts",
		LastUpdated: now.UTC(),
		Matchups:    []ScoreMatchup{},
	}, nil
}

// seasonOpenDateLabel renders DefaultSeasonStartAt as "Month Day" ("September
// 13") for the preseason snapshot's week label. An unparseable value (never
// expected; LoadConfig validates it) falls back to the raw string.
func seasonOpenDateLabel() string {
	start, err := time.Parse(time.RFC3339, DefaultSeasonStartAt)
	if err != nil {
		return DefaultSeasonStartAt
	}
	return start.Format("January 2")
}

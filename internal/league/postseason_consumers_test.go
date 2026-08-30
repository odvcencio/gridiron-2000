package league

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newPostseasonLedgerService(t *testing.T, path string) *Service {
	t.Helper()
	avatarAnchor := t.TempDir()
	store := NewStore(path)
	store.draftLifecycleBypass = true
	t.Cleanup(func() { _ = store.Close() })
	svc := &Service{
		store: store, draftAt: time.Now().Add(-time.Hour), demoMode: true,
		teams: defaultTeams(), players: []Player{
			{ID: "home-qb", Name: "Home QB", Position: "QB", NFLTeam: "BUF"},
			{ID: "away-qb", Name: "Away QB", Position: "QB", NFLTeam: "KC"},
		}, cfg: DefaultConfig(), avatarRoot: filepath.Join(avatarAnchor, "avatars"),
		avatarDurableRoot: avatarAnchor, defaultBadgeRoot: filepath.Join(t.TempDir(), "avatar-defaults"),
	}
	svc.feed = newLiveFeed(nil, svc)
	return svc
}

func playoffLedgerFixture(t *testing.T, roundWeeks int) (*Service, PlayoffState, time.Time) {
	t.Helper()
	now := time.Date(2026, time.December, 7, 12, 0, 0, 0, time.UTC)
	svc := newPostseasonLedgerService(t, filepath.Join(t.TempDir(), "state.json"))
	standings := ps1Standings(8)
	cfg := PlayoffConfig{TeamCount: 2, StartWeek: 15, RoundLengthWeeks: roundWeeks, Reseed: true}
	provenance, err := NewPlayoffProvenance(standings, 17, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	truth, err := BuildPlayoffPreview(standings, nil, cfg, provenance)
	if err != nil {
		t.Fatal(err)
	}
	truth.Status = PlayoffStatusPublished
	truth.PublishedAt = now
	weeks := make([]ScheduleWeek, 0, roundWeeks)
	for offset := 0; offset < roundWeeks; offset++ {
		week := cfg.StartWeek + offset
		weeks = append(weeks, ScheduleWeek{Week: week, Matchups: []LeagueMatchup{{
			ID: "regular-" + string(rune('a'+offset)), Week: week,
			HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true,
		}}})
	}
	schedule := SeasonSchedule{Season: 2026, StartWeek: cfg.StartWeek, Weeks: weeks}
	openSchedule := schedule
	openSchedule.Weeks = make([]ScheduleWeek, len(schedule.Weeks))
	for index, week := range schedule.Weeks {
		openSchedule.Weeks[index] = week
		openSchedule.Weeks[index].Matchups = append([]LeagueMatchup(nil), week.Matchups...)
		for matchupIndex := range openSchedule.Weeks[index].Matchups {
			openSchedule.Weeks[index].Matchups[matchupIndex].Final = false
		}
	}
	if err := svc.store.SetSchedule(openSchedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetPlayoffs(truth); err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < roundWeeks; offset++ {
		week := cfg.StartWeek + offset
		if err := svc.store.SetLineupWeek("team-1", week, map[string]string{"QB": "home-qb"}); err != nil {
			t.Fatal(err)
		}
		if err := svc.store.SetLineupWeek("team-2", week, map[string]string{"QB": "away-qb"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetPhase(PhasePlayoffs); err != nil {
		t.Fatal(err)
	}
	return svc, truth, now
}

func TestPlayoffLedgerAdvancementUsesFinalCompleteWeeksAndCompletesPhase(t *testing.T) {
	svc, truth, now := playoffLedgerFixture(t, 2)
	stats := map[int][]WeekStatLine{
		15: {{Key: normalizePlayerKey("Home QB", "QB"), Stats: map[string]float64{"passTD": 1}}, {Key: normalizePlayerKey("Away QB", "QB"), Stats: map[string]float64{"passTD": 2}}},
		16: {{Key: normalizePlayerKey("Home QB", "QB"), Stats: map[string]float64{"passTD": 3}}, {Key: normalizePlayerKey("Away QB", "QB"), Stats: map[string]float64{"passTD": 1}}},
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return stats[week] })
	request := httptest.NewRequest("POST", "/__actions/playoff-advance", nil)
	advanced, err := svc.AdminAdvancePlayoffsFromLedger(request, now)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Matchups[0].HomeScore <= advanced.Matchups[0].AwayScore || advanced.ChampionTeamID != "team-1" {
		t.Fatalf("ledger result = %+v", advanced.Matchups[0])
	}
	if advanced.Matchups[0].ResultProvenance == nil || advanced.Matchups[0].ResultProvenance.Source != "starter-ledger" || !advanced.Matchups[0].ResultProvenance.Authoritative {
		t.Fatalf("ledger provenance = %+v", advanced.Matchups[0].ResultProvenance)
	}
	if got := svc.store.Snapshot().Phase; got != PhaseSeasonComplete {
		t.Fatalf("phase after championship = %q, want %q", got, PhaseSeasonComplete)
	}
	if got := svc.store.PlayoffTruth(); got == nil || got.Revision != truth.Revision+1 {
		t.Fatalf("persisted truth = %+v", got)
	}
}

func TestPlayoffLedgerRejectsPartialOrUnavailableWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name        string
		stats       map[int][]WeekStatLine
		unavailable bool
		want        string
	}{
		{name: "empty-source", stats: map[int][]WeekStatLine{}, want: "empty"},
		{name: "partial-join", stats: map[int][]WeekStatLine{15: {{Key: normalizePlayerKey("Home QB", "QB"), Stats: map[string]float64{"passTD": 1}}}}, want: "partial"},
		{name: "unavailable-source", unavailable: true, want: "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _, now := playoffLedgerFixture(t, 1)
			if !test.unavailable {
				svc.SetWeekStatsSource(func(week int) []WeekStatLine { return test.stats[week] })
			}
			before := svc.store.Snapshot().Playoffs
			_, err := svc.AdminAdvancePlayoffsFromLedger(httptest.NewRequest("POST", "/__actions/playoff-advance", nil), now)
			if err == nil || !containsError(err, test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
			after := svc.store.Snapshot().Playoffs
			if before.Revision != after.Revision || before.Matchups[0].Final != after.Matchups[0].Final {
				t.Fatalf("rejected advancement mutated truth: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestPlayoffCorrectionAndRestartPreserveAuditedTruth(t *testing.T) {
	svc, truth, now := playoffLedgerFixture(t, 1)
	matchup := truth.Matchups[0]
	matchup.Final = true
	matchup.WinnerTeamID = matchup.HomeTeamID
	matchup.HomeScore, matchup.AwayScore = 11, 4
	truth.Matchups[0] = matchup
	truth.ChampionTeamID, truth.RunnerUpTeamID = matchup.HomeTeamID, matchup.AwayTeamID
	truth.Status = PlayoffStatusPublished
	if err := svc.store.SetPlayoffs(truth); err != nil {
		t.Fatal(err)
	}
	corrected, err := svc.store.CorrectPublishedPlayoff(PlayoffCorrection{
		MatchupID: matchup.ID, WinnerTeamID: matchup.AwayTeamID, HomeScore: 4, AwayScore: 11,
		ScoresProvided: true, Actor: "commissioner", Reason: "verified stat adjustment",
		Confirmation: PlayoffCorrectionConfirmation, At: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Matchups[0].ResultProvenance == nil || corrected.Matchups[0].ResultProvenance.Source != "commissioner-correction" {
		t.Fatalf("correction provenance = %+v", corrected.Matchups[0].ResultProvenance)
	}
	filePath := svc.store.filePath
	if err := svc.store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(filePath)
	t.Cleanup(func() { _ = restarted.Close() })
	got := restarted.PlayoffTruth()
	if got == nil || got.Audit[len(got.Audit)-1].Action != "correction" || got.Matchups[0].WinnerTeamID != matchup.AwayTeamID {
		t.Fatalf("restarted truth = %+v", got)
	}
}

func TestPlayoffCorrectionRecordsSameWinnerStatAdjustment(t *testing.T) {
	svc, truth, now := playoffLedgerFixture(t, 1)
	matchup := truth.Matchups[0]
	matchup.Final = true
	matchup.WinnerTeamID = matchup.HomeTeamID
	matchup.HomeScore, matchup.AwayScore = 11, 4
	truth.Matchups[0] = matchup
	truth.ChampionTeamID, truth.RunnerUpTeamID = matchup.HomeTeamID, matchup.AwayTeamID
	truth.Status = PlayoffStatusPublished
	if err := svc.store.SetPlayoffs(truth); err != nil {
		t.Fatal(err)
	}
	corrected, err := svc.store.CorrectPublishedPlayoff(PlayoffCorrection{
		MatchupID: matchup.ID, WinnerTeamID: matchup.HomeTeamID, HomeScore: 14, AwayScore: 4,
		ScoresProvided: true, Actor: "commissioner", Reason: "audited stat adjustment",
		Confirmation: PlayoffCorrectionConfirmation, At: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Matchups[0].WinnerTeamID != matchup.HomeTeamID || corrected.Matchups[0].HomeScore != 14 || corrected.Matchups[0].AwayScore != 4 {
		t.Fatalf("same-winner stat correction = %+v", corrected.Matchups[0])
	}
	if corrected.Matchups[0].ResultProvenance == nil || corrected.Matchups[0].ResultProvenance.Source != "commissioner-correction" || len(corrected.Audit) != 1 {
		t.Fatalf("same-winner correction audit/provenance = %+v / %+v", corrected.Audit, corrected.Matchups[0].ResultProvenance)
	}
}

func TestPlayoffTruthProjectionHidesPreviewAndRejectsDegradedSource(t *testing.T) {
	svc := newPostseasonLedgerService(t, filepath.Join(t.TempDir(), "state.json"))
	now := time.Date(2026, time.December, 8, 12, 0, 0, 0, time.UTC)
	preview := ps1Preview(t, 2)
	state := PersistedState{Phase: PhasePlayoffs, Playoffs: &preview}
	manager := svc.playoffTruthMap(state, now, false)
	if manager["status"] != "waiting" || manager["has_bracket"] != false || len(manager["seeds"].([]map[string]any)) != 0 {
		t.Fatalf("manager preview projection = %+v", manager)
	}
	commissioner := svc.playoffTruthMap(state, now, true)
	if commissioner["status"] != PlayoffStatusPreview || commissioner["has_bracket"] != true || len(commissioner["seeds"].([]map[string]any)) == 0 {
		t.Fatalf("commissioner preview projection = %+v", commissioner)
	}
	preview.Provenance.SourceState = "stale"
	preview.Provenance.Authoritative = false
	state.Playoffs = &preview
	degraded := svc.playoffTruthMap(state, now, true)
	if degraded["status"] != "waiting" || degraded["has_bracket"] != false || !strings.Contains(strings.ToLower(degraded["detail"].(string)), "stale") {
		t.Fatalf("degraded projection = %+v", degraded)
	}
}

func containsError(err error, want string) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want))
}

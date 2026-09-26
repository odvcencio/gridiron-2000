package league

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func everyonePostseasonConfig() PlayoffConfig {
	return PlayoffConfig{
		TeamCount: 8, StartWeek: 15, RoundLengthWeeks: 1,
		Qualification: "top-record", TiebreakOrder: append([]string(nil), DefaultTiebreakChain...),
		Byes: 0, DivisionWinnersFirst: false, Reseed: true, Consolation: true, ToiletBowl: false,
	}
}

func TestEveryoneScheduleIsDoubleRoundRobinBeforePlayoffs(t *testing.T) {
	cfg := everyonePostseasonConfig()
	if err := ValidatePostseasonConfig(cfg, 8, 2); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlayoffConfig(cfg, 8, 2, 1, 17); err != nil {
		t.Fatal(err)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: defaultTeamIDs(), Divisions: ps1Divisions(8), StartWeek: 1, Weeks: 14, Seed: 47})
	if err != nil {
		t.Fatal(err)
	}
	meetings := map[[2]string]int{}
	for _, week := range schedule.Weeks {
		if week.Week >= cfg.StartWeek || len(week.Matchups) != 4 || week.ByeTeamID != "" {
			t.Fatalf("regular-season week %+v conflicts with eight-team playoff calendar", week)
		}
		for _, matchup := range week.Matchups {
			pair := [2]string{matchup.HomeTeamID, matchup.AwayTeamID}
			if pair[0] > pair[1] {
				pair[0], pair[1] = pair[1], pair[0]
			}
			meetings[pair]++
		}
	}
	if len(meetings) != 28 {
		t.Fatalf("unique pairs = %d, want 28", len(meetings))
	}
	for pair, count := range meetings {
		if count != 2 {
			t.Errorf("pair %v met %d times, want twice", pair, count)
		}
	}
}

func TestEveryoneThreeWayHeadToHeadLoopFallsThroughToSeededDraw(t *testing.T) {
	schedule := fixtureFinalSchedule(
		LeagueMatchup{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90},
		LeagueMatchup{HomeTeamID: "team-2", AwayTeamID: "team-3", HomeScore: 100, AwayScore: 90},
		LeagueMatchup{HomeTeamID: "team-3", AwayTeamID: "team-1", HomeScore: 100, AwayScore: 90},
	)
	inputs := TiebreakInputs{Chain: everyonePostseasonConfig().TiebreakOrder, SeasonSeed: 47}
	first := ComputeStandings(schedule, defaultTeamIDs(), inputs)
	second := ComputeStandings(schedule, defaultTeamIDs(), inputs)
	for i := 0; i < 3; i++ {
		if first[i].TeamID != second[i].TeamID || (i < 2 && first[i].DecidedBy != "seeded-draw") {
			t.Fatalf("three-way loop order or explanation changed: %+v / %+v", first, second)
		}
	}
}

func TestEveryonePreviewUsesPrimaryManagerPickemAndScheduleSeed(t *testing.T) {
	now := time.Date(2026, 12, 8, 12, 0, 0, 0, time.UTC)
	svc := newPostseasonLedgerService(t, filepath.Join(t.TempDir(), "state.json"))
	svc.cfg.Postseason = everyonePostseasonConfig()
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: defaultTeamIDs(), StartWeek: 1, Weeks: 14, Seed: 47})
	if err != nil {
		t.Fatal(err)
	}
	for i := range schedule.Weeks {
		for j := range schedule.Weeks[i].Matchups {
			schedule.Weeks[i].Matchups[j].HomeScore = 100
			schedule.Weeks[i].Matchups[j].AwayScore = 100
			schedule.Weeks[i].Matchups[j].Final = true
		}
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetPhase(PhasePlayoffs); err != nil {
		t.Fatal(err)
	}
	kickoff := now.Add(-48 * time.Hour)
	game := GameInfo{ID: "week14-final", Week: 14, Kickoff: kickoff, Home: "BUF", Away: "KC", HomeScore: 24, AwayScore: 14, Final: true, ScoresPresent: true}
	svc.SetScheduleSource(func() []GameInfo { return []GameInfo{game} })
	svc.store.mu.Lock()
	svc.store.state.Members = map[string]Member{
		"one@example.test": {TeamID: "team-1", Role: "owner"},
		"two@example.test": {TeamID: "team-2", Role: "owner"},
		"co@example.test":  {TeamID: "team-1", Role: "co"},
	}
	svc.store.state.Pickems = map[string]map[string]string{
		"one@example.test": {game.ID: "BUF"},
		"two@example.test": {game.ID: "KC"},
		"co@example.test":  {game.ID: "KC"},
	}
	svc.store.state.PickemEnteredAt = map[string]time.Time{
		"one@example.test": kickoff.Add(-time.Hour), "two@example.test": kickoff.Add(-time.Hour), "co@example.test": kickoff.Add(-time.Hour),
	}
	svc.store.state.PickemMarkets = map[string]PickemMarket{game.ID: {Frozen: true, LinePresent: true}}
	if err := svc.store.persistLocked(colMembers, colPickems, colPickemMarkets); err != nil {
		svc.store.mu.Unlock()
		t.Fatal(err)
	}
	svc.store.mu.Unlock()
	preview, err := svc.AdminPreviewPlayoffs(httptest.NewRequest("POST", "/__actions/playoff-preview", nil), now)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Seeds[0].TeamID != "team-1" || preview.Seeds[0].TieBreakExplanation != "final standings tie-break: pickem" || preview.Seeds[1].TeamID != "team-2" {
		t.Fatalf("Pick'em seeding = %+v", preview.Seeds)
	}
	if preview.Provenance.FinalWeek != 14 || preview.Provenance.SnapshotID == "" {
		t.Fatalf("preview provenance = %+v", preview.Provenance)
	}
	svc.SetScheduleSource(nil)
	if _, err := svc.AdminPreviewPlayoffs(httptest.NewRequest("POST", "/__actions/playoff-preview", nil), now); err == nil {
		t.Fatal("unavailable Pick'em source replaced the final preview")
	}
	game.Final = false
	svc.SetScheduleSource(func() []GameInfo { return []GameInfo{game} })
	if _, err := svc.AdminPreviewPlayoffs(httptest.NewRequest("POST", "/__actions/playoff-preview", nil), now); err == nil {
		t.Fatal("partial Pick'em source replaced the final preview")
	}
	if got := svc.store.PlayoffTruth(); got == nil || got.PreviewID != preview.PreviewID {
		t.Fatalf("source failure changed the persisted preview: %+v", got)
	}
	schedule.Weeks = append(schedule.Weeks, ScheduleWeek{Week: 15, Matchups: []LeagueMatchup{{
		ID: "overlapping-regular-season", Week: 15, HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true,
	}}})
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdminPreviewPlayoffs(httptest.NewRequest("POST", "/__actions/playoff-preview", nil), now); err == nil || !strings.Contains(err.Error(), "after the final regular-season week") {
		t.Fatalf("overlapping Week 15 regular-season matchup was accepted: %v", err)
	}
}

func TestEveryoneLedgerWaitsForFinalNFLGamesWithoutRegularSeasonWeek15(t *testing.T) {
	now := time.Date(2026, 12, 22, 12, 0, 0, 0, time.UTC)
	svc := newPostseasonLedgerService(t, filepath.Join(t.TempDir(), "state.json"))
	cfg := everyonePostseasonConfig()
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: defaultTeamIDs(), StartWeek: 1, Weeks: 14, Seed: 47})
	if err != nil {
		t.Fatal(err)
	}
	for i := range schedule.Weeks {
		for j := range schedule.Weeks[i].Matchups {
			schedule.Weeks[i].Matchups[j].Final = true
		}
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetPhase(PhasePlayoffs); err != nil {
		t.Fatal(err)
	}
	standings := ps1Standings(8)
	provenance, err := NewPlayoffProvenance(standings, 14, now, cfg.TiebreakOrder)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := BuildPlayoffPreview(standings, nil, cfg, provenance)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetPlayoffPreview(preview, "commissioner", now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", now); err != nil {
		t.Fatal(err)
	}
	svc.players = make([]Player, 0, 8)
	lines := make([]WeekStatLine, 0, 8)
	for i, teamID := range defaultTeamIDs() {
		name := fmt.Sprintf("QB %d", i+1)
		id := fmt.Sprintf("qb-%d", i+1)
		svc.players = append(svc.players, Player{ID: id, Name: name, Position: "QB", NFLTeam: "BUF"})
		lines = append(lines, WeekStatLine{Key: normalizePlayerKey(name, "QB"), Stats: map[string]float64{"passTD": float64(8 - i)}})
		if err := svc.store.SetLineupWeek(teamID, 15, map[string]string{"QB": id}); err != nil {
			t.Fatal(err)
		}
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine { return lines })
	game := GameInfo{ID: "nfl-week15", Week: 15, Home: "BUF", Away: "KC", HomeScore: 24, AwayScore: 14}
	svc.SetScheduleSource(func() []GameInfo { return []GameInfo{game} })
	request := httptest.NewRequest("POST", "/__actions/playoff-advance", nil)
	for _, test := range []struct {
		name string
		game GameInfo
		want string
	}{
		{"in-progress", game, "final NFL scores"},
		{"partial-score", func() GameInfo { changed := game; changed.Final = true; return changed }(), "final NFL scores"},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := test.game
			svc.SetScheduleSource(func() []GameInfo { return []GameInfo{current} })
			if _, err := svc.AdminAdvancePlayoffsFromLedger(request, now); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("score source %q advanced: %v", test.name, err)
			}
			if got := svc.store.PlayoffTruth(); got == nil || got.Revision != 2 {
				t.Fatalf("rejected advancement changed persisted revision: %+v", got)
			}
		})
	}
	svc.SetScheduleSource(nil)
	if _, err := svc.AdminAdvancePlayoffsFromLedger(request, now); err == nil || !strings.Contains(err.Error(), "unavailable NFL score source") {
		t.Fatalf("unavailable score source advanced: %v", err)
	}
	game.Final, game.ScoresPresent = true, true
	svc.SetScheduleSource(func() []GameInfo { return []GameInfo{game} })
	advanced, err := svc.AdminAdvancePlayoffsFromLedger(request, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(matchupsForRound(advanced.Matchups, "consolation", 1)) != 2 {
		t.Fatalf("final NFL scores did not create the losers bracket: %+v", advanced.Matchups)
	}
}

func TestEveryoneBracketPublishesReseedsAndNamesBothFinalWinners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	store.draftLifecycleBypass = true
	when := time.Date(2026, 12, 8, 12, 0, 0, 0, time.UTC)
	cfg := everyonePostseasonConfig()
	standings := ps1Standings(8)
	provenance, err := NewPlayoffProvenance(standings, 14, when, cfg.TiebreakOrder)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := BuildPlayoffPreview(standings, ps1Divisions(8), cfg, provenance)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Matchups) != 4 || preview.Matchups[0].HomeSeed != 1 || preview.Matchups[0].AwaySeed != 8 {
		t.Fatalf("quarterfinals = %+v", preview.Matchups)
	}
	if err := store.SetPlayoffPreview(preview, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishPlayoffPreview(preview.PreviewID, "", "commissioner", when); err == nil {
		t.Fatal("unconfirmed publication succeeded")
	}
	first, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when)
	if err != nil || second.Revision != first.Revision || len(second.Audit) != len(first.Audit) {
		t.Fatalf("idempotent publication = %+v, %v", second, err)
	}
	state := *store.PlayoffTruth()
	for week := 15; week <= 17; week++ {
		results := []PlayoffRoundResult{}
		for _, matchup := range state.Matchups {
			if matchup.Week != week || matchup.Final {
				continue
			}
			results = append(results, PlayoffRoundResult{MatchupID: matchup.ID, HomeScore: 110, AwayScore: 90, Final: true, Authoritative: true, SourceState: "final", Source: "starter-ledger", ObservedAt: when.AddDate(0, 0, week-15)})
		}
		if week == 15 {
			if _, err := store.AdvancePublishedPlayoffRound(results[:1]); err == nil {
				t.Fatal("partial quarterfinal results advanced")
			}
			degraded := append([]PlayoffRoundResult(nil), results...)
			degraded[0].SourceState = "degraded"
			if _, err := store.AdvancePublishedPlayoffRound(degraded); err == nil {
				t.Fatal("degraded quarterfinal results advanced")
			}
		}
		state, err = store.AdvancePublishedPlayoffRound(results)
		if err != nil {
			t.Fatal(err)
		}
		if week == 15 {
			if len(matchupsForRound(state.Matchups, "consolation", 1)) != 2 || len(matchupsForRound(state.Matchups, "toilet", 1)) != 0 {
				t.Fatalf("losers bracket or toilet = %+v", state.Matchups)
			}
			semis := matchupsForRound(state.Matchups, "championship", 2)
			if len(semis) != 2 || semis[0].Week != 16 || semis[0].HomeSeed != 1 || semis[0].AwaySeed != 4 || semis[1].HomeSeed != 2 || semis[1].AwaySeed != 3 {
				t.Fatalf("reseeded semifinals = %+v", semis)
			}
			projectionService := newPostseasonLedgerService(t, filepath.Join(t.TempDir(), "round-view.json"))
			view := projectionService.playoffTruthMap(PersistedState{Phase: PhasePlayoffs, Playoffs: &state}, when, false)
			if view["current_round"] != 2 || view["next_matchup_count"] != 4 {
				t.Fatalf("Week 16 shared round summary = round %v, matchups %v", view["current_round"], view["next_matchup_count"])
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = NewStore(path)
			if err := store.StartupError(); err != nil {
				t.Fatal(err)
			}
			state = *store.PlayoffTruth()
		}
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	state = *store.PlayoffTruth()
	if state.ChampionTeamID != "team-1" || state.RunnerUpTeamID != "team-2" || state.ConsolationWinnerTeamID != "team-5" || state.ToiletTeamID != "" {
		t.Fatalf("final outcomes = champion %q, runner-up %q, consolation %q, toilet %q", state.ChampionTeamID, state.RunnerUpTeamID, state.ConsolationWinnerTeamID, state.ToiletTeamID)
	}
	final := matchupsForRound(state.Matchups, "consolation", 2)[0]
	if _, err := store.CorrectPublishedPlayoff(PlayoffCorrection{MatchupID: final.ID, WinnerTeamID: final.AwayTeamID, Actor: "commissioner", Reason: "corrected final stat", At: when, Confirmation: ""}); err == nil {
		t.Fatal("unconfirmed consolation correction succeeded")
	}
	corrected, err := store.CorrectPublishedPlayoff(PlayoffCorrection{MatchupID: final.ID, WinnerTeamID: final.AwayTeamID, Actor: "commissioner", Reason: "corrected final stat", At: when, Confirmation: PlayoffCorrectionConfirmation})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.ConsolationWinnerTeamID != final.AwayTeamID || corrected.Audit[len(corrected.Audit)-1].Action != "correction" {
		t.Fatalf("consolation correction = %+v", corrected)
	}
	svc := newPostseasonLedgerService(t, filepath.Join(t.TempDir(), "projection.json"))
	view := svc.playoffTruthMap(PersistedState{Phase: PhaseSeasonComplete, Playoffs: &corrected}, when, false)
	if view["consolation_winner_team_id"] != final.AwayTeamID || !strings.Contains(view["detail"].(string), "Losers-bracket winner:") {
		t.Fatalf("final shared projection = %+v", view)
	}
	notice := svc.buildPlayoffUpdate(PersistedState{}, corrected, "advanced", Member{TeamID: "team-5", Email: "five@example.test"})
	if !strings.Contains(notice.Text, "CONSOLATION · ROUND 1 · WEEK 16") || !strings.Contains(notice.Text, "CONSOLATION · ROUND 2 · WEEK 17") {
		t.Fatalf("losers-bracket notification does not name its weeks: %q", notice.Text)
	}
}

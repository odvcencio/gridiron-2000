package league

import (
	"strings"
	"testing"
	"time"
)

func weeklyAwardsFixture(t *testing.T) (*Service, PersistedState, ScheduleWeek) {
	t.Helper()
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	week := ScheduleWeek{Week: 1, ClosedAt: now.Add(-24 * time.Hour), Matchups: []LeagueMatchup{
		{ID: "fantasy-1", Week: 1, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 120, AwayScore: 110, Final: true},
		{ID: "fantasy-2", Week: 1, HomeTeamID: "team-3", AwayTeamID: "team-4", HomeScore: 90, AwayScore: 130, Final: true},
	}}
	games := []GameInfo{
		{ID: "pick-1", Week: 1, Kickoff: now.Add(-72 * time.Hour), Away: "A", Home: "B", AwayScore: 24, HomeScore: 17, Final: true, ScoresPresent: true},
		{ID: "pick-2", Week: 1, Kickoff: now.Add(-71 * time.Hour), Away: "C", Home: "D", AwayScore: 20, HomeScore: 17, Final: true, ScoresPresent: true},
		{ID: "week-2", Week: 2, Kickoff: now.Add(24 * time.Hour), Away: "E", Home: "F"},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	state := svc.store.Snapshot()
	state.Schedule = &SeasonSchedule{Season: 2026, StartWeek: 1, Weeks: []ScheduleWeek{week}}
	state.Members = map[string]Member{
		"one@example.com": {Email: "one@example.com", Name: "One", TeamID: "team-1"},
		"two@example.com": {Email: "two@example.com", Name: "Two", TeamID: "team-2"},
	}
	state.Pickems = map[string]map[string]string{
		"one@example.com": {"pick-1": "A", "pick-2": "C"},
		"two@example.com": {"pick-1": "B", "pick-2": "C"},
	}
	state.PickemEnteredAt = map[string]time.Time{
		"one@example.com": games[0].Kickoff.Add(-time.Hour),
		"two@example.com": games[0].Kickoff.Add(-time.Hour),
	}
	state.PickemMarkets = frozenPickemMarkets(games)
	return svc, state, week
}

func TestWeeklyAwardsDeriveFantasyPickemAndNailbiterHonors(t *testing.T) {
	svc, state, week := weeklyAwardsFixture(t)
	awards := svc.awardsForWeek(state, week)
	if len(awards) != 3 {
		t.Fatalf("awards = %+v, want high score, Pick'em winner, and nailbiter", awards)
	}
	want := map[string]string{
		weeklyAwardHighScore: "team-4",
		weeklyAwardPickem:    "team-1",
		weeklyAwardNailbiter: "team-1",
	}
	for _, award := range awards {
		if award.TeamID != want[award.Kind] {
			t.Errorf("%s team = %q, want %q", award.Kind, award.TeamID, want[award.Kind])
		}
	}

	member := state.Members["one@example.com"]
	active := svc.activeWeeklyAwards(state, member, member.Email, 2)
	if len(active) != 2 {
		t.Fatalf("following-week active awards = %+v, want Pick'em and nailbiter", active)
	}
	if got := svc.activeWeeklyAwards(state, member, member.Email, 3); len(got) != 0 {
		t.Fatalf("awards remained beside user after following week: %+v", got)
	}
	if got := svc.weeklyAwardsForTeam(state, "team-1"); len(got) != 2 {
		t.Fatalf("team trophy case = %+v, want two permanent honors", got)
	}
}

func TestWeeklyRecapEmailsEarnedAwardsAndTrophyCaseLink(t *testing.T) {
	svc, state, week := weeklyAwardsFixture(t)
	message := svc.buildMatchupRecap(state, state.Members["one@example.com"], week)
	for _, want := range []string{"YOUR WEEKLY AWARDS", "Pick'em Winner", "Nailbiter", "/team#trophy-case"} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("award recap omitted %q: %s", want, message.Text)
		}
	}
}

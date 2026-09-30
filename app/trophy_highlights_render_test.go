package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
)

func homeAchievementFixture(t *testing.T, fixture string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHomeAchievementFixtureProcess$")
	cmd.Env = append(os.Environ(), "CHEEVOS_HOME_FIXTURE="+fixture, "DATA_FILE="+filepath.Join(t.TempDir(), "state.json"), "DEMO_MODE=true", "APP_ENV=test", "LEAGUE_FILE=", "GOOGLE_CLIENT_ID=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("home achievement fixture: %v\n%s", err, output)
	}
	return string(output)
}
func TestHomeAchievementHighlightAndSpecialCalloutRender(t *testing.T) {
	body := homeAchievementFixture(t, "awards")
	for _, want := range []string{"Latest league awards", "Week 1 highlights", "Leg Day", "Leg Day · Gold", "Special teams stepped up", "data-special=\"true\"", "Render Fixture", "Trophy catalog"} {
		if !strings.Contains(body, want) {
			t.Errorf("highlight missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "No trophies yet.") {
		t.Fatal("earned highlights showed empty state")
	}
}
func TestHomeAchievementHighlightEmptyStateRender(t *testing.T) {
	body := homeAchievementFixture(t, "empty")
	if strings.Count(body, "No trophies yet. They appear after the first week closes.") != 1 {
		t.Fatalf("expected one award empty state: %s", body)
	}
	if strings.Contains(body, "Special teams stepped up") {
		t.Fatal("empty week claimed a special achievement")
	}
}
func TestHomeAchievementFixtureProcess(t *testing.T) {
	fixture := os.Getenv("CHEEVOS_HOME_FIXTURE")
	if fixture == "" {
		t.Skip("fixture helper")
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	state := league.PersistedState{Members: map[string]league.Member{"render@example.com": {Email: "render@example.com", Name: "Render Fixture", TeamID: "team-1"}}}
	if fixture == "awards" {
		state.Picks = []league.DraftPick{{Number: 1, TeamID: "team-1", PlayerID: "fixture-kicker"}}
		state.Lineups = map[string]map[int]map[string]string{"team-1": {1: {"K": "fixture-kicker"}}}
		state.Schedule = &league.SeasonSchedule{Season: 2026, StartWeek: 1, Weeks: []league.ScheduleWeek{{Week: 1, ClosedAt: now, Matchups: []league.LeagueMatchup{{ID: "fixture-matchup", Week: 1, HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 90, Final: true}}}}}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("DATA_FILE"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	svc := league.Default()
	svc.SetClockForTest(func() time.Time { return now })
	svc.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{{ID: "fixture-kicker", Name: "Fixture Kicker", Position: "K", NFLTeam: "BUF", Projection: 99}}, 1, "test"
	})
	svc.SetScheduleSource(func() []league.GameInfo {
		return []league.GameInfo{{ID: "fixture-game", Week: 1, Away: "BUF", Home: "MIA", Kickoff: now.Add(-48 * time.Hour), Final: true, ScoresPresent: true}}
	})
	svc.SetWeekStatsSource(func(int) []league.WeekStatLine {
		return []league.WeekStatLine{{Key: "fixturekicker|K", Stats: map[string]float64{"fgMade": 5}}}
	})
	fmt.Print(renderAuthenticatedHomepage(t))
}

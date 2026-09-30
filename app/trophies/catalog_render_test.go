package trophies

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/auth"
)

func catalogFixture(t *testing.T, state string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCatalogRenderFixtureProcess$")
	cmd.Env = append(os.Environ(), "CHEEVOS_CATALOG_FIXTURE="+state, "DATA_FILE="+filepath.Join(t.TempDir(), "state.json"), "APP_ENV=test", "DEMO_MODE=true", "LEAGUE_FILE=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("catalog fixture: %v\n%s", err, output)
	}
	return string(output)
}
func TestCatalogShowsEarnedLockedProgressHoldersAndHiddenPunter(t *testing.T) {
	body := catalogFixture(t, "earned")
	for _, want := range []string{"Pick&#39;em trophies", "Streaks", "Fantasy week achievements", "Season records", "Manager achievements", "Locked for you", "How to earn:", "Pick&#39;em streak: current 1, best 1, need 5", "Week 1", "Picker", "Career ×0", "current season", "data-achievement=\"perfect_week\" data-awarded=\"true\""} {
		if !strings.Contains(body, want) {
			t.Errorf("catalog missing %q: %s", want, body)
		}
	}
	if !strings.Contains(body, `href="/trophies#by-manager"`) {
		t.Fatal("catalog must link to a rendered manager picker")
	}
	if strings.Contains(body, "Boot Legend") {
		t.Fatal("standard roster must hide punter achievements")
	}
	if strings.Contains(body, "picker@example.com") {
		t.Fatal("catalog exposed private identity")
	}
	if !strings.Contains(body, `aria-current="true">Catalog`) {
		t.Fatal("catalog navigation not selected")
	}
}
func TestCatalogEmptyStateIsSingleAndRulesRemainVisible(t *testing.T) {
	body := catalogFixture(t, "empty")
	if strings.Count(body, "No trophies yet. They appear after the first week closes.") != 1 {
		t.Fatalf("expected one empty state: %s", body)
	}
	for _, want := range []string{"Iron Manager", "Leg Day", "How to earn:", "Locked for you"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty catalog missing %q", want)
		}
	}
	for _, bad := range []string{"NO AWARDS THIS WEEK", "NOT AWARDED YET", "NO TROPHIES YET"} {
		if strings.Contains(body, bad) {
			t.Errorf("stacked empty state %q", bad)
		}
	}
}
func TestUnknownManagerEmptyStateIsSingle(t *testing.T) {
	body := catalogFixture(t, "unknown")
	if strings.Count(body, `class="empty-tape"`) != 1 || !strings.Contains(body, "MANAGER NOT FOUND") {
		t.Fatalf("unknown manager should show one empty state: %s", body)
	}
}

func TestCatalogRenderFixtureProcess(t *testing.T) {
	fixture := os.Getenv("CHEEVOS_CATALOG_FIXTURE")
	if fixture == "" {
		t.Skip("fixture helper")
	}
	svc := league.Default()
	if _, err := svc.EnsureMember("picker@example.com", "Picker"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	svc.SetClockForTest(func() time.Time { return now })
	var games []league.GameInfo
	if fixture == "earned" {
		games = []league.GameInfo{{ID: "pick-game", Week: 1, Away: "BUF", Home: "MIA", Kickoff: now.Add(time.Hour), SpreadLinePresent: true, SpreadLineTenths: 0, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://example.com/schedule"}}
	}
	svc.SetScheduleSource(func() []league.GameInfo { return games })
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "picker", Email: "picker@example.com", Name: "Picker"}, true
	})})
	if fixture == "earned" {
		seed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := svc.PickemSet(r, "pick-game", "BUF"); err != nil {
				t.Fatal(err)
			}
		})
		authn.Middleware(seed).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/pickem", nil))
		games[0].Kickoff = now.Add(-48 * time.Hour)
		games[0].Final = true
		games[0].ScoresPresent = true
		games[0].AwayScore = 24
		games[0].HomeScore = 10
		// Read the settled grades from the market frozen during submission.
		svc.PickemData(httptest.NewRequest("GET", "/pickem?week=1", nil))
	}
	rec := httptest.NewRecorder()
	target := "/?view=catalog"
	if fixture == "unknown" {
		target = "/?manager=m-unknown"
	}
	authn.Middleware(trophiesHandler(t)).ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "WENT DARK") {
		t.Fatalf("catalog render: %d %s", rec.Code, rec.Body.String())
	}
	fmt.Print(rec.Body.String())
}

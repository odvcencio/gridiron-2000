package matchups

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
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func TestMatchupsPagePreseasonAndScheduledCopyIsNotLive(t *testing.T) {
	for _, fixture := range []struct {
		name string
		want string
	}{
		{name: "preseason", want: "COMING SOON."},
		{name: "scheduled", want: "SCHEDULED."},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
			cmd.Env = append(os.Environ(),
				"MATCHUPS_RENDER_FIXTURE="+fixture.name,
				"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
				"DEMO_MODE=true", "GOOGLE_CLIENT_ID=",
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture process: %v\n%s", err, output)
			}
			body := string(output)
			if !strings.Contains(body, fixture.want) {
				t.Fatalf("fixture %s missing %q: %s", fixture.name, fixture.want, body)
			}
			for _, forbidden := range []string{"Feed connected", "Live scoring", "In progress", "LIVE LEAGUE FEED"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("fixture %s contains false live copy %q: %s", fixture.name, forbidden, body)
				}
			}
			for _, binding := range []string{
				`data-gosx-live-bind="status"`,
				`data-gosx-live-bind="headlineTop"`,
				`data-gosx-live-bind="headlineBottom"`,
				`data-gosx-live-bind="refreshLabel"`,
				`data-gosx-live-bind="noteTitle"`,
				`data-gosx-live-bind="noteBody"`,
				`data-gosx-live-bind="liveIndicator"`,
			} {
				if !strings.Contains(body, binding) {
					t.Errorf("fixture %s missing transition binding %s", fixture.name, binding)
				}
			}
			if fixture.name == "scheduled" && !strings.Contains(body, `data-gosx-live-bind="matchupIndicator.`) {
				t.Errorf("scheduled fixture omitted persistent card indicator bindings: %s", body)
			}
			if strings.Contains(body, `data-gosx-revalidate-src="/api/league/version"`) {
				t.Errorf("fixture %s still relies on league-version revalidation", fixture.name)
			}
		})
	}

	layout, err := os.ReadFile(filepath.Join("..", "layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	layoutSource := string(layout)
	if strings.Contains(layoutSource, "LIVE LEAGUE FEED") ||
		!strings.Contains(layoutSource, `<Link href="/matchups"`) ||
		!strings.Contains(layoutSource, "Matchups") ||
		!strings.Contains(layoutSource, "data.league.matchup_footer_label") {
		t.Fatalf("shared layout still has hardcoded live matchup copy:\n%s", layout)
	}
}

func TestMatchupsPageFixtureProcess(t *testing.T) {
	fixture := os.Getenv("MATCHUPS_RENDER_FIXTURE")
	if fixture == "" {
		t.Skip("fixture helper")
	}
	svc := league.Default()
	if fixture == "scheduled" {
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		if _, err := svc.AdminGenerateSchedule(request, 14, 1, 42); err != nil {
			t.Fatal(err)
		}
		kickoff := time.Now().Add(24 * time.Hour)
		svc.SetScheduleSource(func() []league.GameInfo {
			return []league.GameInfo{{ID: "future", Week: 1, Kickoff: kickoff, Away: "BUF", Home: "MIA"}}
		})
	}
	fmt.Print(renderMatchupsPage(t))
}

func renderMatchupsPage(t *testing.T) string {
	t.Helper()
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

// TestMatchupsPageRendersWithRealScheduleData is the regression guard for
// the production render crash this package's page.gsx used to hit:
// "render strict component TeamMark: spread source has type
// map[string]interface {}" (see ScoreTeamProps/MatchupCardProps' doc
// comments in page.gsx for the root cause and the fix).
//
// It drives a real HTTP GET through the actual file router — the same
// router.AddDir mechanism main.go uses to mount every page — against
// this package's page.gsx and page.server.go exactly as they sit on
// disk, after seeding a real generated schedule so data.matchups is
// genuinely non-empty. Before this test, nothing in the repo's suite
// rendered a .gsx template with real data at all: existing tests only
// asserted the map/struct shape MatchupsData returns
// (internal/league/service_test.go and friends), never exercised the
// render path, which is exactly why this crash shipped and reproduced
// unnoticed on an empty-schedule dev checkout.
//
// This test intentionally covers only the matchups page; it does not
// attempt to be a general render-every-page harness (see the audit
// notes in the fix's commit/PR description for why every other .gsx
// page's strict-component boundary was judged safe by inspection
// instead of duplicating this harness per page).
func TestMatchupsPageRendersWithRealScheduleData(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	seedRequest, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := league.Default().AdminGenerateSchedule(seedRequest, 14, 1, 42); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/matchups): AddDir treats it
	// as the route tree's root, so page.gsx here answers "/" — enough to
	// drive one real render without pulling every other page's file
	// modules (and their own env/store needs) into this test.
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (matchups page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("matchups page rendered the error page instead of matchup cards: %s", body)
	}
	if !strings.Contains(body, "matchup-card") {
		t.Fatalf("expected at least one rendered matchup-card in the response, got: %s", body)
	}
	if strings.Contains(body, "NO MATCHUPS YET") {
		t.Fatalf("expected real seeded matchups, got the empty state: %s", body)
	}
}

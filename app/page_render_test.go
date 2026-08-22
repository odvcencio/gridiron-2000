package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderLandingPage(t *testing.T) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")

	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	t.Setenv("LEAGUE_FILE", leagueFile)

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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestPublicLandingPreservesConfiguredModeAndEventTruth(t *testing.T) {
	body := renderLandingPage(t)

	for _, want := range []string{
		"STABLE KERNEL LEAGUE",
		"redraft format",
		"LEAGUE DRAFT",
		"Saturday, August 29, 2026",
		"4:00 PM EDT",
		"America/New_York",
		"SCHEDULED WINDOW",
		"The commissioner controls when the room opens.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page omitted %q; body: %s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "dynasty scoring") {
		t.Error("redraft landing page still contains dynasty scoring copy")
	}
	if strings.Contains(body, "Doors in") {
		t.Error("landing page still uses auto-start-implying Doors in label")
	}

	h1 := strings.Index(body, "<h1")
	cta := strings.Index(body, `class="button button--primary"`)
	event := strings.Index(body, `class="draft-transmission"`)
	if h1 < 0 || cta < 0 || event < 0 {
		t.Fatalf("landing page missing heading, primary CTA, or event card: h1=%d cta=%d event=%d", h1, cta, event)
	}
	if !(h1 < cta && cta < event) {
		t.Fatalf("semantic reading order must be heading, primary CTA, then event details: h1=%d cta=%d event=%d", h1, cta, event)
	}
	if got := strings.Count(body, `class="button button--primary"`); got != 1 {
		t.Fatalf("public landing page has %d primary CTAs, want exactly one", got)
	}
}

func TestPublicEntrySourceKeepsNarrowAndMotionContracts(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	stylesheet := string(css)
	for _, want := range []string{
		"@media (max-width: 20rem)",
		"@media (prefers-reduced-motion: reduce)",
		".hero-command > *",
		".login-stage > *",
		"overflow-wrap: anywhere",
		"outline: 2px solid var(--color-accent-cyan)",
	} {
		if !strings.Contains(stylesheet, want) {
			t.Errorf("stylesheet omitted public-entry contract %q", want)
		}
	}
}

func TestHomepageMatchupPreviewOnlyShowsLiveIndicatorsInProgress(t *testing.T) {
	for _, fixture := range []struct {
		state string
		show  bool
	}{
		{state: "preseason"},
		{state: "scheduled"},
		{state: "in_progress", show: true},
		{state: "final"},
		{state: "degraded"},
	} {
		t.Run(fixture.state, func(t *testing.T) {
			raw := []map[string]any{{
				"id": "matchup-1", "state": fixture.state,
				"show_live_indicator": fixture.show, "status": "Status", "clock": "Clock",
				"away": map[string]any{"id": "team-1", "name": "Away", "manager": "A", "score": "0.0", "tone": "cyan", "abbreviation": "AWY"},
				"home": map[string]any{"id": "team-2", "name": "Home", "manager": "H", "score": "0.0", "tone": "red", "abbreviation": "HME"},
			}}
			cards := dashboardMatchupCards(raw)
			if len(cards) != 1 || cards[0].State != fixture.state || cards[0].ShowLiveIndicator != fixture.show {
				t.Fatalf("converted cards = %+v", cards)
			}
			program, err := route.LoadFileProgram("page.gsx")
			if err != nil {
				t.Fatal(err)
			}
			html, err := route.RenderProgramComponent(program, "MiniMatchup", route.ProgramRenderEnv{
				Values: map[string]any{"props": cards[0]},
			})
			if err != nil {
				t.Fatal(err)
			}
			hasDot := strings.Contains(html, `class="live-dot"`)
			if hasDot != fixture.show {
				t.Fatalf("state %s live-dot presence = %v, want %v: %s", fixture.state, hasDot, fixture.show, html)
			}
		})
	}

	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `<If cond={data.live.show_live_indicator}>`) {
		t.Fatal("homepage matchup preview masthead live dot is not gated by live state")
	}
}

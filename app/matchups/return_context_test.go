package matchups

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

func TestMatchupsProjectionHelpHrefPreservesSelectedTaskContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/matchups?week=3&view=mine", nil)
	got := matchupsProjectionHelpHref(request)
	want := "/help/lineups-locks-matchups-and-scoring?return_to=%2Fmatchups%3Fweek%3D3%26view%3Dmine%23main-content"
	if got != want {
		t.Fatalf("matchups contextual help href = %q, want %q", got, want)
	}
}

func TestMatchupsProjectionHelpMarkupUsesServerHref(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, "href={data.projection_help_href}") {
		t.Fatal("Matchups status markup does not use its server-provided contextual help href")
	}
	if !strings.Contains(source, "Read projection guidance →") {
		t.Fatal("Matchups status markup lost its projection guidance label")
	}
}

func TestMatchupsProjectionHelpRenderUsesContextualHref(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	href := matchupsProjectionHelpHref(httptest.NewRequest(http.MethodGet, "/matchups?week=3", nil))
	data := map[string]any{
		"status_line": map[string]any{
			"live_state":       "LEDGER",
			"closed_early":     false,
			"source_line":      "Weekly ledger (nflverse)",
			"games_final":      "0/16 FINAL",
			"checked_at":       "Unavailable",
			"stats_updated_at": "Unavailable",
		},
		"live": map[string]any{
			"live_indicator": "",
			"live_status":    "CACHED",
			"refresh_label":  "Refresh available",
		},
		"projection_help_href": href,
		"has_week_notice":      false,
	}
	rendered, err := route.RenderProgramComponent(program, "MatchupStatusBlock", route.ProgramRenderEnv{Values: map[string]any{
		"data": data,
	}})
	if err != nil {
		t.Fatal(err)
	}
	escaped := html.EscapeString(href)
	want := "href=\"" + escaped + "\""
	if strings.Count(rendered, want) != 1 {
		t.Fatalf("contextual projection help href rendered %d times, want once: %s", strings.Count(rendered, want), rendered)
	}
	if strings.Contains(rendered, "href=\"/help/lineups-locks-matchups-and-scoring\"") {
		t.Fatalf("rendered status ignored its contextual projection help href: %s", rendered)
	}
}

package matchups

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "gridiron-2000/app/help"
	_ "gridiron-2000/app/help/_topic_id"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderMatchupProjectionStatus(t *testing.T, projectionNote any) string {
	t.Helper()
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]any{
		"live_indicator": "",
		"live_status":    "CACHED",
		"refresh_label":  "Refresh available",
	}
	if projectionNote != nil {
		live["projection_note"] = projectionNote
	}
	data := map[string]any{
		"status_line": map[string]any{
			"live_state":       "LEDGER",
			"closed_early":     false,
			"source_line":      "Weekly ledger (nflverse)",
			"games_final":      "0/16 FINAL",
			"checked_at":       "Unavailable",
			"stats_updated_at": "Unavailable",
		},
		"live":            live,
		"has_week_notice": false,
	}
	html, err := route.RenderProgramComponent(program, "MatchupStatusBlock", route.ProgramRenderEnv{Values: map[string]any{
		"data": data,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return html
}

// TestMatchupProjectionHelpStaysTruthfulAcrossLiveProjectionStates covers
// the projection-note seam's three render shapes: an available note, an
// unavailable/mismatch note, and a missing note. The note is live-bound text,
// but this endpoint exposes no guidance href projection for GoSX to bind. A
// state=unavailable URL would therefore become stale after a poll changes the
// note. The stable owning topic link remains truthful in every state and keeps
// the live status line intact.
func TestMatchupProjectionHelpStaysTruthfulAcrossLiveProjectionStates(t *testing.T) {
	for _, test := range []struct {
		name string
		note any
		want string
	}{
		{name: "available", note: "", want: ""},
		{name: "missing", note: nil, want: ""},
		{name: "unavailable mismatch", note: " · Projections unavailable for Week 1; latest source snapshot is Week 2.", want: "Projections unavailable for Week 1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			html := renderMatchupProjectionStatus(t, test.note)
			link := "<a class=\"access-link matchup-status-line__projection-help\" href=\"/help/lineups-locks-matchups-and-scoring\" data-gosx-link>Read projection guidance →</a>"
			if got := strings.Count(html, link); got != 1 {
				t.Fatalf("projection guidance link rendered %d times, want once: %s", got, html)
			}
			if strings.Contains(html, "state=unavailable") {
				t.Fatal("projection guidance link must not freeze a live projection state")
			}
			if test.want != "" && !strings.Contains(html, test.want) {
				t.Fatalf("projection note omitted %q: %s", test.want, html)
			}

			if !strings.Contains(html, "data-gosx-live-bind=\"projectionNote\"") {
				t.Fatal("projection note lost its live binding")
			}
		})
	}
}

func TestMatchupProjectionHelpTargetReachesOwningTopic(t *testing.T) {
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help", body))
	})
	if err := router.AddDir("../help", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir help: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked help: %v", err)
	}
	handler = http.StripPrefix("/help", handler)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/help/lineups-locks-matchups-and-scoring", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("owning projection-help GET = %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Lineups, locks, matchups, and scoring") {
		t.Fatalf("owning projection-help topic is missing from response: %s", response.Body.String())
	}
}

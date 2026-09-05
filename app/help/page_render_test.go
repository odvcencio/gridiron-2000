package help

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderHelpRoute(t *testing.T, target string) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "help-state.json"))
	t.Setenv("DEMO_MODE", "false")
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestHelpIndexRendersSearchAndProjectionMarkers(t *testing.T) {
	body := renderHelpRoute(t, "/?q=draft+queue")
	for _, want := range []string{
		"HELP CENTER",
		"Search is deterministic",
		"big-board-and-autopick",
		"One corpus. Stable routes.",
		"The checklist follows the person.",
		"Concept transition",
		"CANONICAL VOCABULARY",
		"Every state explains the way back.",
		"Runtime-owned",
		"FAAB",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("help index omitted %q", want)
		}
	}
	if strings.Contains(body, "commissioner"+"@"+"example.invalid") || strings.Contains(body, "stablekernel"+".invalid") {
		t.Fatal("help index leaked identity/domain PII")
	}
}

// TestHelpIndexGuardsSentinelDraftDate is the /help half of the wave-1
// sentinel-date audit finding: the neutral shipped config (no league.json
// in this test tree) carries the placeholder draft instant 2099-01-01
// (config.go), and the masthead console used to print it as a live
// "Next draft meeting" fact instead of the DraftDatePublished guard
// draftSummaryForState already applies to / and /guide.
func TestHelpIndexGuardsSentinelDraftDate(t *testing.T) {
	body := renderHelpRoute(t, "/")
	if strings.Contains(body, "2098") || strings.Contains(body, "2099") {
		t.Fatalf("help index rendered the sentinel draft year: %s", body)
	}
	if !strings.Contains(body, "Not published yet") {
		t.Fatalf("help index did not render the unpublished-draft guard text: %s", body)
	}
}

// TestHelpIndexGlossaryAndMigrationRenderContent pins F1/F2: the glossary and
// migration table used to reach the template as []struct, so the template's
// lowercase map-key reads (entry.term, mapping.canonical, ...) always
// resolved empty and every glossary "Topic ->" link pointed at bare "/help/".
func TestHelpIndexGlossaryAndMigrationRenderContent(t *testing.T) {
	body := renderHelpRoute(t, "/")
	firstGlossary := glossaryEntries[0]
	if !strings.Contains(body, firstGlossary.Term) {
		t.Errorf("help index glossary omitted the first term %q", firstGlossary.Term)
	}
	if !strings.Contains(body, firstGlossary.Definition) {
		t.Errorf("help index glossary omitted the first definition %q", firstGlossary.Definition)
	}
	wantLink := "/help/" + firstGlossary.TopicID
	if !strings.Contains(body, wantLink) {
		t.Errorf("help index glossary link = missing %q", wantLink)
	}
	if strings.Contains(body, `href="/help/" data-gosx-link>Topic`) {
		t.Error("help index glossary still links a blank topic id")
	}

	firstMapping := migrationMappings[0]
	if !strings.Contains(body, firstMapping.Canonical) {
		t.Errorf("help index migration table omitted the first canonical name %q", firstMapping.Canonical)
	}
	if !strings.Contains(body, firstMapping.Difference) {
		t.Errorf("help index migration table omitted the first difference %q", firstMapping.Difference)
	}
}

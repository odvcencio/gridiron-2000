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

// The search-determinism and "Mutable truth — Runtime-owned" sentences this
// test used to pin were F23's own findings (J6, 2026-09-04 audit): internal
// corpus/schema language ("CORPUS 0.1", determinism, the SHA sentence, the
// leaked relevance score) on the one page whose job is to lower the
// vocabulary barrier for a manager. Search still sorts deterministically —
// TestSearchGoldensAreDeterministic pins that behavior — the sentence
// announcing it to a manager is gone.
func TestHelpIndexRendersSearchAndProjectionMarkers(t *testing.T) {
	body := renderHelpRoute(t, "/?q=draft+queue")
	for _, want := range []string{
		"HELP CENTER",
		"Results are sorted by match quality",
		"big-board-and-autopick",
		"One corpus. Stable routes.",
		"The checklist follows the person.",
		"Coming from another app",
		"CANONICAL VOCABULARY",
		"Every state explains the way back.",
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

// TestWaiverModeNoteMatchesConfiguredMode is J6 F8's search-card fix: the
// waivers search result used to name "FAAB" as this league's system no
// matter what the league actually runs. The neutral test tree's config
// (config.go's DefaultConfig) runs perf-priority waivers, so the rendered
// result must say so and must not claim a FAAB budget.
func TestWaiverModeNoteMatchesConfiguredMode(t *testing.T) {
	body := renderHelpRoute(t, "/?q=how+do+waivers+work")
	if !strings.Contains(body, "This league does not use FAAB") {
		t.Errorf("help search result for waivers omitted the configured-mode note: %s", body)
	}
	if strings.Contains(body, "score 1000") || strings.Contains(body, "· score") {
		t.Error("help search result still leaks a relevance score onto the page")
	}
}

// TestHelpIndexOmitsInternalSchemaLanguage pins J6 F23 (2026-09-04 audit):
// the Help Center — the one page whose job is to lower the vocabulary
// barrier — used to show its own corpus version, schema field names, a
// determinism sentence, and a raw source SHA to every manager.
func TestHelpIndexOmitsInternalSchemaLanguage(t *testing.T) {
	body := renderHelpRoute(t, "/")
	for _, banned := range []string{
		"CORPUS 0.1",
		"Mutable truth",
		"Every topic names the actor, prerequisite, privacy, consequence",
		"Search is deterministic",
		"last verified source SHA",
	} {
		if strings.Contains(body, banned) {
			t.Errorf("help index still shows internal corpus language %q", banned)
		}
	}
}

// TestHelpChecklistLinksNameTheirDestination pins J6 F35 (2026-09-04
// audit): roughly twenty role-checklist items all ended with the identical
// link text "Open help/action ->" — a screen reader listing links on the
// page heard the same name for every destination.
func TestHelpChecklistLinksNameTheirDestination(t *testing.T) {
	body := renderHelpRoute(t, "/")
	if strings.Contains(body, "Open help/action") {
		t.Error("help checklist still renders the generic \"Open help/action\" link text")
	}
	if !strings.Contains(body, "Open the Team terminal") {
		t.Error("help checklist did not name a real destination (Open the Team terminal)")
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

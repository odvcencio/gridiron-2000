package blitz

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

func TestBlitzPageRendersDisabledSourceCopy(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("TANK01_API_KEY", "")
	t.Setenv("TANK01_BASE_URL", "")

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
	record := httptest.NewRecorder()
	handler.ServeHTTP(record, httptest.NewRequest(http.MethodGet, "/", nil))
	if record.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200; body: %s", record.Code, record.Body.String())
	}
	body := record.Body.String()
	if strings.Contains(body, "render strict component") || strings.Contains(body, "WENT DARK") {
		t.Fatalf("blitz page rendered an error page: %s", body)
	}
	if !strings.Contains(body, "PRESEASON SCORES NOT OPEN") {
		t.Fatalf("disabled-source copy missing: %s", body)
	}
}

func TestBlitzRoleStateRenderUsesCanonicalPublicEntry(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, want := range []string{
		`<If cond={data.can_enter == false}>`,
		`{data.public_entry.state_label}`,
		`{data.public_entry.detail}`,
		`href={data.public_entry.action_href}`,
		`{data.public_entry.action_label}`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("Blitz role-state render missing canonical public-entry field %q", want)
		}
	}
	if strings.Contains(source, `href="/join"`) {
		t.Fatal("Blitz role-state render must not offer a hard-coded /join action")
	}
}

// TestBlitzArchiveChampionCopyGuardsEmptyChampions is item 5's own
// regression test (2026-09-02 audit): blitzArchiveMap (internal/league/
// blitz.go) can leave overall_champion/pre2_champion/pre3_champion empty
// — no entries were scored for a slate, or no row ranked "01" — and the
// archive copy used to interpolate those blindly, printing "OVERALL
// CHAMPION:" with nothing after the colon and "Preseason Week 2
// champion: . Preseason Week 3 champion: .". Each of the three now
// renders an honest "no champion" fallback instead of a dangling label.
func TestBlitzArchiveChampionCopyGuardsEmptyChampions(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, want := range []string{
		`<If cond={data.archive.overall_champion != ""}>`,
		`<strong>OVERALL CHAMPION: {data.archive.overall_champion}</strong>`,
		`<If cond={data.archive.overall_champion == ""}>`,
		`<strong>OVERALL CHAMPION: no entries were scored</strong>`,
		`<If cond={data.archive.pre2_champion != ""}>Preseason Week 2 champion: {data.archive.pre2_champion}. </If>`,
		`<If cond={data.archive.pre2_champion == ""}>Preseason Week 2 — no champion recorded. </If>`,
		`<If cond={data.archive.pre3_champion != ""}>Preseason Week 3 champion: {data.archive.pre3_champion}.</If>`,
		`<If cond={data.archive.pre3_champion == ""}>Preseason Week 3 — no champion recorded.</If>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("Blitz archive champion guard missing %q", want)
		}
	}
	if strings.Contains(source, "champion: {data.archive.pre2_champion}. Preseason Week 3 champion: {data.archive.pre3_champion}.") {
		t.Error("Blitz archive copy must not interpolate an unguarded pre2/pre3 champion on one shared line")
	}
}

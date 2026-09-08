package help

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

// runHelpHeroFixture renders /help in a fresh subprocess (its own
// league.Default() singleton), the same isolation technique
// app/page_render_test.go's runHomepageStandingsFixture uses — a
// completed draft or an overridden harness clock must never leak into
// this package's other tests, which share one process-wide singleton
// within a single `go test` run.
func runHelpHeroFixture(t *testing.T, fixture string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelpHeroFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"HELP_HERO_RENDER_FIXTURE="+fixture,
		"DATA_FILE="+filepath.Join(t.TempDir(), "help-hero-state.json"),
		"DEMO_MODE=false",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help hero %s fixture: %v\n%s", fixture, err, output)
	}
	return string(output)
}

func TestHelpHeroFixtureProcess(t *testing.T) {
	fixture := os.Getenv("HELP_HERO_RENDER_FIXTURE")
	if fixture == "" {
		t.Skip("fixture helper")
	}
	svc := league.Default()
	switch fixture {
	case "draft-complete":
		if err := svc.CompleteDraftForTest(); err != nil {
			t.Fatal(err)
		}
	case "harness-clock-preseason":
		past := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
		svc.SetClockForTest(func() time.Time { return past })
	}
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
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", rec.Code, rec.Body.String())
	}
	fmt.Print(rec.Body.String())
}

// TestHelpHeroReadsDraftCompleteWithItsDateAfterTheDraft is the Help
// hero card residue: after the draft, /help's runtime console still
// read "Next draft meeting · <date>" as if the meeting were still
// ahead. Once the draft is complete, it now reads "Draft complete",
// never a future-tense "Next draft meeting" promise.
func TestHelpHeroReadsDraftCompleteWithItsDateAfterTheDraft(t *testing.T) {
	body := runHelpHeroFixture(t, "draft-complete")
	if strings.Contains(body, "Next draft meeting") {
		t.Errorf("help hero still reads Next draft meeting after the draft is complete: %s", body)
	}
	if !strings.Contains(body, "Draft complete") {
		t.Errorf("help hero does not read Draft complete: %s", body)
	}
}

// TestHelpHeroPhaseReadsTheHarnessClockLikeTheConsoleDoes pins the
// runtime console's phase field to the same clock the commissioner
// console reads (league.Service.Now, harness-adjustable), not the bare
// wall clock: a harness clock fixed well before season start must read
// PRESEASON even though the real wall clock (whenever this test
// actually runs) may already be well past it.
func TestHelpHeroPhaseReadsTheHarnessClockLikeTheConsoleDoes(t *testing.T) {
	body := runHelpHeroFixture(t, "harness-clock-preseason")
	if !strings.Contains(body, "PRESEASON") {
		t.Fatalf("help hero phase did not follow the harness clock: %s", body)
	}
}

// TestHelpSearchLedeIsPlainWords is the search panel residue: the lede
// read as corpus/schema jargon ("Search the versioned corpus by
// canonical term, incoming alias, or task question."). It now reads in
// plain words a manager coming from another fantasy app understands.
func TestHelpSearchLedeIsPlainWords(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if strings.Contains(source, "Search the versioned corpus by canonical term, incoming alias, or task question.") {
		t.Fatal("page.gsx still carries the jargon search lede")
	}
	if !strings.Contains(source, "Search by a question, a word from the app, or a word from another fantasy app.") {
		t.Fatal("page.gsx is missing the plain-words search lede")
	}
}

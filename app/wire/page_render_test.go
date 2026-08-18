package wire

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	signalwire "gridiron-2000/internal/wire"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestWirePageRendersSignalCardsWithRealData is the regression guard for
// the untyped-legacy retirement: SignalCard and WireEmptyState used to
// read dynamic map fields (props.category, props.wire_configured); they
// are now strict components whose props must resolve through a real
// render. It drives a real HTTP GET through the actual file router — the
// same route.AddDir mechanism main.go uses to mount every page —
// following app/matchups and app/join's harness, after submitting a real
// community sighting so data.signals is genuinely non-empty.
func TestWirePageRendersSignalCardsWithRealData(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	// signalwire.ConfigFromEnv defaults WIRE_ROOT to the on-disk,
	// cwd-relative "data/signal-wire" — the same DATA_FILE-not-DATA_DIR
	// class of footgun this task's instructions warn about for the league
	// store. Scope it to this test's own temp dir so a real sighting
	// submission below never writes into (or reads stale dedup state
	// from) the shared local checkout.
	t.Setenv("WIRE_ROOT", t.TempDir())

	signals, err := signalwire.Default()
	if err != nil {
		t.Fatalf("signalwire.Default: %v", err)
	}
	if _, err := signals.SubmitSighting(signalwire.CommunitySubmission{
		ReporterID:   "demo-commissioner",
		ReporterName: "Demo commissioner",
		EvidenceType: "community",
		SourceName:   "League group chat",
		Summary:      "Starting RB1 looked limited in pregame warmups.",
	}); err != nil {
		t.Fatalf("seed sighting: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Test"), ctx.Head(), body)
	})
	// "." is this package's own directory (app/wire): AddDir treats it as
	// the route tree's root, so page.gsx here answers "/" — enough to
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
		t.Fatalf("GET / (wire page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("wire page rendered the error page instead of signal cards: %s", body)
	}
	if !strings.Contains(body, "wire-event") {
		t.Fatalf("expected at least one rendered wire-event card in the response, got: %s", body)
	}
	if !strings.Contains(body, "League group chat") {
		t.Fatalf("expected the seeded sighting's source to render, got: %s", body)
	}
	if strings.Contains(body, "NO SIGNALS YET") {
		t.Fatalf("expected the seeded sighting to render, not the empty state: %s", body)
	}
}

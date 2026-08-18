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

// TestFeedFragmentMatchesPageRenderByteForByte is the equivalence proof for
// gosx#226's fix: FeedFragment must render the page's own SignalCard /
// WireEmptyState components, not a hand-mirrored second copy of their
// markup, so the fragment data-gosx-region polls for is always
// byte-for-byte the same markup the page itself embeds (mirroring gosx's
// own TestFragmentHandlerServesPageOwnComponentForLiveRegion and
// TestRenderProgramComponentStrictEntryMatchesNestedRenderByteForByte).
//
// It drives real HTTP GETs for the page through the actual file router
// (the same AddDir mechanism main.go uses to mount every page) and
// separately calls FeedFragment against the same signalwire.Service, in
// two ordered phases sharing one process-wide signalwire.Default()
// singleton (mirroring how main.go shares one *signalwire.Service between
// the page's Load and the /wire/fragment handler):
//
//  1. Before any sighting exists, both the page and the fragment render
//     WireEmptyState.
//  2. After one real community sighting is submitted, both the page's own
//     <Each> loop and the fragment render SignalCard from the exact same
//     signalMap data.
//
// In each phase, the fragment's returned HTML must appear verbatim inside
// the page's own body.
func TestFeedFragmentMatchesPageRenderByteForByte(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("WIRE_ROOT", filepath.Join(t.TempDir(), "signal-wire"))
	t.Setenv("OPEN_STATS_ROOT", filepath.Join(t.TempDir(), "open-stats"))

	signals, err := signalwire.Default()
	if err != nil {
		t.Fatalf("signalwire.Default: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Test"), ctx.Head(), body)
	})
	// "." is this package's own directory (app/wire): AddDir treats it as
	// the route tree's root, so page.gsx here answers "/".
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	renderPage := func() string {
		t.Helper()
		pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
		pageRec := httptest.NewRecorder()
		handler.ServeHTTP(pageRec, pageReq)
		if pageRec.Code != http.StatusOK {
			t.Fatalf("GET / (wire page) = %d, want 200; body: %s", pageRec.Code, pageRec.Body.String())
		}
		body := pageRec.Body.String()
		if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
			t.Fatalf("wire page rendered the error page instead of the feed: %s", body)
		}
		return body
	}

	renderFragment := func() string {
		t.Helper()
		fragmentReq := httptest.NewRequest(http.MethodGet, "/wire/fragment", nil)
		html, err := FeedFragment(fragmentReq, signals)
		if err != nil {
			t.Fatalf("FeedFragment: %v", err)
		}
		return html
	}

	// Phase 1: the empty-feed branch, WireEmptyState on both sides.
	if got := len(signals.Recent(50, "")); got != 0 {
		t.Fatalf("expected an empty feed before any sighting is submitted, got %d signals", got)
	}
	emptyPageBody := renderPage()
	if !strings.Contains(emptyPageBody, "data-wire-empty") {
		t.Fatalf("expected the empty-state panel in the page body, got: %s", emptyPageBody)
	}
	emptyFragmentHTML := renderFragment()
	if !strings.Contains(emptyFragmentHTML, "data-wire-empty") {
		t.Fatalf("fragment markup missing the empty-state panel: %s", emptyFragmentHTML)
	}
	if !strings.Contains(emptyPageBody, emptyFragmentHTML) {
		t.Fatalf("page body does not embed the exact empty-state fragment markup verbatim.\nfragment: %s\npage: %s", emptyFragmentHTML, emptyPageBody)
	}

	// Phase 2: the non-empty branch, SignalCard on both sides.
	signal, err := signals.SubmitSighting(signalwire.CommunitySubmission{
		ReporterID:   "manager-1",
		ReporterName: "Alex Manager",
		EvidenceType: "market",
		SourceName:   "PrizePicks",
		SourceURL:    "https://www.prizepicks.com/",
		Summary:      "Player was removed from the board",
	})
	if err != nil {
		t.Fatalf("SubmitSighting: %v", err)
	}
	if signal.Label == "" {
		t.Fatalf("seeded signal has no label: %+v", signal)
	}

	pageBody := renderPage()
	if !strings.Contains(pageBody, "data-wire-event") {
		t.Fatalf("expected at least one rendered wire-event card in the page body, got: %s", pageBody)
	}
	fragmentHTML := renderFragment()
	if fragmentHTML == "" {
		t.Fatal("FeedFragment returned empty markup for a non-empty feed")
	}
	if !strings.Contains(fragmentHTML, "data-wire-event") {
		t.Fatalf("fragment markup missing a wire-event card: %s", fragmentHTML)
	}
	if !strings.Contains(pageBody, fragmentHTML) {
		t.Fatalf("page body does not embed the exact fragment markup verbatim.\nfragment: %s\npage: %s", fragmentHTML, pageBody)
	}
}

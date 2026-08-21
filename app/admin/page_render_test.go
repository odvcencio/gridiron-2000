package admin

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

// renderAdminPage drives a real HTTP GET through the file router against
// this package's page.gsx and page.server.go as they sit on disk. Demo mode
// opens the console to any viewer, which is what lets this render without a
// signed-in commissioner. See app/matchups/page_render_test.go for the
// harness this mirrors.
func renderAdminPage(t *testing.T) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

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
		t.Fatalf("GET / (admin page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestAdminPageOffersSeatTrimBeforeTheDraft guards the control the league
// runs an hour before the draft. The trim itself was implemented and tested
// at the service layer (Service.TrimUnclaimedSeats) but reached no page, so
// the commissioner had no way to invoke it and unclaimed seats would have
// drafted. A service-layer test cannot catch that: the gap was the absence
// of markup, so only a render assertion holds it down.
//
// The stakes are concrete. An unclaimed seat keeps its place in DraftOrder,
// so it takes a turn every round, runs the full pick clock down with nobody
// there, and then autopicks.
func TestAdminPageOffersSeatTrimBeforeTheDraft(t *testing.T) {
	body := renderAdminPage(t)

	if !strings.Contains(body, "seat-trim") {
		t.Fatalf("admin page rendered no seat-trim action with seats unclaimed; body: %s", body)
	}
	if !strings.Contains(body, "Drop unclaimed seats") {
		t.Errorf("seat-trim control rendered without its button label")
	}
	// The runbook must name the control, and name it before randomizing:
	// randomizing first produces an order still listing the trimmed seats.
	trimStep := strings.Index(body, "drop the seats nobody claimed")
	randomizeStep := strings.Index(body, "Randomize the draft order")
	if trimStep < 0 {
		t.Errorf("draft-night runbook does not mention dropping unclaimed seats")
	}
	if randomizeStep >= 0 && trimStep >= 0 && trimStep > randomizeStep {
		t.Errorf("runbook lists randomize before trim; the trim resets draft order, so it must come first")
	}
}

func TestAdminPageHasOnePageLevelIdentityWarning(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	if got := strings.Count(markup, `role="status"`); got != 1 {
		t.Fatalf("admin page has %d status roles, want exactly one page-level identity warning", got)
	}
	warning := strings.Index(markup, `id="admin-identity-status"`)
	seatList := strings.Index(markup, `<div class="seat-list">`)
	if warning < 0 || seatList < 0 || warning > seatList {
		t.Fatalf("identity warning must appear once above seat list: warning=%d seatList=%d", warning, seatList)
	}
	seatRow := strings.Index(markup, "func SeatRow")
	page := strings.Index(markup, "func Page")
	if seatRow < 0 || page < 0 || seatRow >= page {
		t.Fatal("could not isolate SeatRow source")
	}
	if strings.Contains(markup[seatRow:page], `role="status"`) {
		t.Fatal("SeatRow repeats the identity status warning; rows should only hide identity controls")
	}
}

// The companion property — that the control disappears once the first pick
// lands — is pinned in internal/league's TestAdminDataLocksSeatTrimOnceDraftStarts
// rather than here. league.Default() is a sync.Once singleton, so a second
// render in this package cannot be given different state, and MakePick needs
// both a live draft window and a real pool player. The league-package test
// drives the store directly, which is where that state is reachable.

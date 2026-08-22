package trades

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestTradesPageRendersWithRealData is the regression guard for the
// map-to-struct conversion of TradesData's counterparties/roster-option/
// offer values (untyped-legacy retirement): Page() now reads each list
// entry as a typed value (team.ID/team.Name, opt.ID/opt.Label,
// offer.FromTeam/.Give/.Get, ...) instead of a dynamic map, so this
// drives a real HTTP GET through the actual file router — the same
// route.AddDir mechanism main.go uses to mount every page — against this
// package's page.gsx and page.server.go exactly as they sit on disk,
// following app/matchups and app/join's harness. The fixture claims two
// seats so the counterparty list reflects the product rule that only
// managed franchises are valid trade partners. It has no drafted rosters
// yet, so the compose panel's own roster-option list renders empty; the
// inbox/outbox/review/
// vote sections exercise their real (here, honestly empty) states — the
// conversion's main regression risk is any of these typed lists failing
// to flow through their Each loop, which this proves for every one of
// them except the offer-row shape itself (TradeOfferRow), which requires
// a real trade offer this task's scope also cannot seed without touching
// draft/roster state.
func TestTradesPageRendersWithRealData(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "league-state.json")
	t.Setenv("DATA_FILE", dataFile)
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	store := league.NewStore(dataFile)
	if _, _, err := store.AssignMember("one@example.com", "One"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AssignMember("two@example.com", "Two"); err != nil {
		t.Fatal(err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/trades): AddDir treats it
	// as the route tree's root, so page.gsx here answers "/" — enough to
	// drive one real render without pulling every other page's file
	// modules (and their own env/store needs) into this test.
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	// The demo viewer's team is always Teams()[0] ("team-1"), so
	// ?counterparty=team-2 exercises the compose panel's counterparty
	// picker and its (real, if roster-empty) TradeRosterOption list.
	req := httptest.NewRequest(http.MethodGet, "/?counterparty=team-2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /?counterparty=team-2 (trades page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("trades page rendered the error page instead of the trade desk: %s", body)
	}
	if !strings.Contains(body, "filter-button") {
		t.Fatalf("expected at least one rendered counterparty filter button, got: %s", body)
	}
	if !strings.Contains(body, "trade-composer") {
		t.Fatalf("expected the compose form to render for the chosen counterparty, got: %s", body)
	}
	if !strings.Contains(body, "NO INCOMING OFFERS") || !strings.Contains(body, "NO OPEN OR PENDING OFFERS") {
		t.Fatalf("expected the honest empty inbox/outbox state on a fresh league, got: %s", body)
	}
}

package trades

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
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
	fragment, err := tradesFragmentRender(league.Default().TradesDataReadOnly(req), req)
	if err != nil {
		t.Fatalf("render Trade Desk fragment: %v", err)
	}
	if !strings.Contains(fragment, "trade-composer") || !strings.Contains(fragment, "NO INCOMING OFFERS") || strings.Contains(fragment, "<main") {
		t.Fatalf("Trade Desk fragment diverged from its scoped region: %s", fragment)
	}
}

// TestTradesSeatlessBannerPunctuationHasNoSpaceBeforeColon pins wave-2-
// verification item 10: the seatless-viewer banner ("ADMITTED · NO
// FRANCHISE :") carried a space before its colon because
// {data.public_entry.state_label} and the literal ":" sat on separate
// template lines, each padded by the whitespace HTML collapses to one
// visible space. A signed-in member with no team and no open seats left
// (PublicEntryView's PublicEntryAdmittedSeatlessFull state,
// internal/league/public_entry.go) is the only path that reaches this
// banner. league.Default() memoizes its Service singleton for the life of
// the process, so a second in-process test claiming every seat after
// TestTradesPageRendersWithRealData already initialized it against a
// different league-state.json would either reuse stale state or hit
// ErrLeagueFull early; this drives the real HTTP GET in its own
// subprocess instead, mirroring app/admin's own
// TestAdminTaskBoardDraftPhaseFixtureProcess pattern for the identical
// reason.
func TestTradesSeatlessBannerPunctuationHasNoSpaceBeforeColon(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTradesSeatlessBannerFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"TRADES_SEATLESS_BANNER_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seatless-banner fixture: %v\n%s", err, output)
	}
	body := string(output)

	if !strings.Contains(body, "ADMITTED · NO FRANCHISE") {
		t.Fatalf("fixture did not reach the seatless-and-full-league banner state: %s", body)
	}
	if !strings.Contains(body, "<strong>ADMITTED · NO FRANCHISE:</strong>") {
		t.Errorf("seatless banner must not have a space before its colon: %s", body)
	}
	if strings.Contains(body, "ADMITTED · NO FRANCHISE :") {
		t.Errorf("seatless banner still has a space before the colon: %s", body)
	}
}

func TestTradesSeatlessBannerFixtureProcess(t *testing.T) {
	if os.Getenv("TRADES_SEATLESS_BANNER_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	for i, team := range service.Teams() {
		if _, err := service.AssignManager(fmt.Sprintf("seat-%d@example.com", i), team.Name); err != nil {
			t.Fatalf("AssignManager seat %d: %v", i, err)
		}
	}
	const seatlessEmail = "seatless@example.com"
	if _, err := service.EnsureMember(seatlessEmail, "Seatless Person"); err != nil {
		t.Fatalf("EnsureMember seatless viewer: %v", err)
	}

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
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: seatlessEmail, Email: seatlessEmail, Name: "Seatless Person"}, true
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	authn.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (trades page, seatless viewer) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	fmt.Print(rec.Body.String())
}

package trades

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gridiron-2000/internal/league"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/route"
)

// tradesInboxFixtureData builds a hand-written data map covering every
// data.*/offer.*/opt.*/team.*/p.* field page.gsx's TradeDeskRegion reads,
// with one open offer in the inbox — the exact row shape F3's evidence
// (Kathleen's own inbox card) captured. Building the map directly (rather
// than driving a live propose/accept flow through a session) keeps this
// fixture independent of draft/roster simulation while still exercising
// the real TradeDeskRegion() render path byte-for-byte, the same
// technique app/players/region_parity_test.go already uses for its own
// two-step confirmation gate.
func tradesInboxFixtureData() map[string]any {
	inboxOffer := league.TradeOfferRow{
		ID:             "trd-fixture",
		Status:         league.TradeStatusOpen,
		StatusLabel:    "Open",
		FromTeam:       "Los Delfines del Norte",
		FromTeamID:     "team-2",
		Give:           []league.TradePlayerCard{{ID: "p-give", Name: "Josh Allen", Position: "QB", Team: "BUF"}},
		Get:            []league.TradePlayerCard{{ID: "p-get", Name: "Ja'Marr Chase", Position: "WR", Team: "CIN"}},
		CanAccept:      true,
		CanDecline:     true,
		CanCounter:     false,
		HasExpiry:      true,
		Expiry:         "Sep 17, 9:48 PM EDT",
		ExpiryRelative: "in 6 days",
		ExpiryState:    "upcoming",
	}
	return map[string]any{
		"viewer": map[string]any{"demo": false, "team_id": "team-3"},
		"public_entry": map[string]any{
			"state_label":        "",
			"detail":             "",
			"can_claim":          false,
			"action_href":        "/join",
			"action_label":       "",
			"is_commissioner":    false,
			"commissioner_href":  "",
			"commissioner_label": "",
		},
		"can_edit":                  true,
		"can_compose":               true,
		"is_commissioner":           false,
		"veto_mode":                 "commissioner",
		"veto_policy_label":         "Veto policy: commissioner review",
		"veto_summary_sentence":     "Trades are reviewed by the commissioner for 24 hours after acceptance, then execute.",
		"trade_accept_consequence":  "Accept: the trade goes to commissioner review for 24 hours, then executes.",
		"trade_deadline_configured": false,
		"trade_deadline_passed":     false,
		"trade_deadline":            "",
		"trade_deadline_relative":   "",
		"trade_deadline_state":      "none",
		"note_max":                  280,
		"counterparties":            []league.TradeCounterparty{},
		"counterparties_empty":      true,
		"my_options":                []league.TradeRosterOption{},
		"my_options_empty":          true,
		"compose_note":              "",
		"compose_counterparty_id":   "",
		"compose_counterparty_name": "",
		"compose_active":            false,
		"compose_options":           []league.TradeRosterOption{},
		"compose_options_empty":     true,
		"inbox":                     []league.TradeOfferRow{inboxOffer},
		"inbox_empty":               false,
		"empty_inbox_message":       "",
		"outbox":                    []league.TradeOfferRow{},
		"outbox_empty":              true,
		"pending_review":            []league.TradeOfferRow{},
		"pending_review_empty":      true,
		"review":                    []league.TradeOfferRow{},
		"review_empty":              true,
		"vote_panel":                []league.TradeOfferRow{},
		"vote_panel_empty":          true,
		"history":                   []league.TradeOfferRow{},
		"history_empty":             true,
		"section_review_index":      "",
		"section_vote_index":        "",
		"section_history_index":     "04",
	}
}

// tradesInboxFixtureHTML renders TradeDeskRegion() with the fixture above
// and wraps it in a minimal document that links the real stylesheet, so
// the browser check below measures the exact same CSS this app ships.
func tradesInboxFixtureHTML(t *testing.T) string {
	t.Helper()
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatalf("LoadFileProgramHere: %v", err)
	}
	env := route.ProgramRenderEnv{
		Values: map[string]any{
			"data": tradesInboxFixtureData(),
			"csrf": map[string]any{"token": "test-csrf-token", "field": "csrf_token"},
		},
		Funcs: map[string]any{
			"actionPath": func(name string) string { return "/trades/__actions/" + name },
		},
	}
	regionHTML, err := route.RenderProgramComponent(program, "TradeDeskRegion", env)
	if err != nil {
		t.Fatalf("render TradeDeskRegion: %v", err)
	}
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read public/styles.css: %v", err)
	}
	return "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><style>" + string(styles) + "</style></head><body><main class=\"page board-page\" id=\"main-content\">" + regionHTML + "</main></body></html>"
}

// chromeBinaryForTradesTest is a minimal, self-contained copy of the root
// package's own chromePath helper (sim_browser_test.go) — this package
// cannot import the root "main" package's test helpers, and the check
// below needs no other browser-test infrastructure from that file.
func chromeBinaryForTradesTest(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("no Chrome or Chromium on PATH; install one to run browser evidence")
	return ""
}

// TestBrowserTradeInboxOfferBodyDoesNotOverlapAcceptButtonAtPhoneWidth is
// F3's own decisive check, isolated from any live propose/accept session
// flow: .rank-row--wide (the inbox card) kept its desktop two-track
// grid-template-columns (minmax(0, 1fr) auto) at 390px — the auto action
// track (~301px of a 339px row) starved the content track down to 0px, so
// the offer text and the Accept/Decline buttons painted on top of each
// other (public/styles.css:5926, before this fix). The offer body's rect
// must not intersect the Accept button's rect at 390px.
func TestBrowserTradeInboxOfferBodyDoesNotOverlapAcceptButtonAtPhoneWidth(t *testing.T) {
	html := tradesInboxFixtureHTML(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}))
	defer server.Close()

	chrome := chromeBinaryForTradesTest(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome), chromedp.NoSandbox)
	allocator, closeAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	defer closeAllocator()
	ctx, closeBrowser := chromedp.NewContext(allocator)
	defer closeBrowser()
	ctx, cancelBudget := context.WithTimeout(ctx, 30*time.Second)
	defer cancelBudget()

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL),
		chromedp.WaitVisible(`#inbox .rank-row--wide .board-controls`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("load the fixture inbox row: %v", err)
	}

	var intersects bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var row = document.querySelector('#inbox .rank-row--wide');
		if (!row) return true;
		var text = row.querySelector('.pool-player__text');
		var summaries = row.querySelectorAll('.action-confirmation > summary');
		var accept = null;
		for (var i=0;i<summaries.length;i++) {
			if (summaries[i].textContent.indexOf('Accept') >= 0) { accept = summaries[i]; break; }
		}
		if (!text || !accept) return true;
		// getBoundingClientRect() on the container reports its own LAYOUT
		// box — starved to 0px by the grid bug this test pins — not the
		// overflow:visible text it still paints past that box's edge. A
		// Range over the container's contents reports the actual painted
		// line-box rects instead, the same footprint a manager's eyes (or
		// the audit's own screenshot) see.
		var range = document.createRange();
		range.selectNodeContents(text);
		var lineRects = range.getClientRects();
		var b = accept.getBoundingClientRect();
		for (var j=0;j<lineRects.length;j++) {
			var a = lineRects[j];
			if (a.width === 0 || a.height === 0) continue;
			if (!(a.right <= b.left || a.left >= b.right || a.bottom <= b.top || a.top >= b.bottom)) {
				return true;
			}
		}
		return false;
	})()`, &intersects)); err != nil {
		t.Fatalf("read the offer-body/accept-button intersection: %v", err)
	}
	if intersects {
		t.Error("at 390px, the trade inbox offer body rect intersects the Accept button rect")
	}
}

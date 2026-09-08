package trades

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestEmptyInboxMessage is build item 3's copy contract, extended by F12
// (J4 console gap-audit): with no accepted trade awaiting review and no
// open offer sent, the empty inbox reads the generic "nothing waiting"
// line. An accepted trade in review leads (it needs a decision); failing
// that, an open offer the viewer sent reads as open, never "accepted" —
// the trade desk used to call two OPEN offers "accepted trades in review"
// while the review panel itself said there was nothing to review.
func TestEmptyInboxMessage(t *testing.T) {
	cases := []struct {
		name          string
		reviewCount   int
		openSentCount int
		want          string
	}{
		{name: "nothing waiting", reviewCount: 0, openSentCount: 0, want: "Nothing waiting on your response right now."},
		{name: "one accepted trade in review", reviewCount: 1, openSentCount: 0, want: "No new offers — 1 accepted trade is in review below."},
		{name: "multiple accepted trades in review", reviewCount: 2, openSentCount: 0, want: "No new offers — 2 accepted trades are in review below."},
		{name: "one open offer sent, none accepted", reviewCount: 0, openSentCount: 1, want: "No new offers — 1 offer you sent is still open."},
		{name: "two open offers sent, none accepted", reviewCount: 0, openSentCount: 2, want: "No new offers — 2 offers you sent are still open."},
		{name: "review leads over an open offer", reviewCount: 1, openSentCount: 2, want: "No new offers — 1 accepted trade is in review below."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := emptyInboxMessage(c.reviewCount, c.openSentCount); got != c.want {
				t.Errorf("emptyInboxMessage(%d, %d) = %q, want %q", c.reviewCount, c.openSentCount, got, c.want)
			}
		})
	}
}

// TestTradesReviewAndOpenCounts pins F12's own counting (J4 console
// gap-audit), replacing the old tradesAttentionCount's league-wide
// route-prefix match — which could not tell an open offer from an
// accepted one, and conflated another manager's own trades with this
// viewer's inbox. reviewCount reads the viewer's own pending-review rows
// plus any outbox row the other side has already accepted; openSentCount
// reads the viewer's own still-open outbox rows.
func TestTradesReviewAndOpenCounts(t *testing.T) {
	data := map[string]any{
		"pending_review": []league.TradeOfferRow{{ID: "p1", Status: "accepted"}},
		"outbox": []league.TradeOfferRow{
			{ID: "o1", Status: "open"},
			{ID: "o2", Status: "open"},
			{ID: "o3", Status: "accepted"},
		},
	}
	review, open := tradesReviewAndOpenCounts(data)
	if review != 2 {
		t.Errorf("reviewCount = %d, want 2 (1 pending-review + 1 accepted outbox row)", review)
	}
	if open != 2 {
		t.Errorf("openSentCount = %d, want 2 open outbox rows", open)
	}

	if review, open := tradesReviewAndOpenCounts(map[string]any{}); review != 0 || open != 0 {
		t.Errorf("missing keys = (%d, %d), want (0, 0)", review, open)
	}
}

// honestlyEmptyTradesFixture hand-builds a minimal TradeDeskRegion data map
// with an honestly empty inbox and every other section honestly empty too
// (no seat, no compose access) — every field TradeDeskRegion (page.gsx)
// reads, so the render below exercises the real template. It deliberately
// avoids league.Default(): that constructor is a process-wide
// sync.Once singleton (internal/league/service.go), and
// TestTradesPageRendersWithRealData (page_render_test.go) already depends
// on being this package's test binary's first caller of it with its own
// seeded league state — a second caller with different env would either
// win that race or silently reuse the first caller's state.
//
// pendingReview/outbox (F12, J4 console gap-audit) replace the old
// league-wide attention "items" parameter: emptyInboxMessage now counts
// the viewer's own pending-review and outbox rows directly, the same rows
// the page renders below the message, rather than a league-wide item list
// that could not tell an open offer from an accepted one.
func honestlyEmptyTradesFixture(pendingReview, outbox []league.TradeOfferRow) map[string]any {
	emptyOffers := []league.TradeOfferRow{}
	if pendingReview == nil {
		pendingReview = emptyOffers
	}
	if outbox == nil {
		outbox = emptyOffers
	}
	data := map[string]any{
		"viewer":                    map[string]any{"team_id": ""},
		"league":                    map[string]any{"attention": map[string]any{"urgent_count": 0, "items": []map[string]any{}, "has_items": false}},
		"veto_mode":                 "commissioner",
		"veto_policy_label":         "Veto policy: commissioner review",
		"can_edit":                  false,
		"public_entry":              map[string]any{"state_label": "", "detail": "", "action_label": "", "action_href": "/join", "can_claim": false, "is_commissioner": false, "commissioner_href": "", "commissioner_label": ""},
		"can_compose":               false,
		"counterparties":            []league.TradeCounterparty{},
		"counterparties_empty":      true,
		"compose_counterparty_id":   "",
		"compose_active":            false,
		"my_options":                []league.TradeRosterOption{},
		"my_options_empty":          true,
		"compose_counterparty_name": "",
		"compose_options":           []league.TradeRosterOption{},
		"compose_options_empty":     true,
		"note_max":                  280,
		"compose_note":              "",
		"inbox_empty":               true,
		"inbox":                     emptyOffers,
		"outbox_empty":              len(outbox) == 0,
		"outbox":                    outbox,
		"pending_review_empty":      len(pendingReview) == 0,
		"pending_review":            pendingReview,
		"is_commissioner":           false,
		"review_empty":              true,
		"review":                    emptyOffers,
		"vote_panel_empty":          true,
		"vote_panel":                emptyOffers,
		"history_empty":             true,
		"history":                   emptyOffers,
		// section_review_index/section_vote_index/section_history_index
		// (wave-8 audit item 11): neither COMMISSIONER REVIEW (is_commissioner
		// false) nor LEAGUE VOTE (vote_panel_empty true) renders for this
		// fixture's viewer, so HISTORY takes the very next number after
		// PENDING REVIEW's "04".
		"section_review_index":  "",
		"section_vote_index":    "",
		"section_history_index": "05",
	}
	data["empty_inbox_message"] = emptyInboxMessage(tradesReviewAndOpenCounts(data))
	return data
}

// TestTradesEmptyInboxRendersGenericTextWithNoAttentionItem is build item
// 3's honest-empty branch: no pending review and no open outbox offer
// renders the pre-existing generic "nothing waiting" copy.
func TestTradesEmptyInboxRendersGenericTextWithNoAttentionItem(t *testing.T) {
	data := honestlyEmptyTradesFixture(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fragment, err := tradesFragmentRender(data, req)
	if err != nil {
		t.Fatalf("render Trade Desk fragment: %v", err)
	}
	if !strings.Contains(fragment, "NO INCOMING OFFERS") {
		t.Fatalf("fragment missing NO INCOMING OFFERS: %s", fragment)
	}
	if !strings.Contains(fragment, "Nothing waiting on your response right now.") {
		t.Errorf("fragment = %s, want the generic empty-inbox line", fragment)
	}
	if strings.Contains(fragment, "No new offers") {
		t.Errorf("fragment = %s, want no accepted-trade nudge with no attention item", fragment)
	}
}

// TestTradesEmptyInboxNamesAcceptedTradeInReview is build item 3's populated
// branch: an accepted offer sitting in the viewer's own pending-review
// section, with an empty inbox, renders the accepted-trade nudge instead
// of the generic line.
func TestTradesEmptyInboxNamesAcceptedTradeInReview(t *testing.T) {
	data := honestlyEmptyTradesFixture([]league.TradeOfferRow{{ID: "trade-xyz", Status: "accepted"}}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fragment, err := tradesFragmentRender(data, req)
	if err != nil {
		t.Fatalf("render Trade Desk fragment: %v", err)
	}
	if !strings.Contains(fragment, "NO INCOMING OFFERS") {
		t.Fatalf("fragment missing NO INCOMING OFFERS: %s", fragment)
	}
	if !strings.Contains(fragment, "No new offers — 1 accepted trade is in review below.") {
		t.Errorf("fragment = %s, want the accepted-trade-in-review nudge", fragment)
	}
	if strings.Contains(fragment, "Nothing waiting on your response right now.") {
		t.Errorf("fragment = %s, want the generic line replaced", fragment)
	}
}

// TestTradesEmptyInboxReadsOpenOffersAsOpenNotAccepted pins F12 (J4
// console gap-audit): two OPEN offers a manager sent used to render "No
// new offers — 2 accepted trades are in review below." — the review
// panel below said NOTHING TO REVIEW, because both offers were still
// open. With outbox rows Status "open" and an empty pending-review list,
// the message must call them open, never accepted.
func TestTradesEmptyInboxReadsOpenOffersAsOpenNotAccepted(t *testing.T) {
	data := honestlyEmptyTradesFixture(nil, []league.TradeOfferRow{
		{ID: "trade-1", Status: "open"},
		{ID: "trade-2", Status: "open"},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fragment, err := tradesFragmentRender(data, req)
	if err != nil {
		t.Fatalf("render Trade Desk fragment: %v", err)
	}
	want := "No new offers — 2 offers you sent are still open."
	if !strings.Contains(fragment, want) {
		t.Errorf("fragment = %s, want the open-offers nudge %q", fragment, want)
	}
	if strings.Contains(fragment, "2 accepted trade") {
		t.Errorf("fragment = %s, must not call the two open offers accepted", fragment)
	}
}

// TestTradesHistoryEmptyStateDropsTerminalJargon is the coordinator's own
// follow-up (2026-09-08): "NO TERMINAL TRADE HISTORY" used "terminal" as
// unexplained schema jargon (an offer's terminal status: executed,
// declined, withdrawn, and so on). The heading now reads "No trade
// history yet"; the explanatory sentence naming those terminal statuses
// in plain words stays.
func TestTradesHistoryEmptyStateDropsTerminalJargon(t *testing.T) {
	data := honestlyEmptyTradesFixture(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fragment, err := tradesFragmentRender(data, req)
	if err != nil {
		t.Fatalf("render Trade Desk fragment: %v", err)
	}
	if !strings.Contains(fragment, "No trade history yet") {
		t.Errorf("fragment = %s, want the plain-language history empty heading", fragment)
	}
	if strings.Contains(fragment, "TERMINAL") {
		t.Errorf("fragment = %s, must not use the internal word \"terminal\"", fragment)
	}
	if !strings.Contains(fragment, "Executed, declined, withdrawn, countered, vetoed, expired, and failed offers appear here for the participating seats.") {
		t.Errorf("fragment = %s, missing the existing explanatory sentence", fragment)
	}
}

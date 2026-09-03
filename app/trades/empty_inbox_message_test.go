package trades

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestEmptyInboxMessage is build item 3's copy contract: with no accepted
// trade awaiting review the empty inbox reads the generic "nothing
// waiting" line; with one or more, it names the count instead, agreeing
// in both noun (league.Plural) and verb.
func TestEmptyInboxMessage(t *testing.T) {
	cases := []struct {
		name        string
		reviewCount int
		want        string
	}{
		{name: "no accepted trades in review", reviewCount: 0, want: "Nothing waiting on your response right now."},
		{name: "one accepted trade in review", reviewCount: 1, want: "No new offers — 1 accepted trade is in review below."},
		{name: "multiple accepted trades in review", reviewCount: 2, want: "No new offers — 2 accepted trades are in review below."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := emptyInboxMessage(c.reviewCount); got != c.want {
				t.Errorf("emptyInboxMessage(%d) = %q, want %q", c.reviewCount, got, c.want)
			}
		})
	}
}

// TestTradesAttentionCount pins tradesAttentionCount's route-prefix match
// against data.league.attention.items (internal/league's attentionMap):
// only entries whose route starts with /trades count, a /pickem item
// must not, and a missing or reshaped "league"/"attention"/"items" key
// degrades to zero instead of panicking.
func TestTradesAttentionCount(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want int
	}{
		{name: "missing league key", data: map[string]any{}, want: 0},
		{name: "missing attention key", data: map[string]any{"league": map[string]any{}}, want: 0},
		{
			name: "no trades items",
			data: map[string]any{"league": map[string]any{"attention": map[string]any{
				"items": []map[string]any{{"route": "/pickem", "label": "1 open pick'em game this week"}},
			}}},
			want: 0,
		},
		{
			name: "one trades item alongside a pickem item",
			data: map[string]any{"league": map[string]any{"attention": map[string]any{
				"items": []map[string]any{
					{"route": "/trades#trade-abc", "label": "Accepted trade between A and B is in review"},
					{"route": "/pickem", "label": "1 open pick'em game this week"},
				},
			}}},
			want: 1,
		},
		{
			name: "two trades items",
			data: map[string]any{"league": map[string]any{"attention": map[string]any{
				"items": []map[string]any{
					{"route": "/trades#trade-abc", "label": "Accepted trade between A and B is in review"},
					{"route": "/trades#trade-def", "label": "Accepted trade between C and D is in review"},
				},
			}}},
			want: 2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tradesAttentionCount(c.data); got != c.want {
				t.Errorf("tradesAttentionCount(%#v) = %d, want %d", c.data, got, c.want)
			}
		})
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
func honestlyEmptyTradesFixture(items []map[string]any) map[string]any {
	emptyOffers := []league.TradeOfferRow{}
	data := map[string]any{
		"viewer":                    map[string]any{"team_id": ""},
		"league":                    map[string]any{"attention": map[string]any{"urgent_count": len(items), "items": items, "has_items": len(items) > 0}},
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
		"outbox_empty":              true,
		"outbox":                    emptyOffers,
		"pending_review_empty":      true,
		"pending_review":            emptyOffers,
		"is_commissioner":           false,
		"review_empty":              true,
		"review":                    emptyOffers,
		"vote_panel_empty":          true,
		"vote_panel":                emptyOffers,
		"history_empty":             true,
		"history":                   emptyOffers,
	}
	data["empty_inbox_message"] = emptyInboxMessage(tradesAttentionCount(data))
	return data
}

// TestTradesEmptyInboxRendersGenericTextWithNoAttentionItem is build item
// 3's honest-empty branch: no /trades attention item and an empty inbox
// renders the pre-existing generic "nothing waiting" copy.
func TestTradesEmptyInboxRendersGenericTextWithNoAttentionItem(t *testing.T) {
	data := honestlyEmptyTradesFixture(nil)
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
// branch: a /trades attention item (an accepted trade elsewhere in the
// league, in review) with an empty inbox renders the accepted-trade nudge
// instead of the generic line.
func TestTradesEmptyInboxNamesAcceptedTradeInReview(t *testing.T) {
	data := honestlyEmptyTradesFixture([]map[string]any{
		{"route": "/trades#trade-xyz", "label": "Accepted trade between One and Two is in review"},
	})
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

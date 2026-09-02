package league

import (
	"strings"
	"testing"
	"time"
)

// TestAttentionMapDerivesFromAcceptedTradesAndOpenPickemGames is gap-audit
// item 6's data contract: leagueMap's "attention" key must list an accepted
// (review-pending) trade and this week's still-open pick'em games — the
// same underlying facts that put those two tasks on the home Action
// Center — so the shared chrome chip and the home page never disagree.
// An open (not yet accepted) trade offer must NOT appear: only an
// AcceptedReview trade is a review-pending task anyone else must act on.
func TestAttentionMapDerivesFromAcceptedTradesAndOpenPickemGames(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 2 {
		t.Fatal("fixture league needs at least two teams")
	}
	fromID, toID := teamIDs[0], teamIDs[1]
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "game-open", Week: 2, Kickoff: now.Add(2 * time.Hour), Away: "MIA", Home: "BUF"},
			{ID: "game-kicked", Week: 2, Kickoff: now.Add(-2 * time.Hour), Away: "NYJ", Home: "NE"},
		}
	})
	state := PersistedState{
		TradeOffers: []TradeOffer{
			{ID: "trd-open", FromTeamID: fromID, ToTeamID: toID, Status: TradeStatusOpen},
			{ID: "trd-review", FromTeamID: fromID, ToTeamID: toID, Status: TradeStatusAccepted},
		},
	}

	attention := svc.attentionMap(state, now)
	items, ok := attention["items"].([]map[string]any)
	if !ok {
		t.Fatalf("attention[items] = %#v, want []map[string]any", attention["items"])
	}
	if got := attention["urgent_count"]; got != len(items) {
		t.Fatalf("urgent_count = %v, want len(items) = %d", got, len(items))
	}
	if attention["has_items"] != true {
		t.Fatalf("has_items = %v, want true", attention["has_items"])
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v, want exactly 2 (one review trade, one open-pickem summary)", items)
	}

	fromName, toName := svc.teamByID(fromID).Name, svc.teamByID(toID).Name
	var sawTrade, sawPickem bool
	for _, item := range items {
		route, _ := item["route"].(string)
		label, _ := item["label"].(string)
		switch {
		case route == "/trades#trade-trd-review":
			sawTrade = true
			if !strings.Contains(label, fromName) || !strings.Contains(label, toName) {
				t.Errorf("trade item label %q does not name both %q and %q", label, fromName, toName)
			}
		case route == "/pickem":
			sawPickem = true
			if !strings.Contains(label, "1 open pick'em game") {
				t.Errorf("pickem item label = %q, want a 1-open-game count", label)
			}
		default:
			t.Errorf("unexpected attention item: %#v", item)
		}
	}
	if !sawTrade {
		t.Error("attention items missing the accepted-trade-in-review entry")
	}
	if !sawPickem {
		t.Error("attention items missing the open-pickem-games entry")
	}

	// An open (not yet accepted) offer must never surface as a review task.
	for _, item := range items {
		if route, _ := item["route"].(string); route == "/trades#trade-trd-open" {
			t.Errorf("open (non-accepted) trade offer leaked into attention items: %#v", item)
		}
	}
}

// TestAttentionMapEmptyWhenNoUrgentFacts is the honest-empty-state
// counterpart: no accepted trades and no schedule at all must render zero
// items, not a placeholder or a stale count.
func TestAttentionMapEmptyWhenNoUrgentFacts(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	svc.SetScheduleSource(func() []GameInfo { return nil })

	attention := svc.attentionMap(PersistedState{}, now)
	items, ok := attention["items"].([]map[string]any)
	if !ok {
		t.Fatalf("attention[items] = %#v, want []map[string]any", attention["items"])
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v, want empty", items)
	}
	if attention["urgent_count"] != 0 {
		t.Fatalf("urgent_count = %v, want 0", attention["urgent_count"])
	}
	if attention["has_items"] != false {
		t.Fatalf("has_items = %v, want false", attention["has_items"])
	}
}

// TestLeagueMapEmbedsAttention pins leagueMap's own composition: every
// route's data function already includes "league": s.leagueMap() (the
// pattern latest_announcement relies on above it in service.go), so
// nesting attention there — rather than threading a *http.Request through
// leagueMap's 19 call sites across 10 other files — is what actually gets
// it to every route's shared layout chrome.
func TestLeagueMapEmbedsAttention(t *testing.T) {
	svc := newTestService(t, false)
	got := svc.leagueMap()
	attention, ok := got["attention"].(map[string]any)
	if !ok {
		t.Fatalf("leagueMap()[attention] = %#v, want map[string]any", got["attention"])
	}
	for _, key := range []string{"urgent_count", "items", "has_items"} {
		if _, ok := attention[key]; !ok {
			t.Errorf("leagueMap()[attention] missing key %q: %#v", key, attention)
		}
	}
}

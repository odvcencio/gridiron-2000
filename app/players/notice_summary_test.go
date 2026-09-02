package players

import "testing"

// TestPlayersNoticeSummaryCountsAndOrdersNotices is wave-7 re-audit item
// 4's own decisive unit coverage for playersNoticeSummary (page.server.go):
// the count must match exactly how many of the seven notice conditions
// are true, and firstKind must name the FIRST one true in page.gsx's own
// source order (flash, error, demo, public_entry, fa_closed, pool_status,
// matchup) — the same order the template's own always-visible slot and
// phone-only <details> overflow slot both key off.
func TestPlayersNoticeSummaryCountsAndOrdersNotices(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"viewer":             map[string]any{"demo": false},
			"pool_status":        map[string]any{"has_notice": false},
			"pool_unavailable":   false,
			"can_edit":           true,
			"free_agency_open":   true,
			"has_matchup_source": false,
			"has_notice":         false,
			"has_players_error":  false,
		}
	}

	t.Run("none active", func(t *testing.T) {
		count, first := playersNoticeSummary(base())
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}
		if first != "" {
			t.Errorf("firstKind = %q, want empty", first)
		}
	})

	t.Run("one active reports itself as first", func(t *testing.T) {
		data := base()
		data["has_matchup_source"] = true
		count, first := playersNoticeSummary(data)
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
		if first != "matchup" {
			t.Errorf("firstKind = %q, want \"matchup\"", first)
		}
	})

	t.Run("multiple active — source order wins, not declaration order", func(t *testing.T) {
		data := base()
		// Set the LATER-in-source-order ones first in the map literal
		// (Go map iteration order is randomized) to prove firstKind comes
		// from page.gsx's own fixed priority, not map/slice build order.
		data["has_matchup_source"] = true
		data["pool_status"] = map[string]any{"has_notice": true}
		data["viewer"] = map[string]any{"demo": true}
		data["has_players_error"] = true
		count, first := playersNoticeSummary(data)
		if count != 4 {
			t.Errorf("count = %d, want 4", count)
		}
		if first != "error" {
			t.Errorf("firstKind = %q, want \"error\" (page.gsx's own second-listed, but first TRUE, condition)", first)
		}
	})

	t.Run("public_entry and fa_closed both require pool_unavailable false", func(t *testing.T) {
		data := base()
		data["pool_unavailable"] = true
		data["can_edit"] = false
		data["free_agency_open"] = false
		count, first := playersNoticeSummary(data)
		if count != 0 {
			t.Errorf("count = %d, want 0 (pool_unavailable gates both public_entry and fa_closed off)", count)
		}
		if first != "" {
			t.Errorf("firstKind = %q, want empty", first)
		}
	})

	t.Run("public_entry fires on no-team, fa_closed fires pre-draft", func(t *testing.T) {
		data := base()
		data["can_edit"] = false
		data["free_agency_open"] = false
		count, first := playersNoticeSummary(data)
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
		if first != "public_entry" {
			t.Errorf("firstKind = %q, want \"public_entry\" (page.gsx lists it before fa_closed)", first)
		}
	})
}

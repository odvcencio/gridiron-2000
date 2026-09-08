package commissioner

import (
	"testing"
	"time"

	"gridiron-2000/app/admin"
	"gridiron-2000/internal/commissionerhq"
)

// TestLocalCardAttentionAgreesWithAdminAttentionCounts is J4 F29's own
// residue (wave E): /commissioner used to build its "Attention by
// league" panel entirely from commissionerhq.Summary.Attention, the
// PII-free federation wire payload CommissionerSummary emits (see its
// own doc comment: "It intentionally does not reuse AdminData, whose
// seat and invite rows carry manager identities"). That payload never
// carries invite or board-gap counts, so a league with two pending
// invites and two board gaps on /admin read "No open flags" on HQ.
//
// The local card (only the viewing commissioner's own instance, never a
// federation peer) now folds in admin.AdminAttentionReadoutFromData's own
// counts — the exact same function and data /admin's own Load callback
// calls — so the two pages can never disagree about this league's own
// state again.
func TestLocalCardAttentionAgreesWithAdminAttentionCounts(t *testing.T) {
	now := time.Date(2026, time.September, 15, 14, 0, 0, 0, time.UTC)
	summary := commissionerhq.Summary{
		Instance: commissionerhq.Instance{Name: "THE LEAGUE", PublicURL: "https://gridiron.example"},
	}
	entry := commissionerhq.FleetEntry{PeerID: "local", PublicURL: "https://gridiron.example", Summary: summary}

	// The same shape /admin's own AdminData produces (internal/league/admin.go),
	// fed through the exact function /admin's Load callback calls.
	adminData := map[string]any{
		"invite_count":    2,
		"board_gap_count": 2,
	}
	adminAttention := admin.AdminAttentionReadoutFromData(adminData)
	if adminAttention.InviteCount != 2 || adminAttention.BoardGapCount != 2 {
		t.Fatalf("admin.AdminAttentionReadoutFromData = %+v, want InviteCount=2 BoardGapCount=2", adminAttention)
	}

	local := cardView(entry, now, time.UTC, true, adminAttention)
	if !local.HasAttention {
		t.Fatal("local card has no attention despite two pending invites and two board gaps on /admin")
	}
	var gotInvite, gotBoardGap int
	for _, item := range local.Attention {
		switch item.Code {
		case "invite_pending":
			gotInvite = item.Count
		case "board_gap":
			gotBoardGap = item.Count
		}
	}
	// The decisive assertion: HQ's own displayed counts are literally
	// admin.AdminAttentionReadoutFromData's counts, not a second,
	// independently computed number that could drift from /admin's.
	if gotInvite != adminAttention.InviteCount {
		t.Errorf("HQ invite_pending count = %d, want %d (admin.AdminAttentionReadoutFromData's own InviteCount)", gotInvite, adminAttention.InviteCount)
	}
	if gotBoardGap != adminAttention.BoardGapCount {
		t.Errorf("HQ board_gap count = %d, want %d (admin.AdminAttentionReadoutFromData's own BoardGapCount)", gotBoardGap, adminAttention.BoardGapCount)
	}

	// A remote peer's card never receives this league's own local admin
	// attention data — the PII boundary CommissionerSummary's own doc
	// comment describes stays intact.
	remote := cardView(entry, now, time.UTC, false, adminAttention)
	for _, item := range remote.Attention {
		if item.Code == "invite_pending" || item.Code == "board_gap" {
			t.Errorf("a non-local card carried a local-only attention item %q; this must stay local-only", item.Code)
		}
	}

	// Zero counts add no synthetic attention noise.
	clear := cardView(entry, now, time.UTC, true, admin.EmptyAdminAttentionReadout())
	for _, item := range clear.Attention {
		if item.Code == "invite_pending" || item.Code == "board_gap" {
			t.Errorf("zero admin attention counts still produced %q", item.Code)
		}
	}
}

// TestBuildFleetViewFoldsLocalAdminAttentionIntoQueue proves the counts
// reach the page's own "Attention by league" queue and its top-line
// AttentionCount, not just the per-card struct.
func TestBuildFleetViewFoldsLocalAdminAttentionIntoQueue(t *testing.T) {
	now := time.Date(2026, time.September, 15, 14, 0, 0, 0, time.UTC)
	entries := []commissionerhq.FleetEntry{{
		PeerID: "local", PublicURL: "https://gridiron.example",
		Summary: commissionerhq.Summary{Instance: commissionerhq.Instance{Name: "THE LEAGUE", PublicURL: "https://gridiron.example"}},
	}}
	adminAttention := admin.AdminAttentionReadoutFromData(map[string]any{"invite_count": 1, "board_gap_count": 1})

	view := buildFleetView(entries, now, time.UTC, adminAttention)
	found := map[string]bool{}
	for _, item := range view.Attention {
		found[item.Code] = true
	}
	if !found["invite_pending"] || !found["board_gap"] {
		t.Fatalf("fleet view attention queue = %+v, want invite_pending and board_gap present", view.Attention)
	}
	if view.AttentionCount < 2 {
		t.Errorf("view.AttentionCount = %d, want at least 2 for the two local admin attention items", view.AttentionCount)
	}
}

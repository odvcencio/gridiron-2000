package league

import "testing"

// TestPoolFreezesDuringAnInProgressDraft is rules-audit item 1 (HIGH): a
// FANTASY_SYNC_INTERVAL resync mid-draft must never reshuffle the pool a
// running clock and a live board are reading. Once DraftStarted is true
// and the draft is not yet complete, pool() must keep serving the cache
// it already built, no matter what the source reports on the next call.
//
// SetPlayerSource is called exactly once, matching its own "call it once
// during startup" contract (service.go) — it zeroes poolCache on every
// call, so a test (or a caller) that re-invokes it to simulate a resync
// would trivially "pass" by wiping the very cache the freeze is supposed
// to protect. A resync is the SAME closure reporting a new version/label
// on a later call, exactly like the real fantasy.Service wiring in
// app_build.go does.
func TestPoolFreezesDuringAnInProgressDraft(t *testing.T) {
	service := newTestService(t, false)
	original := testPool(20)
	resynced := append([]Player{}, original[1:]...) // drops the soon-to-be-drafted player
	current, version, label := original, int64(1), "live"
	service.SetPlayerSource(func() ([]Player, int64, string) { return current, version, label })

	first := service.pool()
	if first.version != 1 {
		t.Fatalf("setup: first pool version = %d, want 1", first.version)
	}

	// Open the draft with one pick recorded — started, not complete
	// (defaultTeams() * CurrentDraftRounds() picks are required to close
	// it; one pick is nowhere near that).
	service.store.state.DraftStarted = true
	service.store.state.Picks = append(service.store.state.Picks, DraftPick{
		Number: 1, Round: 1, TeamID: defaultTeams()[0].ID, PlayerID: original[0].ID,
	})
	if draftComplete(service.store.state) {
		t.Fatal("setup: one pick must not already satisfy draftComplete")
	}

	// A resync lands mid-draft: the source's next call reports a new
	// version and a reordered/shrunk list that drops the player just
	// picked.
	current, version, label = resynced, 2, "live"

	frozen := service.pool()
	if frozen.version != 1 || frozen.label != "live" {
		t.Fatalf("pool must stay frozen mid-draft: version=%d label=%q, want version=1", frozen.version, frozen.label)
	}
	if len(frozen.byID) != len(first.byID) {
		t.Fatalf("frozen pool must be byte-for-byte the prior cache: byID sizes %d vs %d", len(frozen.byID), len(first.byID))
	}
	if _, ok := frozen.byID[original[0].ID]; !ok {
		t.Fatalf("drafted player %s must still resolve while frozen", original[0].ID)
	}

	// Calling pool() again while still frozen must not rebuild either.
	again := service.pool()
	if again.version != 1 {
		t.Fatalf("second frozen call must still report version 1, got %d", again.version)
	}

	// Fill out every remaining pick to close the draft, then confirm the
	// pending version finally applies.
	teams := defaultTeams()
	rounds := CurrentDraftRounds()
	total := len(teams) * rounds
	number := 2
	for round := 1; round <= rounds && len(service.store.state.Picks) < total; round++ {
		for _, team := range teams {
			if len(service.store.state.Picks) >= total {
				break
			}
			service.store.state.Picks = append(service.store.state.Picks, DraftPick{
				Number: number, Round: round, TeamID: team.ID, PlayerID: original[0].ID + "-extra",
			})
			number++
		}
	}
	if !draftComplete(service.store.state) {
		t.Fatalf("setup: expected draftComplete after filling every pick, have %d picks", len(service.store.state.Picks))
	}

	settled := service.pool()
	if settled.version != 2 {
		t.Fatalf("pool must adopt the pending version once the draft completes: version = %d, want 2", settled.version)
	}
}

// TestBuildPoolCarriesForwardDroppedRosteredPlayer is rules-audit item 1's
// second half: independent of the freeze above, buildPool must never let
// a resync (mid-draft or not) orphan a player already committed to a
// pick, by carrying his last-known fields forward into byID even when
// the new source list no longer contains him.
func TestBuildPoolCarriesForwardDroppedRosteredPlayer(t *testing.T) {
	service := newTestService(t, false)
	original := testPool(5)
	shrunk := append([]Player{}, original[1:]...) // drops original[0]
	current, version := original, int64(1)
	service.SetPlayerSource(func() ([]Player, int64, string) { return current, version, "live" })

	first := service.pool()
	rostered, ok := first.byID[original[0].ID]
	if !ok {
		t.Fatalf("setup: player %s missing from the first build", original[0].ID)
	}

	// DraftStarted is left false on purpose: this isolates buildPool's
	// carry-forward from the pool() freeze itself (see
	// TestPoolFreezesDuringAnInProgressDraft), which only engages once
	// DraftStarted is true and the draft is not yet complete. The
	// carry-forward guarantee must hold in both states.
	service.store.state.Picks = append(service.store.state.Picks, DraftPick{
		Number: 1, Round: 1, TeamID: defaultTeams()[0].ID, PlayerID: original[0].ID,
	})

	// The next resync drops the drafted player from its own list — an
	// ordinary ADP-cut move, not a defect in the source.
	current, version = shrunk, 2

	second := service.pool()
	if second.version != 2 {
		t.Fatalf("resync must apply outside a draft: version = %d, want 2", second.version)
	}
	got, ok := second.byID[original[0].ID]
	if !ok {
		t.Fatalf("drafted player %s must remain resolvable by ID after the source dropped him", original[0].ID)
	}
	if got.Name != rostered.Name || got.Position != rostered.Position {
		t.Fatalf("carried-forward player fields drifted: got %+v, want name %q position %q", got, rostered.Name, rostered.Position)
	}
	for _, p := range second.players {
		if p.ID == original[0].ID {
			t.Fatalf("carried-forward player %s must not reappear as an available board candidate", original[0].ID)
		}
	}
}

package league

import (
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestMoveClaimNormalizesAndAuthorizesPrivateOrder(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC)
	for index, addID := range []string{"wv-1", "wv-2", "wv-3"} {
		claim := WaiverClaim{ID: "claim-" + addID, TeamID: "team-1", AddID: addID, FiledAt: now.Add(time.Duration(index) * time.Minute)}
		if err := store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CancelClaim("team-1", "claim-wv-2"); err != nil {
		t.Fatal(err)
	}
	if moved, err := store.MoveClaim("team-1", "claim-wv-3", "up"); err != nil || !moved {
		t.Fatalf("MoveClaim = %v, %v; want moved", moved, err)
	}

	claims := store.Snapshot().WaiverClaims
	if len(claims) != 2 || claims[0].ID != "claim-wv-1" || claims[0].Priority != 2 || claims[1].ID != "claim-wv-3" || claims[1].Priority != 1 {
		t.Fatalf("claims after cancel/move = %+v, want gap-free reordered priorities", claims)
	}
	before := store.Snapshot()
	if moved, err := store.MoveClaim("team-2", "claim-wv-3", "down"); err == nil || moved {
		t.Fatalf("cross-team MoveClaim = %v, %v; want generic ownership failure", moved, err)
	}
	if got := store.Snapshot(); !reflect.DeepEqual(got, before) {
		t.Fatal("a cross-team move mutated state")
	}
	if moved, err := store.MoveClaim("team-1", "claim-wv-3", "up"); err != nil || moved {
		t.Fatalf("boundary MoveClaim = %v, %v; want harmless no-op", moved, err)
	}
}

func TestMoveClaimConcurrentRequestsKeepPermutation(t *testing.T) {
	store := NewStore("")
	now := time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC)
	for index := 0; index < 8; index++ {
		claim := WaiverClaim{
			ID: "claim-" + string(rune('a'+index)), TeamID: "team-1",
			AddID: "player-" + string(rune('a'+index)), FiledAt: now.Add(time.Duration(index) * time.Minute),
		}
		if err := store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for index := 0; index < 64; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			direction := "up"
			if index%2 == 1 {
				direction = "down"
			}
			_, _ = store.MoveClaim("team-1", "claim-d", direction)
		}(index)
	}
	wg.Wait()
	claims := store.Snapshot().WaiverClaims
	priorities := make([]int, 0, len(claims))
	for _, claim := range claims {
		priorities = append(priorities, claim.Priority)
	}
	sort.Ints(priorities)
	for index, priority := range priorities {
		if priority != index+1 {
			t.Fatalf("concurrent priorities = %v, want exact 1..8 permutation", priorities)
		}
	}
}

func TestMoveClaimEqualFAABBidsChangesProcessingOrder(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for _, claim := range []WaiverClaim{
		{ID: "clm-first", TeamID: "team-7", AddID: "wv-1", Bid: 10, FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-second", TeamID: "team-7", AddID: "wv-2", Bid: 10, FiledAt: now.Add(-time.Hour)},
	} {
		if err := store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	if moved, err := store.MoveClaim("team-7", "clm-second", "up"); err != nil || !moved {
		t.Fatalf("MoveClaim = %v, %v", moved, err)
	}
	cfg := processWaiversCfg()
	cfg.Waivers.Mode = "faab"
	results, err := store.ProcessWaivers(now, cfg, nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Claim.ID != "clm-second" {
		t.Fatalf("equal-bid results = %+v, want moved claim processed first", results)
	}
}

func TestProcessWaiversPersistsPrivateReceiptsForEveryOutcome(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for _, claim := range []WaiverClaim{
		{ID: "clm-win", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-3 * time.Hour)},
		{ID: "clm-beaten", TeamID: "team-2", AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-failed", TeamID: "team-3", AddID: "gone-player", FiledAt: now.Add(-time.Hour)},
	} {
		if err := store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v, want three outcomes", results)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 0 || len(state.WaiverReceipts) != 3 {
		t.Fatalf("claims/receipts = %d/%d, want 0/3", len(state.WaiverClaims), len(state.WaiverReceipts))
	}
	byID := make(map[string]WaiverReceipt, len(state.WaiverReceipts))
	for _, receipt := range state.WaiverReceipts {
		byID[receipt.ClaimID] = receipt
		if receipt.Season != 2026 || receipt.Week < 1 || !receipt.ResolvedAt.Equal(now) {
			t.Errorf("receipt timing/config = %+v", receipt)
		}
		if receipt.SubmittedPriority != 1 || receipt.WaiverPosition < 1 || receipt.WaiverTeamCount != 8 {
			t.Errorf("receipt ordering snapshot = %+v", receipt)
		}
	}
	if got := byID["clm-win"]; got.Outcome != "won" || got.Add.Name != "Wire One" || got.WinningTeamID != "" || got.Reason != "Claim awarded." {
		t.Fatalf("winning receipt = %+v", got)
	}
	if got := byID["clm-beaten"]; got.Outcome != "beaten" || got.WinningTeamID != "team-7" || got.Reason == "" {
		t.Fatalf("beaten receipt = %+v", got)
	}
	if got := byID["clm-failed"]; got.Outcome != "failed" || got.WinningTeamID != "" || got.Add.PlayerID != "gone-player" || got.Reason == "" {
		t.Fatalf("failed receipt = %+v", got)
	}
}

func TestBeatenFAABReceiptRecordsActualWinningBid(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for _, claim := range []WaiverClaim{
		{ID: "clm-winner", TeamID: "team-7", AddID: "wv-1", Bid: 21, FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-loser", TeamID: "team-2", AddID: "wv-1", Bid: 9, FiledAt: now.Add(-time.Hour)},
	} {
		if err := store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	cfg := processWaiversCfg()
	cfg.Waivers.Mode = "faab"
	if _, err := store.ProcessWaivers(now, cfg, nil, processWaiversFixturePool(), 99); err != nil {
		t.Fatal(err)
	}
	byID := map[string]WaiverReceipt{}
	for _, receipt := range store.Snapshot().WaiverReceipts {
		byID[receipt.ClaimID] = receipt
	}
	if won := byID["clm-winner"]; won.WinningTeamID != "" || won.WinningBidKnown {
		t.Fatalf("won receipt redundantly exposes winner metadata: %+v", won)
	}
	if lost := byID["clm-loser"]; lost.Outcome != "beaten" || lost.WinningTeamID != "team-7" || !lost.WinningBidKnown || lost.WinningBid != 21 || lost.Bid != 9 {
		t.Fatalf("beaten receipt = %+v, want losing bid 9 and actual winning bid 21", lost)
	}
}

func TestBeatenFAABReceiptDoesNotReuseHistoricalBidAfterDirectAdd(t *testing.T) {
	store := processWaiversFixtureStore(t)
	wonAt := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	cfg := processWaiversCfg()
	cfg.Waivers.Mode = "faab"

	if err := store.FileClaim(WaiverClaim{
		ID: "clm-historical", TeamID: "team-7", AddID: "wv-1", Bid: 21,
		FiledAt: wonAt.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ProcessWaivers(wonAt, cfg, nil, processWaiversFixturePool(), 99); err != nil {
		t.Fatal(err)
	}

	droppedAt := wonAt.Add(24 * time.Hour)
	if err := store.RecordTransaction(Transaction{
		ID: "txn-drop", Season: cfg.Season, Week: 1, Type: "drop", TeamID: "team-7",
		Drops: []TransactionPlayer{{PlayerID: "wv-1"}}, By: "manager", At: droppedAt,
	}, 99); err != nil {
		t.Fatal(err)
	}
	// File the competing claim while the player is unrostered; the later
	// direct add makes it resolve as beaten without creating a new claim
	// transaction for the current owner.
	if err := store.FileClaim(WaiverClaim{
		ID: "clm-after-direct-add", TeamID: "team-2", AddID: "wv-1", Bid: 9,
		FiledAt: droppedAt.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordTransaction(Transaction{
		ID: "txn-direct-add", Season: cfg.Season, Week: 1, Type: "add", TeamID: "team-7",
		Adds: []TransactionPlayer{{PlayerID: "wv-1"}}, By: "manager", At: droppedAt.Add(2 * time.Hour),
	}, 99); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ProcessWaivers(droppedAt.Add(3*time.Hour), cfg, nil, processWaiversFixturePool(), 99); err != nil {
		t.Fatal(err)
	}
	var receipt WaiverReceipt
	for _, candidate := range store.Snapshot().WaiverReceipts {
		if candidate.ClaimID == "clm-after-direct-add" {
			receipt = candidate
			break
		}
	}
	if receipt.Outcome != "beaten" || receipt.WinningTeamID != "team-7" || receipt.WinningBidKnown || receipt.WinningBid != 0 {
		t.Fatalf("receipt = %+v, want beaten by team-7 with unknown winning bid after direct add", receipt)
	}
}

func TestWaiverMutationPersistenceFailureRollsBackClaimsAndReceipts(t *testing.T) {
	t.Run("move", func(t *testing.T) {
		store := newTestStore(t)
		now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
		for _, claim := range []WaiverClaim{
			{ID: "clm-a", TeamID: "team-1", AddID: "wv-1", FiledAt: now.Add(-time.Hour)},
			{ID: "clm-b", TeamID: "team-1", AddID: "wv-2", FiledAt: now},
		} {
			if err := store.FileClaim(claim); err != nil {
				t.Fatal(err)
			}
		}
		before := store.Snapshot()
		failThisStorePersist(store)
		if _, err := store.MoveClaim("team-1", "clm-b", "up"); err == nil {
			t.Fatal("MoveClaim succeeded despite injected persistence failure")
		}
		if got := store.Snapshot(); !reflect.DeepEqual(got, before) {
			t.Fatal("failed move did not restore the in-memory state")
		}
	})

	t.Run("processing", func(t *testing.T) {
		store := processWaiversFixtureStore(t)
		now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
		if err := store.FileClaim(WaiverClaim{ID: "clm-win", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
		before := store.Snapshot()
		failThisStorePersist(store)
		if _, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99); err == nil {
			t.Fatal("ProcessWaivers succeeded despite injected persistence failure")
		}
		if got := store.Snapshot(); !reflect.DeepEqual(got, before) {
			t.Fatal("failed processing diverged claims, receipts, transaction, or scalar state")
		}
	})
}

func TestWaiverReceiptAndMovedOrderSurviveRestart(t *testing.T) {
	store := processWaiversFixtureStore(t)
	path := store.filePath
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for _, claim := range []WaiverClaim{
		{ID: "clm-a", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-b", TeamID: "team-7", AddID: "wv-2", FiledAt: now.Add(-time.Hour)},
	} {
		if err := store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	if moved, err := store.MoveClaim("team-7", "clm-b", "up"); err != nil || !moved {
		t.Fatalf("MoveClaim = %v, %v", moved, err)
	}
	if _, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99); err != nil {
		t.Fatal(err)
	}
	want := store.Snapshot().WaiverReceipts
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(path)
	t.Cleanup(func() { _ = restarted.Close() })
	if err := restarted.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := restarted.Snapshot().WaiverReceipts; !reflect.DeepEqual(got, want) {
		t.Fatalf("restarted receipts = %+v, want %+v", got, want)
	}
}

func TestSeasonResetsClearWaiverDesk(t *testing.T) {
	for name, reset := range map[string]func(*Store) error{
		"draft":  (*Store).ResetDraft,
		"league": (*Store).ResetLeague,
	} {
		t.Run(name, func(t *testing.T) {
			store := newTestStore(t)
			store.mu.Lock()
			store.state.WaiverClaims = []WaiverClaim{{ID: "clm", TeamID: "team-1", AddID: "p", Priority: 1}}
			store.state.WaiverReceipts = []WaiverReceipt{{ClaimID: "old", Season: 2026, TeamID: "team-1"}}
			store.mu.Unlock()
			if err := reset(store); err != nil {
				t.Fatal(err)
			}
			state := store.Snapshot()
			if len(state.WaiverClaims) != 0 || len(state.WaiverReceipts) != 0 {
				t.Fatalf("reset retained waiver desk state: %+v %+v", state.WaiverClaims, state.WaiverReceipts)
			}
		})
	}
}

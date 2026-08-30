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

// TestProcessWaiversPersistsPrivateReceiptsForEveryOutcome covers both
// terminal receipt outcomes (won, beaten). F6: a claim whose AddID has
// left the bounded pool this run is deferred, not resolved as "failed" —
// TestProcessWaiversDeferredClaimStaysOpenWhenAddIDLeavesPool below
// covers that outcome instead of a third terminal receipt here.
func TestProcessWaiversPersistsPrivateReceiptsForEveryOutcome(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for _, claim := range []WaiverClaim{
		{ID: "clm-win", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-3 * time.Hour)},
		{ID: "clm-beaten", TeamID: "team-2", AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
	} {
		if err := store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want two outcomes", results)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 0 || len(state.WaiverReceipts) != 2 {
		t.Fatalf("claims/receipts = %d/%d, want 0/2", len(state.WaiverClaims), len(state.WaiverReceipts))
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
}

// TestProcessWaiversDeferredClaimStaysOpenWhenAddIDLeavesPool pins F6: a
// claim whose AddID has left this run's bounded pool (a roster shuffle or
// source refresh, not the manager's fault) stays open — no receipt, no
// transaction, no destroyed claim — and resolves normally once the
// player is back in the pool on a later run.
func TestProcessWaiversDeferredClaimStaysOpenWhenAddIDLeavesPool(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if err := store.FileClaim(WaiverClaim{ID: "clm-deferred", TeamID: "team-3", AddID: "gone-player", FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	pool := processWaiversFixturePool() // "gone-player" is not in this pool
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, pool, 99)
	if err != nil {
		t.Fatal(err)
	}
	// F6 follow-up (2026-08-30 review, finding 6): the first deferral fires
	// a one-time "deferred" notice — not a resolution — so the manager is
	// not left silently waiting on a claim that is quietly holding a cap
	// slot. It is not a "resolution": no receipt, no transaction, and the
	// claim itself stays open (checked below).
	if len(results) != 1 || results[0].Outcome != "deferred" {
		t.Fatalf("results = %+v, want one deferred notice while the player is out of the pool", results)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 1 || state.WaiverClaims[0].ID != "clm-deferred" {
		t.Fatalf("WaiverClaims = %+v, want clm-deferred still open", state.WaiverClaims)
	}
	if state.WaiverClaims[0].DeferredStreak != 1 {
		t.Fatalf("DeferredStreak = %d, want 1 after the first deferred run", state.WaiverClaims[0].DeferredStreak)
	}
	if len(state.WaiverReceipts) != 0 || len(state.Transactions) != 0 {
		t.Fatalf("a deferred claim must not produce a receipt or transaction: receipts=%d transactions=%d", len(state.WaiverReceipts), len(state.Transactions))
	}

	// The player returns to the pool on a later run: the still-open claim
	// resolves normally.
	pool["gone-player"] = Player{ID: "gone-player", Name: "Gone Player", Position: "WR", NFLTeam: "PIT"}
	later := now.Add(24 * time.Hour)
	results, err = store.ProcessWaivers(later, processWaiversCfg(), nil, pool, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "won" {
		t.Fatalf("results after the player returns = %+v, want one won outcome", results)
	}
	state = store.Snapshot()
	if len(state.WaiverClaims) != 0 || len(state.WaiverReceipts) != 1 {
		t.Fatalf("claims/receipts after recovery = %d/%d, want 0/1", len(state.WaiverClaims), len(state.WaiverReceipts))
	}
}

// TestProcessWaiversExpiresClaimAfterThreeConsecutiveDeferrals pins the
// rest of finding 6 (2026-08-30 review): a claim that never recovers must
// not hold its team's cap slot forever. It expires automatically after
// waiverClaimDeferralLimit (3) consecutive deferred runs, with a final
// "expired" notification/receipt naming the reason, and no notice fires
// again for the second, still-silent middle run.
func TestProcessWaiversExpiresClaimAfterThreeConsecutiveDeferrals(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if err := store.FileClaim(WaiverClaim{ID: "clm-stuck", TeamID: "team-3", AddID: "gone-player", FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	pool := processWaiversFixturePool() // "gone-player" never returns to this pool
	cfg := processWaiversCfg()

	run1, err := store.ProcessWaivers(now, cfg, nil, pool, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(run1) != 1 || run1[0].Outcome != "deferred" {
		t.Fatalf("run 1 results = %+v, want one deferred notice", run1)
	}

	run2At := now.Add(24 * time.Hour)
	run2, err := store.ProcessWaivers(run2At, cfg, nil, pool, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(run2) != 0 {
		t.Fatalf("run 2 results = %+v, want none — the second consecutive deferral must stay silent", run2)
	}
	if got := store.Snapshot().WaiverClaims[0].DeferredStreak; got != 2 {
		t.Fatalf("DeferredStreak after run 2 = %d, want 2", got)
	}

	run3At := run2At.Add(24 * time.Hour)
	run3, err := store.ProcessWaivers(run3At, cfg, nil, pool, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(run3) != 1 || run3[0].Outcome != "expired" || run3[0].Reason == "" {
		t.Fatalf("run 3 results = %+v, want one expired outcome naming a reason", run3)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 0 {
		t.Fatalf("WaiverClaims after the third deferral = %+v, want the claim gone", state.WaiverClaims)
	}
	if len(state.WaiverReceipts) != 1 || state.WaiverReceipts[0].Outcome != "expired" {
		t.Fatalf("WaiverReceipts = %+v, want one expired receipt", state.WaiverReceipts)
	}
	if len(state.Transactions) != 0 {
		t.Fatalf("Transactions = %+v, want none — an expiry is not an award", state.Transactions)
	}
}

// TestProcessWaiversDeferralSurvivesFourRapidSameDayRuns pins finding 2 of
// the 2026-08-30 review round 3: the existing 24-hour-gap coverage above
// lands its expiring run exactly at waiverClaimDeferralWindow (48h),
// which passes under a run-count-only rule and the time-gated rule
// alike. Four runs an hour apart, all the same day, exercise the time
// gate on its own: DeferredStreak reaching, and passing,
// waiverClaimDeferralLimit must not expire the claim before
// waiverClaimDeferralWindow has actually elapsed, and FirstDeferredAt
// must stay pinned to the very first deferral in the streak throughout.
func TestProcessWaiversDeferralSurvivesFourRapidSameDayRuns(t *testing.T) {
	store := processWaiversFixtureStore(t)
	firstRun := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if err := store.FileClaim(WaiverClaim{ID: "clm-rapid", TeamID: "team-3", AddID: "gone-player", FiledAt: firstRun.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	pool := processWaiversFixturePool() // "gone-player" never returns to this pool
	cfg := processWaiversCfg()

	for i := 0; i < 4; i++ {
		runAt := firstRun.Add(time.Duration(i) * time.Hour) // four runs, an hour apart, same day
		if _, err := store.ProcessWaivers(runAt, cfg, nil, pool, 99); err != nil {
			t.Fatal(err)
		}
	}

	state := store.Snapshot()
	if len(state.WaiverClaims) != 1 || state.WaiverClaims[0].ID != "clm-rapid" {
		t.Fatalf("WaiverClaims after 4 rapid same-day runs = %+v, want clm-rapid still open (48h has not elapsed)", state.WaiverClaims)
	}
	if got := state.WaiverClaims[0].DeferredStreak; got != 4 {
		t.Fatalf("DeferredStreak after 4 rapid runs = %d, want 4", got)
	}
	if got := state.WaiverClaims[0].FirstDeferredAt; !got.Equal(firstRun) {
		t.Fatalf("FirstDeferredAt = %v, want the first deferral instant %v pinned across all 4 runs", got, firstRun)
	}
	if len(state.WaiverReceipts) != 0 {
		t.Fatalf("WaiverReceipts = %+v, want none — the claim must not expire before 48h elapses regardless of run count", state.WaiverReceipts)
	}
}

// TestProcessWaiversNonDeferredEvaluationResetsStreakAndRestartsClock pins
// the rest of finding 2 (2026-08-30 review round 3): a run in which the
// claim's AddID IS present in the pool, but is not yet due for a
// different reason (a live kickoff lock, not a pool-absence deferral),
// must reset both DeferredStreak and FirstDeferredAt — and a later
// pool-absence deferral must restart the 48h clock at its own instant,
// not stay pinned to the streak's original one.
func TestProcessWaiversNonDeferredEvaluationResetsStreakAndRestartsClock(t *testing.T) {
	store := processWaiversFixtureStore(t)
	run1 := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if err := store.FileClaim(WaiverClaim{ID: "clm-restart", TeamID: "team-3", AddID: "wv-1", FiledAt: run1.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := processWaiversCfg()
	emptyPool := map[string]Player{} // wv-1 absent from the pool this run: a source outage

	if _, err := store.ProcessWaivers(run1, cfg, nil, emptyPool, 99); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().WaiverClaims[0]; got.DeferredStreak != 1 || !got.FirstDeferredAt.Equal(run1) {
		t.Fatalf("after run 1 = %+v, want DeferredStreak 1 and FirstDeferredAt %v", got, run1)
	}

	// Run 2, 2h later, well inside the 48h window: wv-1 is back in the
	// pool but kickoff-locked (a live, not-yet-final game) — a
	// non-deferred evaluation, not a pool-absence deferral.
	run2 := run1.Add(2 * time.Hour)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: run2.Add(-time.Hour), Away: "PIT", Home: "NYJ", Final: false}}
	lockedPool := processWaiversFixturePool() // "wv-1": NFLTeam PIT
	if _, err := store.ProcessWaivers(run2, cfg, games, lockedPool, 99); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().WaiverClaims[0]; got.DeferredStreak != 0 || !got.FirstDeferredAt.IsZero() {
		t.Fatalf("after run 2 (non-deferred evaluation) = %+v, want DeferredStreak and FirstDeferredAt both reset", got)
	}

	// Run 3, another 2h later: wv-1 is absent from the pool again — a
	// fresh pool-absence deferral. The clock must restart at run3, not
	// stay pinned to run1.
	run3 := run2.Add(2 * time.Hour)
	if _, err := store.ProcessWaivers(run3, cfg, nil, emptyPool, 99); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot().WaiverClaims[0]
	if got.DeferredStreak != 1 {
		t.Fatalf("DeferredStreak after run 3's fresh deferral = %d, want 1 (a new streak, not a continuation of run 1's)", got.DeferredStreak)
	}
	if !got.FirstDeferredAt.Equal(run3) {
		t.Fatalf("FirstDeferredAt after run 3 = %v, want %v — the 48h clock must restart at the new deferral, not stay pinned to run 1 (%v)", got.FirstDeferredAt, run3, run1)
	}
}

// TestProcessWaiversExpiryReasonReportsActualStreakNotTheConstant pins
// finding 4 (2026-08-30 review round 3, store.go:3493): the expiry
// reason must report the claim's own DeferredStreak, not the
// waiverClaimDeferralLimit constant it happened to first cross. An
// outage replay whose run cadence is wider than waiverClaimDeferralLimit
// consecutive runs (here, four 16h-spaced runs against the 3-run limit)
// still needs waiverClaimDeferralWindow (48h) of real time before it
// expires, so the streak that finally crosses both gates is 4, not 3.
func TestProcessWaiversExpiryReasonReportsActualStreakNotTheConstant(t *testing.T) {
	store := processWaiversFixtureStore(t)
	firstRun := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if err := store.FileClaim(WaiverClaim{ID: "clm-outage", TeamID: "team-3", AddID: "gone-player", FiledAt: firstRun.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	pool := processWaiversFixturePool() // "gone-player" never returns to this pool
	cfg := processWaiversCfg()

	var last []WaiverResult
	for i := 0; i < 4; i++ {
		runAt := firstRun.Add(time.Duration(i) * 16 * time.Hour) // 4 runs, 16h apart: streak 4 by the time 48h elapses
		results, err := store.ProcessWaivers(runAt, cfg, nil, pool, 99)
		if err != nil {
			t.Fatal(err)
		}
		last = results
	}

	if len(last) != 1 || last[0].Outcome != "expired" {
		t.Fatalf("final run results = %+v, want one expired outcome", last)
	}
	want := "this claim deferred for 4 consecutive runs across at least 48 hours because gone-player never returned to the player pool; it expired automatically"
	if last[0].Reason != want {
		t.Fatalf("expiry reason = %q, want %q (the actual streak, not the waiverClaimDeferralLimit constant)", last[0].Reason, want)
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

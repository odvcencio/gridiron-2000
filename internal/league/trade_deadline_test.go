package league

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

// TestTradeDeadlineStateBoundary uses a non-UTC league timezone and checks
// the strict-open/closed boundary that T8 and the read model must share.
func TestTradeDeadlineStateBoundary(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2027, 1, 15, 15, 0, 0, 0, location)
	cfg := DefaultConfig()
	cfg.Timezone = location.String()
	cfg.Trades.Deadline = deadline.Format(time.RFC3339)
	wantLabel := formatResolvesAt(cfg, deadline)
	for _, tc := range []struct {
		name   string
		now    time.Time
		passed bool
	}{
		{name: "just before", now: deadline.Add(-time.Nanosecond), passed: false},
		{name: "at", now: deadline, passed: true},
		{name: "just after", now: deadline.Add(time.Nanosecond), passed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotDeadline, configured, passed, label := tradeDeadlineState(cfg, tc.now)
			if !configured || !gotDeadline.Equal(deadline) || passed != tc.passed || label != wantLabel {
				t.Fatalf("state = deadline:%v configured:%v passed:%v label:%q; want deadline:%v configured:true passed:%v label:%q", gotDeadline, configured, passed, label, deadline, tc.passed, wantLabel)
			}
		})
	}
}

// TestTradesDataDeadlineBoundaryAndActionFlags proves the read model takes
// one service-clock snapshot, closes compose/accept/counter at T8, and keeps
// decline/withdraw truthfully available for open offers.
func TestTradesDataDeadlineBoundaryAndActionFlags(t *testing.T) {
	service, start := newTradesTestService(t, "")
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	deadline := start.Add(time.Hour)
	service.cfg.Timezone = location.String()
	service.cfg.Trades.Deadline = deadline.Format(time.RFC3339)
	current := deadline.Add(-time.Nanosecond)
	service.now = func() time.Time { return current }
	request, _ := http.NewRequest(http.MethodGet, "/trades?counterparty=team-2", nil)
	for _, tc := range []struct {
		name   string
		now    time.Time
		closed bool
	}{
		{name: "just before", now: deadline.Add(-time.Nanosecond), closed: false},
		{name: "at", now: deadline, closed: true},
		{name: "just after", now: deadline.Add(time.Nanosecond), closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current = tc.now
			data := service.TradesData(request)
			if data["can_edit"] != true || data["trade_deadline_configured"] != true || data["trade_deadline_passed"] != tc.closed {
				t.Fatalf("deadline data = can_edit:%v configured:%v passed:%v; want true,true,%v", data["can_edit"], data["trade_deadline_configured"], data["trade_deadline_passed"], tc.closed)
			}
			if data["can_compose"] != !tc.closed || data["compose_active"] != !tc.closed || data["trade_deadline"] == "" {
				t.Fatalf("compose/deadline data = can_compose:%v active:%v label:%q; want compose/active %v and a label", data["can_compose"], data["compose_active"], data["trade_deadline"], !tc.closed)
			}
			wantState := "upcoming"
			if tc.closed {
				wantState = "passed"
			}
			if data["trade_deadline_state"] != wantState || data["trade_deadline_relative"] == "" {
				t.Fatalf("deadline timing = state:%v relative:%q; want %s and a relative label", data["trade_deadline_state"], data["trade_deadline_relative"], wantState)
			}
		})
	}

	offer := TradeOffer{ID: "trd-deadline", FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: []string{"t2-a"}, Status: TradeStatusOpen, CreatedAt: start}
	pool := service.pool()
	open := service.tradeOfferRow(pool, offer, "team-2", true, false, false, 3)
	closed := service.tradeOfferRow(pool, offer, "team-2", true, true, false, 3)
	if !open.CanAccept || !open.CanCounter || !open.CanDecline {
		t.Fatalf("open recipient actions = accept:%v counter:%v decline:%v; want all true", open.CanAccept, open.CanCounter, open.CanDecline)
	}
	if closed.CanAccept || closed.CanCounter || !closed.CanDecline {
		t.Fatalf("closed recipient actions = accept:%v counter:%v decline:%v; want accept/counter false, decline true", closed.CanAccept, closed.CanCounter, closed.CanDecline)
	}
	if !service.tradeOfferRow(pool, offer, "team-1", true, true, false, 3).CanWithdraw {
		t.Fatal("closed sender lost the still-valid withdraw action")
	}
}

// TestTradeDeadlineStalePostsDoNotMutate covers stale propose, accept, and
// counter requests, then confirms decline and withdraw remain valid cleanup.
func TestTradeDeadlineStalePostsDoNotMutate(t *testing.T) {
	service, now := newTradesTestService(t, "")
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	service.cfg.Trades.Deadline = now.Add(time.Hour).Format(time.RFC3339)
	if _, err := service.ProposeTrade(request, "team-1", "team-2", []string{"t1-a"}, []string{"t2-a"}, nil, ""); err != nil {
		t.Fatalf("seed ProposeTrade: %v", err)
	}
	seed := service.store.Snapshot()
	service.cfg.Trades.Deadline = now.Format(time.RFC3339)
	before := service.store.Snapshot()
	if _, err := service.AcceptTrade(request, "team-2", seed.TradeOffers[0].ID, tradeAcceptConfirmation); err == nil {
		t.Fatal("stale AcceptTrade unexpectedly succeeded")
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(before.TradeOffers, after.TradeOffers) {
		t.Fatalf("stale AcceptTrade mutated offers: before=%+v after=%+v", before.TradeOffers, after.TradeOffers)
	}
	before = service.store.Snapshot()
	if _, err := service.CounterTrade(request, "team-2", seed.TradeOffers[0].ID, []string{"t2-a"}, []string{"t1-a"}, nil, ""); err == nil {
		t.Fatal("stale CounterTrade unexpectedly succeeded")
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(before.TradeOffers, after.TradeOffers) {
		t.Fatalf("stale CounterTrade mutated offers: before=%+v after=%+v", before.TradeOffers, after.TradeOffers)
	}
	before = service.store.Snapshot()
	if _, err := service.ProposeTrade(request, "team-1", "team-2", []string{"t1-b"}, []string{"t2-b"}, nil, ""); err == nil {
		t.Fatal("stale ProposeTrade unexpectedly succeeded")
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(before.TradeOffers, after.TradeOffers) {
		t.Fatalf("stale ProposeTrade mutated offers: before=%+v after=%+v", before.TradeOffers, after.TradeOffers)
	}

	service.cfg.Trades.Deadline = now.Add(time.Hour).Format(time.RFC3339)
	if _, err := service.ProposeTrade(request, "team-1", "team-2", []string{"t1-b"}, []string{"t2-b"}, nil, ""); err != nil {
		t.Fatalf("seed cleanup offer: %v", err)
	}
	offers := service.store.Snapshot().TradeOffers
	cleanupID := offers[len(offers)-1].ID
	service.cfg.Trades.Deadline = now.Format(time.RFC3339)
	if _, err := service.DeclineTrade(request, "team-2", seed.TradeOffers[0].ID); err != nil {
		t.Fatalf("DeclineTrade after deadline: %v", err)
	}
	if got := service.store.Snapshot().TradeOffers[0].Status; got != TradeStatusDeclined {
		t.Fatalf("declined status = %q, want %q", got, TradeStatusDeclined)
	}
	if _, err := service.WithdrawTrade(request, "team-1", cleanupID); err != nil {
		t.Fatalf("WithdrawTrade after deadline: %v", err)
	}
	if got := service.store.Snapshot().TradeOffers[len(offers)-1].Status; got != TradeStatusWithdrawn {
		t.Fatalf("withdrawn status = %q, want %q", got, TradeStatusWithdrawn)
	}
}

// TestTradeApproveAfterDeadlineExecutesAcceptedOfferOnce proves that an
// offer accepted while T8 is open remains executable after the global
// deadline, and that a second approval cannot append another transaction.
func TestTradeApproveAfterDeadlineExecutesAcceptedOfferOnce(t *testing.T) {
	service, start := newTradesTestService(t, "")
	service.cfg.Trades.Veto = "commissioner"
	deadline := start.Add(time.Hour)
	service.cfg.Trades.Deadline = deadline.Format(time.RFC3339)
	current := start
	service.now = func() time.Time { return current }
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	offerID := proposeFixtureOffer(t, service)

	current = deadline.Add(-time.Nanosecond)
	if _, err := service.AcceptTrade(request, "team-2", offerID, tradeAcceptConfirmation); err != nil {
		t.Fatalf("pre-deadline AcceptTrade: %v", err)
	}
	current = deadline.Add(time.Minute)
	if _, err := service.ApproveTrade(request, offerID, tradeApproveConfirmation); err != nil {
		t.Fatalf("post-deadline ApproveTrade: %v", err)
	}
	state := service.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusExecuted {
		t.Fatalf("status = %q, want executed", state.TradeOffers[0].Status)
	}
	if len(state.Transactions) != 1 || state.Transactions[0].Type != "trade" || state.Transactions[0].OfferID != offerID {
		t.Fatalf("Transactions = %+v, want one trade transaction for %s", state.Transactions, offerID)
	}

	current = current.Add(time.Minute)
	if _, err := service.ApproveTrade(request, offerID, tradeApproveConfirmation); err == nil {
		t.Fatal("second post-deadline approval unexpectedly succeeded")
	}
	if got := len(service.store.Snapshot().Transactions); got != 1 {
		t.Fatalf("Transactions after duplicate approval = %d, want 1", got)
	}
}

// TestTradeTickExecutesAcceptedOfferAfterDeadlineOnce covers the automatic
// review tick crossing T8. The accepted offer executes once and a later tick
// cannot duplicate its transaction.
func TestTradeTickExecutesAcceptedOfferAfterDeadlineOnce(t *testing.T) {
	service, start := newTradesTestService(t, "")
	service.cfg.Trades.Veto = "commissioner"
	service.cfg.Trades.ReviewHours = 24
	deadline := start.Add(2 * time.Hour)
	service.cfg.Trades.Deadline = deadline.Format(time.RFC3339)
	current := start
	service.now = func() time.Time { return current }
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	offerID := proposeFixtureOffer(t, service)

	current = deadline.Add(-time.Minute)
	if _, err := service.AcceptTrade(request, "team-2", offerID, tradeAcceptConfirmation); err != nil {
		t.Fatalf("pre-deadline AcceptTrade: %v", err)
	}
	execAt := start.Add(26 * time.Hour)
	service.rosterOpsTick(execAt)
	state := service.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusExecuted {
		t.Fatalf("status = %q, want executed (FailReason=%q)", state.TradeOffers[0].Status, state.TradeOffers[0].FailReason)
	}
	if len(state.Transactions) != 1 || state.Transactions[0].Type != "trade" || state.Transactions[0].OfferID != offerID {
		t.Fatalf("Transactions = %+v, want one trade transaction for %s", state.Transactions, offerID)
	}

	service.rosterOpsTick(execAt.Add(time.Minute))
	if got := len(service.store.Snapshot().Transactions); got != 1 {
		t.Fatalf("Transactions after duplicate automatic tick = %d, want 1", got)
	}
}

// TestTradeAcceptAtDeadlineRemainsClosed pins the exact T8 boundary: an
// offer may be accepted strictly before the deadline, but not at it.
func TestTradeAcceptAtDeadlineRemainsClosed(t *testing.T) {
	service, start := newTradesTestService(t, "")
	deadline := start.Add(time.Hour)
	service.cfg.Trades.Deadline = deadline.Format(time.RFC3339)
	current := start
	service.now = func() time.Time { return current }
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	offerID := proposeFixtureOffer(t, service)

	current = deadline
	if _, err := service.AcceptTrade(request, "team-2", offerID, tradeAcceptConfirmation); err == nil {
		t.Fatal("AcceptTrade at the exact deadline unexpectedly succeeded")
	}
	state := service.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusOpen {
		t.Fatalf("status after boundary rejection = %q, want open", state.TradeOffers[0].Status)
	}
	if len(state.Transactions) != 0 {
		t.Fatalf("Transactions after boundary rejection = %+v, want none", state.Transactions)
	}
}

// TestTradeExecutionAfterDeadlineStillFailsInvalidAssets proves execution
// keeps its live ownership re-validation even though T8 is no longer an
// execution failure once the offer was accepted before the deadline.
func TestTradeExecutionAfterDeadlineStillFailsInvalidAssets(t *testing.T) {
	service, start := newTradesTestService(t, "")
	service.cfg.Trades.Veto = "commissioner"
	service.cfg.Trades.ReviewHours = 24
	deadline := start.Add(time.Hour)
	service.cfg.Trades.Deadline = deadline.Format(time.RFC3339)
	current := start
	service.now = func() time.Time { return current }
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	offerID := proposeFixtureOffer(t, service)

	current = deadline.Add(-time.Minute)
	if _, err := service.AcceptTrade(request, "team-2", offerID, tradeAcceptConfirmation); err != nil {
		t.Fatalf("pre-deadline AcceptTrade: %v", err)
	}
	dropAt := current.Add(time.Minute)
	if err := service.store.RecordTransaction(Transaction{
		ID: "txn-deadline-drop", Season: 2026, Week: 1, Type: "drop", TeamID: "team-1",
		Drops: []TransactionPlayer{{PlayerID: "t1-a", Name: "Team1 A", Position: "RB", NFLTeam: "PIT"}},
		By:    "manager", At: dropAt,
	}, 3); err != nil {
		t.Fatalf("mid-window drop: %v", err)
	}
	transactionsBefore := len(service.store.Snapshot().Transactions)

	execAt := start.Add(25 * time.Hour)
	service.rosterOpsTick(execAt)
	state := service.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusFailed {
		t.Fatalf("status = %q, want failed", state.TradeOffers[0].Status)
	}
	if want := "Team1 A is not on East 1's roster"; state.TradeOffers[0].FailReason != want {
		t.Fatalf("FailReason = %q, want %q", state.TradeOffers[0].FailReason, want)
	}
	if len(state.Transactions) != transactionsBefore {
		t.Fatalf("Transactions = %d, want unchanged at %d", len(state.Transactions), transactionsBefore)
	}
	service.rosterOpsTick(execAt.Add(time.Minute))
	if got := len(service.store.Snapshot().Transactions); got != transactionsBefore {
		t.Fatalf("Transactions after duplicate failed tick = %d, want %d", got, transactionsBefore)
	}
}
func TestTradeOfferExpiryStates(t *testing.T) {
	svc, now := newTradesTestService(t, "")
	current := now
	svc.now = func() time.Time { return current }
	pool := svc.pool()
	future := svc.tradeOfferRow(pool, TradeOffer{ID: "future", FromTeamID: "team-1", ToTeamID: "team-2", Status: TradeStatusOpen, CreatedAt: now}, "team-2", true, false, false, 3)
	if future.ExpiryState != "upcoming" || future.Expiry == "" || future.ExpiryRelative != "in 7 days" {
		t.Fatalf("future expiry = state:%q exact:%q relative:%q", future.ExpiryState, future.Expiry, future.ExpiryRelative)
	}
	past := svc.tradeOfferRow(pool, TradeOffer{ID: "past", FromTeamID: "team-1", ToTeamID: "team-2", Status: TradeStatusOpen, CreatedAt: now.Add(-8 * 24 * time.Hour)}, "team-2", true, false, false, 3)
	if past.ExpiryState != "overdue" || past.Expiry == "" || past.ExpiryRelative != "1 day ago" {
		t.Fatalf("past expiry = state:%q exact:%q relative:%q", past.ExpiryState, past.Expiry, past.ExpiryRelative)
	}
	unknown := svc.tradeOfferRow(pool, TradeOffer{ID: "unknown", FromTeamID: "team-1", ToTeamID: "team-2", Status: TradeStatusOpen}, "team-2", true, false, false, 3)
	if unknown.ExpiryState != "unknown" || unknown.Expiry != "" || unknown.ExpiryRelative != "" {
		t.Fatalf("unknown expiry = state:%q exact:%q relative:%q", unknown.ExpiryState, unknown.Expiry, unknown.ExpiryRelative)
	}
	resolved := svc.tradeOfferRow(pool, TradeOffer{ID: "resolved", FromTeamID: "team-1", ToTeamID: "team-2", Status: TradeStatusDeclined, CreatedAt: now}, "team-2", true, false, false, 3)
	if resolved.HasExpiry || resolved.ExpiryState != "" {
		t.Fatalf("terminal offer carried expiry = %+v", resolved)
	}
}

package league

import (
	"net/http"
	"testing"
)

func confirmationValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func TestPlayerDropRequiresConfirmationBeforeMutation(t *testing.T) {
	testCases := []struct {
		name  string
		value []string
	}{
		{name: "missing"},
		{name: "wrong", value: []string{"drop-player-wrong"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newPlayersTestService(t)
			request, _ := http.NewRequest(http.MethodPost, "/players", nil)
			before := svc.store.Snapshot()
			_, err := svc.DropPlayer(request, "team-1", "rb-open", confirmationValue(tc.value))
			if err == nil || err.Error() != "this action requires explicit confirmation" {
				t.Fatalf("DropPlayer error = %v, want explicit confirmation rejection", err)
			}
			after := svc.store.Snapshot()
			if len(after.Transactions) != len(before.Transactions) {
				t.Fatalf("rejected drop appended a transaction: before=%d after=%d", len(before.Transactions), len(after.Transactions))
			}
			if owner := rosterOwner(currentRosters(after)); owner["rb-open"] != "team-1" {
				t.Fatalf("rejected drop changed ownership: %q", owner["rb-open"])
			}
		})
	}

	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.DropPlayer(request, "team-1", "rb-open", playerDropConfirmation); err != nil {
		t.Fatalf("confirmed DropPlayer: %v", err)
	}
	if owner := rosterOwner(currentRosters(svc.store.Snapshot())); owner["rb-open"] != "" {
		t.Fatalf("confirmed drop owner = %q, want free agent", owner["rb-open"])
	}
}

func TestPlayerAddDropRequiresConfirmationBeforeMutation(t *testing.T) {
	testCases := []struct {
		name  string
		value []string
	}{
		{name: "missing"},
		{name: "wrong", value: []string{"drop-player"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newPlayersTestService(t)
			request, _ := http.NewRequest(http.MethodPost, "/players", nil)
			before := svc.store.Snapshot()
			_, err := svc.AddPlayer(request, "team-1", "fa-open", "wr-open", confirmationValue(tc.value))
			if err == nil || err.Error() != "this action requires explicit confirmation" {
				t.Fatalf("AddPlayer error = %v, want explicit confirmation rejection", err)
			}
			after := svc.store.Snapshot()
			if len(after.Transactions) != len(before.Transactions) {
				t.Fatalf("rejected add/drop appended a transaction: before=%d after=%d", len(before.Transactions), len(after.Transactions))
			}
			owner := rosterOwner(currentRosters(after))
			if owner["fa-open"] != "" || owner["wr-open"] != "team-1" {
				t.Fatalf("rejected add/drop changed ownership: fa-open=%q wr-open=%q", owner["fa-open"], owner["wr-open"])
			}
		})
	}

	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.AddPlayer(request, "team-1", "fa-open", "wr-open", playerAddDropConfirmation); err != nil {
		t.Fatalf("confirmed AddPlayer swap: %v", err)
	}
	owner := rosterOwner(currentRosters(svc.store.Snapshot()))
	if owner["fa-open"] != "team-1" || owner["wr-open"] != "" {
		t.Fatalf("confirmed add/drop ownership: fa-open=%q wr-open=%q", owner["fa-open"], owner["wr-open"])
	}
}

func TestTradeAcceptRequiresConfirmationBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value []string
	}{{name: "missing"}, {name: "wrong", value: []string{"approve-trade"}}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTradesTestService(t, "")
			offerID := proposeFixtureOffer(t, svc)
			request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
			before := svc.store.Snapshot()
			_, err := svc.AcceptTrade(request, "team-2", offerID, confirmationValue(tc.value))
			if err == nil || err.Error() != "this action requires explicit confirmation" {
				t.Fatalf("AcceptTrade error = %v, want explicit confirmation rejection", err)
			}
			after := svc.store.Snapshot()
			if after.TradeOffers[0].Status != TradeStatusOpen || len(after.Transactions) != len(before.Transactions) {
				t.Fatalf("rejected accept mutated state: offer=%q transactions=%d/%d", after.TradeOffers[0].Status, len(after.Transactions), len(before.Transactions))
			}
		})
	}

	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID, tradeAcceptConfirmation); err != nil {
		t.Fatalf("confirmed AcceptTrade: %v", err)
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusAccepted {
		t.Fatalf("confirmed accept status = %q, want accepted", got)
	}
}

func TestTradeCommissionerConfirmationsRequireExactValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		invoke func(*Service, *http.Request, string) error
	}{
		{name: "approve missing", invoke: func(s *Service, r *http.Request, id string) error { _, err := s.ApproveTrade(r, id, ""); return err }},
		{name: "approve wrong", invoke: func(s *Service, r *http.Request, id string) error {
			_, err := s.ApproveTrade(r, id, tradeVetoConfirmation)
			return err
		}},
		{name: "veto missing", invoke: func(s *Service, r *http.Request, id string) error {
			_, err := s.CommissionerVetoTrade(r, id, "")
			return err
		}},
		{name: "veto wrong", invoke: func(s *Service, r *http.Request, id string) error {
			_, err := s.CommissionerVetoTrade(r, id, tradeApproveConfirmation)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTradesTestService(t, "")
			offerID := proposeFixtureOffer(t, svc)
			request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
			if _, err := svc.AcceptTrade(request, "team-2", offerID, tradeAcceptConfirmation); err != nil {
				t.Fatalf("seed AcceptTrade: %v", err)
			}
			before := svc.store.Snapshot()
			if err := tc.invoke(svc, request, offerID); err == nil || err.Error() != "this action requires explicit confirmation" {
				t.Fatalf("%s error = %v, want explicit confirmation rejection", tc.name, err)
			}
			after := svc.store.Snapshot()
			if after.TradeOffers[0].Status != TradeStatusAccepted || len(after.Transactions) != len(before.Transactions) {
				t.Fatalf("rejected %s mutated state: offer=%q transactions=%d/%d", tc.name, after.TradeOffers[0].Status, len(after.Transactions), len(before.Transactions))
			}
		})
	}

	for _, tc := range []struct {
		name   string
		invoke func(*Service, *http.Request, string) error
		want   string
	}{
		{name: "approve", invoke: func(s *Service, r *http.Request, id string) error {
			_, err := s.ApproveTrade(r, id, tradeApproveConfirmation)
			return err
		}, want: TradeStatusExecuted},
		{name: "veto", invoke: func(s *Service, r *http.Request, id string) error {
			_, err := s.CommissionerVetoTrade(r, id, tradeVetoConfirmation)
			return err
		}, want: TradeStatusVetoed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTradesTestService(t, "")
			offerID := proposeFixtureOffer(t, svc)
			request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
			if _, err := svc.AcceptTrade(request, "team-2", offerID, tradeAcceptConfirmation); err != nil {
				t.Fatalf("seed AcceptTrade: %v", err)
			}
			if err := tc.invoke(svc, request, offerID); err != nil {
				t.Fatalf("confirmed %s: %v", tc.name, err)
			}
			if got := svc.store.Snapshot().TradeOffers[0].Status; got != tc.want {
				t.Fatalf("confirmed %s status = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

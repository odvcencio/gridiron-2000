package trades

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
)

// TestTradesPageDeadlineRenderContract pins the template-side truth boundary:
// the service action flags can close new-trade controls without removing the
// truthful deadline banner or the still-valid decline/withdraw controls.
func TestTradesPageDeadlineRenderContract(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, marker := range []string{
		"<If cond={data.trade_deadline_passed}>",
		"<If cond={data.can_compose}>",
		"<If cond={offer.CanAccept}>",
		"<If cond={offer.CanCounter}>",
		"<If cond={offer.CanDecline}>",
		"<If cond={offer.CanWithdraw}>",
		"TRADE DEADLINE CLOSED:",
		"<If cond={data.trade_deadline_configured && data.trade_deadline_passed == false}>",
		"TRADE CREATION OPEN:",
		"data.trade_deadline_relative",
		"offer.ExpiryState",
		"offer.ExpiryRelative",
		"Existing offers can still be declined or withdrawn.",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("trades template lost deadline/action marker %q", marker)
		}
	}
	for _, pair := range []struct {
		name  string
		guard string
		form  string
	}{
		{name: "accept", guard: "<If cond={offer.CanAccept}>", form: "actionPath(\"trade-accept\")"},
		{name: "counter", guard: "<If cond={offer.CanCounter}>", form: "actionPath(\"trade-counter\")"},
		{name: "decline", guard: "<If cond={offer.CanDecline}>", form: "actionPath(\"trade-decline\")"},
	} {
		if strings.Index(body, pair.form) < strings.Index(body, pair.guard) {
			t.Fatalf("%s form appears before its action guard", pair.name)
		}
	}
}

func TestTradeValidationPreservesCheckboxesNoteAndCounterparty(t *testing.T) {
	form := url.Values{}
	form.Add("team_id", "team-1")
	form.Add("to_team_id", "team-2")
	form.Add("counterparty", "team-2")
	form.Add("give", "t1-a")
	form.Add("give", "t1-b")
	form.Add("get", "t2-a")
	form.Add("get", "t2-c")
	form.Set("note", "keep this note")
	request := httptest.NewRequest(http.MethodPost, "/trades/__actions/trade-propose", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := &action.Context{Request: request, FormData: map[string]string{
		"team_id": "team-1", "to_team_id": "team-2", "counterparty": "team-2", "note": "keep this note",
	}}
	resultErr, ok := tradeValidation(ctx, errors.New("invalid trade")).(*action.ResultError)
	if !ok {
		t.Fatalf("tradeValidation returned %T, want *action.ResultError", tradeValidation(ctx, errors.New("invalid trade")))
	}
	if got := resultErr.Result.Values["give"]; got != "t1-a,t1-b" {
		t.Fatalf("give recovery = %q, want both submitted assets", got)
	}
	if got := resultErr.Result.Values["get"]; got != "t2-a,t2-c" {
		t.Fatalf("get recovery = %q, want both submitted assets", got)
	}
	for key, want := range map[string]string{"note": "keep this note", "to_team_id": "team-2", "counterparty": "team-2"} {
		if got := resultErr.Result.Values[key]; got != want {
			t.Fatalf("%s recovery = %q, want %q", key, got, want)
		}
	}
}

func TestTradeSelectOptionsRejectsStaleIDs(t *testing.T) {
	options := tradeSelectOptions([]league.TradeRosterOption{{ID: "valid"}, {ID: "other"}}, "valid,stale")
	if !options[0].Selected || options[1].Selected {
		t.Fatalf("selected options = %#v, want only current valid ID selected", options)
	}
	if len(options) != 2 {
		t.Fatalf("stale submitted ID was added to roster options: %#v", options)
	}
}

func TestTradesTemplateContainsRecoveryHistoryAndFailureContracts(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, marker := range []string{
		"checked={opt.Selected}",
		"CounterGetOptions",
		"data-gosx-field-error=\"offer_id\"",
		"data.history",
		"Failure reason:",
		"StatusLabel",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("trades template lost lifecycle/recovery marker %q", marker)
		}
	}
}

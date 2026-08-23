package trades

import (
	"os"
	"strings"
	"testing"
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

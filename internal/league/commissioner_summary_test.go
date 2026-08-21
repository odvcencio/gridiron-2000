package league

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func TestCommissionerSummaryIsPIIFreeAndExplainsPoolCoverage(t *testing.T) {
	service := newTestService(t, false)
	service.store.state.Members = map[string]Member{
		"manager@example.com": {TeamID: "team-1", Name: "Manager", Email: "manager@example.com"},
		"co@example.com":      {TeamID: "team-1", Name: "Co", Email: "co@example.com", Role: "co"},
	}
	service.store.state.Ready["team-1"] = true
	service.store.state.Ready["retired-team"] = true
	capacity := len(service.Teams()) * CurrentDraftRounds()
	target := int(float64(capacity) * 2.5)
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{
		Mode: "live", Players: target, Target: target, LastSync: time.Now(),
	})
	if summary.Membership.ClaimedSeats != 1 || summary.Membership.Members != 2 || summary.Membership.ReadySeats != 1 {
		t.Fatalf("membership = %+v", summary.Membership)
	}
	if summary.Pool.RosterCapacity != capacity || summary.Pool.Cushion != target-capacity || summary.Pool.Coverage != 2.5 {
		t.Fatalf("pool = %+v", summary.Pool)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"manager@example.com", "co@example.com", `"email"`, `"invites"`, `"token"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestCommissionerSummaryKeepsUsablePartialPoolAsWarning(t *testing.T) {
	service := newTestService(t, false)
	capacity := len(service.Teams()) * CurrentDraftRounds()
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{
		Mode: "live", Players: capacity * 2, Target: capacity * 2, Error: "projection refresh timed out",
	})
	seenDegraded := false
	for _, item := range summary.Attention {
		if item.Code == "pool_unavailable" {
			t.Fatalf("usable live pool mislabeled unavailable: %+v", summary.Attention)
		}
		if item.Code == "pool_degraded" && item.Severity == "warning" {
			seenDegraded = true
		}
	}
	if !seenDegraded {
		t.Fatalf("partial live pool missing degraded warning: %+v", summary.Attention)
	}
}

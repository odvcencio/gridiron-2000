package league

import (
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func TestCommissionerSummaryPrioritizesDegradedBlitzRecovery(t *testing.T) {
	service := newTestService(t, false)
	now := time.Now()
	service.now = func() time.Time { return now }
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{Health: BlitzHealth{
			Enabled: true, State: BlitzStateDegraded,
			LastAttempt: now, LastSuccess: now.Add(-time.Hour), SafeError: "source temporarily unavailable",
			ExpectedGames: 4, FetchedGames: 3,
			Slates: map[string]BlitzSlateHealth{"pre2": {State: BlitzStateDegraded, ExpectedGames: 4, FetchedGames: 3}},
		}}
	})
	summary := service.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{Mode: "live", Actual: 300, Target: 300})
	if summary.Blitz.State != BlitzStateDegraded || summary.Blitz.ExpectedGames != 4 || summary.Blitz.FetchedGames != 3 {
		t.Fatalf("summary Blitz = %+v", summary.Blitz)
	}
	found := false
	for _, item := range summary.Attention {
		if item.Code == "blitz_source_degraded" {
			found = true
			if item.Severity != commissionerhq.AttentionSeverityWarning || item.Area != commissionerhq.AttentionAreaBlitz {
				t.Fatalf("Blitz attention = %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("missing degraded Blitz attention: %+v", summary.Attention)
	}
}

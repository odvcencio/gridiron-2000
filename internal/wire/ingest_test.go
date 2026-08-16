package wire

import (
	"testing"
	"time"
)

func TestCommunityMarketSightingIsLowTrustAndProvisional(t *testing.T) {
	now := time.Date(2026, 9, 13, 20, 12, 0, 0, time.UTC)
	service, err := NewService(Config{
		Root:    t.TempDir(),
		Enabled: false,
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	signal, err := service.SubmitSighting(CommunitySubmission{
		ReporterID: "manager-1", ReporterName: "Alex", EvidenceType: "market",
		SourceName: "PrizePicks", SourceURL: "https://www.prizepicks.com/", Summary: "Player was removed from the board",
	})
	if err != nil {
		t.Fatal(err)
	}
	if signal.Category != "market" || signal.TrustTier != "MARKET WATCH" || !signal.Provisional || signal.Confidence >= 0.3 {
		t.Fatalf("market signal = %+v", signal)
	}
	if _, err := service.SubmitSighting(CommunitySubmission{
		ReporterID: "manager-1", EvidenceType: "market", SourceName: "PrizePicks", Summary: "Player was removed from the board",
		SourceURL: "javascript:alert(1)",
	}); err == nil {
		t.Fatal("unsafe source URL was accepted")
	}
}

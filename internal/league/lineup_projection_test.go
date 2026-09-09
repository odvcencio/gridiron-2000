package league

import (
	"math"
	"testing"
)

func TestTeamBenchProjectedTotalSeparatesUnknownFromZero(t *testing.T) {
	lineup := EffectiveLineup{Bench: []Player{
		{ID: "known-a", Projection: 11.6},
		{ID: "unknown", Projection: 0},
		{ID: "known-b", Projection: 11.7},
	}}
	total, known := TeamBenchProjectedTotal(lineup)
	if math.Abs(total-23.3) > 1e-9 {
		t.Fatalf("bench total = %v, want 23.3", total)
	}
	if !known {
		t.Fatal("bench total known = false, want true when a bench player has a projection")
	}

	total, known = TeamBenchProjectedTotal(EffectiveLineup{Bench: []Player{{ID: "unknown", Projection: 0}}})
	if total != 0 {
		t.Fatalf("unknown-only bench total = %v, want zero before display formatting", total)
	}
	if known {
		t.Fatal("unknown-only bench total known = true, want false so the UI can render an em dash")
	}
}

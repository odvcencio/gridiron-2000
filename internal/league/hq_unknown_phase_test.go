package league

import (
	"strings"
	"testing"
)

func TestActionCenterDoesNotCallUnknownSeasonPreseason(t *testing.T) {
	for _, phase := range []string{"future-phase", "unknown"} {
		facts := ActionCenterFacts{Admitted: true, HasSeat: true, DraftComplete: true, SeasonPhase: phase}
		center := BuildActionCenter(facts)
		if strings.Contains(center.StageLabel, "PRESEASON") {
			t.Fatalf("phase %q mislabeled %q", phase, center.StageLabel)
		}
		if !strings.Contains(center.StageLabel, "UNAVAILABLE") || !strings.Contains(center.Summary, "commissioner") {
			t.Fatalf("phase %q needs an explicit unavailable state and recovery: %+v", phase, center)
		}
	}
	for _, phase := range []string{"", "preseason"} {
		center := BuildActionCenter(ActionCenterFacts{Admitted: true, HasSeat: true, DraftComplete: true, SeasonPhase: phase})
		if center.Stage != ActionCenterPostDraftPreseason {
			t.Fatalf("known preseason %q changed to %q", phase, center.Stage)
		}
	}
}

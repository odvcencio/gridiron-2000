package blitz

import (
	"os"
	"strings"
	"testing"
)

func TestBlitzUsesRecordedScoreCopy(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	if strings.Contains(source, "Live score") {
		t.Fatal("blitz still presents breakdown totals as live score")
	}
	if strings.Contains(source, "No live stats yet") {
		t.Fatal("blitz still promises live stats in the empty breakdown state")
	}
	if strings.Count(source, "Recorded score") != 2 {
		t.Fatalf("Recorded score labels = %d, want slot and leaderboard totals", strings.Count(source, "Recorded score"))
	}
	if !strings.Contains(source, "No recorded scoring stats yet.") {
		t.Fatal("blitz missing neutral empty scoring copy")
	}
}

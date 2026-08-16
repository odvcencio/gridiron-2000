package league

import (
	"testing"
	"time"
)

func TestSnakeDraftOrder(t *testing.T) {
	want := []string{"team-1", "team-2", "team-3", "team-4", "team-5", "team-6", "team-7", "team-8", "team-8", "team-7"}
	for i, expected := range want {
		if got := teamOnClock(i + 1); got != expected {
			t.Fatalf("pick %d: expected %s, got %s", i+1, expected, got)
		}
	}
}

func TestDefaultDraftDate(t *testing.T) {
	draft := parseDraftAt("")
	if got := draft.Format(time.RFC3339); got != DefaultDraftAt {
		t.Fatalf("expected %s, got %s", DefaultDraftAt, got)
	}
	if draft.Weekday() != time.Sunday {
		t.Fatalf("expected a Sunday, got %s", draft.Weekday())
	}
	svc := &Service{draftAt: draft}
	summary := svc.draftSummary(draft.Add(-7 * 24 * time.Hour))
	if got := summary["date"]; got != "SUN · AUG 16" {
		t.Fatalf("expected display date SUN · AUG 16, got %v", got)
	}
}

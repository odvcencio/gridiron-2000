package matchups

import "testing"

// TestPostedFinalWeekReadsAsFinal pins the historical-results fix: a week
// that is closed and posted carries the LEDGER live-state token (the
// A5 spec reuses it for both ends of the week), and the page used to map
// LEDGER to the pre-game phase, so every card on a past week showed its
// projection big under a PROJ label, or a bare dash where no projection
// existed for that week, instead of the result. posted_final decides it.
func TestPostedFinalWeekReadsAsFinal(t *testing.T) {
	if got := matchupStateClass("LEDGER", true); got != "state--final" {
		t.Errorf("posted-final class = %q, want state--final", got)
	}
	if got := matchupPhaseLabel("LEDGER", true); got != "FINAL" {
		t.Errorf("posted-final label = %q, want FINAL", got)
	}
	if got := matchupStateClass("LEDGER", false); got != "state--pre" {
		t.Errorf("unposted ledger class = %q, want state--pre (before kickoff)", got)
	}
	if got := matchupPhaseLabel("LEDGER", false); got != "PROJ" {
		t.Errorf("unposted ledger label = %q, want PROJ", got)
	}
	if got := matchupStateClass("LIVE", false); got != "state--live" {
		t.Errorf("live class = %q", got)
	}
	if got := matchupPhaseLabel("UNDERWAY", false); got != "" {
		t.Errorf("underway label = %q, want none", got)
	}
}

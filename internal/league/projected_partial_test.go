package league

import "testing"

// TestProjectedLiveTextShowsAPartialTotalInsteadOfADash pins the owner's
// 2026-09-16 decision: a starter the source gives no forecast for counts
// as zero and the number still shows, so a manager sees a low projection
// and knows to change the lineup. Withholding it blanked the estimate for
// both sides of a matchup with nothing on the card to explain why.
func TestProjectedLiveTextShowsAPartialTotalInsteadOfADash(t *testing.T) {
	for _, tc := range []struct {
		name            string
		total           float64
		starters, known int
		usable          bool
		want            string
	}{
		{"every starter forecast", 118.4, 9, 9, true, "118.4"},
		{"one starter on IR with no forecast", 104.9, 9, 8, true, "104.9*"},
		{"no starter forecast at all", 0, 9, 0, true, "0.0*"},
		{"no lineup set", 0, 0, 0, true, winProbabilityDashText},
		// A source pinned to another week is wrong, not partial: without
		// this gate projectedTotal falls back to summing actual points and
		// the card would label that sum a forecast.
		{"forecasts are for a different week", 12.0, 9, 0, false, winProbabilityDashText},
	} {
		if got := projectedLiveText(tc.total, tc.starters, tc.known, tc.usable); got != tc.want {
			t.Errorf("%s: projectedLiveText(%v, %d, %d, %v) = %q, want %q",
				tc.name, tc.total, tc.starters, tc.known, tc.usable, got, tc.want)
		}
	}
}

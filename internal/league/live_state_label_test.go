package league

import "testing"

// TestLiveStateLabelSplitsTheAmbiguousToken is the 2026-09-10
// understandability sweep. LEDGER was rendered directly as chip text and
// meant both "nothing has kicked off" and "the week is posted and
// official" — the two opposite ends of a week, under one word.
func TestLiveStateLabelSplitsTheAmbiguousToken(t *testing.T) {
	cases := []struct {
		state       string
		postedFinal bool
		want        string
	}{
		{LiveStateLedger, false, "SCHEDULED"},
		{LiveStateLedger, true, "FINAL"},
		{LiveStateLive, false, "LIVE"},
		{LiveStateUnderway, false, "UNDERWAY"},
		// "PAUSED" reads as though the game stopped; it is our feed.
		{LiveStatePaused, false, "FEED DELAYED"},
		// Every game done, the official ledger not yet posted.
		{LiveStateFinal, false, "AWAITING FINAL"},
		// An unknown token still resolves to something a reader can use.
		{"", false, "SCHEDULED"},
		{"", true, "FINAL"},
	}
	for _, tc := range cases {
		if got := LiveStateLabel(tc.state, tc.postedFinal); got != tc.want {
			t.Errorf("LiveStateLabel(%q, postedFinal=%v) = %q, want %q", tc.state, tc.postedFinal, got, tc.want)
		}
	}
}

// TestLiveStateLabelNeverLeaksATokenVerbatim: no label may be the raw
// token, or the sweep has achieved nothing.
func TestLiveStateLabelNeverLeaksATokenVerbatim(t *testing.T) {
	for _, state := range []string{LiveStateLedger, LiveStatePaused} {
		for _, posted := range []bool{false, true} {
			if got := LiveStateLabel(state, posted); got == state {
				t.Errorf("LiveStateLabel(%q, %v) returned the raw token", state, posted)
			}
		}
	}
}

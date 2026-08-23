package league

import "testing"

func TestClockReasonLabelsMakeNotSeenSafetyClockExplicit(t *testing.T) {
	for _, test := range []struct {
		reason string
		want   string
	}{
		{reason: "clock", want: "ON THE CLOCK"},
		{reason: "autopick", want: "AUTOPICK ARMED"},
		{reason: "not_seen", want: "NOT SEEN — SHORT SAFETY CLOCK"},
	} {
		if got := clockReasonLabel(test.reason); got != test.want {
			t.Errorf("clockReasonLabel(%q) = %q, want %q", test.reason, got, test.want)
		}
	}
	if got := clockReasonLabel("away-cap"); got == "AWAY — SHORT CLOCK" {
		t.Fatalf("stale away-cap label = %q; AWAY must retain the normal clock", got)
	}
}

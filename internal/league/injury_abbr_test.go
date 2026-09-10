package league

import "testing"

// TestInjuryAbbrHasOneSource is holistic-review finding 1.3: the compact
// chip code was derived twice — once by the canonical vocabulary and once
// by a private switch that had drifted out of step with it. PUP and
// SUSPENDED had no case, so a player carrying either rendered the whole
// sentence into a chip sized for two characters.
func TestInjuryAbbrHasOneSource(t *testing.T) {
	for _, designation := range []string{
		"Out", "Doubtful", "Questionable", "Injured reserve",
		"Physically unable to perform", "Suspended", "",
	} {
		want := NormalizeInjury(designation, "").Code
		if got := injuryDesignationAbbr(designation); got != want {
			t.Errorf("injuryDesignationAbbr(%q) = %q, want the canonical %q", designation, got, want)
		}
	}
	// The two that used to fall through to the full sentence.
	if got := injuryDesignationAbbr("Physically unable to perform"); got != "PUP" {
		t.Errorf("PUP abbreviated to %q, want PUP", got)
	}
	if got := injuryDesignationAbbr("Suspended"); got != "SUS" {
		t.Errorf("Suspended abbreviated to %q, want SUS", got)
	}
	// An unknown word a real feed reported is still kept, not dropped.
	if got := injuryDesignationAbbr("Limited"); got != "Limited" {
		t.Errorf("unknown designation = %q, want it preserved", got)
	}
}

package gameclock

import "testing"

func TestNormalizePeriod(t *testing.T) {
	for input, want := range map[string]string{
		"1st": "Q1", "2nd": "Q2", "3rd": "Q3", "4th": "Q4",
		"1": "Q1", "2": "Q2", "3": "Q3", "4": "Q4",
		" first quarter ": "Q1", "second": "Q2", "third": "Q3", "fourth": "Q4",
		"Q1": "Q1", "q2": "Q2", " 2nd 12:04 ": "Q2", "Q3 8:12": "Q3",
		"Q4": "Q4", "HALF": "HALF", "Halftime": "HALF", "HALF TIME": "HALF",
		"OT": "OT", "Overtime": "OT", "2OT": "OT", "Final": "Final",
		"": "", "  ": "", " Scheduled ": "Scheduled", "Delayed": "Delayed",
		"Q10": "Q10", "1stDown": "1stDown", "OTTER": "OTTER",
	} {
		if got := NormalizePeriod(input); got != want {
			t.Errorf("NormalizePeriod(%q) = %q, want %q", input, got, want)
		}
	}
}

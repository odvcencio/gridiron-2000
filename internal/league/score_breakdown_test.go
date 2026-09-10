package league

import (
	"strings"
	"testing"
)

// TestScoreBreakdownTextExplainsAScore is the owner's 2026-09-09 request:
// a score should say where it came from, not only what it totals.
func TestScoreBreakdownTextExplainsAScore(t *testing.T) {
	values := breakdownDefaultValues()
	// The breakdown is one line per contributing rule, the value padded
	// into a shared column. Asserting the exact padding would pin a width
	// that legitimately changes with the longest label in the set, so
	// these check the STRUCTURE: which rules appear, in what order, and
	// what each is worth.
	cases := []struct {
		name  string
		stats map[string]float64
		want  [][2]string // label, value — in order
	}{
		{
			name:  "a defense's whole line, in rule order",
			stats: map[string]float64{"dstInt": 3, "dstSack": 2, "dstPointsAllowed7": 1, "dstYardsAllowed300": 1},
			want: [][2]string{
				{"Sack x2", "2.0"},
				{"Interception x3", "6.0"},
				{"7-13 points allowed", "4.0"},
				{"300-399 yards allowed", "0.0"},
			},
		},
		{
			// A count of one carries no multiplier.
			name:  "a single event",
			stats: map[string]float64{"dstTD": 1},
			want:  [][2]string{{"Defensive TD", "6.0"}},
		},
		{
			name:  "a quarterback's line",
			stats: map[string]float64{"passYards": 250, "passTD": 2, "passInt": 1},
			want: [][2]string{
				{"Passing yards (per yard) x250", "10.0"},
				{"Passing TD x2", "8.0"},
				{"Interception thrown", "-2.0"},
			},
		},
		{
			// A band worth nothing is still part of the explanation.
			name:  "a band that scores zero is still listed",
			stats: map[string]float64{"dstPointsAllowed21": 1},
			want:  [][2]string{{"21-27 points allowed", "0.0"}},
		},
		{name: "nothing to explain", stats: map[string]float64{}, want: nil},
		{name: "an unknown key is not explained", stats: map[string]float64{"somethingElse": 4}, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScoreBreakdownText(tc.stats, values)
			if len(tc.want) == 0 {
				if got != "" {
					t.Fatalf("ScoreBreakdownText = %q, want empty", got)
				}
				return
			}
			lines := strings.Split(got, "\n")
			if len(lines) != len(tc.want) {
				t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(tc.want), got)
			}
			valueColumn := -1
			for i, line := range lines {
				label, value := tc.want[i][0], tc.want[i][1]
				if !strings.HasPrefix(line, label) {
					t.Errorf("line %d = %q, want it to start with %q", i, line, label)
				}
				if !strings.HasSuffix(line, value) {
					t.Errorf("line %d = %q, want it to end with %q", i, line, value)
				}
				// Every value starts in the same column, so the numbers
				// read down the tooltip as a column rather than a sentence.
				at := len(line) - len(value)
				if valueColumn == -1 {
					valueColumn = at
				} else if at != valueColumn {
					t.Errorf("line %d puts its value at column %d, want %d (values must align):\n%s", i, at, valueColumn, got)
				}
				// The gap is real whitespace, never a squashed join.
				if !strings.HasSuffix(strings.TrimSuffix(line, value), "  ") {
					t.Errorf("line %d has no clear gap before its value: %q", i, line)
				}
			}
		})
	}
}

// TestScoreBreakdownMatchesTheScore is the invariant that matters: the
// explanation must add up to the number it sits beside.
func TestScoreBreakdownMatchesTheScore(t *testing.T) {
	values := breakdownDefaultValues()
	stats := map[string]float64{
		"dstInt": 3, "dstSack": 2, "dstPointsAllowed7": 1, "dstYardsAllowed300": 1,
		"dstForcedFumble": 1, "dstBlockedKick": 1,
	}
	// Every segment's own points, summed straight out of the same rules.
	total := scorePlayerStats(stats, values)
	if total != 15 {
		t.Fatalf("scored %v, want 15 (6 INT + 2 sacks + 4 band + 0 yards + 1 forced fumble + 2 block)", total)
	}
	if ScoreBreakdownText(stats, values) == "" {
		t.Fatal("a scored line produced no explanation")
	}
}

// TestScoreBreakdownHonoursCommissionerValues proves the explanation
// reads the league's own rule values, never the shipped defaults.
func TestScoreBreakdownHonoursCommissionerValues(t *testing.T) {
	values := breakdownDefaultValues()
	values["dstInt"] = 3
	got := ScoreBreakdownText(map[string]float64{"dstInt": 2}, values)
	if !strings.HasPrefix(got, "Interception x2") || !strings.HasSuffix(got, "6.0") {
		t.Fatalf("ScoreBreakdownText = %q, want the league's own 3-point interception", got)
	}
}

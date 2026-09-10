package league

import "testing"

// TestScoreBreakdownTextExplainsAScore is the owner's 2026-09-09 request:
// a score should say where it came from, not only what it totals.
func TestScoreBreakdownTextExplainsAScore(t *testing.T) {
	values := breakdownDefaultValues()
	cases := []struct {
		name  string
		stats map[string]float64
		want  string
	}{
		{
			name:  "a defense's whole line, in rule order",
			stats: map[string]float64{"dstInt": 3, "dstSack": 2, "dstPointsAllowed7": 1, "dstYardsAllowed300": 1},
			want:  "Sack x2 2.0 · Interception x3 6.0 · 7-13 points allowed 4.0 · 300-399 yards allowed 0.0",
		},
		{
			// A count of one carries no multiplier.
			name:  "a single event",
			stats: map[string]float64{"dstTD": 1},
			want:  "Defensive TD 6.0",
		},
		{
			// Per-yard rules keep their real count and no trailing zero.
			name:  "a quarterback's line",
			stats: map[string]float64{"passYards": 250, "passTD": 2, "passInt": 1},
			want:  "Passing yards (per yard) x250 10.0 · Passing TD x2 8.0 · Interception thrown -2.0",
		},
		{
			// A band worth nothing is still part of the explanation: a
			// reader must not be left hunting for points that were never
			// there.
			name:  "a band that scores zero is still listed",
			stats: map[string]float64{"dstPointsAllowed21": 1},
			want:  "21-27 points allowed 0.0",
		},
		{
			name:  "nothing to explain",
			stats: map[string]float64{},
			want:  "",
		},
		{
			// A key no rule defines contributes no points, so it must not
			// appear in an explanation of them either.
			name:  "an unknown key is not explained",
			stats: map[string]float64{"somethingElse": 4},
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScoreBreakdownText(tc.stats, values); got != tc.want {
				t.Fatalf("ScoreBreakdownText = %q,\n                want %q", got, tc.want)
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
	if got != "Interception x2 6.0" {
		t.Fatalf("ScoreBreakdownText = %q, want the league's own 3-point interception", got)
	}
}

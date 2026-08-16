package openstats

import "testing"

// TestAggregatePlayerSeasonSummariesSumsAcrossWeeks mirrors the fixture-CSV
// style used by the service tests: build PlayerWeekStat rows directly rather
// than round-tripping through a CSV file, since parsePlayerStats is already
// covered by parser-shaped fixtures elsewhere.
func TestAggregatePlayerSeasonSummariesSumsAcrossWeeks(t *testing.T) {
	stats := []PlayerWeekStat{
		{
			PlayerID: "00-100", PlayerName: "Steady Runner", Position: "RB", Season: 2025, Week: 1,
			Team: "DAL", RushingYards: 80, RushingTDs: 1, Receptions: 2, ReceivingYards: 10,
			FantasyPoints: 12.0, FantasyPointsPPR: 14.0,
		},
		{
			PlayerID: "00-100", PlayerName: "Steady Runner", Position: "RB", Season: 2025, Week: 2,
			Team: "DAL", RushingYards: 60, RushingTDs: 0, Receptions: 3, ReceivingYards: 15,
			FantasyPoints: 8.0, FantasyPointsPPR: 11.0,
		},
		{
			// A midseason trade: the same player later shows a new team. The
			// summary should keep the last team seen in row order.
			PlayerID: "00-100", PlayerName: "Steady Runner", Position: "RB", Season: 2025, Week: 3,
			Team: "NYG", RushingYards: 40, RushingTDs: 1, Receptions: 1, ReceivingYards: 5,
			FantasyPoints: 7.0, FantasyPointsPPR: 8.0,
		},
		{
			PlayerID: "00-200", PlayerName: "Deep Threat", Position: "WR", Season: 2025, Week: 1,
			Team: "SEA", ReceivingYards: 90, ReceivingTDs: 1, Receptions: 4,
			FantasyPoints: 9.0, FantasyPointsPPR: 13.0,
		},
		{
			// A row with no player_id must not create a phantom summary.
			PlayerID: "", PlayerName: "No ID", Position: "TE", Season: 2025, Week: 1,
			Team: "SEA", FantasyPoints: 5.0, FantasyPointsPPR: 5.0,
		},
	}
	summaries := aggregatePlayerSeasonSummaries(stats)
	if len(summaries) != 2 {
		t.Fatalf("summaries = %+v", summaries)
	}

	// Sorted by descending half-PPR fantasy points: Steady Runner totals
	// (12+14)/2 + (8+11)/2 + (7+8)/2 = 13 + 9.5 + 7.5 = 30, Deep Threat totals
	// (9+13)/2 = 11.
	runner := summaries[0]
	if runner.PlayerName != "Steady Runner" || runner.Team != "NYG" || runner.Games != 3 {
		t.Fatalf("runner identity/games wrong: %+v", runner)
	}
	if runner.RushYds != 180 || runner.RushTD != 2 || runner.Receptions != 6 || runner.RecYds != 30 {
		t.Fatalf("runner stat sums wrong: %+v", runner)
	}
	if runner.FantasyPoints != 30 {
		t.Fatalf("runner half-PPR points = %v, want 30", runner.FantasyPoints)
	}

	threat := summaries[1]
	if threat.PlayerName != "Deep Threat" || threat.Team != "SEA" || threat.Games != 1 {
		t.Fatalf("threat identity/games wrong: %+v", threat)
	}
	if threat.RecYds != 90 || threat.RecTD != 1 || threat.Receptions != 4 {
		t.Fatalf("threat stat sums wrong: %+v", threat)
	}
	if threat.FantasyPoints != 11 {
		t.Fatalf("threat half-PPR points = %v, want 11", threat.FantasyPoints)
	}
}

func TestAggregatePlayerSeasonSummariesEmptyInput(t *testing.T) {
	if summaries := aggregatePlayerSeasonSummaries(nil); len(summaries) != 0 {
		t.Fatalf("expected no summaries, got %+v", summaries)
	}
}

func TestNormalizePlayerKeyCollapsesStyleVariants(t *testing.T) {
	cases := []struct {
		name        string
		nameA, posA string
		nameB, posB string
	}{
		{
			name:  "punctuation and casing collapse",
			nameA: "Amon-Ra St. Brown", posA: "WR",
			nameB: "amon-ra st. brown", posB: "wr",
		},
		{
			name:  "apostrophe and spacing collapse",
			nameA: "Ka'imi Fairbairn", posA: "K",
			nameB: "kaimi  fairbairn", posB: "k",
		},
		{
			name:  "suffix punctuation collapses",
			nameA: "Michael Pittman Jr.", posA: "WR",
			nameB: "michael pittman jr", posB: "WR",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyA := NormalizePlayerKey(tc.nameA, tc.posA)
			keyB := NormalizePlayerKey(tc.nameB, tc.posB)
			if keyA != keyB {
				t.Fatalf("keys did not collapse: %q vs %q", keyA, keyB)
			}
		})
	}
}

func TestNormalizePlayerKeyDistinguishesPosition(t *testing.T) {
	// Same normalized name, different position: must not collide, since a
	// team can carry a QB and a WR who share a surname-driven key otherwise.
	first := NormalizePlayerKey("Josh Allen", "QB")
	second := NormalizePlayerKey("Josh Allen", "LB")
	if first == second {
		t.Fatalf("expected distinct keys for different positions, got %q for both", first)
	}
}

func TestNormalizePlayerKeyExactFormat(t *testing.T) {
	got := NormalizePlayerKey("Amon-Ra St. Brown", "WR")
	want := "amonrastbrown|WR"
	if got != want {
		t.Fatalf("NormalizePlayerKey = %q, want %q", got, want)
	}
}

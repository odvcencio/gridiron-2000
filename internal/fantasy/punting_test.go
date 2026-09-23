package fantasy

import "testing"

func TestParseLivePuntingAggregates(t *testing.T) {
	// Field names/values verified in the 2026-09-13 live relay payload.
	box := ParseBoxScore([]byte(`{"gameID":"20260913_TB@CIN","currentPeriod":"2nd","gameStatusCode":"1","playerStats":{"4608820":{"longName":"Ryan Rehkow","teamAbv":"CIN","Punting":{"punts":"1","puntYds":"51","puntLong":"51","puntsin20":"0","puntTouchBacks":"0"}}}}`))
	line, ok := box.Players["4608820"]
	if !ok || line.Stats["puntYards"] != 51 || line.Stats["puntLong50"] != 1 || line.Team != "CIN" {
		t.Fatalf("live punter discarded or misparsed: %+v", line)
	}
}

func TestLivePuntingDoesNotInventPerPuntBonuses(t *testing.T) {
	stats := map[string]float64{}
	entry := map[string]any{"Punting": map[string]any{"punts": "3", "puntYds": "146", "puntLong": "51", "puntsin20": "2", "puntTouchBacks": "1"}}
	if !addLivePuntingStats(stats, entry) || stats["puntIn20"] != 2 || stats["puntTouchback"] != 1 || stats["puntLong50"] != 1 || stats["puntYards"] != 51 {
		t.Fatalf("known aggregates = %v", stats)
	}
	for _, key := range []string{"coffinCorner", "puntDownedInside5", "puntBlocked"} {
		if _, invented := stats[key]; invented {
			t.Fatalf("aggregate invented %s: %v", key, stats)
		}
	}
}

func TestMultiplePuntsRetainVerifiedQualifyingDistance(t *testing.T) {
	stats := map[string]float64{}
	entry := map[string]any{"Punting": map[string]any{"punts": "2", "puntYds": "90", "puntLong": "45", "puntsin20": "0", "puntTouchBacks": "0"}}
	if !addLivePuntingStats(stats, entry) || stats["puntYards"] != 45 || len(stats) != 1 {
		t.Fatalf("known longest punt must earn points, without scoring all aggregate yards: %v", stats)
	}
}

func TestLivePuntingRetainsConfirmedZeroButNotReturnerOrInvalidRows(t *testing.T) {
	for _, raw := range []string{
		`{"punts":"0","puntYds":"0","puntLong":"0","puntsin20":"0","puntTouchBacks":"0"}`,
		`{"punts":"1","puntYds":"39","puntLong":"39","puntsin20":"0","puntTouchBacks":"0"}`,
	} {
		box := ParseBoxScore([]byte(`{"playerStats":{"p":{"longName":"Test Punter","Punting":` + raw + `}}}`))
		if line, ok := box.Players["p"]; !ok || len(line.Stats) != 0 {
			t.Fatalf("confirmed zero row not retained: %+v", box.Players)
		}
	}
	for _, raw := range []string{`{"puntReturnYds":"24"}`, `{"punts":null}`, `{"punts":"NaN"}`, `{"punts":"-1"}`, `{"punts":"1.5"}`} {
		box := ParseBoxScore([]byte(`{"playerStats":{"p":{"Punting":` + raw + `}}}`))
		if _, ok := box.Players["p"]; ok {
			t.Fatalf("invalid/non-punter row retained: %s", raw)
		}
	}
}

// TestPuntQualifiesForYardsBonus pins the single shared 40+-yard-bonus
// threshold (audit item 6: punting had two independently maintained
// thresholds, main.go and this package). A blocked punt never qualifies
// regardless of its recorded distance.
func TestPuntQualifiesForYardsBonus(t *testing.T) {
	cases := []struct {
		distance float64
		blocked  bool
		want     bool
	}{
		{distance: 39, blocked: false, want: false},
		{distance: 40, blocked: false, want: true},
		{distance: 65, blocked: false, want: true},
		{distance: 65, blocked: true, want: false},
	}
	for _, c := range cases {
		if got := PuntQualifiesForYardsBonus(c.distance, c.blocked); got != c.want {
			t.Errorf("PuntQualifiesForYardsBonus(%v, %v) = %v, want %v", c.distance, c.blocked, got, c.want)
		}
	}
}

// TestPuntQualifiesForLong50 pins the single shared 50+-yard "long punt"
// threshold, the other constant this audit item found duplicated.
func TestPuntQualifiesForLong50(t *testing.T) {
	cases := []struct {
		distance float64
		blocked  bool
		want     bool
	}{
		{distance: 49, blocked: false, want: false},
		{distance: 50, blocked: false, want: true},
		{distance: 70, blocked: false, want: true},
		{distance: 70, blocked: true, want: false},
	}
	for _, c := range cases {
		if got := PuntQualifiesForLong50(c.distance, c.blocked); got != c.want {
			t.Errorf("PuntQualifiesForLong50(%v, %v) = %v, want %v", c.distance, c.blocked, got, c.want)
		}
	}
}

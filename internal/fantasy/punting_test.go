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
	if !addLivePuntingStats(stats, entry) || stats["puntIn20"] != 2 || stats["puntTouchback"] != 1 || stats["puntLong50"] != 1 {
		t.Fatalf("known aggregates = %v", stats)
	}
	for _, key := range []string{"puntYards", "coffinCorner", "puntDownedInside5", "puntBlocked"} {
		if _, invented := stats[key]; invented {
			t.Fatalf("aggregate invented %s: %v", key, stats)
		}
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

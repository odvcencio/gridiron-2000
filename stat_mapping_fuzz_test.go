package main

import (
	"encoding/json"
	"math"
	"testing"

	"gridiron-2000/internal/openstats"
)

// FuzzOffenseStatLine feeds arbitrary bytes, decoded as JSON, into
// offenseStatLine's openstats.PlayerWeekStat input (the Tank01/nflverse
// weekly box-score row every offense scoring key maps from — see
// offenseStatLine's own doc comment). offenseStatLine is a pure function:
// no I/O, no global state, so a crash or a non-finite output here can only
// come from the mapping itself, never from an unrelated fixture.
//
// The property under test is narrow and load-bearing: every value
// offenseStatLine emits must be finite. A NaN or +/-Inf silently
// contaminates league.ScoreRuleStats' weighted sum (both the live matchup
// cache, matchup_cache.go, and week-close scoring, leagueWeekStatsSource,
// score through this same map), and a NaN total compares false against
// every other total — a manager's week could go from "highest score" to
// "unrankable" with no error anywhere in the chain. A source that once
// emitted a non-finite float (float64(json.Number) never does, but a
// mirror can carry an out-of-range or malformed numeric column) must not
// reach that comparison.
func FuzzOffenseStatLine(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"passing_yards":275,"passing_tds":2,"passing_interceptions":1,"rushing_yards":12,"rushing_tds":0,"receptions":6,"receiving_yards":80,"receiving_tds":1,"fumbles_lost":0}`,
		`{"passing_yards":-40,"rushing_yards":-999999,"receiving_tds":-3}`,
		`{"passing_yards":1e308,"rushing_yards":1e308,"receiving_yards":1e308}`,
		`{"passing_2pt_conversions":1,"rushing_2pt_conversions":1,"receiving_2pt_conversions":1}`,
		`{"fg_made":4,"fg_missed":1,"fg_blocked":1,"xp_made":3}`,
		`{"special_teams_tds":1}`,
		`{"position":"K","fg_made":0,"fg_missed":0}`,
		`null`,
		`{"passing_yards":0.1,"rushing_yards":0.0000001}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		var row openstats.PlayerWeekStat
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Skip("not a PlayerWeekStat-shaped JSON document")
		}
		stats := offenseStatLine(row)
		for key, value := range stats {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("offenseStatLine(%+v)[%q] = %v, want a finite score", row, key, value)
			}
		}
	})
}

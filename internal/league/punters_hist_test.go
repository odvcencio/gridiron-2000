package league

import "testing"

// TestPunterHistLineMatchesTownsendHOU pins the embedded 2025 punter index's
// canonical match: team + last name, case-insensitive, against the
// Townsend/HOU entry the roster-ops spec (section 4.1.2 / WP-R0) names
// explicitly.
func TestPunterHistLineMatchesTownsendHOU(t *testing.T) {
	line, ok := punterHistLine("Tommy Townsend", "HOU")
	if !ok || line == "" {
		t.Fatalf("Townsend/HOU must match the embedded index; got ok=%v line=%q", ok, line)
	}
	// Case-insensitive on both the team and the last name.
	if line2, ok2 := punterHistLine("tommy townsend", "hou"); !ok2 || line2 != line {
		t.Fatalf("lower-cased name/team must still match: ok=%v line=%q, want %q", ok2, line2, line)
	}
}

// TestPunterHistLineNoMatch pins the fail-quiet contract: an unknown last
// name, a known last name on the wrong team, and an empty name all return
// ("", false) rather than a wrong attribution or a panic.
func TestPunterHistLineNoMatch(t *testing.T) {
	cases := []struct {
		name string
		team string
	}{
		{"Nobody Punter", "HOU"},   // unknown last name
		{"Tommy Townsend", "DAL"},  // right last name, wrong team
		{"", "HOU"},                // empty name
		{"   ", "HOU"},             // whitespace-only name
	}
	for _, tc := range cases {
		if line, ok := punterHistLine(tc.name, tc.team); ok {
			t.Errorf("punterHistLine(%q, %q) = (%q, true), want a miss", tc.name, tc.team, line)
		}
	}
}

// TestPunterHistLineUsesLastWord checks that matching keys on the last
// space-separated token of the full name, so a multi-word first/middle
// name (for example a hyphenated or suffixed name) still resolves.
func TestPunterHistLineUsesLastWord(t *testing.T) {
	if _, ok := punterHistLine("A.J. Cole", "LV"); !ok {
		t.Error("A.J. Cole (LV) must match the embedded Cole/LV entry")
	}
	if got := lastWord("A.J. Cole"); got != "Cole" {
		t.Errorf("lastWord(%q) = %q, want Cole", "A.J. Cole", got)
	}
	if got := lastWord(""); got != "" {
		t.Errorf("lastWord empty name = %q, want empty", got)
	}
}

// TestWithHistoricalAttachesPunterFallback checks the full merge path
// (service.go's withHistorical): a Position "P" player whose primary
// historicalFn source misses (nflverse carries no punting columns) still
// gets its Hist filled from the embedded punter index; a non-punter
// position never triggers the fallback; an already-set Hist is left
// untouched.
func TestWithHistoricalAttachesPunterFallback(t *testing.T) {
	service := newTestService(t, true)
	service.SetHistoricalSource(func(name, position string) (string, bool) {
		return "", false // nflverse never matches a punter (no punting columns)
	})

	punter := service.withHistorical(Player{Name: "Tommy Townsend", Position: "P", NFLTeam: "HOU"})
	if punter.Hist == "" {
		t.Fatalf("punter fallback did not attach a hist line: %+v", punter)
	}

	unknownPunter := service.withHistorical(Player{Name: "Nobody Punter", Position: "P", NFLTeam: "HOU"})
	if unknownPunter.Hist != "" {
		t.Fatalf("an unmatched punter must attach nothing: %+v", unknownPunter)
	}

	kicker := service.withHistorical(Player{Name: "Tommy Townsend", Position: "K", NFLTeam: "HOU"})
	if kicker.Hist != "" {
		t.Fatalf("the punter fallback must never apply to a non-P position: %+v", kicker)
	}

	already := service.withHistorical(Player{Name: "Tommy Townsend", Position: "P", NFLTeam: "HOU", Hist: "already set"})
	if already.Hist != "already set" {
		t.Fatalf("existing Hist must not be overwritten: %+v", already)
	}
}

// TestWithHistoricalPunterFallbackWithNoPrimarySource checks that the
// punter fallback still applies when no HistoricalSource is attached at
// all (SetHistoricalSource never called) — the fallback does not depend on
// the primary source existing.
func TestWithHistoricalPunterFallbackWithNoPrimarySource(t *testing.T) {
	service := newTestService(t, true)
	punter := service.withHistorical(Player{Name: "Tommy Townsend", Position: "P", NFLTeam: "HOU"})
	if punter.Hist == "" {
		t.Fatalf("punter fallback must work with no primary source attached: %+v", punter)
	}
}

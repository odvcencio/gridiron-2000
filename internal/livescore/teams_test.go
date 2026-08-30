package livescore

import "testing"

// TestDSTNamesCoversAllThirtyTwoTeams pins the team-abbreviation join
// table against source drift: exactly 32 entries, every value formatted
// "{Nickname} D/ST" (the fantasy pool's established naming convention,
// internal/fantasy/fallback.go). Moved here from package main's
// TestDSTNicknamesCoversAllThirtyTwoTeams (openstats_adapter_wpr2_test.go)
// when Task 5 deleted main.go's dstNicknames copy in favor of this table.
func TestDSTNamesCoversAllThirtyTwoTeams(t *testing.T) {
	if len(dstNames) != 32 {
		t.Fatalf("dstNames has %d entries, want 32", len(dstNames))
	}
	for team, name := range dstNames {
		if len(name) < 6 || name[len(name)-5:] != " D/ST" {
			t.Errorf("dstNames[%q] = %q, want a \"... D/ST\" suffix", team, name)
		}
	}
}

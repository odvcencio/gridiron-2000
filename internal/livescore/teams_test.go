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

// TestTeamAliasesCoversAllThirtyTwoTeams pins the wire-trigger seam's own
// team alias table (GC-2 layer 3) against source drift: exactly 32
// entries, each carrying a non-empty city and nickname.
func TestTeamAliasesCoversAllThirtyTwoTeams(t *testing.T) {
	if len(teamAliases) != 32 {
		t.Fatalf("teamAliases has %d entries, want 32", len(teamAliases))
	}
	for team, aliases := range teamAliases {
		if len(aliases) != 2 || aliases[0] == "" || aliases[1] == "" {
			t.Errorf("teamAliases[%q] = %v, want [city, nickname]", team, aliases)
		}
	}
}

// TestTeamMentionedMatchesNicknameCityOrIsUnrecognized covers
// TeamMentioned's own matching rule: a nickname or city mention matches,
// case-insensitively; an unrelated team's mention does not; an
// unrecognized abbreviation always reports false.
func TestTeamMentionedMatchesNicknameCityOrIsUnrecognized(t *testing.T) {
	if !TeamMentioned("BUF", "TOUCHDOWN bills!!") {
		t.Fatal("a nickname mention (case-insensitive) must match")
	}
	if !TeamMentioned("KC", "Kansas City punches it in") {
		t.Fatal("a city mention must match")
	}
	if TeamMentioned("BUF", "Dolphins score on a big play") {
		t.Fatal("an unrelated team's mention must not match")
	}
	if TeamMentioned("ZZZ", "anything at all") {
		t.Fatal("an unrecognized abbreviation must never match")
	}
}

package help

import (
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

func TestHelpNamesEveryoneInFormatWithoutByesOrToiletBowl(t *testing.T) {
	cfg := league.PlayoffConfig{
		TeamCount: 8, StartWeek: 15, RoundLengthWeeks: 1, Qualification: "top-record",
		TiebreakOrder: []string{"record", "head-to-head", "points-for", "pickem", "seeded-draw"},
		Reseed:        true, Consolation: true,
	}
	note := postseasonHelpNote(cfg)
	for _, want := range []string{"All 8 teams", "head-to-head", "Pick'em", "Week 15 has quarterfinals", "Week 16 has reseeded championship semifinals and losers-bracket semifinals", "Week 17 has the championship and losers-bracket final", "no byes or toilet bowl"} {
		if !strings.Contains(note, want) {
			t.Errorf("help note %q lacks %q", note, want)
		}
	}
}

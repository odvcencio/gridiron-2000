package matchups

import "testing"

func TestWheelFinalResults(t *testing.T) {
	for _, tc := range []struct{ in, first, second string }{{"WON", "WON", "LOST"}, {"LOST", "LOST", "WON"}, {"TIED", "TIED", "TIED"}} {
		p := StarterProgressData{Teams: []StarterProgressTeamData{{TeamID: "a"}, {TeamID: "b"}}}
		got := starterProgressWinChances(p, tc.in, false)
		if got.Teams[0].WinChance != tc.first || got.Teams[1].WinChance != tc.second || got.Teams[0].WinCaption != "Final" {
			t.Fatalf("%+v", got)
		}
	}
}

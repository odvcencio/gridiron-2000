package matchups

import "testing"

func TestStarterProgressWinChances(t *testing.T) {
	for _, tc := range []struct {
		chance      string
		second      bool
		left, right string
	}{
		{"14%", false, "14%", "86%"}, {"14%", true, "86%", "14%"},
		{"50%", false, "50%", "50%"}, {"100%", false, "100%", "0%"},
		{"—", false, "", ""}, {"", false, "", ""}, {"101%", false, "", ""},
	} {
		p := StarterProgressData{Teams: []StarterProgressTeamData{{TeamID: "left"}, {TeamID: "right"}}}
		got := starterProgressWinChances(p, tc.chance, tc.second)
		if got.Teams[0].WinChance != tc.left || got.Teams[1].WinChance != tc.right {
			t.Fatalf("%q second=%v: %+v", tc.chance, tc.second, got.Teams)
		}
		if tc.left == "" && (got.Teams[0].WinCaption != "" || got.Teams[1].WinCaption != "") {
			t.Fatalf("%q rendered an unavailable-estimate caption: %+v", tc.chance, got.Teams)
		}
		if got.Teams[0].TeamID != "left" || got.Teams[1].TeamID != "right" {
			t.Fatal("probability assignment reordered the teams")
		}
	}
}

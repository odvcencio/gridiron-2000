package pickem

import (
	"net/http/httptest"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

func TestPickemRendersScoreClockAndEveryPickStanding(t *testing.T) {
	for _, tc := range []struct {
		name, state, score, standing, label string
		final                               bool
	}{
		{"scheduled", "SCHEDULED", "", "", "", false},
		{"winning", "Q2 8:12", "10-14", "winning", "WINNING ATS", false},
		{"losing", "Q3 2:08", "14-10", "losing", "LOSING ATS", false},
		{"tied", "Q1 15:00", "0-0", "tied", "TIED ATS", false},
		{"overtime", "OT 4:01", "17-17", "tied", "TIED ATS", false},
		{"waiting for freeze", "Q1 14:30", "0-0", "pending", "WAITING FOR LINE", false},
		{"missing live", "AWAITING LIVE SCORE", "", "", "", false},
		{"final", "FINAL", "17-24", "", "", true},
		{"final missing scores", "FINAL", "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := league.PickemGameRow{ID: "game", Week: 3, Away: "BUF", Home: "MIA", Label: "BUF @ MIA",
				KickoffDisplay: "Sun Sep 27 · 1:00 PM EDT", GameState: tc.state, ScoreDisplay: tc.score,
				HasScores: tc.score != "", Final: tc.final, Locked: tc.state != "SCHEDULED", Picked: true,
				LivePickState: tc.standing, LivePickLabel: tc.label, ResultLabel: "LOCKED · IN PROGRESS"}
			if tc.final {
				row.ResultLabel = "MIA COVERED"
			}
			row.LeaguePicks = []league.PickemGamePickView{{Name: "Home picker", PickLabel: "MIA", HasPick: true,
				Outcome: "pending", StateLabel: "PENDING", LivePickState: tc.standing, LivePickLabel: tc.label}}
			row.HasLeaguePicks = true
			request := httptest.NewRequest("GET", "/pickem?week=3", nil)
			data := preparePickemData(map[string]any{"week": 3, "can_pick": true, "games": []league.PickemGameRow{row}}, request, "")
			html, err := pickemFragmentRender(data, request)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Join(strings.Fields(html), " ")
			for _, want := range []string{tc.state, row.KickoffDisplay, "BUF @ MIA"} {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %q in row: %s", want, body)
				}
			}
			if tc.score == "" && strings.Contains(body, "data-game-score") {
				t.Fatal("missing scores rendered as zero")
			}
			if tc.score != "" && !strings.Contains(body, "BUF "+tc.score+" MIA") {
				t.Fatalf("missing team-labeled score: %s", body)
			}
			if tc.label != "" && strings.Count(body, tc.label) != 2 {
				t.Fatalf("viewer and ledger must show %q: %s", tc.label, body)
			}
			if !row.Locked && strings.Contains(body, "Home picker") {
				t.Fatal("scheduled game leaked league picks")
			}
		})
	}
}

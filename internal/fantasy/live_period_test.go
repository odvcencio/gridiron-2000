package fantasy

import (
	"encoding/json"
	"testing"
)

func TestLiveFeedOrdinalPeriodsNormalizeAtBothIngestionPaths(t *testing.T) {
	for input, want := range map[string]string{
		"1st": "Q1", "2nd": "Q2", "3rd": "Q3", "4th": "Q4",
		"Halftime": "HALF", "Overtime": "OT", "2": "Q2", "Delayed": "Delayed",
	} {
		t.Run(input, func(t *testing.T) {
			body := map[string]any{
				"gameID": "20260913_AWY@HOM", "away": "AWY", "home": "HOM",
				"gameStatusCode": "1", "currentPeriod": input, "gameClock": "12:04",
				"lineScore": map[string]any{"period": input, "gameClock": "12:04"},
			}
			boxRaw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			box := ParseBoxScore(boxRaw)
			if box.Period != want || box.Clock != "12:04" || !box.InProgress || box.Final {
				t.Fatalf("box period=%q clock=%q inProgress=%v final=%v", box.Period, box.Clock, box.InProgress, box.Final)
			}
			if box.Raw["currentPeriod"] != input {
				t.Fatal("provider raw period was modified")
			}
			scoresRaw, err := json.Marshal(map[string]any{"20260913_AWY@HOM": body})
			if err != nil {
				t.Fatal(err)
			}
			games := ParseScoresOnly(scoresRaw)
			if len(games) != 1 {
				t.Fatalf("scoreboard games=%d, want 1", len(games))
			}
			game := games[0]
			if game.Period != want || game.Clock != "12:04" || !game.InProgress || game.Final {
				t.Fatalf("scoreboard period=%q clock=%q inProgress=%v final=%v", game.Period, game.Clock, game.InProgress, game.Final)
			}
			if game.Raw["lineScore"].(map[string]any)["period"] != input {
				t.Fatal("provider raw scoreboard period was modified")
			}
		})
	}
}

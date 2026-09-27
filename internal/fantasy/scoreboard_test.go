package fantasy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadScoreboardFixture(t *testing.T, name string) []ScoreboardGame {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return ParseScoresOnly(raw)
}

func scoreboardGameByID(t *testing.T, games []ScoreboardGame, id string) ScoreboardGame {
	t.Helper()
	for _, game := range games {
		if game.GameID == id {
			return game
		}
	}
	t.Fatalf("game %s not in %d parsed games", id, len(games))
	return ScoreboardGame{}
}

// testdata/scoresonly-20250907.json is a real getNFLScoresOnly capture
// (gameDate=20250907, fetched live 2026-08-31): a full 13-game final
// Sunday. Every game on it is Completed, so it grounds the final-day
// shape; the in-progress shape is the -sample fixture below.
func TestParseScoresOnlyFinalDayCapture(t *testing.T) {
	games := loadScoreboardFixture(t, "scoresonly-20250907.json")
	if len(games) != 13 {
		t.Fatalf("got %d games, want 13", len(games))
	}
	game := scoreboardGameByID(t, games, "20250907_CAR@JAX")
	if game.Away != "CAR" || game.Home != "JAX" {
		t.Fatalf("teams = %q @ %q", game.Away, game.Home)
	}
	if game.AwayPoints != 10 || game.HomePoints != 26 {
		t.Fatalf("points = %v-%v", game.AwayPoints, game.HomePoints)
	}
	if !game.Final || game.InProgress || !game.ScoresPresent || game.StatusCode != "2" || game.Period != "Final" || game.Clock != "" {
		t.Fatalf("status = final %v inProgress %v scoresPresent %v code %q period %q clock %q",
			game.Final, game.InProgress, game.ScoresPresent, game.StatusCode, game.Period, game.Clock)
	}
	if game.Raw == nil {
		t.Fatal("Raw must carry the decoded entry for the possession seam")
	}
	if _, ok := game.Raw["lineScore"].(map[string]any); !ok {
		t.Fatal("Raw must retain the lineScore object for the possession seam")
	}
}

// The in-progress sample is synthetic (no live capture exists yet — the
// 2026-09-10 TNF capture is the pinning event) but follows the real
// capture's exact key set, with the live values the final capture cannot
// show: gameStatusCode "1", a running clock, a lineScore period, and one
// side's currentlyInPossession reading "True".
func TestParseScoresOnlyInProgressSample(t *testing.T) {
	games := loadScoreboardFixture(t, "scoresonly-inprogress-sample.json")
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	game := games[0]
	if game.GameID != "20260913_AWY@HOM" || game.Away != "AWY" || game.Home != "HOM" {
		t.Fatalf("identity = %+v", game)
	}
	if game.Final || !game.InProgress || game.StatusCode != "1" {
		t.Fatalf("status = final %v inProgress %v code %q", game.Final, game.InProgress, game.StatusCode)
	}
	if game.Period != "Q3" || game.Clock != "8:12" {
		t.Fatalf("period %q clock %q", game.Period, game.Clock)
	}
	if game.AwayPoints != 10 || game.HomePoints != 3 {
		t.Fatalf("points = %v-%v", game.AwayPoints, game.HomePoints)
	}
}

func TestParseScoresOnlyMalformed(t *testing.T) {
	if games := ParseScoresOnly([]byte("not json")); len(games) != 0 {
		t.Fatalf("malformed body parsed to %d games", len(games))
	}
	if games := ParseScoresOnly([]byte(`{"statusCode":200,"body":"nope"}`)); len(games) != 0 {
		t.Fatalf("non-object body parsed to %d games", len(games))
	}
}

func TestParseScoresOnlyTracksCompleteUsableScores(t *testing.T) {
	for _, tc := range []struct {
		name string
		away string
		home string
		want bool
	}{
		{name: "explicit zero zero is a score", away: `"0"`, home: `"0"`, want: true},
		{name: "missing home score", away: `"7"`, home: "missing", want: false},
		{name: "blank score", away: `"7"`, home: `" "`, want: false},
		{name: "non numeric score", away: `"seven"`, home: `"7"`, want: false},
		{name: "fractional score", away: `"7.5"`, home: `"7"`, want: false},
		{name: "negative score", away: `"-1"`, home: `"7"`, want: false},
		{name: "non finite score", away: `"NaN"`, home: `"7"`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := `{"gameID":"g1","away":"BUF","home":"MIA","gameStatus":"Final","gameStatusCode":"2"}`
			entry = strings.TrimSuffix(entry, "}")
			entry += `,"awayPts":` + tc.away
			if tc.home != "missing" {
				entry += `,"homePts":` + tc.home
			}
			entry += `}`
			games := ParseScoresOnly([]byte(`{"g1":` + entry + `}`))
			if len(games) != 1 || games[0].ScoresPresent != tc.want {
				t.Fatalf("parsed scores = %+v, want ScoresPresent=%v", games, tc.want)
			}
		})
	}
}

func TestParseBoxScoreTracksCompleteUsableScores(t *testing.T) {
	zeroZero := ParseBoxScore([]byte(`{"gameID":"g1","away":"BUF","home":"MIA","awayPts":"0","homePts":"0","gameStatus":"Final","gameStatusCode":"2"}`))
	if !zeroZero.ScoresPresent || zeroZero.AwayPoints != 0 || zeroZero.HomePoints != 0 {
		t.Fatalf("explicit 0-0 box scores = %+v, want a present 0-0 result", zeroZero)
	}
	missing := ParseBoxScore([]byte(`{"gameID":"g1","away":"BUF","home":"MIA","awayPts":"0","homePts":""}`))
	if missing.ScoresPresent {
		t.Fatalf("incomplete box scores reported present: %+v", missing)
	}
}

// FetchScoresOnly must hit /getNFLScoresOnly with the gameDate query and
// parse the enveloped reply — the same relay-shaped path FetchBoxScore
// and FetchGamesForWeek already use.
func TestFetchScoresOnlyQueryWiring(t *testing.T) {
	var gotPath, gotDate string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotDate = r.URL.Query().Get("gameDate")
		raw, err := os.ReadFile(filepath.Join("testdata", "scoresonly-inprogress-sample.json"))
		if err != nil {
			t.Error(err)
		}
		w.Write(raw)
	}))
	defer server.Close()
	client, err := NewBoxScoreClient(server.URL, 2026, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	games, err := client.FetchScoresOnly(context.Background(), "20260913")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/getNFLScoresOnly" || gotDate != "20260913" {
		t.Fatalf("request = %s?gameDate=%s", gotPath, gotDate)
	}
	if len(games) != 1 || games[0].GameID != "20260913_AWY@HOM" {
		t.Fatalf("games = %+v", games)
	}
}

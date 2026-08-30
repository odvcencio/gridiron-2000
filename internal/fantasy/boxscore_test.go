package fantasy

import (
	"os"
	"path/filepath"
	"testing"
)

func loadBoxFixture(t *testing.T, name string) BoxScore {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return ParseBoxScore(raw)
}

func TestParseBoxScoreFinalGameFields(t *testing.T) {
	box := loadBoxFixture(t, "box-20250904_DAL-PHI.json")
	if box.GameID != "20250904_DAL@PHI" || box.Away != "DAL" || box.Home != "PHI" {
		t.Fatalf("identity = %+v", box)
	}
	if !box.Final || box.StatusCode != "2" || box.Period != "Final" || box.Clock != "" || box.InProgress {
		t.Fatalf("status = final %v code %q period %q clock %q inProgress %v", box.Final, box.StatusCode, box.Period, box.Clock, box.InProgress)
	}
	if box.AwayPoints != 20 || box.HomePoints != 24 {
		t.Fatalf("points = %v-%v", box.AwayPoints, box.HomePoints)
	}
	hurts := box.Players["4040715"]
	if hurts.Name != "Jalen Hurts" || hurts.Team != "PHI" || hurts.Stats["rushTD"] != 2 || hurts.Stats["passYds"] != 152 {
		t.Fatalf("Hurts = %+v", hurts)
	}
	if _, ok := box.Players["4686911"]; ok {
		t.Fatal("a defense-only row must stay out of Players")
	}
}

func TestParseBoxScoreDSTBranch(t *testing.T) {
	box := loadBoxFixture(t, "box-20250904_DAL-PHI.json")
	dal := box.DST["DAL"]
	if dal["sacks"] != 1 || dal["ptsAllowed"] != 24 {
		t.Fatalf("DAL D/ST = %v", dal)
	}
	if value, ok := dal["fumblesRecovered"]; !ok || value != 0 {
		t.Fatalf("DAL fumblesRecovered = %v (present=%v), want an explicit 0", value, ok)
	}
	phi := box.DST["PHI"]
	if phi["fumblesRecovered"] != 1 || phi["ptsAllowed"] != 20 {
		t.Fatalf("PHI D/ST = %v", phi)
	}
	if value, ok := phi["safeties"]; !ok || value != 0 {
		t.Fatalf("PHI safeties = %v (present=%v), want an explicit 0", value, ok)
	}
}

func TestParseBoxScoreInProgressKeepsZeroPointsAllowed(t *testing.T) {
	box := loadBoxFixture(t, "box-inprogress-sample.json")
	if box.Final || !box.InProgress || box.Period != "Q3" || box.Clock != "8:12" || box.StatusCode != "1" {
		t.Fatalf("in-progress status = %+v", box)
	}
	if allowed, ok := box.DST["AWY"]["ptsAllowed"]; !ok || allowed != 0 {
		t.Fatalf("a live zero points-allowed must survive as an explicit 0: %v %v", allowed, ok)
	}
	if box.DST["AWY"]["sacks"] != 2 || box.DST["HOM"]["ptsAllowed"] != 10 || box.Players["1"].Team != "AWY" {
		t.Fatalf("sample = %+v", box)
	}
}

// TestParseBoxScoreDSTPointsAllowedFallback pins ptsAllowed's fallback to
// the opponent's score: it must fire on a genuinely missing value (absent
// key, blank string, JSON null) and must not fire on a value that parses,
// including an explicit "0" (covered separately by
// TestParseBoxScoreInProgressKeepsZeroPointsAllowed). A blank or null
// ptsAllowed shows up on an early live frame before Tank01 has populated
// it; reading that as a parsed 0 would fake a shutout the game has not
// produced.
func TestParseBoxScoreDSTPointsAllowedFallback(t *testing.T) {
	cases := map[string]string{
		"absent key":   `{"awayPts":"10","homePts":"24","DST":{"away":{"teamAbv":"AWY"},"home":{"teamAbv":"HOM"}}}`,
		"empty string": `{"awayPts":"10","homePts":"24","DST":{"away":{"teamAbv":"AWY","ptsAllowed":""},"home":{"teamAbv":"HOM","ptsAllowed":""}}}`,
		"json null":    `{"awayPts":"10","homePts":"24","DST":{"away":{"teamAbv":"AWY","ptsAllowed":null},"home":{"teamAbv":"HOM","ptsAllowed":null}}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			box := parseBoxScore([]byte(raw))
			if box.DST["AWY"]["ptsAllowed"] != 24 {
				t.Fatalf("AWY ptsAllowed = %v, want the home score (24) as fallback", box.DST["AWY"]["ptsAllowed"])
			}
			if box.DST["HOM"]["ptsAllowed"] != 10 {
				t.Fatalf("HOM ptsAllowed = %v, want the away score (10) as fallback", box.DST["HOM"]["ptsAllowed"])
			}
		})
	}
}

func TestParseBoxScoreStatusCodeRule(t *testing.T) {
	for raw, want := range map[string]struct{ final, inProgress bool }{
		`{"gameStatusCode":"2","currentPeriod":"Final"}`:                 {true, false},
		`{"gameStatusCode":"1","currentPeriod":"Q1"}`:                    {false, true},
		`{"gameStatusCode":"0","currentPeriod":""}`:                      {false, false},
		`{"gameStatusCode":"","currentPeriod":""}`:                       {false, false},
		`{"gameStatusCode":"7","currentPeriod":"Q4","gameClock":"1:00"}`: {false, true},
	} {
		box := parseBoxScore([]byte(raw))
		if box.Final != want.final || box.InProgress != want.inProgress {
			t.Errorf("%s: final=%v inProgress=%v want %+v", raw, box.Final, box.InProgress, want)
		}
	}
}

func TestPreseasonWrapperStillMatchesOldShape(t *testing.T) {
	stats, final := parsePreseasonBoxScore(loadPreseasonFixture(t))
	if !final || stats["4038524"]["passYds"] != 101 {
		t.Fatalf("wrapper drifted: final=%v minshew=%v", final, stats["4038524"])
	}
}

func TestParseGamesForWeekKeepsDateAndStatusCode(t *testing.T) {
	raw := []byte(`{"statusCode":200,"body":[{"gameID":"20250907_HOU@LAR","gameWeek":"Week 1","away":"HOU","home":"LAR","gameDate":"20250907","gameTime_epoch":"1757276700.0","gameStatus":"Final","gameStatusCode":"2"}]}`)
	games := parsePreseasonWeek(unwrapEnvelope(raw))
	if len(games) != 1 || games[0].Date != "20250907" || games[0].StatusCode != "2" || games[0].Home != "LAR" {
		t.Fatalf("listing = %+v", games)
	}
}

func TestNewBoxScoreClientDefaultsAndValidation(t *testing.T) {
	if _, err := NewBoxScoreClient("", 2025, nil, 0); err == nil {
		t.Fatal("an empty baseURL must return an error, not a client that can never succeed")
	}
	client, err := NewBoxScoreClient("https://relay.example.test", 2025, nil, 0)
	if err != nil {
		t.Fatalf("NewBoxScoreClient: %v", err)
	}
	if client.client.client == nil {
		t.Fatal("a nil httpClient must default to a usable client")
	}
	if client.client.maxBody != defaultBoxScoreMaxBody {
		t.Fatalf("maxBody = %d, want the default %d when maxBodyBytes <= 0", client.client.maxBody, defaultBoxScoreMaxBody)
	}
	sized, err := NewBoxScoreClient("https://relay.example.test", 2025, nil, 8<<20)
	if err != nil {
		t.Fatalf("NewBoxScoreClient: %v", err)
	}
	if sized.client.maxBody != 8<<20 {
		t.Fatalf("maxBody = %d, want the passed 8 MiB limit", sized.client.maxBody)
	}
}

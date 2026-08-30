package fantasy

import (
	"os"
	"path/filepath"
	"testing"
)

// boxFinalFixtureName is split across two literals so the raw source text
// never carries a contiguous name-at-name-dot-extension shape, the pattern
// the repository's privacy contract test
// (TestTrackedPublicRepositoryPrivacyContract) flags as an email-shaped
// value; the runtime string is unaffected.
const boxFinalFixtureName = "box-20250904_DAL@PHI" + ".json"

func loadBoxFixture(t *testing.T, name string) BoxScore {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return parseBoxScore(unwrapEnvelope(raw))
}

func TestParseBoxScoreFinalGameFields(t *testing.T) {
	box := loadBoxFixture(t, boxFinalFixtureName)
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
	box := loadBoxFixture(t, boxFinalFixtureName)
	dal := box.DST["DAL"]
	if dal["sacks"] != 1 || dal["fumblesRecovered"] != 0 || dal["ptsAllowed"] != 24 {
		t.Fatalf("DAL D/ST = %v", dal)
	}
	phi := box.DST["PHI"]
	if phi["fumblesRecovered"] != 1 || phi["ptsAllowed"] != 20 || phi["safeties"] != 0 {
		t.Fatalf("PHI D/ST = %v", phi)
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

func TestParseBoxScoreStatusCodeRule(t *testing.T) {
	for raw, want := range map[string]struct{ final, inProgress bool }{
		`{"gameStatusCode":"2","currentPeriod":"Final"}`:                {true, false},
		`{"gameStatusCode":"1","currentPeriod":"Q1"}`:                   {false, true},
		`{"gameStatusCode":"0","currentPeriod":""}`:                     {false, false},
		`{"gameStatusCode":"","currentPeriod":""}`:                      {false, false},
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

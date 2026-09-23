package fantasy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBoxScoreParsesLiveTwoPointPuntsAndDefense(t *testing.T) {
	const raw = `{"statusCode":200,"body":{
  "gameID":"20260920_NO@BAL","away":"NO","home":"BAL","gameStatusCode":"2","gameStatus":"Completed",
  "awayPts":"24","homePts":"17",
  "playerStats":{
    "etienne":{"longName":"Travis Etienne Jr.","teamAbv":"NO","Rushing":{"rushYds":"25","rushingTwoPointConversion":"1"}},
    "wright":{"longName":"Ryan Wright","teamAbv":"NO","Punting":{"punts":"2","puntYds":"87","puntLong":"47","puntsin20":"1","puntTouchBacks":"0"}},
    "defender":{"longName":"Danny Stutsman","teamAbv":"NO","Defense":{"forcedFumbles":"1"}},
    "kicker":{"longName":"Tyler Loop","teamAbv":"BAL","Kicking":{"fgMissed":"1"}}
  },
  "DST":{
    "away":{"teamAbv":"NO","defTD":"0","sacks":"3","defensiveInterceptions":"1","fumblesRecovered":"0","safeties":"0","ydsAllowed":"312","ptsAllowed":"17"},
    "home":{"teamAbv":"BAL","defTD":"0","sacks":"3","defensiveInterceptions":"0","fumblesRecovered":"0","safeties":"0","ydsAllowed":"298","ptsAllowed":"24"}
  },
  "teamStats":{
    "away":{"teamID":"23","teamAbv":"NO","blockedPunt":"0","blockedFG":"1","blockedXP":"0","defensiveTwoPointConversionReturns":"0","defensiveOrSpecialTeamsTds":"0"},
    "home":{"teamID":"3","teamAbv":"BAL","blockedPunt":"0","blockedFG":"0","blockedXP":"0","defensiveTwoPointConversionReturns":"0","defensiveOrSpecialTeamsTds":"0"}
  },
  "allPlayByPlay":[
    {"play":"R.Wright punts 47 yards to BLT 24, fair catch by B.Brown.","playerStats":{"wright":{"Kicking":{"punts":"1","puntYds":"47"}}}},
    {"play":"R.Wright punts 40 yards to BLT 4, downed by NO-J.Price.","playerStats":{"wright":{"Kicking":{"punts":"1","puntYds":"40"}}}},
    {"play":"C.Moore FUMBLES (D.Stutsman), RECOVERED by NO-J.Sanker. The play was REVERSED. C.Moore was down by contact.","teamID":"3","playerStats":{}}
  ]
}}`
	box := ParseBoxScore([]byte(raw))
	if !box.Final || !box.ScoringComplete {
		t.Fatalf("complete final box not recognized: final=%v complete=%v", box.Final, box.ScoringComplete)
	}
	if got := box.Players["etienne"].Stats["twoPt"]; got != 1 {
		t.Fatalf("live two-point conversion = %v, want 1", got)
	}
	if got := box.Players["kicker"].Stats["fgMissed"]; got != 1 {
		t.Fatalf("blocked or missed field-goal attempt = %v, want 1", got)
	}
	punter := box.Players["wright"].Stats
	if punter["puntYards"] != 87 || punter["puntLong50"] != 0 || punter["puntDownedInside5"] != 1 || punter["puntIn20"] != 1 {
		t.Fatalf("per-punt live scoring = %+v", punter)
	}
	if got := box.DST["NO"]; got["blockedKicks"] != 1 || got["forcedFumbles"] != 0 {
		t.Fatalf("D/ST live stats ignored blocked kick or overturned fumble: %+v", got)
	}
}

func TestPuntDistanceUsesCorrectedSpot(t *testing.T) {
	tests := []struct {
		play string
		raw  float64
		want float64
	}{
		{"K.Kroeger punts 47 yards to CIN 40, impetus ends at CIN 48.", 47, 39},
		{"D.Whelan punts 60 yards to NYJ 8. I.Williams MUFFS catch, recovered by NYJ-Q.Stiggers at NYJ 2.", 60, 66},
		{"L.Cooke punts 53 yards to DEN 15. K.Abrams-Draine MUFFS catch, and recovers at DEN 13.", 53, 55},
		{"A.McNamara punts 41 yards to GB 16. S.Moore MUFFS catch, and recovers at GB 15.", 41, 42},
	}
	for _, test := range tests {
		got, _ := puntDistanceAndLanding(test.play, test.raw)
		if got != test.want {
			t.Errorf("%q: distance = %v, want %v", test.play, got, test.want)
		}
	}
}

func TestLiveClientRetriesIncompleteFinalPlayList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("playByPlay") != "true" {
			t.Errorf("box request did not ask for play-by-play: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"statusCode":200,"body":{"gameID":"game","gameStatusCode":"2"}}`))
	}))
	defer server.Close()
	client, err := NewBoxScoreClient(server.URL, 2026, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	client.requireFinalPBP = true
	if _, err := client.FetchBoxScore(context.Background(), "game"); err == nil || !strings.Contains(err.Error(), "missing or inconsistent scoring details") {
		t.Fatalf("incomplete final box accepted: %v", err)
	}
}

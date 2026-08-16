package fantasy

import (
	"encoding/json"
	"testing"
)

func TestUnwrapEnvelope(t *testing.T) {
	wrapped := []byte(`{"statusCode":200,"body":[{"playerID":"1"}]}`)
	if got := string(unwrapEnvelope(wrapped)); got != `[{"playerID":"1"}]` {
		t.Fatalf("unwrap wrapped = %s", got)
	}
	bare := []byte(`[{"playerID":"1"}]`)
	if got := string(unwrapEnvelope(bare)); got != string(bare) {
		t.Fatalf("unwrap bare = %s", got)
	}
}

func TestParsePlayerListFiltersAndNormalizes(t *testing.T) {
	raw := json.RawMessage(`[
		{"playerID":"100","longName":"Test Receiver","pos":"WR","team":"cin",
		 "espnHeadshot":"https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png"},
		{"playerID":"101","longName":"Test Kicker","pos":"PK","team":"DAL"},
		{"playerID":"102","longName":"Test Lineman","pos":"OT","team":"DAL"},
		{"playerID":"","longName":"No ID","pos":"WR","team":"DAL"},
		{"playerID":"103","espnName":"Fallback Name","pos":"QB","team":"BUF",
		 "injury":{"designation":"Questionable"},
		 "espnHeadshot":"http://insecure.example.com/4429795.png"}
	]`)
	players := parsePlayerList(raw)
	if len(players) != 3 {
		t.Fatalf("expected 3 players, got %d", len(players))
	}
	if players["100"].NFLTeam != "CIN" {
		t.Errorf("team not upper-cased: %q", players["100"].NFLTeam)
	}
	if players["100"].Headshot != "https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png" {
		t.Errorf("headshot not captured: %q", players["100"].Headshot)
	}
	if players["101"].Position != "K" {
		t.Errorf("PK not normalized to K: %q", players["101"].Position)
	}
	if players["101"].Headshot != "" {
		t.Errorf("player with no espnHeadshot should have empty Headshot: %q", players["101"].Headshot)
	}
	if players["103"].Name != "Fallback Name" {
		t.Errorf("espnName fallback failed: %q", players["103"].Name)
	}
	if players["103"].Injury != "Questionable" {
		t.Errorf("injury designation lost: %q", players["103"].Injury)
	}
	if players["103"].Headshot != "" {
		t.Errorf("non-https headshot should be dropped: %q", players["103"].Headshot)
	}
}

func TestParseADPHandlesWrapperAndStringNumbers(t *testing.T) {
	wrapped := json.RawMessage(`{"adpList":[
		{"playerID":"2","adp":"3.5"},
		{"playerID":"1","overallADP":1.2,"longName":"Synth Player","pos":"WR","team":"kc"},
		{"playerID":"3","adp":"not-a-number"},
		{"playerID":"","adp":"9"}
	]}`)
	entries := parseADP(wrapped)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].PlayerID != "1" || entries[1].PlayerID != "2" {
		t.Errorf("entries not sorted by ADP: %+v", entries)
	}
	if entries[0].Team != "KC" {
		t.Errorf("team not upper-cased: %q", entries[0].Team)
	}
	bare := json.RawMessage(`[{"playerID":"9","adp":12}]`)
	if entries := parseADP(bare); len(entries) != 1 {
		t.Fatalf("bare list parse failed: %+v", entries)
	}
}

// Shapes below mirror the live feed observed on 2026-08-16: DST rows carry
// posADP + teamAbv and no playerID; news rows carry only link + title.
func TestParseADPLiveShapeSynthesizesDST(t *testing.T) {
	raw := json.RawMessage(`{"adpList":[
		{"posADP":"DST1","overallADP":"154.3","teamAbv":"HOU","teamID":"13","longName":"Houston Texans DST"},
		{"posADP":"K1","overallADP":"184.6","playerID":"3953687","longName":"Brandon Aubrey"},
		{"posADP":"WR9","overallADP":"12.1","playerID":"77","longName":"Some Receiver"}
	]}`)
	entries := parseADP(raw)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3 (%+v)", len(entries), entries)
	}
	dst := entries[1]
	if dst.PlayerID != "DST-HOU" || dst.Position != "DST" || dst.Team != "HOU" || dst.Name != "Houston Texans DST" {
		t.Errorf("DST entry wrong: %+v", dst)
	}
	if entries[2].Position != "K" || entries[2].PlayerID != "3953687" {
		t.Errorf("kicker posADP prefix not derived: %+v", entries[2])
	}
}

func TestParseNewsFallsBackToTitleName(t *testing.T) {
	raw := json.RawMessage(`[
		{"link":"https://example.com/1","title":"Keon Coleman: appeared to suffer a lower body injury"},
		{"link":"https://example.com/2","title":"No colon headline"},
		{"playerID":"55","title":"By ID: still keyed on the ID"}
	]`)
	news := parseNews(raw)
	if news["Keon Coleman"] == "" {
		t.Errorf("name-prefix key missing: %v", news)
	}
	if news["55"] == "" {
		t.Errorf("playerID key missing: %v", news)
	}
}

func TestMergePoolMatchesNewsByName(t *testing.T) {
	base := map[string]Player{"1": {ID: "1", Name: "Keon Coleman", Position: "WR", NFLTeam: "BUF"}}
	adp := []adpEntry{{PlayerID: "1", ADP: 30}}
	news := map[string]string{"Keon Coleman": "Keon Coleman: limited in practice"}
	pool := mergePool(base, adp, nil, news, nil, 10)
	if pool[0].News != "Keon Coleman: limited in practice" {
		t.Errorf("news by name not applied: %+v", pool[0])
	}
}

func TestParseProjectionsMapAndScoring(t *testing.T) {
	raw := json.RawMessage(`{"playerProjections":{
		"1":{"fantasyPointsDefault":{"standard":"10.0","PPR":"14.0","halfPPR":"12.0"}},
		"2":{"fantasyPoints":"8.5"},
		"3":{"fantasyPoints":"0"}
	}}`)
	points := parseProjections(raw, "half_ppr")
	if points["1"] != 12.0 {
		t.Errorf("halfPPR projection = %v", points["1"])
	}
	if points["2"] != 8.5 {
		t.Errorf("fantasyPoints fallback = %v", points["2"])
	}
	if _, ok := points["3"]; ok {
		t.Errorf("zero projection should be dropped")
	}
	if points := parseProjections(raw, "ppr"); points["1"] != 14.0 {
		t.Errorf("PPR projection = %v", points["1"])
	}
}

func TestParseProjectionsList(t *testing.T) {
	raw := json.RawMessage(`[{"playerID":"7","fantasyPoints":11.25}]`)
	points := parseProjections(raw, "half_ppr")
	if points["7"] != 11.25 {
		t.Errorf("list projections = %v", points)
	}
}

func TestParseTeamByes(t *testing.T) {
	raw := json.RawMessage(`[
		{"teamAbv":"cin","byeWeeks":{"2026":["10"]}},
		{"teamAbv":"DAL","byeWeeks":{"2025":[7]}}
	]`)
	byes := parseTeamByes(raw, 2026)
	if byes["CIN"] != 10 {
		t.Errorf("CIN bye = %d", byes["CIN"])
	}
	if byes["DAL"] != 7 {
		t.Errorf("DAL bye fallback to any season = %d", byes["DAL"])
	}
}

func TestMergePoolOrderingAndSynthesis(t *testing.T) {
	base := map[string]Player{
		"1": {ID: "1", Name: "Ranked One", Position: "WR", NFLTeam: "CIN"},
		"2": {ID: "2", Name: "Ranked Two", Position: "RB", NFLTeam: "DET"},
		"4": {ID: "4", Name: "Unranked High", Position: "QB", NFLTeam: "BUF"},
		"5": {ID: "5", Name: "Unranked Low", Position: "TE", NFLTeam: "LV"},
	}
	adp := []adpEntry{
		{PlayerID: "1", ADP: 1.5},
		{PlayerID: "3", ADP: 2.5, Name: "Synth DST", Position: "DST", Team: "BAL"},
		{PlayerID: "2", ADP: 4.0},
	}
	projections := map[string]float64{"4": 20, "5": 9, "1": 18}
	byes := map[string]int{"CIN": 10}
	pool := mergePool(base, adp, projections, map[string]string{"1": "Big news"}, byes, 10)
	if len(pool) != 5 {
		t.Fatalf("pool size = %d", len(pool))
	}
	wantOrder := []string{"1", "3", "2", "4", "5"}
	for i, id := range wantOrder {
		if pool[i].ID != id {
			t.Fatalf("pool[%d] = %s, want %s (%+v)", i, pool[i].ID, id, pool)
		}
	}
	if pool[0].ADPRank != 1 || pool[2].ADPRank != 3 {
		t.Errorf("ADP ranks wrong: %+v", pool[:3])
	}
	if pool[3].ADPRank != 0 {
		t.Errorf("unranked player has ADP rank: %+v", pool[3])
	}
	if pool[0].ByeWeek != 10 || pool[0].News != "Big news" || pool[0].Projection != 18 {
		t.Errorf("merge fields wrong: %+v", pool[0])
	}
	if pool[1].Name != "Synth DST" || pool[1].Position != "DST" {
		t.Errorf("synthesized entry wrong: %+v", pool[1])
	}
	capped := mergePool(base, adp, projections, nil, nil, 2)
	if len(capped) != 2 {
		t.Errorf("pool cap ignored: %d", len(capped))
	}
}

func TestOfflinePoolIsDraftable(t *testing.T) {
	pool := OfflinePool()
	if len(pool) < 130 {
		t.Fatalf("offline pool too small for a 120-pick draft: %d", len(pool))
	}
	seen := map[string]bool{}
	positions := map[string]int{}
	for _, player := range pool {
		if player.ID == "" || player.Name == "" {
			t.Fatalf("player missing identity: %+v", player)
		}
		if seen[player.ID] {
			t.Fatalf("duplicate player id %q", player.ID)
		}
		seen[player.ID] = true
		positions[player.Position]++
	}
	for _, position := range []string{"QB", "RB", "WR", "TE", "K", "DST"} {
		if positions[position] < 8 {
			t.Errorf("position %s has only %d players; eight teams need more", position, positions[position])
		}
	}
}

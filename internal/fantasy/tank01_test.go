package fantasy

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		{"playerID":"100","longName":"Test Receiver","pos":"WR","team":"cin","jerseyNum":"18",
		 "espnHeadshot":"https://a.espncdn.com/i/headshots/nfl/players/full/4429795.png","exp":"R"},
		{"playerID":"101","longName":"Test Kicker","pos":"PK","team":"DAL","jerseyNum":"0"},
		{"playerID":"102","longName":"Test Lineman","pos":"OT","team":"DAL"},
		{"playerID":"","longName":"No ID","pos":"WR","team":"DAL"},
		{"playerID":"103","espnName":"Fallback Name","pos":"QB","team":"BUF",
		 "injury":{"designation":"Questionable"},
		 "espnHeadshot":"http://insecure.example.com/4429795.png","exp":"9"}
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
	if players["100"].Jersey != "18" {
		t.Errorf("jersey number not captured: %q", players["100"].Jersey)
	}
	if players["101"].Jersey != "" {
		t.Errorf("jerseyNum \"0\" should mean unknown, not be stored: %q", players["101"].Jersey)
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
	// Tank01's raw player list carries a career-experience field (verified
	// live via getNFLPlayerList, 2026-08-16): "R" for a rookie, a season
	// count for a veteran, absent entirely for a player Tank01 never
	// reported it on. parsePlayerList must not drop it.
	if players["100"].Exp != "R" || !players["100"].IsRookie() {
		t.Errorf("rookie exp not captured: Exp=%q IsRookie=%v", players["100"].Exp, players["100"].IsRookie())
	}
	if players["103"].Exp != "9" || players["103"].IsRookie() {
		t.Errorf("veteran exp not captured: Exp=%q IsRookie=%v", players["103"].Exp, players["103"].IsRookie())
	}
	if players["101"].Exp != "" || players["101"].IsRookie() {
		t.Errorf("a player with no exp field should decode to an empty Exp and IsRookie() == false: Exp=%q IsRookie=%v", players["101"].Exp, players["101"].IsRookie())
	}
}

// TestParsePlayerListIncludesPunters checks that pos "P" parses into the
// pool as Position "P" (roster-ops spec section 4.1.2 / WP-R0), carrying
// jerseyNum and espnHeadshot the same as any other position — the live
// feed populates both for punters.
func TestParsePlayerListIncludesPunters(t *testing.T) {
	raw := json.RawMessage(`[
		{"playerID":"200","longName":"Test Punter","pos":"P","team":"hou","jerseyNum":"4",
		 "espnHeadshot":"https://a.espncdn.com/i/headshots/nfl/players/full/4241469.png"}
	]`)
	players := parsePlayerList(raw)
	punter, ok := players["200"]
	if !ok {
		t.Fatalf("punter dropped from parsePlayerList: %+v", players)
	}
	if punter.Position != "P" {
		t.Errorf("punter position = %q, want P", punter.Position)
	}
	if punter.NFLTeam != "HOU" {
		t.Errorf("punter team not upper-cased: %q", punter.NFLTeam)
	}
	if punter.Jersey != "4" {
		t.Errorf("punter jersey lost: %q", punter.Jersey)
	}
	if punter.Headshot == "" {
		t.Errorf("punter headshot lost")
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
	if points["1"].Points != 12.0 {
		t.Errorf("halfPPR projection = %v", points["1"].Points)
	}
	if points["2"].Points != 8.5 {
		t.Errorf("fantasyPoints fallback = %v", points["2"].Points)
	}
	if _, ok := points["3"]; ok {
		t.Errorf("zero projection should be dropped")
	}
	if points := parseProjections(raw, "ppr"); points["1"].Points != 14.0 {
		t.Errorf("PPR projection = %v", points["1"].Points)
	}
}

func TestParseProjectionsList(t *testing.T) {
	raw := json.RawMessage(`[{"playerID":"7","fantasyPoints":11.25}]`)
	points := parseProjections(raw, "half_ppr")
	if points["7"].Points != 11.25 {
		t.Errorf("list projections = %v", points)
	}
}

// TestParseProjectionsNestedStatGroups mirrors the live projection shape
// observed on 2026-08-16: per-play-type groups with string-encoded numbers,
// plus a top-level fumblesLost. Zero-valued stats must not appear.
func TestParseProjectionsNestedStatGroups(t *testing.T) {
	raw := json.RawMessage(`{"playerProjections":{
		"1":{"Rushing":{"rushYds":"78.0","carries":"16.3","rushTD":"0.8"},
		     "Passing":{"passAttempts":"0","passTD":"0","passYds":"0","int":"0","passCompletions":"0"},
		     "Receiving":{"receptions":"3.9","targets":"4.9","recTD":"0.2","recYds":"30.2"},
		     "twoPointConversion":"0.1","fumblesLost":"0.1","fantasyPoints":"2.05",
		     "fantasyPointsDefault":{"standard":"16.92","PPR":"20.82","halfPPR":"18.87"}}
	}}`)
	entries := parseProjections(raw, "half_ppr")
	entry, ok := entries["1"]
	if !ok {
		t.Fatalf("player 1 missing from projections: %+v", entries)
	}
	if entry.Points != 18.87 {
		t.Errorf("halfPPR points = %v", entry.Points)
	}
	want := map[string]float64{
		"rushYds": 78.0, "carries": 16.3, "rushTD": 0.8,
		"receptions": 3.9, "targets": 4.9, "recTD": 0.2, "recYds": 30.2,
		"fumblesLost": 0.1,
	}
	if len(entry.Stats) != len(want) {
		t.Fatalf("stats = %+v, want %+v", entry.Stats, want)
	}
	for key, value := range want {
		if entry.Stats[key] != value {
			t.Errorf("stats[%q] = %v, want %v", key, entry.Stats[key], value)
		}
	}
	for _, zeroKey := range []string{"passAttempts", "passTD", "passYds", "passInt", "passCompletions"} {
		if _, present := entry.Stats[zeroKey]; present {
			t.Errorf("zero-valued passing stat %q should be dropped: %+v", zeroKey, entry.Stats)
		}
	}
}

// TestParseProjectionsDefenseHasNoStatGroups mirrors a DST/kicker entry: no
// Rushing/Passing/Receiving groups at all. Stats must be empty, not nil, and
// parsing must not panic.
func TestParseProjectionsDefenseHasNoStatGroups(t *testing.T) {
	raw := json.RawMessage(`{"playerProjections":{
		"DST-HOU":{"fantasyPoints":"7.5","fantasyPointsDefault":{"standard":"7.5","PPR":"7.5","halfPPR":"7.5"}}
	}}`)
	entries := parseProjections(raw, "half_ppr")
	entry, ok := entries["DST-HOU"]
	if !ok {
		t.Fatalf("DST entry missing: %+v", entries)
	}
	if entry.Points != 7.5 {
		t.Errorf("DST points = %v", entry.Points)
	}
	if entry.Stats == nil {
		t.Error("DST stats map must be non-nil")
	}
	if len(entry.Stats) != 0 {
		t.Errorf("DST stats should be empty: %+v", entry.Stats)
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
	projections := map[string]projEntry{
		"4": {Points: 20},
		"5": {Points: 9},
		"1": {Points: 18, Stats: map[string]float64{"passYds": 250}},
	}
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
	if pool[0].ProjStats["passYds"] != 250 {
		t.Errorf("proj stats not merged: %+v", pool[0].ProjStats)
	}
	if pool[1].Name != "Synth DST" || pool[1].Position != "DST" {
		t.Errorf("synthesized entry wrong: %+v", pool[1])
	}
	capped := mergePool(base, adp, projections, nil, nil, 2)
	if len(capped) != 2 {
		t.Errorf("pool cap ignored: %d", len(capped))
	}
}

// TestMergePoolPuntersSortToTailWithNoCrash checks that punters — which
// carry no ADP and no Tank01 projections (roster-ops spec section 4.1.2) —
// merge safely: no ADP entry means they fall into mergePool's "rest" branch
// alongside any other unranked player, sorted by projection (zero for all
// of them, so ties break by name), with zero ADPRank and zero Projection.
// The zero values are plain float64/int zero values, never a nil pointer,
// so no ranking path can panic on a punter row.
func TestMergePoolPuntersSortToTailWithNoCrash(t *testing.T) {
	base := map[string]Player{
		"1": {ID: "1", Name: "Ranked Receiver", Position: "WR", NFLTeam: "CIN"},
		"9": {ID: "9", Name: "Zed Punter", Position: "P", NFLTeam: "HOU"},
		"8": {ID: "8", Name: "Able Punter", Position: "P", NFLTeam: "DAL"},
	}
	adp := []adpEntry{{PlayerID: "1", ADP: 1.5}}
	pool := mergePool(base, adp, nil, nil, nil, 10)
	if len(pool) != 3 {
		t.Fatalf("pool size = %d, want 3", len(pool))
	}
	if pool[0].ID != "1" {
		t.Fatalf("ranked player must lead the pool: %+v", pool[0])
	}
	// Both punters tie at zero projection, so they order by name.
	if pool[1].ID != "8" || pool[2].ID != "9" {
		t.Fatalf("punters must sort to the tail by name on a projection tie: %+v", pool[1:])
	}
	for _, punter := range pool[1:] {
		if punter.ADPRank != 0 {
			t.Errorf("punter %s ADPRank = %d, want 0 (no ADP)", punter.ID, punter.ADPRank)
		}
		if punter.Projection != 0 {
			t.Errorf("punter %s Projection = %v, want 0 (no Tank01 projection)", punter.ID, punter.Projection)
		}
	}
}

// TestMergePoolRestOrderIsDeterministicAcrossCalls is the pool-order
// determinism fix's own regression test: base is a map, and Go map
// iteration order is unspecified — it can vary from one range over the
// same map to the next, even within one process, which used to leak
// straight into mergePool's "rest" branch (every player with no ADP
// entry: defenses, kickers, deep bench players) and from there into the
// rendered draft and board pools, so two page loads could show a
// different order with no code change and no data change in between.
// Every one of these 64 players ties at zero projection (none has a
// projections entry), and only 8 distinct names cover all 64 (8 players
// per name) — a real tie under mergePool's own (Points, Name) ranking, the
// shape that actually depends on "rest"'s pre-sort order. Before the fix
// this run would flake open non-deterministically; sort.Strings on the
// collected map keys, ahead of the range, now fixes rest's starting
// order, and the sort.SliceStable call's own ID tiebreak closes the same
// gap a second, independent way — either alone is sufficient, and this
// test proves both hold together.
func TestMergePoolRestOrderIsDeterministicAcrossCalls(t *testing.T) {
	base := make(map[string]Player, 64)
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("p%02d", i)
		base[id] = Player{ID: id, Name: fmt.Sprintf("Bench Player %d", i%8), Position: "WR", NFLTeam: "FA"}
	}

	first := mergePool(base, nil, nil, nil, nil, 100)
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}

	for i := 0; i < 25; i++ {
		next := mergePool(base, nil, nil, nil, nil, 100)
		nextJSON, err := json.Marshal(next)
		if err != nil {
			t.Fatalf("run %d: marshal: %v", i, err)
		}
		if !bytes.Equal(firstJSON, nextJSON) {
			t.Fatalf("run %d: mergePool's rest order is not deterministic (map iteration leak):\nfirst: %s\nnext:  %s", i, firstJSON, nextJSON)
		}
	}
}

func TestOfflinePoolIsDraftable(t *testing.T) {
	pool := OfflinePool()
	// WP-R0 moved league.DraftRounds 15 -> 17 (8 teams x 17 = 136 picks);
	// the offline fallback pool must still outsize a full draft.
	if len(pool) < 136 {
		t.Fatalf("offline pool too small for a 136-pick draft: %d", len(pool))
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

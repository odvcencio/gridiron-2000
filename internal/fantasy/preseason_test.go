package fantasy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// loadPreseasonFixture reads the verified sample (section 3 of the design
// spec) and runs it through unwrapEnvelope, exactly as tank01Client.get
// would before handing the body to parsePreseasonBoxScore (F1).
func loadPreseasonFixture(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "preseason-boxscore-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	return unwrapEnvelope(raw)
}

// TestParsePreseasonBoxScoreStatMaps pins the worked example (design spec
// section 6) against the verified sample: exact stat maps for the five
// entry-builder players, and the game's final flag.
func TestParsePreseasonBoxScoreStatMaps(t *testing.T) {
	body := loadPreseasonFixture(t)
	stats, final := parsePreseasonBoxScore(body)
	if !final {
		t.Fatal("the sample game is Completed/Final (P7); want final == true")
	}

	cases := []struct {
		name string
		id   string
		want map[string]float64
	}{
		{"Minshew", "4038524", map[string]float64{"passYds": 101, "passTD": 2, "passAttempts": 16, "passCompletions": 14}},
		{"Ryland", "4363538", map[string]float64{"fgMade": 2, "fgMissed": 1, "xpMade": 3}},
		{"Bech", "4603186", map[string]float64{"receptions": 3, "recYds": 25, "recTD": 1, "targets": 3}},
		{"Mayer", "4429086", map[string]float64{"receptions": 2, "recYds": 19, "recTD": 1, "targets": 2}},
		{"Kiner", "4431431", map[string]float64{"rushYds": 59, "carries": 10, "receptions": 2, "recYds": 13, "targets": 2}},
	}
	for _, tc := range cases {
		got := stats[tc.id]
		if len(got) != len(tc.want) {
			t.Fatalf("%s (%s) stat map = %+v, want %+v", tc.name, tc.id, got, tc.want)
		}
		for key, value := range tc.want {
			if got[key] != value {
				t.Errorf("%s[%q] = %v, want %v", tc.name, key, got[key], value)
			}
		}
	}
}

// TestParsePreseasonBoxScoreSmithMarsetteNoKickerLeak checks P6: a return
// man's row carries Kicking.kickReturns, but the parser must key on field
// names, never on group presence — no fgMade/fgMissed/xpMade, and no
// returnTD since his kickReturnTD and puntReturnTD are both zero.
func TestParsePreseasonBoxScoreSmithMarsetteNoKickerLeak(t *testing.T) {
	body := loadPreseasonFixture(t)
	stats, _ := parsePreseasonBoxScore(body)
	got := stats["4240573"]
	for _, key := range []string{"fgMade", "fgMissed", "xpMade", "returnTD"} {
		if _, present := got[key]; present {
			t.Errorf("Smith-Marsette must not carry %q: %+v", key, got)
		}
	}
	want := map[string]float64{"receptions": 1, "recYds": 7, "targets": 1}
	if len(got) != len(want) {
		t.Fatalf("Smith-Marsette stat map = %+v, want %+v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("Smith-Marsette[%q] = %v, want %v", key, got[key], value)
		}
	}
}

// TestPreseasonPlayerStatsNeverFabricatesTwoPt pins GC-1 fix 3's live-side
// boundary: Tank01's box score carries no per-player two-point field at
// all (verified against both fixtures this file uses — the only
// twoPointConversions field either carries is a team-level total, not
// attributable to a player), so preseasonPlayerStats must never invent a
// "twoPt" stat key, even from a look-alike field name a future response
// shape could carry. The rule scores from the closed-week ledger only
// (main.go's offenseStatLine); see this function's own doc comment.
func TestPreseasonPlayerStatsNeverFabricatesTwoPt(t *testing.T) {
	entry := map[string]any{
		"longName":            "Example Runner",
		"teamAbv":             "BUF",
		"Rushing":             map[string]any{"rushYds": "12", "carries": "3", "rushTD": "1"},
		"twoPointConversions": "1", // a team-level field name; must not leak into a player row
	}
	if _, present := preseasonPlayerStats(entry)["twoPt"]; present {
		t.Fatalf("preseasonPlayerStats must never fabricate a twoPt key: %+v", preseasonPlayerStats(entry))
	}
}

// TestParsePreseasonBoxScoreDropsDefenseOnlyRow checks that a defense-only
// row (Eku Leota, playerID 4361272) never appears in the output: it has no
// scored group, so it is dropped rather than emitted as an empty map — the
// blitz leaderboard must be able to ignore a box-score playerID absent from
// the pool without special-casing an empty stat line (T6).
func TestParsePreseasonBoxScoreDropsDefenseOnlyRow(t *testing.T) {
	body := loadPreseasonFixture(t)
	stats, _ := parsePreseasonBoxScore(body)
	if got, ok := stats["4361272"]; ok {
		t.Errorf("a defense-only row (Leota) must be dropped, got %+v", got)
	}
}

// TestParsePreseasonBoxScoreDropsPunterOnlyRow checks that a punter's row
// (Punting.puntYds/punts only, no Kicking group) is dropped: punter fields
// are ignored per section 4.3.
func TestParsePreseasonBoxScoreDropsPunterOnlyRow(t *testing.T) {
	body := loadPreseasonFixture(t)
	stats, _ := parsePreseasonBoxScore(body)
	if got, ok := stats["3686689"]; ok { // AJ Cole, punter
		t.Errorf("a punter-only row must be dropped, got %+v", got)
	}
}

// TestSelectPreseasonGamesOffsetDefense is T3: a getNFLGamesForWeek fixture
// whose "week" param disagrees with its labels must still bucket games by
// gameWeek label only (P1); a response carrying zero games with the target
// label selects nothing.
func TestSelectPreseasonGamesOffsetDefense(t *testing.T) {
	// Simulates P1's observed offset: a request for week=2 actually
	// returned games labeled "Preseason Week 1". parsePreseasonWeek never
	// reads the request's week param at all, so this fixture only needs to
	// carry the (wrong-looking) labels the response actually shipped.
	offsetRaw := json.RawMessage(`[
		{"gameID": "20260813_ARI@LV", "gameWeek": "Preseason Week 1", "away": "ARI", "home": "LV", "gameStatus": "Final", "gameStatusCode": "2"},
		{"gameID": "20260814_NYJ@NYG", "gameWeek": "Preseason Week 1", "away": "NYJ", "home": "NYG", "gameStatus": "Final", "gameStatusCode": "2"}
	]`)
	games := parsePreseasonWeek(offsetRaw)
	if len(games) != 2 {
		t.Fatalf("parsed %d games, want 2", len(games))
	}

	// The target label ("Preseason Week 2") never appears in this response,
	// so selecting it must return nothing rather than falling back to
	// whatever the request's week param implied.
	if matched := SelectPreseasonGames(games, "Preseason Week 2"); len(matched) != 0 {
		t.Fatalf("selecting an absent label matched %d games, want 0: %+v", len(matched), matched)
	}

	// The label that IS present must match, case-insensitively, regardless
	// of what week param produced the response.
	matched := SelectPreseasonGames(games, "preseason week 1")
	if len(matched) != 2 {
		t.Fatalf("selecting the present label (case-insensitive) matched %d games, want 2", len(matched))
	}
	if matched[0].ID != "20260813_ARI@LV" || matched[1].ID != "20260814_NYJ@NYG" {
		t.Fatalf("matched games = %+v, want the two Week 1 games in order", matched)
	}
}

// TestPreseasonKickoffFromEpoch checks the primary kickoff path (section
// 5.1): gameTime_epoch via flexFloat, tolerant of the string encoding P2
// observed elsewhere in the feed.
func TestPreseasonKickoffFromEpoch(t *testing.T) {
	raw := json.RawMessage(`[{"gameID":"20260820_LV@HOU","gameWeek":"Preseason Week 2","away":"LV","home":"HOU","gameTime_epoch":"1787270400","gameStatus":"Scheduled"}]`)
	games := parsePreseasonWeek(raw)
	if len(games) != 1 {
		t.Fatalf("parsed %d games, want 1", len(games))
	}
	want := time.Unix(1787270400, 0).UTC()
	if !games[0].Kickoff.Equal(want) {
		t.Fatalf("kickoff = %v, want %v", games[0].Kickoff, want)
	}
	if games[0].Final {
		t.Fatal("a Scheduled game must not be final")
	}
}

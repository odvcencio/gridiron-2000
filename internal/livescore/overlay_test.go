package livescore

import (
	"testing"

	"gridiron-2000/internal/league"
)

func overlayResolver(id, name string) (league.Player, bool) {
	switch name {
	case "Josh Allen":
		return league.Player{ID: "p-09", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"}, true
	case "Lamar Jackson":
		return league.Player{ID: "p-06", Name: "Lamar Jackson", Position: "QB", NFLTeam: "BAL"}, true
	}
	return league.Player{}, false
}

func overlaySnapshot(inProgress bool) Snapshot {
	return Snapshot{Version: 1,
		Weeks: map[int]WeekLines{1: {
			Lines: []Line{
				{PlayerID: "3918298", Name: "Josh Allen", Team: "BUF", Stats: map[string]float64{"passYds": 55, "passTD": 1}, Final: !inProgress},
				{PlayerID: "0", Name: "Nobody Known", Team: "BUF", Stats: map[string]float64{"rushYds": 9}},
			},
			DST: map[string]DSTLine{"BUF": {Stats: map[string]float64{"sacks": 2, "ptsAllowed": 0}, Final: !inProgress}},
		}},
		Games: map[string]GameState{"g1": {ID: "g1", Week: 1, Away: "BAL", Home: "BUF", Period: "Q2", InProgress: inProgress, Final: !inProgress}},
	}
}

func TestMergeLinesLiveWinsWhileTheGameIsInProgress(t *testing.T) {
	base := []league.WeekStatLine{{Key: "joshallen|QB", Stats: map[string]float64{"passYards": 0}, Source: league.StatSourceLedger}}
	merged := MergeLines(base, 1, overlaySnapshot(true), overlayResolver)
	byKey := map[string]league.WeekStatLine{}
	for _, line := range merged {
		byKey[line.Key] = line
	}
	if allen := byKey["joshallen|QB"]; allen.Source != league.StatSourceLive || allen.Stats["passYards"] != 55 || allen.Stats["passTD"] != 1 {
		t.Fatalf("a stale zero ledger row beat the live row: %+v", allen)
	}
	dst := byKey["billsdst|DST"]
	if dst.Source != league.StatSourceLive || dst.Stats["dstSack"] != 2 {
		t.Fatalf("D/ST = %+v", dst)
	}
	// The comma-ok form proves the in-progress gate actually withholds
	// the shutout key, rather than merely reading a map-miss zero value
	// that would pass this check either way (round-2 note 12).
	if _, ok := dst.Stats["dstShutout"]; ok {
		t.Fatalf("an in-progress D/ST unit must not carry a shutout key: %+v", dst)
	}
	if _, ok := byKey["nobodyknown|QB"]; ok || len(merged) != 2 {
		t.Fatalf("an unresolved name leaked or rows duplicated: %+v", merged)
	}
}

func TestMergeLinesLedgerWinsOnceTheGameIsFinal(t *testing.T) {
	base := []league.WeekStatLine{{Key: "joshallen|QB", Stats: map[string]float64{"passYards": 300}, Source: league.StatSourceLedger}}
	merged := MergeLines(base, 1, overlaySnapshot(false), overlayResolver)
	if len(merged) != 2 {
		t.Fatalf("merged = %+v", merged)
	}
	for _, line := range merged {
		if line.Key == "joshallen|QB" && (line.Source != league.StatSourceLedger || line.Stats["passYards"] != 300) {
			t.Fatalf("the ledger row lost to a final live row: %+v", line)
		}
		if line.Key == "billsdst|DST" && (line.Source != league.StatSourceLiveFinal || line.Stats["dstShutout"] != 1) {
			t.Fatalf("a final D/ST row without a ledger row must be live-final with the shutout: %+v", line)
		}
	}
	if got := MergeLines(base, 2, overlaySnapshot(true), overlayResolver); len(got) != 1 {
		t.Fatalf("a week without live data must return the base unchanged: %+v", got)
	}
}

// TestMergeLinesFinalLiveRowBeatsAPartialLedgerRow covers ledgerBehind
// (round-2 note 44): a final live row beats a ledger row that is a
// partial mirror of it (every ledger value <= the live value and at
// least one strictly lower), and loses to a ledger row that is not
// behind (a ledger value exceeds the live value).
func TestMergeLinesFinalLiveRowBeatsAPartialLedgerRow(t *testing.T) {
	finalSnapshot := func(passYds, passTD float64) Snapshot {
		return Snapshot{Version: 1,
			Weeks: map[int]WeekLines{1: {
				Lines: []Line{
					{PlayerID: "3918298", Name: "Josh Allen", Team: "BUF", Stats: map[string]float64{"passYds": passYds, "passTD": passTD}, Final: true},
				},
			}},
			Games: map[string]GameState{"g1": {ID: "g1", Week: 1, Away: "BAL", Home: "BUF", Period: "Final", InProgress: false, Final: true}},
		}
	}

	partial := []league.WeekStatLine{{Key: "joshallen|QB", Stats: map[string]float64{"passYards": 150}, Source: league.StatSourceLedger}}
	merged := MergeLines(partial, 1, finalSnapshot(394, 2), overlayResolver)
	if len(merged) != 1 || merged[0].Source != league.StatSourceLiveFinal || merged[0].Stats["passYards"] != 394 {
		t.Fatalf("a final live row did not beat a partial ledger row: %+v", merged)
	}

	complete := []league.WeekStatLine{{Key: "joshallen|QB", Stats: map[string]float64{"passYards": 394}, Source: league.StatSourceLedger}}
	merged = MergeLines(complete, 1, finalSnapshot(300, 1), overlayResolver)
	if len(merged) != 1 || merged[0].Source != league.StatSourceLedger || merged[0].Stats["passYards"] != 394 {
		t.Fatalf("a stale-looking final live row beat a complete ledger row: %+v", merged)
	}
}

// TestMergeLinesFallsBackToGameIDWhenTeamIsEmpty covers round-2 note 3:
// Tank01 sometimes omits teamAbv, leaving Line.Team empty. The overlay
// must still recognize the player's game as in progress by GameID, or a
// stale ledger row would win by default.
func TestMergeLinesFallsBackToGameIDWhenTeamIsEmpty(t *testing.T) {
	snapshot := Snapshot{Version: 1,
		Weeks: map[int]WeekLines{1: {
			Lines: []Line{
				{PlayerID: "3918298", Name: "Josh Allen", Team: "", GameID: "g1", Stats: map[string]float64{"passYds": 55}, Final: false},
			},
		}},
		Games: map[string]GameState{"g1": {ID: "g1", Week: 1, Away: "BAL", Home: "BUF", Period: "Q2", InProgress: true, Final: false}},
	}
	base := []league.WeekStatLine{{Key: "joshallen|QB", Stats: map[string]float64{"passYards": 0}, Source: league.StatSourceLedger}}
	merged := MergeLines(base, 1, snapshot, overlayResolver)
	if len(merged) != 1 || merged[0].Source != league.StatSourceLive || merged[0].Stats["passYards"] != 55 {
		t.Fatalf("an empty-team live line lost to a stale ledger row: %+v", merged)
	}
}

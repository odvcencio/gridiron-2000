package league

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestOriginalProjectionSummaryPartialIncludesPlayersAfterGap(t *testing.T) {
	rows := []StarterLedgerRow{
		{Slot: "QB", PlayerID: "a", PlayerName: "First"},
		{Slot: "WR1", PlayerID: "missing", PlayerName: "A.J. Brown", GameFinal: true, Points: 4.1},
		{Slot: "WR2", PlayerID: "b", PlayerName: "Last"},
		{Slot: "TE"},
	}
	pool := map[string]Player{"a": {ID: "a", Projection: 19}, "b": {ID: "b", Projection: 12}}
	summary := summarizeOriginalProjections(rows, pool)
	if summary.Total != 31 || summary.Known != 2 || summary.Starters != 3 || summary.complete() || summary.text() != "31.0*" || summary.coverage() != "Partial" {
		t.Fatalf("partial summary = %+v, text=%q coverage=%q", summary, summary.text(), summary.coverage())
	}
	if !strings.Contains(summary.note(), "2 of 3") || !strings.Contains(summary.note(), "A.J. Brown") || !strings.Contains(summary.note(), "not substituted") {
		t.Fatalf("partial note = %q", summary.note())
	}
	if total, known := originalProjectedTotal(rows, pool); total != 31 || known {
		t.Fatalf("strict total = %v, %v; must sum all known forecasts but remain incomplete", total, known)
	}
	if hasKnownStarterProjections(rows, pool) {
		t.Fatal("partial display must not relax complete-forecast gate")
	}
	if got := originalStarterProjectedText(rows[1], pool); got != "—" {
		t.Fatalf("missing forecast replaced with actual: %q", got)
	}
}

func TestOriginalProjectionSummaryCoverageStates(t *testing.T) {
	rows := []StarterLedgerRow{{PlayerID: "a", PlayerName: "Starter"}}
	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		s := summarizeOriginalProjections(rows, map[string]Player{"a": {ID: "a", Projection: value}})
		if s.text() != "—" || s.coverage() != "" || s.complete() || !strings.Contains(s.note(), "0 of 1") {
			t.Fatalf("unknown %v: %+v, %q", value, s, s.note())
		}
	}
	s := summarizeOriginalProjections(rows, map[string]Player{"a": {ID: "a", Projection: 18.5}})
	if !s.complete() || s.text() != "18.5" || s.coverage() != "" || strings.Contains(s.note(), "Missing") {
		t.Fatalf("complete summary = %+v", s)
	}
	s = summarizeOriginalProjections(nil, nil)
	if s.complete() || s.text() != "—" || !strings.Contains(s.note(), "Set a starting lineup") {
		t.Fatalf("empty summary = %+v", s)
	}
	s = summarizeOriginalProjections([]StarterLedgerRow{{PlayerID: "a", Slot: "QB"}, {PlayerID: "b"}, {PlayerID: "c", PlayerName: "Third"}, {PlayerID: "d", PlayerName: "Fourth"}}, nil)
	if !strings.Contains(s.note(), "QB, Unnamed starter, Third and 1 other") || strings.Contains(s.note(), "Fourth") {
		t.Fatalf("missing names must be bounded and have fallbacks: %q", s.note())
	}
}

func TestPartialOriginalProjectionIsConsistentAcrossPageAndLive(t *testing.T) {
	svc, _ := featuredMatchupFixture(t)
	// Add a second starter with a forecast. Allen's missing forecast must
	// not blank this later slot or be replaced by his live actual points.
	pool := append([]Player(nil), svc.players...)
	for i := range pool {
		if pool[i].ID == "p-09" {
			pool[i].Projection = 0
		}
	}
	pool = append(pool, Player{ID: "coverage-extra", Name: "Available Forecast", Position: "WR", NFLTeam: "KC", Projection: 12})
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 99, "test" })
	teams := svc.Teams()
	for i := 2; i < len(teams); i++ {
		if err := svc.SeedStarterForTest(teams[i].ID, 1, "QB", fmt.Sprintf("coverage-fill-a-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(teams) - 1; i > 0; i-- {
		if err := svc.SeedStarterForTest(teams[i].ID, 1, "WR1", fmt.Sprintf("coverage-fill-b-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SeedStarterForTest("team-1", 1, "WR1", "coverage-extra"); err != nil {
		t.Fatal(err)
	}
	view := svc.LiveScoresView(context.Background())
	if got := view["originalProjected"].(map[string]string)["team-1"]; got != "12.0*" {
		t.Fatalf("live partial projection = %q", got)
	}
	if got := view["originalProjectionCoverage"].(map[string]string)["team-1"]; got != "Partial" {
		t.Fatalf("live coverage = %q", got)
	}
	note := view["originalProjectionNote"].(map[string]string)["team-1"]
	if !strings.Contains(note, "Josh Allen") || !strings.Contains(note, "1 of 2") {
		t.Fatalf("live note = %q", note)
	}
	data := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	featured := data["my_matchup"].(map[string]any)
	found := false
	for _, key := range []string{"mine", "theirs"} {
		side := featured[key].(map[string]any)
		if side["id"] == "team-1" {
			found = true
			if side["projected"] != "12.0*" || side["projection_coverage"] != "Partial" || side["projection_note"] != note {
				t.Fatalf("page differs from live: %#v", side)
			}
		}
	}
	if !found {
		t.Fatal("fixture did not feature team-1")
	}
	// The same label and tooltip appear when this team is not featured.
	otherID := data["other_matchups"].([]map[string]any)[0]["id"].(string)
	data = svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?m="+otherID))
	found = false
	for _, entry := range data["other_matchups"].([]map[string]any) {
		for _, key := range []string{"away", "home"} {
			side := entry[key].(map[string]any)
			if side["id"] == "team-1" {
				found = true
				if entry["projected_"+key] != "12.0*" || entry["projected_"+key+"_coverage"] != "Partial" || entry["projected_"+key+"_note"] != note {
					t.Fatalf("scorebug differs from live: %#v", entry)
				}
			}
		}
	}
	if !found {
		t.Fatal("fixture did not include team-1 in another scorebug")
	}
	for i := range pool {
		if pool[i].ID == "p-09" {
			pool[i].Projection = 20
		}
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 100, "test" })
	view = svc.LiveScoresView(context.Background())
	if view["originalProjected"].(map[string]string)["team-1"] != "32.0" || view["originalProjectionCoverage"].(map[string]string)["team-1"] != "" || strings.Contains(view["originalProjectionNote"].(map[string]string)["team-1"], "Missing") {
		t.Fatal("completed forecast did not clear partial marker and explanation")
	}
	svc.SetPoolStatus(func() PlayerPoolStatus { return PlayerPoolStatus{ProjectionWeek: 2} })
	view = svc.LiveScoresView(context.Background())
	if view["originalProjected"].(map[string]string)["team-1"] != "—" || view["originalProjectionCoverage"].(map[string]string)["team-1"] != "" {
		t.Fatal("wrong-week forecasts leaked into partial display")
	}
}

package league

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------
// Wave 7 item 2: positional_depth ("2 QB · 4 RB · 5 WR · 2 TE · 1 K · 1
// DST") and its chip-list twin, both derived from the general roster.
// ---------------------------------------------------------------------

func TestPositionalDepthSummaryAndChipsShareOneOrderedCount(t *testing.T) {
	general := []Player{
		{ID: "1", Position: "WR"}, {ID: "2", Position: "QB"}, {ID: "3", Position: "RB"},
		{ID: "4", Position: "WR"}, {ID: "5", Position: "DST"}, {ID: "6", Position: "K"},
		{ID: "7", Position: "TE"}, {ID: "8", Position: "TE"}, {ID: "9", Position: "P"},
	}
	want := "1 QB · 1 RB · 2 WR · 2 TE · 1 K · 1 DST · 1 P"
	if got := positionalDepthSummary(general); got != want {
		t.Fatalf("positionalDepthSummary = %q, want %q", got, want)
	}
	chips := positionalDepthChips(general)
	if len(chips) != 7 {
		t.Fatalf("positionalDepthChips length = %d, want 7: %+v", len(chips), chips)
	}
	if chips[0]["label"] != "1 QB" || chips[len(chips)-1]["label"] != "1 P" {
		t.Fatalf("positionalDepthChips edges = %v .. %v, want \"1 QB\" .. \"1 P\"", chips[0]["label"], chips[len(chips)-1]["label"])
	}
}

func TestPositionalDepthSummaryEmptyRosterRendersEmptyString(t *testing.T) {
	if got := positionalDepthSummary(nil); got != "" {
		t.Fatalf("positionalDepthSummary(nil) = %q, want \"\"", got)
	}
	if chips := positionalDepthChips(nil); len(chips) != 0 {
		t.Fatalf("positionalDepthChips(nil) = %+v, want empty", chips)
	}
}

// ---------------------------------------------------------------------
// Wave 7 item 1 (row-decoration half): addBenchGroupHeaders marks only
// the first row of each new position group.
// ---------------------------------------------------------------------

func TestAddBenchGroupHeadersMarksOnlyFirstOfEachGroup(t *testing.T) {
	rows := []map[string]any{
		{"position": "QB"},
		{"position": "QB"},
		{"position": "RB"},
		{"position": "WR"},
		{"position": "WR"},
	}
	addBenchGroupHeaders(rows)
	want := []struct {
		has   bool
		label string
	}{
		{true, "QB"}, {false, ""}, {true, "RB"}, {true, "WR"}, {false, ""},
	}
	for i, w := range want {
		if rows[i]["has_group_header"] != w.has || rows[i]["group_header"] != w.label {
			t.Errorf("rows[%d] = has:%v label:%q, want has:%v label:%q", i, rows[i]["has_group_header"], rows[i]["group_header"], w.has, w.label)
		}
	}
}

// ---------------------------------------------------------------------
// Wave 7 item 4 (row-decoration half): addScheduleLabels zips bench
// players onto their own already-rendered rows.
// ---------------------------------------------------------------------

func TestAddScheduleLabelsAppliesUnconditionalKickoffAndBye(t *testing.T) {
	kickoff := time.Date(2026, 9, 13, 20, 25, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "PIT", Home: "NYJ"}}
	players := []Player{
		{ID: "p1", NFLTeam: "PIT"},
		{ID: "p2", NFLTeam: "TB", ByeWeek: 1},
	}
	rows := []map[string]any{{"id": "p1"}, {"id": "p2"}}
	addScheduleLabels(rows, players, games, 1, time.UTC)
	if rows[0]["has_kickoff_label"] != true || rows[0]["has_bye_label"] != false {
		t.Fatalf("scheduled row = %+v, want has_kickoff_label=true has_bye_label=false", rows[0])
	}
	if rows[1]["has_kickoff_label"] != false || rows[1]["has_bye_label"] != true || rows[1]["bye_label"] != "BYE 1" {
		t.Fatalf("bye row = %+v, want has_kickoff_label=false has_bye_label=true bye_label=\"BYE 1\"", rows[1])
	}
}

// ---------------------------------------------------------------------
// Wave 7 item 5: the drafted-round chip now comes from playerMap's own
// is_drafted/drafted_round/drafted_pick/drafted_label fields (hazel's
// draftedByPlayerID, draft_history.go), threaded through starterRowMaps
// and playerMapsWithScoring — see draft_history_test.go for
// draftedByPlayerID's own coverage and service_test.go/players_test.go
// for playerMap's. This file covers only the /team-specific wiring
// (below): that teamData actually passes drafted through to both row
// builders.
// ---------------------------------------------------------------------

// ---------------------------------------------------------------------
// Wave 7 item 6: draftClassTeaser.
// ---------------------------------------------------------------------

// TestDraftClassTeaserOrdersByRoundThenPickAndLimitsToThree proves the
// teaser sorts by pick order (round, then overall number) rather than
// trusting state.Picks' own insertion order — team-1's four entries
// below are inserted out of round order on purpose — and that it stops
// at the requested limit and never leaks another team's pick.
func TestDraftClassTeaserOrdersByRoundThenPickAndLimitsToThree(t *testing.T) {
	svc := newTestService(t, true)
	players := []Player{
		{ID: "qb-first", Name: "First Rounder QB", Position: "QB", Projection: 20},
		{ID: "rb-second", Name: "Second Rounder RB", Position: "RB", Projection: 15},
		{ID: "wr-third", Name: "Third Rounder WR", Position: "WR", Projection: 10},
		{ID: "te-fourth", Name: "Fourth Rounder TE", Position: "TE", Projection: 8},
		{ID: "rival-qb", Name: "Rival QB", Position: "QB", Projection: 22},
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 1, "test" })
	state := PersistedState{
		DraftStarted: true,
		Picks: []DraftPick{
			{Number: 25, Round: 4, TeamID: "team-1", PlayerID: "te-fourth"},
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "qb-first"},
			{Number: 40, Round: 5, TeamID: "team-2", PlayerID: "rival-qb"},
			{Number: 17, Round: 3, TeamID: "team-1", PlayerID: "wr-third"},
			{Number: 9, Round: 2, TeamID: "team-1", PlayerID: "rb-second"},
		},
	}
	teaser := svc.draftClassTeaser(state, "team-1", 3)
	if len(teaser) != 3 {
		t.Fatalf("draftClassTeaser returned %d entries, want 3: %+v", len(teaser), teaser)
	}
	wantLabel := []string{"R1 · P1", "R2 · P9", "R3 · P17"}
	wantName := []string{"First Rounder QB", "Second Rounder RB", "Third Rounder WR"}
	for i := range teaser {
		if teaser[i]["label"] != wantLabel[i] {
			t.Errorf("teaser[%d].label = %v, want %v", i, teaser[i]["label"], wantLabel[i])
		}
		if teaser[i]["name"] != wantName[i] {
			t.Errorf("teaser[%d].name = %v, want %v", i, teaser[i]["name"], wantName[i])
		}
	}
}

func TestDraftClassTeaserEmptyForATeamWithNoPicks(t *testing.T) {
	svc := newTestService(t, true)
	state := PersistedState{}
	if teaser := svc.draftClassTeaser(state, "team-1", 3); len(teaser) != 0 {
		t.Fatalf("draftClassTeaser for an undrafted team = %+v, want empty", teaser)
	}
}

// ---------------------------------------------------------------------
// Wave 7 items 2/5/6 wired end to end through TeamData.
// ---------------------------------------------------------------------

// TestTeamDataCarriesPositionalDepthAndDraftedLabels covers the full
// teamData wiring (not just the pure helpers above): a drafted starter
// carries positional_depth text plus its own drafted_label chip, and
// (draftClassTeaser cares only about state.Picks, not the page's own
// separate team_terminal_roster_complete gate) the teaser itself is
// non-empty the moment the team has any picks at all.
func TestTeamDataCarriesPositionalDepthAndDraftedLabels(t *testing.T) {
	svc, _, _ := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/team", nil)
	data := svc.TeamData(request)

	depth, _ := data["positional_depth"].(string)
	if depth == "" {
		t.Fatal("positional_depth is empty for a team with a drafted roster")
	}
	chips, ok := data["positional_depth_chips"].([]map[string]any)
	if !ok || len(chips) == 0 {
		t.Fatalf("positional_depth_chips = %#v, want a non-empty chip list", data["positional_depth_chips"])
	}

	starters, ok := data["starters"].([]map[string]any)
	if !ok {
		t.Fatal("starters is not []map[string]any")
	}
	sawDraftedStarter := false
	for _, slot := range starters {
		if slot["has_player"] != true {
			continue
		}
		if _, hasKey := slot["is_drafted"]; !hasKey {
			t.Fatalf("occupied starter slot %+v carries no is_drafted key", slot)
		}
		if slot["is_drafted"] == true {
			sawDraftedStarter = true
			if slot["drafted_label"] == "" {
				t.Fatalf("starter slot is_drafted=true but drafted_label is empty: %+v", slot)
			}
		}
	}
	if !sawDraftedStarter {
		t.Fatal("no occupied starter slot carried a drafted_label — newLineupTestService's fixture drafts every starter onto team-1")
	}

	if data["draft_class_teaser_empty"] != false {
		t.Fatalf("draft_class_teaser_empty = %v, want false — newLineupTestService's fixture already drafted 5 players onto team-1", data["draft_class_teaser_empty"])
	}
	if href, _ := data["draft_class_href"].(string); href == "" || !strings.Contains(href, "/draft/results?team=") {
		t.Fatalf("draft_class_href = %q, want a /draft/results?team=<code> URL", href)
	}
}

// TestTeamDataDraftClassTeaserPopulatesOnceRosterIsComplete covers the
// post-draft half: once the league-wide draft finishes (team_terminal_
// roster_complete), the team's own bench/starters/teaser all resolve
// together with no crash and a non-empty teaser for a team the
// round-robin fixture actually drafted onto.
func TestTeamDataDraftClassTeaserPopulatesOnceRosterIsComplete(t *testing.T) {
	svc := newTestService(t, true)
	teams := defaultTeams()
	players := defaultPlayers()
	totalPicks := len(teams) * CurrentDraftRounds()
	svc.store.mu.Lock()
	svc.store.state.DraftStarted = true
	svc.store.state.Picks = terminalDraftPicks(players, teams, totalPicks, nil)
	svc.store.mu.Unlock()

	request, _ := http.NewRequest(http.MethodGet, "/team", nil)
	data := svc.TeamData(request)
	if data["team_terminal_roster_complete"] != true {
		t.Fatalf("team_terminal_roster_complete = %v, want true", data["team_terminal_roster_complete"])
	}
	if data["draft_class_teaser_empty"] != false {
		t.Fatalf("draft_class_teaser_empty = %v, want false once the round-robin fixture has drafted onto team-1", data["draft_class_teaser_empty"])
	}
	teaser, ok := data["draft_class_teaser"].([]map[string]any)
	if !ok || len(teaser) == 0 || len(teaser) > 3 {
		t.Fatalf("draft_class_teaser = %#v, want 1-3 entries", data["draft_class_teaser"])
	}
}

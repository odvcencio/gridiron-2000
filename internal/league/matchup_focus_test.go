package league

import (
	"context"
	"strings"
	"testing"
)

// TestFeaturedMatchupIndexFocus covers the resolution order the owner's
// 2026-09-09 request added: an explicit focus wins, the viewer's own
// matchup is still the default, and isViewer keeps describing the
// RESOLVED matchup rather than how it was chosen.
func TestFeaturedMatchupIndexFocus(t *testing.T) {
	matchups := []ScoreMatchup{
		{ID: "m-1", Away: ScoreTeam{ID: "team-1"}, Home: ScoreTeam{ID: "team-2"}},
		{ID: "m-2", Away: ScoreTeam{ID: "team-3"}, Home: ScoreTeam{ID: "team-4"}},
		{ID: "m-3", Away: ScoreTeam{ID: "team-5"}, Home: ScoreTeam{ID: "team-6"}},
	}
	cases := []struct {
		name         string
		teamID       string
		focusID      string
		wantIndex    int
		wantIsViewer bool
	}{
		{name: "no viewer, no focus: the week's first matchup", wantIndex: 0},
		{name: "viewer's own matchup wins by default", teamID: "team-4", wantIndex: 1, wantIsViewer: true},
		{name: "focus overrides the viewer's own", teamID: "team-4", focusID: "m-3", wantIndex: 2},
		{name: "focusing your own matchup keeps the viewer labels", teamID: "team-4", focusID: "m-2", wantIndex: 1, wantIsViewer: true},
		{name: "focus works with no seated viewer at all", focusID: "m-3", wantIndex: 2},
		{name: "unknown focus falls back to the viewer's own", teamID: "team-4", focusID: "m-nope", wantIndex: 1, wantIsViewer: true},
		{name: "unknown focus, no viewer: the week's first", focusID: "m-nope", wantIndex: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index, isViewer := featuredMatchupIndex(matchups, tc.teamID, tc.focusID)
			if index != tc.wantIndex || isViewer != tc.wantIsViewer {
				t.Fatalf("featuredMatchupIndex(teamID=%q, focusID=%q) = (%d, %v), want (%d, %v)",
					tc.teamID, tc.focusID, index, isViewer, tc.wantIndex, tc.wantIsViewer)
			}
		})
	}
	if index, _ := featuredMatchupIndex(nil, "team-1", "m-1"); index != -1 {
		t.Fatalf("featuredMatchupIndex on an empty week = %d, want -1", index)
	}
}

// TestMatchupsDataFocusFeaturesAnyMatchup is the page-level half: "?m="
// moves the full-width featured card onto the named matchup, every other
// card carries its own link to do the same, and an id that names nothing
// this week says so instead of failing silently.
func TestMatchupsDataFocusFeaturesAnyMatchup(t *testing.T) {
	svc, now := featuredMatchupFixture(t)
	live, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Matchups) < 2 {
		t.Fatalf("fixture produced %d matchups, need at least 2", len(live.Matchups))
	}
	target := live.Matchups[len(live.Matchups)-1]

	focused := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=1&m="+target.ID))
	featured, ok := focused["my_matchup"].(map[string]any)
	if !ok {
		t.Fatalf("my_matchup = %#v, want a map", focused["my_matchup"])
	}
	if got := featured["id"]; got != target.ID {
		t.Fatalf("featured matchup id = %v, want %q", got, target.ID)
	}
	if focused["focus_active"] != true {
		t.Fatalf("focus_active = %v, want true", focused["focus_active"])
	}
	if href, _ := focused["focus_clear_href"].(string); href != "/matchups?week=1" {
		t.Fatalf("focus_clear_href = %q, want %q", href, "/matchups?week=1")
	}
	// The focused matchup must not also appear in the rail beside itself,
	// and every card still there must carry its own way to the same view.
	others, ok := focused["other_matchups"].([]map[string]any)
	if !ok {
		t.Fatalf("other_matchups = %#v, want a slice of maps", focused["other_matchups"])
	}
	if len(others) != len(live.Matchups)-1 {
		t.Fatalf("other_matchups has %d entries, want %d", len(others), len(live.Matchups)-1)
	}
	for _, entry := range others {
		id, _ := entry["id"].(string)
		if id == target.ID {
			t.Fatalf("focused matchup %q is also listed around the league", target.ID)
		}
		href, _ := entry["focus_href"].(string)
		if !strings.Contains(href, "m="+id) || !strings.Contains(href, "week=1") {
			t.Fatalf("focus_href for %q = %q, want it to name week 1 and that matchup", id, href)
		}
	}

	// Default view: no focus, no back link, and the week's own choice.
	plain := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=1"))
	if plain["focus_active"] != false {
		t.Fatalf("focus_active on the default view = %v, want false", plain["focus_active"])
	}

	// An id naming no matchup this week is reported, not ignored.
	unknown := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups?week=1&m=m-not-real"))
	if unknown["focus_active"] != false {
		t.Fatalf("focus_active for an unknown matchup = %v, want false", unknown["focus_active"])
	}
	if unknown["has_week_notice"] != true {
		t.Fatalf("has_week_notice for an unknown matchup = %v, want true", unknown["has_week_notice"])
	}
	notice, _ := unknown["week_notice"].(string)
	if !strings.Contains(notice, "not on Week 1") {
		t.Fatalf("week_notice = %q, want it to say the matchup is not on week 1", notice)
	}
	unknownFeatured, _ := unknown["my_matchup"].(map[string]any)
	plainFeatured, _ := plain["my_matchup"].(map[string]any)
	if unknownFeatured["id"] != plainFeatured["id"] {
		t.Fatalf("unknown focus featured %v, want the default %v", unknownFeatured["id"], plainFeatured["id"])
	}
}

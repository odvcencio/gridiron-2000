package matchups

import (
	"strconv"
	"testing"
)

func TestFeaturedStarterProgressCanonicalLiveKeys(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		viewer, home                           bool
		first, second, firstLabel, secondLabel string
	}{
		{"home manager", true, true, "home", "away", "YOU", "OPPONENT"},
		{"away manager", true, false, "away", "home", "YOU", "OPPONENT"},
		{"spectator", false, true, "home", "away", "HOME", "AWAY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			segments := []map[string]any{
				{"index": 0, "active": true, "q1": "■", "q2": "■", "q3": "■", "q4": "■", "bind_key": "m-mine-0"},
				{"index": 1, "active": true, "q1": "■", "q2": "", "q3": "", "q4": "", "bind_key": "m-mine-1"},
			}
			got := featuredStarterProgressData(map[string]any{
				"id": "m", "is_viewer": tc.viewer, "mine_is_home": tc.home,
				"mine":             map[string]any{"id": "team-1", "name": "First Team"},
				"theirs":           map[string]any{"id": "team-2", "name": "Second Team"},
				"starter_progress": map[string]any{"mine": segments, "theirs": segments},
			})
			if len(got.Teams) != 2 {
				t.Fatalf("teams = %d", len(got.Teams))
			}
			for i, side := range []string{tc.first, tc.second} {
				team := got.Teams[i]
				if team.BindID != "m-"+side || team.Complete != "1/2" {
					t.Fatalf("team = %+v", team)
				}
				for j, segment := range team.Segments {
					if segment.BindKey != team.BindID+"-"+strconv.Itoa(j) {
						t.Fatalf("noncanonical binding: %s", segment.BindKey)
					}
				}
			}
			if got.Teams[0].SideLabel != tc.firstLabel || got.Teams[1].SideLabel != tc.secondLabel {
				t.Fatalf("misleading side labels: %q / %q", got.Teams[0].SideLabel, got.Teams[1].SideLabel)
			}
		})
	}
}

func TestStarterProgressCompleteCountExcludesInactiveSegments(t *testing.T) {
	got := starterProgressData(map[string]any{"home": []map[string]any{
		{"active": true, "q4": "■"}, {"active": false, "q4": "■"}, {"active": true, "q3": "■"},
	}}, nil, "m", nil)
	if got.Teams[1].Complete != "1/3" {
		t.Fatalf("complete = %s", got.Teams[1].Complete)
	}
}

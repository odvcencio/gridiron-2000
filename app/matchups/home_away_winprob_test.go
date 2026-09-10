package matchups

import "testing"

// TestFeaturedCardNamesItsSidesAndItsPercentage covers the owner's
// 2026-09-10 report: home versus away was not distinguishable, and the win
// percentage did not say whose it was or who was favoured.
func TestFeaturedCardNamesItsSidesAndItsPercentage(t *testing.T) {
	viewerAtHome := featuredMatchupData(map[string]any{
		"has_matchup":   true,
		"is_viewer":     true,
		"live_state":    "LIVE",
		"mine_is_home":  true,
		"win_prob":      "62%",
		"win_prob_team": "KP",
		"mine":          map[string]any{"id": "team-1", "abbreviation": "KP"},
		"theirs":        map[string]any{"id": "team-2", "abbreviation": "SC"},
	})
	if !viewerAtHome.MineIsHome {
		t.Error("MineIsHome = false for a viewer hosting the matchup")
	}
	if viewerAtHome.WinProbTeam != "KP" {
		t.Errorf("WinProbTeam = %q, want the team the percentage describes", viewerAtHome.WinProbTeam)
	}

	// The viewer on the road: the same card must flip the labels, not
	// assume "mine" is always home.
	viewerAway := featuredMatchupData(map[string]any{
		"has_matchup":   true,
		"is_viewer":     true,
		"live_state":    "LIVE",
		"mine_is_home":  false,
		"win_prob":      "38%",
		"win_prob_team": "SC",
		"mine":          map[string]any{"id": "team-2", "abbreviation": "SC"},
		"theirs":        map[string]any{"id": "team-1", "abbreviation": "KP"},
	})
	if viewerAway.MineIsHome {
		t.Error("MineIsHome = true for a viewer on the road")
	}
	if viewerAway.WinProbTeam != "SC" {
		t.Errorf("WinProbTeam = %q, want the viewer's own team", viewerAway.WinProbTeam)
	}

	// An empty week carries neither claim rather than defaulting to one.
	empty := featuredMatchupData(map[string]any{"has_matchup": false})
	if empty.MineIsHome || empty.WinProbTeam != "" {
		t.Errorf("empty card asserted a side: MineIsHome=%v WinProbTeam=%q", empty.MineIsHome, empty.WinProbTeam)
	}
}

// TestScorebugAttributesItsPercentage: the around-the-league card reports
// the HOME side's probability, so it has to name that side.
func TestScorebugAttributesItsPercentage(t *testing.T) {
	got := matchupsPageScorebugs([]map[string]any{{
		"id":            "m-2",
		"live_state":    "LIVE",
		"win_prob_home": "55%",
		"win_prob_team": "NP",
		"away":          map[string]any{"id": "team-3", "abbreviation": "GC"},
		"home":          map[string]any{"id": "team-4", "abbreviation": "NP"},
	}})
	if len(got) != 1 {
		t.Fatalf("got %d scorebugs, want 1", len(got))
	}
	if got[0].WinProbTeam != "NP" {
		t.Errorf("WinProbTeam = %q, want the home side's abbreviation", got[0].WinProbTeam)
	}
	if got[0].WinProbHome != "55%" {
		t.Errorf("WinProbHome = %q", got[0].WinProbHome)
	}
}

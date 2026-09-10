package matchups

import "testing"

// TestStarterStateClassReadsAResultAsFinal covers the chip class after
// starterGameState learned to render a finished game as its own result
// ("W 27-20") instead of the bare word FINAL: both forms must still get
// the final treatment, and no other state text may be mistaken for one.
func TestStarterStateClassReadsAResultAsFinal(t *testing.T) {
	cases := []struct {
		gameState string
		want      string
	}{
		{gameState: "W 27-20", want: "state--final"},
		{gameState: "L 20-27", want: "state--final"},
		{gameState: "T 20-20", want: "state--final"},
		{gameState: "W 24-0", want: "state--final"},
		{gameState: "FINAL", want: "state--final"},
		{gameState: "Q3 8:12", want: "state--live"},
		{gameState: "OT 1:20", want: "state--live"},
		{gameState: "SUN 4:25 PM", want: "state--pre"},
		{gameState: "THU 8:20 PM", want: "state--pre"},
		{gameState: "TUE 7:00 PM", want: "state--pre"},
		{gameState: "WED 8:20 PM", want: "state--pre"},
		{gameState: "BYE", want: "state--pre"},
		{gameState: "", want: "state--pre"},
	}
	for _, tc := range cases {
		t.Run(tc.gameState, func(t *testing.T) {
			if got := starterStateClass(tc.gameState); got != tc.want {
				t.Fatalf("starterStateClass(%q) = %q, want %q", tc.gameState, got, tc.want)
			}
		})
	}
}

// TestScorebugCarriesItsFocusLink proves the around-the-league card's own
// link to the full-width view survives the strict-component conversion.
func TestScorebugCarriesItsFocusLink(t *testing.T) {
	got := matchupsPageScorebugs([]map[string]any{{
		"id":         "m-2",
		"live_state": "LIVE",
		"focus_href": "/matchups?week=3&m=m-2",
		"away":       map[string]any{"id": "team-3"},
		"home":       map[string]any{"id": "team-4"},
	}})
	if len(got) != 1 {
		t.Fatalf("matchupsPageScorebugs returned %d entries, want 1", len(got))
	}
	if got[0].FocusHref != "/matchups?week=3&m=m-2" {
		t.Fatalf("FocusHref = %q, want %q", got[0].FocusHref, "/matchups?week=3&m=m-2")
	}
}

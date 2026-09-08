package league

import "testing"

// TestDraftPoolStatusMapSimplifiesCachedLabel is J1 F33's own evidence
// (owner debrief 2026-09-06): the draft room's own cached-pool banner
// showed the shared, jargon label ("CACHED SNAPSHOT") and a longer
// technical sentence, clipped on a phone. draftPoolStatusMap overrides
// only the "cached" state with the room's own plain sentence, leaving
// every other state (and every other field on the map) untouched — the
// shared poolFreshnessMap/playerPoolStateLabel stay exactly as /players
// and /board still read them.
func TestDraftPoolStatusMapSimplifiesCachedLabel(t *testing.T) {
	in := map[string]any{
		"state":            "cached",
		"label":            "CACHED SNAPSHOT",
		"detail":           "A saved player-data snapshot is serving this page while the next refresh is pending. Rankings, Big Board work, and draft actions remain available.",
		"live":             false,
		"has_notice":       true,
		"has_last_success": true,
	}
	out := draftPoolStatusMap(in)
	if got, want := out["label"], "PLAYER DATA"; got != want {
		t.Errorf("label = %v, want %v", got, want)
	}
	if got, want := out["detail"], "Player data is a saved copy. Picks are live."; got != want {
		t.Errorf("detail = %v, want %v", got, want)
	}
	// Every other field carries through unchanged.
	if out["live"] != false || out["has_notice"] != true || out["has_last_success"] != true {
		t.Errorf("unrelated fields changed: %+v", out)
	}
}

// TestDraftPoolStatusMapLeavesOtherStatesUntouched proves the override's
// own narrow scope: every state besides "cached" passes through byte for
// byte, including the label/detail this wave has no evidence to rewrite.
func TestDraftPoolStatusMapLeavesOtherStatesUntouched(t *testing.T) {
	for _, state := range []string{"live", "stale", "degraded", "offline", "unavailable"} {
		in := map[string]any{"state": state, "label": "SOME LABEL", "detail": "some detail"}
		out := draftPoolStatusMap(in)
		if out["label"] != "SOME LABEL" || out["detail"] != "some detail" {
			t.Errorf("state %q was rewritten: %+v", state, out)
		}
	}
}

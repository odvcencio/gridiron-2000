package draft

import (
	"strings"
	"testing"
)

// TestDraftRoomStatusIsTruthfulBeforeTheDraftStarts is item 3's own
// regression test (comb — oleander, 2026-09-02 audit) for the visually-
// hidden aria-live region draftRoomStatus builds (DraftRoom and
// DraftCommandBar, page.gsx, share this one string). Before this fix,
// draftRoomStatus ignored data.draft.started entirely and always read
// "Pick %d; %s on the clock; ...; %s." — pre-draft that announced "Pick
// 1; <team abbreviation> on the clock; 4 of 8 ready; the clock is not
// running" in the SAME render where the command pill correctly said
// "DRAFT NOT STARTED." A screen-reader visitor heard a live draft that
// was not actually happening.
func TestDraftRoomStatusIsTruthfulBeforeTheDraftStarts(t *testing.T) {
	data := map[string]any{
		"draft": map[string]any{
			"complete": false,
			"started":  false,
			"date":     "SUN · SEP 6",
			"time":     "4:05 PM EDT",
		},
		"on_clock":      map[string]any{"abbreviation": "OR2"},
		"clock":         map[string]any{"paused": false, "armed": false},
		"pick_number":   1,
		"ready_count":   4,
		"manager_count": 8,
	}
	got := draftRoomStatus(data)
	for _, forbidden := range []string{"Pick 1", "on the clock", "the clock is not running"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("pre-draft status %q must not claim a pick or a clock is in progress (found %q)", got, forbidden)
		}
	}
	for _, want := range []string{"Draft not started", "4 of 8 ready", "opens SUN · SEP 6 at 4:05 PM EDT"} {
		if !strings.Contains(got, want) {
			t.Errorf("pre-draft status %q missing %q", got, want)
		}
	}
}

// TestDraftRoomStatusStillReportsTheClockOnceStarted pins the fix's own
// positive case: once the draft is running, the SAME function must keep
// reporting the real pick/team/clock state — the fix must not silence
// this region once the draft is actually under way.
func TestDraftRoomStatusStillReportsTheClockOnceStarted(t *testing.T) {
	data := map[string]any{
		"draft": map[string]any{
			"complete": false,
			"started":  true,
		},
		"on_clock":      map[string]any{"name": "Los Delfines del Norte", "abbreviation": "OR2"},
		"clock":         map[string]any{"paused": false, "armed": true},
		"pick_number":   5,
		"ready_count":   8,
		"manager_count": 8,
	}
	got := draftRoomStatus(data)
	// F8 (J2, spruce audit, 2026-09-04): the sentence names the team, not
	// the internal seat code — a screen-reader listener should hear
	// "Los Delfines del Norte on the clock", never "OR2 on the clock".
	for _, want := range []string{"Pick 5", "Los Delfines del Norte on the clock", "8 of 8 ready", "clock running"} {
		if !strings.Contains(got, want) {
			t.Errorf("in-progress status %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "Draft not started") {
		t.Errorf("in-progress status %q must not claim the draft has not started", got)
	}
	if strings.Contains(got, "OR2") {
		t.Errorf("in-progress status %q leaks the internal seat abbreviation, want the team name only", got)
	}
}

// TestDraftRoomStatusReportsCompleteRegardlessOfStarted pins the
// existing complete-draft branch, which already short-circuits before
// reaching the started check — unaffected by this item's fix, but worth
// pinning so a future edit cannot silently reorder the two checks.
func TestDraftRoomStatusReportsCompleteRegardlessOfStarted(t *testing.T) {
	data := map[string]any{
		"draft":       map[string]any{"complete": true, "started": true},
		"pick_number": 120,
	}
	got := draftRoomStatus(data)
	if !strings.Contains(got, "Draft complete") || !strings.Contains(got, "120 picks locked") {
		t.Errorf("complete status = %q, want the complete-draft sentence", got)
	}
}

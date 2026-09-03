package league

import (
	"net/http"
	"testing"
)

// TestPreDraftSurfacesAgreeTheDraftHasNotStarted is item 3's own
// regression test (comb — oleander, 2026-09-02 audit). Before this fix,
// three surfaces disagreed with the command pill's own truthful "DRAFT
// NOT STARTED" text: the board's cell 1.01 painted "on the clock," and
// the commissioner drawer's per-seat badge painted "ON CLOCK" — both
// because their own OnClock/on_clock fields read only "!complete &&
// number == next" / "team.ID == onClockID," with no check that the
// draft had actually started. nextNumber is always pick 1's own team
// before the first pick, the same number pick 1 legitimately holds once
// the draft is running, so pre-draft these fields could never tell the
// two states apart.
func TestPreDraftSurfacesAgreeTheDraftHasNotStarted(t *testing.T) {
	// newEventTestService (draft_events_test.go) starts the draft as part
	// of its own setup (startTestDraft), so this test builds its own
	// pre-draft fixture instead — the same steps minus that one call.
	service := newTestService(t, false)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(150), 1, "live" })
	member, err := service.AssignManager("pick@example.com", "Pick Test")
	if err != nil {
		t.Fatal(err)
	}
	order := []string{member.TeamID}
	for _, team := range service.Teams() {
		if team.ID != member.TeamID {
			order = append(order, team.ID)
		}
	}
	if err := service.store.SetDraftOrder(order); err != nil {
		t.Fatal(err)
	}
	state := service.store.Snapshot()
	if state.DraftStarted {
		t.Fatal("test setup: the draft must not be started yet")
	}

	history := service.DraftHistory(state, "")
	firstCell := history.Board.Rows[0].Cells[0]
	if firstCell.Number != 1 {
		t.Fatalf("test setup: board cell 0,0 is pick %d, want pick 1", firstCell.Number)
	}
	if firstCell.OnClock {
		t.Errorf("board cell %s claims OnClock pre-draft: %+v", firstCell.Label, firstCell)
	}

	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	data := service.DraftData(request)
	teams, ok := data["teams"].([]map[string]any)
	if !ok || len(teams) == 0 {
		t.Fatalf("no teams in draft data: %#v", data["teams"])
	}
	for _, team := range teams {
		if onClock, _ := team["on_clock"].(bool); onClock {
			t.Errorf("team %v claims on_clock pre-draft: %+v", team["id"], team)
		}
	}
}

// TestPreDraftSurfacesShowOnClockOnceTheDraftStarts is the fix's own
// positive case: once the draft is running, the SAME two fields must
// correctly identify the team on the clock — the fix must not simply
// suppress OnClock/on_clock everywhere, only before the room opens.
func TestPreDraftSurfacesShowOnClockOnceTheDraftStarts(t *testing.T) {
	// newEventTestService already starts the draft as part of its own
	// setup (startTestDraft) — this is exactly the "running" state the
	// pre-draft test above deliberately avoids.
	service, _, _ := newEventTestService(t)
	state := service.store.Snapshot()
	if !state.DraftStarted {
		t.Fatal("test setup: the draft must already be started")
	}
	onClockID := teamOnClock(state.DraftOrder, 1)

	history := service.DraftHistory(state, "")
	firstCell := history.Board.Rows[0].Cells[0]
	if !firstCell.OnClock {
		t.Errorf("board cell %s must claim OnClock once the draft has started: %+v", firstCell.Label, firstCell)
	}

	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	data := service.DraftData(request)
	teams, ok := data["teams"].([]map[string]any)
	if !ok || len(teams) == 0 {
		t.Fatalf("no teams in draft data: %#v", data["teams"])
	}
	found := false
	for _, team := range teams {
		onClock, _ := team["on_clock"].(bool)
		id, _ := team["id"].(string)
		if id == onClockID {
			found = true
			if !onClock {
				t.Errorf("team %s (on the clock) must have on_clock=true once the draft has started: %+v", id, team)
			}
		} else if onClock {
			t.Errorf("team %s is not on the clock but on_clock=true: %+v", id, team)
		}
	}
	if !found {
		t.Fatalf("the on-clock team %q was not present in the team list", onClockID)
	}
}

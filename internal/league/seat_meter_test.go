package league

import (
	"strconv"
	"testing"
)

// TestSeatMeterDataMarksTakenAndOpenSeatsByText is gap-audit item 8: the
// meter must mark each seat taken/open with text (not colour alone) and
// carry an aria_label with the count of open seats.
func TestSeatMeterDataMarksTakenAndOpenSeatsByText(t *testing.T) {
	svc := newTestService(t, false)
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 2 {
		t.Fatal("fixture league needs at least two teams")
	}
	if _, err := svc.AssignManager("seat-meter-render@example.com", "Seat Meter Render"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	attention := svc.SeatMeterData()
	seats, ok := attention["seats"].([]map[string]any)
	if !ok {
		t.Fatalf("seats = %#v, want []map[string]any", attention["seats"])
	}
	if len(seats) != len(teamIDs) {
		t.Fatalf("len(seats) = %d, want %d (one pill per configured team)", len(seats), len(teamIDs))
	}

	takenCount, openCount := 0, 0
	for _, seat := range seats {
		taken, _ := seat["taken"].(bool)
		status, _ := seat["status"].(string)
		label, _ := seat["label"].(string)
		if taken {
			takenCount++
			if status != "TAKEN" {
				t.Errorf("taken seat status = %q, want TAKEN: %#v", status, seat)
			}
			if label == "" || label == "Seat "+seat["number"].(string)+": open" {
				t.Errorf("taken seat carries no distinguishing text label: %#v", seat)
			}
		} else {
			openCount++
			if status != "OPEN" {
				t.Errorf("open seat status = %q, want OPEN: %#v", status, seat)
			}
		}
	}
	if takenCount != 1 {
		t.Fatalf("takenCount = %d, want exactly 1 (one AssignManager call)", takenCount)
	}
	if openCount != len(teamIDs)-1 {
		t.Fatalf("openCount = %d, want %d", openCount, len(teamIDs)-1)
	}
	if got := attention["open_count"]; got != openCount {
		t.Fatalf("open_count = %v, want %d", got, openCount)
	}
	if got := attention["total_count"]; got != len(teamIDs) {
		t.Fatalf("total_count = %v, want %d", got, len(teamIDs))
	}
	wantLabel := strconv.Itoa(openCount) + " of " + strconv.Itoa(len(teamIDs)) + " seats open"
	if got, _ := attention["aria_label"].(string); got != wantLabel {
		t.Fatalf("aria_label = %q, want %q", got, wantLabel)
	}
}

// TestSeatMeterDataAllSeatsOpen is the honest-empty-league counterpart:
// with no claimed seats, every pill reads OPEN and the aria_label counts
// every configured seat as open.
func TestSeatMeterDataAllSeatsOpen(t *testing.T) {
	svc := newTestService(t, false)
	teamIDs := defaultTeamIDs()
	attention := svc.SeatMeterData()
	if got := attention["open_count"]; got != len(teamIDs) {
		t.Fatalf("open_count = %v, want %d (no seats claimed)", got, len(teamIDs))
	}
	seats := attention["seats"].([]map[string]any)
	for _, seat := range seats {
		if taken, _ := seat["taken"].(bool); taken {
			t.Fatalf("seat marked taken with no members claimed: %#v", seat)
		}
	}
}

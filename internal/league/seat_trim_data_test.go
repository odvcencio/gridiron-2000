package league

import (
	"net/http"
	"testing"
	"time"
)

// TestAdminDataCountsUnclaimedSeats pins the two fields the commissioner
// console's seat-trim control reads. The control is the T-1hr action the
// league runs an hour before the draft: it drops every seat nobody claimed
// and locks the league to its claimed team count.
//
// The count has to be right for a reason beyond tidiness. An unclaimed seat
// is still in DraftOrder, so teamOnClock lands on it, it runs the full pick
// clock down with nobody there to pick, and then autopicks. Across a
// 17-round draft that is a long stall repeated every round. A commissioner
// deciding whether to trim needs the console to state exactly how many
// seats are affected before they commit.
//
// claimedSeatIDs treats a seat as claimed for a primary manager or a
// co-manager, which is the same rule Store.TrimUnclaimedSeats drops on, so
// this count and that action always agree.
func TestAdminDataCountsUnclaimedSeats(t *testing.T) {
	service := newTestService(t, true)

	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	data := service.AdminData(request)

	seatCount, ok := data["seat_count"].(int)
	if !ok {
		t.Fatalf("seat_count missing or not an int: %T", data["seat_count"])
	}
	if got, want := data["unclaimed_seat_count"], seatCount; got != want {
		t.Errorf("unclaimed_seat_count = %v with no seats claimed, want %v", got, want)
	}
	if data["has_unclaimed_seats"] != true {
		t.Errorf("has_unclaimed_seats = %v with no seats claimed, want true", data["has_unclaimed_seats"])
	}

	admitSeatlessForClaim(t, service, "a@example.com")
	if _, err := service.claimFantasySeat("a@example.com", "A", "First Claim", "wolf"); err != nil {
		t.Fatalf("claimFantasySeat: %v", err)
	}

	data = service.AdminData(request)
	if got, want := data["unclaimed_seat_count"], seatCount-1; got != want {
		t.Errorf("unclaimed_seat_count = %v after one claim, want %v", got, want)
	}
	if data["has_unclaimed_seats"] != true {
		t.Errorf("has_unclaimed_seats = %v with seats still open, want true", data["has_unclaimed_seats"])
	}
}

// TestAdminDataLocksSeatTrimOnceDraftStarts is the companion to
// app/admin's TestAdminPageOffersSeatTrimBeforeTheDraft. The console hides
// the seat-trim control on this flag once the first pick lands, because
// Store.TrimUnclaimedSeats rejects a late trim outright ("seats lock once
// the draft starts"). Offering a commissioner a button whose only possible
// outcome is an error is worst during a live draft, which is the one time
// they are under pressure.
//
// This lives here rather than in app/admin because league.Default() is a
// sync.Once singleton: a second render in that package cannot be handed
// different state. Driving the store directly is the only way to reach the
// after-first-pick case.
func TestAdminDataLocksSeatTrimOnceDraftStarts(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	if got := service.AdminData(request)["draft_started"]; got != false {
		t.Fatalf("draft_started = %v before any pick, want false", got)
	}

	teamID := service.Teams()[0].ID
	startTestDraft(t, service.store)
	if _, err := service.store.MakePick(teamID, "test-player-1", "manager", service.clock(), time.Time{}); err != nil {
		t.Fatalf("store.MakePick: %v", err)
	}

	if got := service.AdminData(request)["draft_started"]; got != true {
		t.Errorf("draft_started = %v after a pick landed, want true", got)
	}
	if _, _, err := service.TrimUnclaimedSeats(request); err == nil {
		t.Errorf("TrimUnclaimedSeats succeeded after the draft started, want it rejected")
	}
}

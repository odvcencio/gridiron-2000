package league

import (
	"net/http"
	"testing"
	"time"
)

// newEventTestService seats one manager first in draft order, gives the
// presence tracker a real start instant, opens the draft, and captures every
// sink call.
func newEventTestService(t *testing.T) (*Service, *[]DraftEvent, Member) {
	t.Helper()
	service := newTestService(t, false)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(150), 1, "live" })
	service.presence = newPresenceTracker(time.Now())
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
	if err := service.store.SetDraftOrder(order); err != nil { // store.go:1779
		t.Fatal(err)
	}
	startTestDraft(t, service.store) // store_test.go:24: DraftStarted, clock cleared
	var events []DraftEvent
	service.SetDraftEventSink(func(event DraftEvent) { events = append(events, event) })
	return service, &events, member
}

func pickRequest(t *testing.T, email string) *http.Request {
	return authenticatedJourneyRequest(t, email, "Pick Test", "/draft")
}

func TestMakePickEmitsExactlyOneDraftPick(t *testing.T) {
	service, events, member := newEventTestService(t)
	if _, _, _, err := service.MakePick(pickRequest(t, member.Email), member.TeamID, "pool-001"); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 1 || (*events)[0].Name != "draft:pick" || (*events)[0].Generation != 1 {
		t.Fatalf("events = %+v, want one draft:pick", *events)
	}
	payload := (*events)[0].Payload
	if payload["number"] != 1 || payload["player_id"] != "pool-001" || payload["team_id"] != member.TeamID {
		t.Fatalf("payload = %v", payload)
	}
	if cell := payload["cell"].(map[string]any)["1"].(map[string]any); cell["1"] != "Pool Player 001" {
		t.Fatalf("cell binds = %v", cell)
	}
	if taken := payload["player"].(map[string]any)["pool-001"].(map[string]any); taken["taken"] != true {
		t.Fatalf("player binds = %v", taken)
	}
	if _, ok := payload["clock"].(map[string]any)["effective_deadline"]; !ok {
		t.Fatal("payload lacks clock.effective_deadline")
	}
}

func TestRejectedPickEmitsNothing(t *testing.T) {
	service, events, member := newEventTestService(t)
	if _, _, _, err := service.MakePick(pickRequest(t, member.Email), member.TeamID, "no-such-player"); err == nil {
		t.Fatal("expected a rejection")
	}
	if len(*events) != 0 {
		t.Fatalf("events = %+v, want none", *events)
	}
}

func TestSeatAndClockEventsCarryTheStateVocabulary(t *testing.T) {
	service, events, member := newEventTestService(t)
	if _, _, err := service.ToggleReady(pickRequest(t, member.Email), member.TeamID); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 1 || (*events)[0].Name != "draft:seat" || (*events)[0].Payload["ready"] != true {
		t.Fatalf("events = %+v", *events)
	}
	fixed := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixed }
	// A seat nobody has heartbeated is NOT SEEN and effectiveDeadline caps it
	// at armAt+20s (draftclock.go:198-247); a tracker started now sits inside
	// the boot grace, and one heartbeat keeps the full deadline either way.
	service.presence = newPresenceTracker(fixed)
	service.RecordPresence(pickRequest(t, member.Email), fixed)
	if err := service.store.ArmClock(fixed.Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	service.emitDraftClock(service.store.Snapshot())
	last := (*events)[len(*events)-1]
	if last.Name != "draft:clock" || last.Payload["state"] != "RUNNING" || last.Payload["deadline"] != "2026-09-06T17:01:30Z" || last.Payload["effective_deadline"] != "2026-09-06T17:01:30Z" {
		t.Fatalf("clock event = %+v", last)
	}
	if last.Payload["duration_sec"] != int(DefaultPickClock.Seconds()) || last.Payload["remaining_sec"] != 90 || last.Payload["remaining_label"] != "1:30" {
		t.Fatalf("clock fields = %+v", last.Payload)
	}
}

func TestPresenceEmitsSeatOnlyOnStateChange(t *testing.T) {
	service, events, member := newEventTestService(t)
	now := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.presence = newPresenceTracker(now.Add(-time.Hour))
	service.RecordPresence(pickRequest(t, member.Email), now)
	service.RecordPresence(pickRequest(t, member.Email), now.Add(4*time.Second))
	// The socket also carries draft:clock (the first clockTick re-arms the
	// cleared clock, draftclock.go:136), so compare only the seat events.
	seats := func() []DraftEvent {
		var out []DraftEvent
		for _, event := range *events {
			if event.Name == "draft:seat" {
				out = append(out, event)
			}
		}
		return out
	}
	if got := seats(); len(got) != 1 || got[0].Payload["presence"] != "here" {
		t.Fatalf("seat events = %+v, want one draft:seat here", got)
	}
	// Time passing moves HERE to IDLE with no heartbeat; clockTick emits the
	// transition once and never again while the state holds.
	service.now = func() time.Time { return now.Add(30 * time.Second) }
	service.clockTick(service.clock())
	service.clockTick(service.clock())
	if got := seats(); len(got) != 2 || got[1].Payload["presence"] != "idle" {
		t.Fatalf("seat events = %+v, want one idle transition", got)
	}
}

func TestClockViewCarriesDurationLabel(t *testing.T) {
	service := newTestService(t, false)
	want := countdownMMSSLabel(int(DefaultPickClock.Seconds()))
	if got := service.clockView(service.store.Snapshot(), time.Now())["duration_label"]; got != want {
		t.Fatalf("duration_label = %v, want %s", got, want)
	}
}

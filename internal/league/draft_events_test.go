package league

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// draftEventRecorder collects every event a test's sink receives. The
// dispatch queue (draft_events.go's draftEventDrain) delivers off the
// caller's own goroutine, so a test that reads events while the drain
// goroutine might still be writing needs its own synchronization — a plain
// slice written by one goroutine and read by another is a race even when
// only one side ever writes concurrently with reads.
type draftEventRecorder struct {
	mu     sync.Mutex
	events []DraftEvent
}

func (r *draftEventRecorder) record(event DraftEvent) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *draftEventRecorder) snapshot() []DraftEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]DraftEvent(nil), r.events...)
}

func (r *draftEventRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// filterDraftEvents returns the events in events named name, in the order
// they were recorded.
func filterDraftEvents(events []DraftEvent, name string) []DraftEvent {
	var out []DraftEvent
	for _, event := range events {
		if event.Name == name {
			out = append(out, event)
		}
	}
	return out
}

// waitForEvents polls r until it holds at least n events or 2s elapses.
// Every assertion against the recorder goes through this first: the sink
// dispatch is asynchronous, so a synchronous read right after a service
// call would race the drain goroutine rather than observe its result.
func waitForEvents(t *testing.T, r *draftEventRecorder, n int) []DraftEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if events := r.snapshot(); len(events) >= n {
			return events
		}
		if time.Now().After(deadline) {
			t.Fatalf("events = %+v, want at least %d within 2s", r.snapshot(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// assertEventCount waits for at least want events, then settles briefly and
// checks no more arrived: the async-dispatch equivalent of the old
// synchronous "len(events) != want" check.
func assertEventCount(t *testing.T, r *draftEventRecorder, want int) []DraftEvent {
	t.Helper()
	events := waitForEvents(t, r, want)
	time.Sleep(50 * time.Millisecond)
	if got := r.len(); got != want {
		t.Fatalf("events = %+v, want exactly %d", r.snapshot(), want)
	}
	return events
}

// waitForNamedCount is assertEventCount scoped to one event name, mirroring
// the seats() closure the presence test used before the recorder existed.
func waitForNamedCount(t *testing.T, r *draftEventRecorder, name string, want int) []DraftEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var matched []DraftEvent
	for {
		matched = filterDraftEvents(r.snapshot(), name)
		if len(matched) >= want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s events = %+v, want at least %d within 2s", name, matched, want)
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if matched = filterDraftEvents(r.snapshot(), name); len(matched) != want {
		t.Fatalf("%s events = %+v, want exactly %d", name, matched, want)
	}
	return matched
}

// waitForDraftComplete polls for the one draft:state event whose complete
// field is true, or fails after 2s.
func waitForDraftComplete(t *testing.T, r *draftEventRecorder) DraftEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, event := range r.snapshot() {
			if event.Name == "draft:state" && event.Payload["complete"] == true {
				return event
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no draft:state{complete:true} arrived within 2s")
		}
		time.Sleep(time.Millisecond)
	}
}

// newEventTestService seats one manager first in draft order, gives the
// presence tracker a real start instant, opens the draft, and captures every
// sink call in a draftEventRecorder.
func newEventTestService(t *testing.T) (*Service, *draftEventRecorder, Member) {
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
	recorder := &draftEventRecorder{}
	service.SetDraftEventSink(recorder.record)
	t.Cleanup(service.StopDraftEvents)
	return service, recorder, member
}

// newFullySeatedEventTestService is newEventTestService's multi-seat
// sibling: every seat has a claimed manager, so any team can act for
// itself. Tests that complete a draft, or contend across seats, start here.
func newFullySeatedEventTestService(t *testing.T) (*Service, *draftEventRecorder, map[string]*http.Request) {
	t.Helper()
	service := newTestService(t, false)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(150), 1, "live" })
	service.presence = newPresenceTracker(time.Now())
	requests := make(map[string]*http.Request, len(service.Teams()))
	for _, team := range service.Teams() {
		email := strings.ToLower(team.ID) + "@example.com"
		if _, err := service.AssignManager(email, team.ID); err != nil {
			t.Fatal(err)
		}
		requests[team.ID] = pickRequest(t, email)
	}
	startTestDraft(t, service.store)
	recorder := &draftEventRecorder{}
	service.SetDraftEventSink(recorder.record)
	t.Cleanup(service.StopDraftEvents)
	return service, recorder, requests
}

func pickRequest(t *testing.T, email string) *http.Request {
	return authenticatedJourneyRequest(t, email, "Pick Test", "/draft")
}

func TestMakePickEmitsExactlyOneDraftPick(t *testing.T) {
	service, recorder, member := newEventTestService(t)
	if _, _, _, err := service.MakePick(pickRequest(t, member.Email), member.TeamID, "pool-001"); err != nil {
		t.Fatal(err)
	}
	events := assertEventCount(t, recorder, 1)
	if events[0].Name != "draft:pick" || events[0].Generation != 1 {
		t.Fatalf("events = %+v, want one draft:pick", events)
	}
	payload := events[0].Payload
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
	service, recorder, member := newEventTestService(t)
	if _, _, _, err := service.MakePick(pickRequest(t, member.Email), member.TeamID, "no-such-player"); err == nil {
		t.Fatal("expected a rejection")
	}
	time.Sleep(50 * time.Millisecond)
	if got := recorder.len(); got != 0 {
		t.Fatalf("events = %+v, want none", recorder.snapshot())
	}
}

func TestSeatAndClockEventsCarryTheStateVocabulary(t *testing.T) {
	service, recorder, member := newEventTestService(t)
	if _, _, err := service.ToggleReady(pickRequest(t, member.Email), member.TeamID); err != nil {
		t.Fatal(err)
	}
	events := assertEventCount(t, recorder, 1)
	if events[0].Name != "draft:seat" || events[0].Payload["ready"] != true {
		t.Fatalf("events = %+v", events)
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
	events = waitForEvents(t, recorder, 3) // ToggleReady's seat, RecordPresence's seat, emitDraftClock
	time.Sleep(50 * time.Millisecond)
	events = recorder.snapshot()
	last := events[len(events)-1]
	if last.Name != "draft:clock" || last.Payload["state"] != "RUNNING" || last.Payload["deadline"] != "2026-09-06T17:01:30Z" || last.Payload["effective_deadline"] != "2026-09-06T17:01:30Z" {
		t.Fatalf("clock event = %+v", last)
	}
	if last.Payload["duration_sec"] != int(DefaultPickClock.Seconds()) || last.Payload["remaining_sec"] != 90 || last.Payload["remaining_label"] != "1:30" {
		t.Fatalf("clock fields = %+v", last.Payload)
	}
}

func TestPresenceEmitsSeatOnlyOnStateChange(t *testing.T) {
	service, recorder, member := newEventTestService(t)
	now := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.presence = newPresenceTracker(now.Add(-time.Hour))
	service.RecordPresence(pickRequest(t, member.Email), now)
	service.RecordPresence(pickRequest(t, member.Email), now.Add(4*time.Second))
	// The socket also carries draft:clock (the first clockTick re-arms the
	// cleared clock, draftclock.go:136), so compare only the seat events.
	waitForNamedCount(t, recorder, "draft:seat", 1)
	seats := filterDraftEvents(recorder.snapshot(), "draft:seat")
	if len(seats) != 1 || seats[0].Payload["presence"] != "here" {
		t.Fatalf("seat events = %+v, want one draft:seat here", seats)
	}
	// Time passing moves HERE to IDLE with no heartbeat; clockTick emits the
	// transition once and never again while the state holds.
	service.now = func() time.Time { return now.Add(30 * time.Second) }
	service.clockTick(service.clock())
	service.clockTick(service.clock())
	seats = waitForNamedCount(t, recorder, "draft:seat", 2)
	if seats[1].Payload["presence"] != "idle" {
		t.Fatalf("seat events = %+v, want one idle transition", seats)
	}
}

func TestClockViewCarriesDurationLabel(t *testing.T) {
	service := newTestService(t, false)
	want := countdownMMSSLabel(int(DefaultPickClock.Seconds()))
	if got := service.clockView(service.store.Snapshot(), time.Now())["duration_label"]; got != want {
		t.Fatalf("duration_label = %v, want %s", got, want)
	}
}

// TestLastPickCompletesTheDraftExactlyOnce proves maybeEmitDraftComplete:
// Store.MakePick already zeroes the clock fields on the pick that completes
// the draft, which used to mean clockTick's own leftover-clock-clear branch
// (the only place draft:state{complete:true} ever fired) had nothing left
// to clear and never ran. A one-round, one-slot roster keeps the fixture to
// exactly len(defaultTeams()) picks.
func TestLastPickCompletesTheDraftExactlyOnce(t *testing.T) {
	t.Cleanup(clearRosterShape)
	setRosterShape(RosterPreset{Name: "complete-fixture", Slots: map[string]int{}, Bench: 1})
	service, recorder, requests := newFullySeatedEventTestService(t)
	teams := service.Teams()
	for made := 0; made < len(teams); made++ {
		state := service.store.Snapshot()
		teamID := teamOnClock(state.DraftOrder, len(state.Picks)+1)
		playerID := fmt.Sprintf("pool-%03d", made+1)
		if _, _, _, err := service.MakePick(requests[teamID], teamID, playerID); err != nil {
			t.Fatalf("pick %d for %s: %v", made+1, teamID, err)
		}
	}
	completion := waitForDraftComplete(t, recorder)
	if completion.Payload["started"] != true {
		t.Fatalf("completion event = %+v, want started true", completion)
	}
	time.Sleep(50 * time.Millisecond)
	count := 0
	for _, event := range recorder.snapshot() {
		if event.Name == "draft:state" && event.Payload["complete"] == true {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("draft:state complete events = %d, want exactly 1", count)
	}
	if !draftComplete(service.store.Snapshot()) {
		t.Fatal("the store does not report the draft complete")
	}
}

// TestConcurrentPicksAndTickerEmitIncreasingGenerations drives the two real
// call sites that race in production — a manager's MakePick from an HTTP
// goroutine and the enforcement loop's clockTick from a ticker goroutine —
// concurrently against every seat, and proves every event the recorder
// observed still arrived in strictly increasing generation order. That
// order is emitDraft's core guarantee (draftEmitMu serializes "assign the
// generation" with "push onto the queue"), and the one thing a plain
// unsynchronized sink call could not have promised once dispatch moved off
// the caller's own goroutine.
func TestConcurrentPicksAndTickerEmitIncreasingGenerations(t *testing.T) {
	t.Cleanup(clearRosterShape)
	setRosterShape(RosterPreset{Name: "order-fixture", Slots: map[string]int{}, Bench: 1})
	service, recorder, requests := newFullySeatedEventTestService(t)
	teams := service.Teams()
	playerFor := make(map[string]string, len(teams))
	for index, team := range teams {
		playerFor[team.ID] = fmt.Sprintf("pool-%03d", index+1)
	}

	stop := make(chan struct{})
	var ticking sync.WaitGroup
	ticking.Add(1)
	go func() {
		defer ticking.Done()
		for {
			select {
			case <-stop:
				return
			default:
				service.clockTick(service.clock())
				time.Sleep(time.Millisecond)
			}
		}
	}()

	var picking sync.WaitGroup
	for _, team := range teams {
		teamID := team.ID
		picking.Add(1)
		go func() {
			defer picking.Done()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				state := service.store.Snapshot()
				if draftComplete(state) {
					return
				}
				if teamOnClock(state.DraftOrder, len(state.Picks)+1) != teamID {
					time.Sleep(time.Millisecond)
					continue
				}
				if _, _, _, err := service.MakePick(requests[teamID], teamID, playerFor[teamID]); err == nil {
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Errorf("%s never landed its pick", teamID)
		}()
	}
	picking.Wait()
	close(stop)
	ticking.Wait()

	waitForDraftComplete(t, recorder)
	time.Sleep(50 * time.Millisecond)
	events := recorder.snapshot()
	var last uint64
	for _, event := range events {
		if event.Generation <= last {
			t.Fatalf("generations out of order at %+v (previous %d)", event, last)
		}
		last = event.Generation
	}
	picks := filterDraftEvents(events, "draft:pick")
	if len(picks) != len(teams) {
		t.Fatalf("draft:pick events = %d, want %d", len(picks), len(teams))
	}
}

// TestAdminSetReadyIsANoOpWhenAlreadySet proves the no-op guard shared by
// AdminSetClockSeconds, AdminSetAutopick, and AdminSetReady: setting a seat
// to the value it already holds must emit nothing, so a commissioner who
// double-submits a form does not fan out a spurious draft:seat and its
// generation bump to every connected room.
func TestAdminSetReadyIsANoOpWhenAlreadySet(t *testing.T) {
	service, recorder, member := newEventTestService(t)
	t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")
	commish := authenticatedJourneyRequest(t, "boss@example.com", "Commissioner", "/admin")
	if err := service.AdminSetReady(commish, member.TeamID, true); err != nil {
		t.Fatal(err)
	}
	assertEventCount(t, recorder, 1)
	if err := service.AdminSetReady(commish, member.TeamID, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := recorder.len(); got != 1 {
		t.Fatalf("events = %+v after a no-op AdminSetReady, want still 1", recorder.snapshot())
	}
}

// TestMaybeEmitDraftCompleteLatchIsRaceSafe proves draftCompleteEmitted's
// CompareAndSwap is the actual single-emit guarantee, not an artifact of the
// store's own per-pick serialization (each call site reads its own before
// and after snapshots on its own goroutine, outside any store lock, so two
// such reads can race each other even though the store itself only ever
// lets one commit be "the" pick that completes the draft). Two goroutines
// racing the identical before-incomplete/after-complete transition must
// still produce exactly one draft:state{complete:true}.
func TestMaybeEmitDraftCompleteLatchIsRaceSafe(t *testing.T) {
	service, recorder, _ := newEventTestService(t)
	before := PersistedState{}
	after := service.store.Snapshot()
	// The latch, not draftComplete's own arithmetic, is what this test
	// exercises: force after into "complete" directly instead of playing
	// out draftTeamCount()*CurrentDraftRounds() real picks.
	after.Picks = make([]DraftPick, draftTeamCount()*CurrentDraftRounds())

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			service.maybeEmitDraftComplete(before, after, time.Now())
		}()
	}
	wg.Wait()

	waitForDraftComplete(t, recorder)
	time.Sleep(50 * time.Millisecond)
	count := 0
	for _, event := range recorder.snapshot() {
		if event.Name == "draft:state" && event.Payload["complete"] == true {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("draft:state complete events = %d, want exactly 1", count)
	}
}

// TestAdminUndoPickReopeningACompletedDraftEmitsLifecycleState proves
// undo's reopen-the-final-slot case: draft:undo alone only describes the
// reopened cell, never the started/complete flags, so a room that just
// watched the draft finish needs a second, explicit draft:state to learn
// it is playable again. It also proves the completion latch clears: a
// fresh pick that re-completes the reopened draft must emit
// draft:state{complete:true} a second time.
func TestAdminUndoPickReopeningACompletedDraftEmitsLifecycleState(t *testing.T) {
	t.Cleanup(clearRosterShape)
	setRosterShape(RosterPreset{Name: "undo-complete-fixture", Slots: map[string]int{}, Bench: 1})
	service, recorder, requests := newFullySeatedEventTestService(t)
	teams := service.Teams()
	for made := 0; made < len(teams); made++ {
		state := service.store.Snapshot()
		teamID := teamOnClock(state.DraftOrder, len(state.Picks)+1)
		playerID := fmt.Sprintf("pool-%03d", made+1)
		if _, _, _, err := service.MakePick(requests[teamID], teamID, playerID); err != nil {
			t.Fatalf("pick %d for %s: %v", made+1, teamID, err)
		}
	}
	waitForDraftComplete(t, recorder)
	if !service.draftCompleteEmitted.Load() {
		t.Fatal("the completion latch did not arm")
	}

	t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")
	commish := authenticatedJourneyRequest(t, "boss@example.com", "Commissioner", "/admin")
	token := draftPreviousPickToken(service.store.Snapshot())
	if err := service.AdminUndoPick(commish, token); err != nil {
		t.Fatal(err)
	}

	waitForNamedCount(t, recorder, "draft:undo", 1)
	deadline := time.Now().Add(2 * time.Second)
	var reopened []DraftEvent
	for {
		reopened = nil
		for _, event := range recorder.snapshot() {
			if event.Name == "draft:state" && event.Payload["started"] == true && event.Payload["complete"] == false {
				reopened = append(reopened, event)
			}
		}
		if len(reopened) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(reopened) != 1 {
		t.Fatalf("draft:state{started:true,complete:false} events = %+v, want exactly 1", reopened)
	}
	if service.draftCompleteEmitted.Load() {
		t.Fatal("the completion latch did not clear after the undo reopened the final slot")
	}

	state := service.store.Snapshot()
	teamID := teamOnClock(state.DraftOrder, len(state.Picks)+1)
	playerID := fmt.Sprintf("pool-%03d", len(teams)+1)
	if _, _, _, err := service.MakePick(requests[teamID], teamID, playerID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	var completions int
	for {
		completions = 0
		for _, event := range recorder.snapshot() {
			if event.Name == "draft:state" && event.Payload["complete"] == true {
				completions++
			}
		}
		if completions > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if completions != 1 {
		t.Fatalf("draft:state complete events after the redo = %d, want exactly 1 (the latch clear must allow exactly one more)", completions)
	}
}

// TestDropPathSchedulesOneCoalescedRepair fills the dispatch queue to
// capacity by hand (no consumer running yet, so the fill is atomic and
// leaves no room to race a real drain goroutine), forces exactly one
// emitDraft drop against that full queue, and only then starts the drain
// and repair-loop goroutines. One producer drop must produce exactly one
// draft:state carrying repair: true, delivered once the backlog clears.
func TestDropPathSchedulesOneCoalescedRepair(t *testing.T) {
	service, _, _ := newEventTestService(t) // draft setup only; replace its sink below
	service.StopDraftEvents()

	queue := make(chan DraftEvent, draftEventQueueSize)
	for i := 0; i < draftEventQueueSize; i++ {
		queue <- DraftEvent{Name: "draft:seat", Payload: map[string]any{"filler": i}}
	}
	signal := make(chan struct{}, 1)
	service.poolMu.Lock()
	service.draftQueue = queue
	service.draftRepairSignal = signal
	service.poolMu.Unlock()

	// The queue is full and nothing is consuming it yet: this drop is
	// deterministic, not a race against a consumer goroutine.
	service.emitDraft("draft:seat", map[string]any{"probe": true})
	if service.draftDropped.Load() != 1 {
		t.Fatalf("draftDropped = %d, want exactly 1", service.draftDropped.Load())
	}

	var recorder draftEventRecorder
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go draftEventDrain(ctx, queue, recorder.record)
	go service.draftRepairLoop(ctx, signal)

	deadline := time.Now().Add(2 * time.Second)
	var repairs []DraftEvent
	for {
		repairs = nil
		for _, event := range recorder.snapshot() {
			if event.Name == "draft:state" && event.Payload["repair"] == true {
				repairs = append(repairs, event)
			}
		}
		if len(repairs) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(repairs) == 0 {
		t.Fatal("no repair draft:state arrived after the drop")
	}
	time.Sleep(50 * time.Millisecond)
	repairs = nil
	for _, event := range recorder.snapshot() {
		if event.Name == "draft:state" && event.Payload["repair"] == true {
			repairs = append(repairs, event)
		}
	}
	if len(repairs) != 1 {
		t.Fatalf("repair draft:state events = %d, want exactly 1", len(repairs))
	}
}

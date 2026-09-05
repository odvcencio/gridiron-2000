package league

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
)

// newPracticeBase builds a seated, pre-draft league the practice registry
// can sandbox: every default seat holds a member (m1..mN@example.com), a
// 200-player live pool is attached, and the clock is a fake the test
// advances by hand. The real draft is NOT started — that is the state the
// owner's ask targets (practice before Sunday's draft).
func newPracticeBase(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	svc := &Service{
		store:            NewStore(filepath.Join(t.TempDir(), "state.json")),
		draftAt:          clock.Add(24 * time.Hour),
		draftTZ:          time.UTC,
		teams:            defaultTeams(),
		players:          defaultPlayers(),
		cfg:              DefaultConfig(),
		presence:         newPresenceTracker(clock.Add(-24 * time.Hour)),
		now:              func() time.Time { return clock },
		pickClockDefault: 90 * time.Second,
	}
	svc.feed = newLiveFeed(nil, svc)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return testPool(200), 1, "live" })
	for index := range defaultTeamIDs() {
		email := fmt.Sprintf("m%d@example.com", index+1)
		if _, _, err := svc.store.AssignMember(email, fmt.Sprintf("Manager %d", index+1)); err != nil {
			t.Fatalf("seat %s: %v", email, err)
		}
	}
	return svc, &clock
}

// practiceRequest carries an authenticated auth.User for email through a
// real auth.Manager middleware, the same recipe attention_test.go's
// signedInPickemRequest uses (auth.Current's context key is unexported).
func practiceRequest(email string) *http.Request {
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			if email == "" {
				return auth.User{}, false
			}
			return auth.User{ID: email, Email: email, Name: email}, true
		}),
	})
	var captured *http.Request
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, PracticeRoomPath, nil))
	return captured
}

// viewerSeatEmail returns the email seated at teamID in the fixture.
func viewerSeatEmail(t *testing.T, svc *Service, teamID string) string {
	t.Helper()
	email := memberForTeam(svc.store.Snapshot().Members, teamID).Email
	if email == "" {
		t.Fatalf("no member seated at %s", teamID)
	}
	return email
}

// driveUntilViewerOnClock ticks the practice forward one second at a time
// until the viewer's seat is on the clock, or fails after limit ticks.
func driveUntilViewerOnClock(t *testing.T, practice *PracticeDraft, clock *time.Time, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		if practice.ViewerOnClock() {
			return
		}
		*clock = clock.Add(time.Second)
		practice.Tick(*clock)
	}
	t.Fatalf("viewer never came on the clock within %d ticks (picks=%d)", limit, len(practice.Snapshot().Picks))
}

func TestPracticeDraftNeverTouchesTheRealStore(t *testing.T) {
	base, clock := newPracticeBase(t)
	var realEvents atomic.Int64
	base.SetDraftEventSink(func(DraftEvent) { realEvents.Add(1) })
	t.Cleanup(base.StopDraftEvents)
	before := base.StateFingerprint(1)
	realBefore := base.store.Snapshot()

	registry := NewPracticeRegistry(base)
	var sandboxEvents atomic.Int64
	registry.SetEventSink(func(string, DraftEvent) { sandboxEvents.Add(1) })
	order := realBefore.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	// Seat 3 in the order: two bots pick before the viewer, so the viewer's
	// first turn only arrives after bot think time has elapsed.
	viewerTeam := order[2]
	viewer := practiceRequest(viewerSeatEmail(t, base, viewerTeam))

	practice, err := registry.Start(viewer, 1)
	if err != nil {
		t.Fatalf("start practice: %v", err)
	}
	if practice.TeamID() != viewerTeam {
		t.Fatalf("practice seat = %s, want the viewer's real seat %s", practice.TeamID(), viewerTeam)
	}
	driveUntilViewerOnClock(t, practice, clock, 60)
	if picks := practice.Snapshot().Picks; len(picks) != 2 {
		t.Fatalf("expected two bot picks before the viewer's turn, got %d", len(picks))
	}
	for _, pick := range practice.Snapshot().Picks {
		if pick.MadeBy != "auto" {
			t.Fatalf("bot pick %d made_by = %q, want auto", pick.Number, pick.MadeBy)
		}
	}
	data := practice.Data(viewer)
	if can, _ := data["can_pick"].(bool); !can {
		t.Fatal("viewer on the clock must be able to pick")
	}
	available, _ := data["available"].([]map[string]any)
	if len(available) == 0 {
		t.Fatal("no available players rendered for the practice room")
	}
	playerID, _ := available[0]["id"].(string)
	pick, _, _, err := practice.MakePick(viewer, playerID)
	if err != nil {
		t.Fatalf("viewer pick: %v", err)
	}
	if pick.Number != 3 || pick.TeamID != viewerTeam || pick.MadeBy != "manager" {
		t.Fatalf("viewer pick = %+v", pick)
	}
	// A few more ticks: the next bots keep drafting after the viewer.
	for i := 0; i < 12; i++ {
		*clock = clock.Add(time.Second)
		practice.Tick(*clock)
	}
	if got := len(practice.Snapshot().Picks); got < 4 {
		t.Fatalf("bots stopped after the viewer's pick: %d picks", got)
	}

	// The whole point: the real league is byte-for-byte untouched.
	realAfter := base.store.Snapshot()
	if len(realAfter.Picks) != 0 || realAfter.DraftStarted || !realAfter.ClockDeadline.IsZero() {
		t.Fatalf("real store changed: picks=%d started=%v deadline=%v", len(realAfter.Picks), realAfter.DraftStarted, realAfter.ClockDeadline)
	}
	if after := base.StateFingerprint(1); after != before {
		t.Fatalf("real fingerprint moved:\n before %s\n after  %s", before, after)
	}
	base.presence.mu.Lock()
	seen := len(base.presence.lastSeen)
	base.presence.mu.Unlock()
	if seen != 0 {
		t.Fatalf("practice recorded %d presence keys on the real tracker", seen)
	}
	// Give the real sink's drain goroutine a moment: it must have nothing.
	time.Sleep(50 * time.Millisecond)
	if got := realEvents.Load(); got != 0 {
		t.Fatalf("real draft event sink received %d practice events", got)
	}
	registry.Leave(viewer)
	if got := practice.svc.draftQueue; got != nil {
		t.Fatal("leaving practice must stop the sandbox's own event drain")
	}
	if _, ok := registry.Current(viewer); ok {
		t.Fatal("session survived Leave")
	}
}

func TestPracticeStartRefusals(t *testing.T) {
	base, _ := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	seated := viewerSeatEmail(t, base, defaultTeamIDs()[0])

	cases := []struct {
		name    string
		request *http.Request
		prepare func()
		want    string
	}{
		{name: "anonymous", request: practiceRequest(""), want: "Sign in to practice."},
		{name: "seatless", request: practiceRequest("nobody@example.com"), want: "You need a seat to practice."},
		{name: "real draft live", request: practiceRequest(seated), prepare: func() {
			if _, err := base.store.StartDraft(base.clock(), DefaultPickClock); err != nil {
				t.Fatal(err)
			}
		}, want: "The real draft is live."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare()
			}
			availability := base.PracticeAvailability(tc.request)
			if availability.Allowed || availability.Reason != tc.want {
				t.Fatalf("availability = %+v, want refused with %q", availability, tc.want)
			}
			if _, err := registry.Start(tc.request, 1); err == nil || err.Error() != tc.want {
				t.Fatalf("start = %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("real draft complete", func(t *testing.T) {
		complete, _ := newPracticeBase(t)
		total := draftTeamCount() * CurrentDraftRounds()
		if _, err := complete.store.StartDraft(complete.clock(), DefaultPickClock); err != nil {
			t.Fatal(err)
		}
		pool := testPool(200)
		for number := 1; number <= total; number++ {
			team := teamOnClock(complete.store.Snapshot().DraftOrder, number)
			if _, err := complete.store.MakePick(team, pool[number-1].ID, "auto", complete.clock(), time.Time{}); err != nil {
				t.Fatalf("pick %d: %v", number, err)
			}
		}
		request := practiceRequest(viewerSeatEmail(t, complete, defaultTeamIDs()[0]))
		availability := complete.PracticeAvailability(request)
		if availability.Allowed || availability.Reason != "The draft is complete." {
			t.Fatalf("availability = %+v", availability)
		}
	})
}

func TestPracticeFastForwardsToTheStartRound(t *testing.T) {
	base, clock := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	teams := draftTeamCount()
	viewer := practiceRequest(viewerSeatEmail(t, base, defaultTeamIDs()[0]))

	practice, err := registry.Start(viewer, 10)
	if err != nil {
		t.Fatalf("start at round 10: %v", err)
	}
	state := practice.Snapshot()
	if got, want := len(state.Picks), 9*teams; got != want {
		t.Fatalf("fast-forward made %d picks, want %d", got, want)
	}
	perTeam := map[string]int{}
	for _, pick := range state.Picks {
		perTeam[pick.TeamID]++
	}
	for _, teamID := range defaultTeamIDs() {
		if perTeam[teamID] != 9 {
			t.Fatalf("%s holds %d picks after nine rounds", teamID, perTeam[teamID])
		}
	}
	last := CurrentDraftRounds()
	if practice.StartRound() != 10 || practice.EndRound() != last {
		t.Fatalf("rounds = %d..%d, want 10..%d (no round cap)", practice.StartRound(), practice.EndRound(), last)
	}
	if practice.Complete() {
		t.Fatal("practice must be open at the start of round 10")
	}
	// Every roster must still be fillable: the viewer's own next pick has a
	// legal candidate, and so does every bot's.
	for _, teamID := range defaultTeamIDs() {
		if _, ok := practice.svc.autopickChoice(state, teamID); !ok {
			t.Fatalf("%s has no legal candidate after the fast-forward", teamID)
		}
	}
	// Drive to the sandbox draft's final pick: the viewer's own turns fall
	// to the clock's autopick once the deadline passes. Nothing short of
	// the final pick completes a practice.
	for i := 0; i < 20*teams*last && !practice.Complete(); i++ {
		*clock = clock.Add(10 * time.Second)
		practice.Tick(*clock)
		if picks := len(practice.Snapshot().Picks); picks < last*teams && practice.Complete() {
			t.Fatalf("practice completed early at pick %d of %d", picks, last*teams)
		}
	}
	if !practice.Complete() {
		t.Fatalf("practice never completed: %d picks", len(practice.Snapshot().Picks))
	}
	if got, want := len(practice.Snapshot().Picks), last*teams; got != want {
		t.Fatalf("practice ended with %d picks, want %d (the final pick)", got, want)
	}
	if view, _ := practice.Data(viewer)["practice"].(map[string]any); !strings.Contains(view["summary_full"].(string), "final pick") {
		t.Fatalf("summary_full at the end = %v", view["summary_full"])
	}
	if deadline := practice.Snapshot().ClockDeadline; !deadline.IsZero() {
		t.Fatalf("clock still armed after practice completed: %v", deadline)
	}
	data := practice.Data(viewer)
	if can, _ := data["can_pick"].(bool); can {
		t.Fatal("a completed practice must not offer a pick")
	}
	if practiceView, _ := data["practice"].(map[string]any); practiceView["complete"] != true {
		t.Fatalf("practice view = %+v, want complete", practiceView)
	}
}

func TestPracticeStartRoundIsClampedToTheDraft(t *testing.T) {
	base, _ := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	viewer := practiceRequest(viewerSeatEmail(t, base, defaultTeamIDs()[0]))
	practice, err := registry.Start(viewer, 99)
	if err != nil {
		t.Fatal(err)
	}
	last := CurrentDraftRounds()
	if practice.StartRound() != 1 || practice.EndRound() != last {
		t.Fatalf("an unknown round must fall back to round 1 and run to round %d, got %d..%d", last, practice.StartRound(), practice.EndRound())
	}
	practice, err = registry.Start(viewer, 15)
	if err != nil {
		t.Fatal(err)
	}
	if practice.EndRound() != last {
		t.Fatalf("end round = %d, want the draft's last round %d", practice.EndRound(), last)
	}
	options := PracticeStartOptions()
	for _, option := range options {
		if option.Round > last {
			t.Fatalf("option %+v exceeds the draft's %d rounds", option, last)
		}
	}
}

func TestPracticeBotsPickOnThinkTimeAndTheViewerClockAutopicks(t *testing.T) {
	base, clock := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	order := base.store.Snapshot().DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	// The viewer holds the FIRST seat, so the viewer's clock runs from the
	// start and the first bot only picks after the viewer.
	viewerTeam := order[0]
	viewer := practiceRequest(viewerSeatEmail(t, base, viewerTeam))
	practice, err := registry.Start(viewer, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !practice.ViewerOnClock() {
		t.Fatal("viewer holds pick one and must be on the clock")
	}
	state := practice.Snapshot()
	if state.ClockDeadline.IsZero() {
		t.Fatal("the viewer's clock must be armed at the start")
	}
	if got, want := practice.svc.pickClock(state), base.pickClock(base.store.Snapshot()); got != want {
		t.Fatalf("practice clock %v, want the league's %v", got, want)
	}
	// Viewer expiry: past the deadline the seat autopicks like the real room.
	*clock = state.ClockDeadline.Add(time.Second)
	practice.Tick(*clock)
	picks := practice.Snapshot().Picks
	if len(picks) != 1 || picks[0].TeamID != viewerTeam || picks[0].MadeBy != "auto" {
		t.Fatalf("expiry autopick = %+v", picks)
	}
	// Bot think time: the second seat is a bot; it must not pick on the
	// very next tick, and must pick within five seconds.
	*clock = clock.Add(time.Second)
	practice.Tick(*clock)
	if got := len(practice.Snapshot().Picks); got != 1 {
		t.Fatalf("bot picked instantly (%d picks); think time expected", got)
	}
	for i := 0; i < 5; i++ {
		*clock = clock.Add(time.Second)
		practice.Tick(*clock)
	}
	if got := len(practice.Snapshot().Picks); got != 2 {
		t.Fatalf("bot never picked within five seconds (%d picks)", got)
	}
	if think := practiceThinkTime(2); think < 2*time.Second || think > 5*time.Second {
		t.Fatalf("think time %v outside 2..5s", think)
	}
	// Every seat reads HERE in practice: the room never shows NOT SEEN.
	for _, team := range defaultTeams() {
		label, _, _ := practice.svc.teamPresence(practice.Snapshot(), team.ID, *clock)
		if label != "here" {
			t.Fatalf("%s presence = %s, want here", team.ID, label)
		}
	}
}

func TestPracticeRegistryEvictsIdleSessionsAndCapsThem(t *testing.T) {
	base, clock := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	registry.max = 2
	seats := defaultTeamIDs()
	first := practiceRequest(viewerSeatEmail(t, base, seats[0]))
	second := practiceRequest(viewerSeatEmail(t, base, seats[1]))
	third := practiceRequest(viewerSeatEmail(t, base, seats[2]))
	if _, err := registry.Start(first, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Start(second, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Start(third, 1); err == nil {
		t.Fatal("third session must be refused at the cap")
	}
	if registry.Len() != 2 {
		t.Fatalf("len = %d", registry.Len())
	}
	// Restarting the same viewer replaces, never adds.
	if _, err := registry.Start(first, 5); err != nil {
		t.Fatal(err)
	}
	if registry.Len() != 2 {
		t.Fatalf("restart grew the registry to %d", registry.Len())
	}
	// Idle for the TTL (twelve hours, so a tab left open through draft
	// weekend survives): swept. A recent touch keeps the other alive.
	if practiceIdleTTL < 12*time.Hour {
		t.Fatalf("idle TTL = %v, want at least 12h", practiceIdleTTL)
	}
	*clock = clock.Add(practiceIdleTTL + time.Minute)
	if _, ok := registry.Current(second); !ok {
		t.Fatal("Current must still find the second session before the sweep")
	}
	if evicted := registry.Sweep(*clock); evicted != 1 {
		t.Fatalf("sweep evicted %d, want 1 (the first, untouched session)", evicted)
	}
	if _, ok := registry.Current(first); ok {
		t.Fatal("idle session survived the sweep")
	}
	if _, err := registry.Start(third, 1); err != nil {
		t.Fatalf("after the sweep the cap has room again: %v", err)
	}
}

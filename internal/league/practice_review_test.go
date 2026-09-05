package league

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Reviewer-requested coverage (2026-09-04, "SHIP WITH FIXES"): the real
// draft going live ends a practice, a failed restart keeps the old
// session, a co-manager can practice from the shared seat, Run's shutdown
// closes every session, and two tabs share one session.

func TestPracticeStopsWhenTheRealDraftStarts(t *testing.T) {
	base, clock := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	viewer := practiceRequest(viewerSeatEmail(t, base, defaultTeamIDs()[0]))
	practice, err := registry.Start(viewer, 1)
	if err != nil {
		t.Fatal(err)
	}
	before := practice.Data(viewer)
	if view, _ := before["practice"].(map[string]any); view["real_started"] != false {
		t.Fatalf("real_started before the real draft = %v", view["real_started"])
	}
	if _, err := base.store.StartDraft(*clock, DefaultPickClock); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Second)
	practice.Tick(*clock)
	if !practice.Complete() {
		t.Fatal("the practice must end once the real draft is live")
	}
	if deadline := practice.Snapshot().ClockDeadline; !deadline.IsZero() {
		t.Fatalf("practice clock still armed after the real draft started: %v", deadline)
	}
	data := practice.Data(viewer)
	view, _ := data["practice"].(map[string]any)
	if view["real_started"] != true || view["real_room_href"] != "/draft" {
		t.Fatalf("practice view = %+v, want real_started with the real room link", view)
	}
	if short, _ := view["summary_short"].(string); !strings.Contains(short, "real draft has started") {
		t.Fatalf("summary_short = %q", short)
	}
	if can, _ := data["can_pick"].(bool); can {
		t.Fatal("no practice pick once the real draft is live")
	}
	// The room's own banner slot is the sandbox's, never blanked by Data.
	if _, ok := data["banner"]; !ok {
		t.Fatal("Data must leave the room's banner key in place")
	}
	// Ticks after the end stay inert.
	picks := len(practice.Snapshot().Picks)
	for i := 0; i < 5; i++ {
		*clock = clock.Add(time.Second)
		practice.Tick(*clock)
	}
	if got := len(practice.Snapshot().Picks); got != picks {
		t.Fatalf("a finished practice kept drafting: %d -> %d", picks, got)
	}
	// The session stays through the grace (so an open room can fetch the
	// line), then the sweep evicts it.
	if evicted := registry.Sweep(*clock); evicted != 0 {
		t.Fatalf("sweep evicted %d sessions inside the grace", evicted)
	}
	if _, ok := registry.Current(viewer); !ok {
		t.Fatal("the session must survive the grace window")
	}
	*clock = clock.Add(practiceRealStartedGrace + time.Second)
	if evicted := registry.Sweep(*clock); evicted != 1 {
		t.Fatalf("sweep evicted %d sessions after the grace, want 1", evicted)
	}
	if _, ok := registry.Current(viewer); ok {
		t.Fatal("a practice the real draft ended must be evicted after the grace")
	}
	if _, err := registry.Start(viewer, 1); err == nil {
		t.Fatal("no new practice once the real draft is live")
	}
}

func TestPracticeStartKeepsTheOldSessionWhenTheNewBuildFails(t *testing.T) {
	base, _ := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	viewer := practiceRequest(viewerSeatEmail(t, base, defaultTeamIDs()[0]))
	first, err := registry.Start(viewer, 5)
	if err != nil {
		t.Fatal(err)
	}
	// The pool goes away: the next build must fail without touching the
	// in-progress practice.
	base.SetPlayerSource(func() ([]Player, int64, string) { return nil, 2, "unavailable" })
	if _, err := registry.Start(viewer, 1); err == nil {
		t.Fatal("a build with no pool must fail")
	}
	current, ok := registry.Current(viewer)
	if !ok || current != first {
		t.Fatal("the failed restart must leave the previous session in place")
	}
	if first.Complete() {
		t.Fatal("the previous session must not be closed by a failed restart")
	}
	if first.StartRound() != 5 {
		t.Fatalf("previous session start round = %d", first.StartRound())
	}
}

func TestPracticeStartsForACoManager(t *testing.T) {
	base, _ := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	teamID := defaultTeamIDs()[1]
	const co = "co@example.com"
	if err := base.store.InviteCoManager(teamID, co); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := base.store.BindCoManager(co, "Co Manager"); err != nil || !bound {
		t.Fatalf("bind co-manager: bound=%v err=%v", bound, err)
	}
	// The primary's board is the seat's board: the co-manager's practice
	// autopick reads it too.
	primary := viewerSeatEmail(t, base, teamID)
	pool := testPool(200)
	if err := base.store.BoardAdd(primary, pool[10].ID); err != nil {
		t.Fatal(err)
	}
	practice, err := registry.Start(practiceRequest(co), 1)
	if err != nil {
		t.Fatalf("co-manager start: %v", err)
	}
	if practice.TeamID() != teamID {
		t.Fatalf("co-manager practices seat %s, want %s", practice.TeamID(), teamID)
	}
	state := practice.Snapshot()
	if key := practice.svc.boardKeyForTeam(state, teamID); key != primary {
		t.Fatalf("board key for the shared seat = %q, want the primary %q", key, primary)
	}
	if len(state.Boards[primary]) != 1 {
		t.Fatalf("the seat's real board did not reach the sandbox: %v", state.Boards[primary])
	}
}

func TestPracticeRunShutdownClosesEverySession(t *testing.T) {
	base, _ := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	seats := defaultTeamIDs()
	first, err := registry.Start(practiceRequest(viewerSeatEmail(t, base, seats[0])), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Start(practiceRequest(viewerSeatEmail(t, base, seats[1])), 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	registry.Run(ctx)
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for registry.Len() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if registry.Len() != 0 {
		t.Fatalf("shutdown left %d sessions open", registry.Len())
	}
	if !first.Complete() {
		t.Fatal("shutdown must close each session")
	}
}

func TestPracticeTwoTabsShareOneSession(t *testing.T) {
	base, clock := newPracticeBase(t)
	registry := NewPracticeRegistry(base)
	order := base.store.Snapshot().DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	email := viewerSeatEmail(t, base, order[0])
	tabA := practiceRequest(email)
	tabB := practiceRequest(email)
	practice, err := registry.Start(tabA, 1)
	if err != nil {
		t.Fatal(err)
	}
	fromB, ok := registry.Current(tabB)
	if !ok || fromB != practice {
		t.Fatal("a second tab must resolve the same session")
	}
	if registry.Len() != 1 {
		t.Fatalf("two tabs opened %d sessions", registry.Len())
	}
	available, _ := practice.Data(tabB)["available"].([]map[string]any)
	playerID, _ := available[0]["id"].(string)
	if _, _, _, err := practice.MakePick(tabB, playerID); err != nil {
		t.Fatalf("pick from the second tab: %v", err)
	}
	*clock = clock.Add(time.Second)
	if got := len(practice.Snapshot().Picks); got != 1 {
		t.Fatalf("picks after the second tab's pick = %d", got)
	}
	if data := practice.Data(tabA); data["picks_empty"] != false {
		t.Fatal("the first tab must see the second tab's pick")
	}
}

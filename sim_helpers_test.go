package main

import (
	"io"
	"net/http"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/gorilla/websocket"
)

// advanceClock moves the child's league clock forward by d. The harness
// route installs the override on its first call, so a scenario that never
// calls this runs on wall time. The draft clock's own ticker still fires
// once per real second; it reads the overridden clock, so the next real
// tick sees the advanced instant.
func advanceClock(t *testing.T, base string, d time.Duration) {
	t.Helper()
	response, err := simChildHTTP.Get(base + "/test/clock?advance=" + d.String())
	if err != nil {
		t.Fatalf("advance clock by %s: %v", d, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("advance clock by %s: status %d: %s", d, response.StatusCode, body)
	}
}

// waitForPicks polls the draft state until it holds at least want picks.
// The pick clock fires from a one-second ticker, so a scenario that
// advanced the clock past a deadline must wait for that tick rather than
// read the state once.
func waitForPicks(t *testing.T, bot *draft.Bot, want int, within time.Duration) draft.DraftState {
	t.Helper()
	deadline := time.Now().Add(within)
	last := draft.DraftState{}
	for time.Now().Before(deadline) {
		state, err := bot.State()
		if err != nil {
			t.Fatalf("read draft state while waiting for %d picks: %v", want, err)
		}
		if len(state.Picks) >= want {
			return state
		}
		last = state
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the draft holds %d picks after %s, want %d (on clock %q, clock %v)",
		len(last.Picks), within, want, last.OnClockID, last.Clock)
	return draft.DraftState{}
}

// simQueueCandidate returns an eligible available player that the
// best-available rule would not reach first. The pool page lists players in
// the same order the auto-pick's best-available pass walks, so any row past
// the head proves the queue won. The scan starts at skip and keeps the
// first row the server marks draft_eligible for the on-clock seat.
func simQueueCandidate(t *testing.T, state draft.DraftState, skip int) string {
	t.Helper()
	for index := skip; index < len(state.Available); index++ {
		row := state.Available[index]
		eligible, _ := row["draft_eligible"].(bool)
		id, _ := row["id"].(string)
		if eligible && id != "" {
			return id
		}
	}
	t.Fatalf("no eligible available player past index %d in a page of %d", skip, len(state.Available))
	return ""
}

// repoint aims every bot in the league at a restarted child and primes a
// fresh session against it. The seat each bot already holds is persisted
// league state, so TeamID survives the restart untouched; only the base
// address and the session behind it have to be rebuilt.
func (l *simLeague) repoint(t *testing.T, child *simChild) {
	t.Helper()
	for _, bot := range append([]*draft.Bot{l.commish}, l.bots...) {
		bot.BaseURL = child.URL
		if err := bot.Prime(); err != nil {
			t.Fatalf("re-prime %s against the restarted child: %v", bot.Email, err)
		}
	}
}

// simReadEvent reads one hub event or fails the scenario. what names the
// event the caller expected, so a timeout reports which step stalled.
func simReadEvent(t *testing.T, conn *websocket.Conn, timeout time.Duration, what string) draft.HubEvent {
	t.Helper()
	event, err := draft.ReadEvent(conn, timeout)
	if err != nil {
		t.Fatalf("read %s within %s: %v", what, timeout, err)
	}
	return event
}

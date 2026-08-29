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

// simBestAvailable returns the id of the first available row the server
// marks draft_eligible for the on-clock seat. The pool page lists players
// in the same order the auto-pick's best-available pass walks
// (draftclock.go autopickChoice), so this is the player an empty queue
// produces.
func simBestAvailable(t *testing.T, state draft.DraftState) string {
	t.Helper()
	for _, row := range state.Available {
		eligible, _ := row["draft_eligible"].(bool)
		id, _ := row["id"].(string)
		if eligible && id != "" {
			return id
		}
	}
	t.Fatalf("no eligible available player in a page of %d", len(state.Available))
	return ""
}

// simQueueCandidate returns an eligible available player that the
// best-available rule would not reach first. The scan starts at skip and
// keeps the first row the server marks draft_eligible for the on-clock
// seat.
//
// It first asserts the head of the page is itself eligible. That is the
// scenario's whole premise: an empty queue takes the head, so a queued pick
// from past the head proves the board won only while the head was a legal
// choice.
func simQueueCandidate(t *testing.T, state draft.DraftState, skip int) string {
	t.Helper()
	if len(state.Available) <= skip {
		t.Fatalf("the pool page holds %d rows, want more than %d", len(state.Available), skip)
	}
	if eligible, _ := state.Available[0]["draft_eligible"].(bool); !eligible {
		t.Fatalf("the head of the pool page (%v) is not draft_eligible, so a pick past it would not prove the board beat best available",
			state.Available[0]["id"])
	}
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

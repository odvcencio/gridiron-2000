package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/gorilla/websocket"
)

// simTeamNames names the eight seats a simulated league claims, in the
// order the managers claim them.
var simTeamNames = []string{
	"Kernel Panic",
	"Segfault City",
	"Null Pointers",
	"Garbage Collectors",
	"Race Condition",
	"Big Endians",
	"Stack Overflow",
	"Hot Path",
}

const (
	// simDraftChangedEvent is the one event the draft hub broadcasts.
	simDraftChangedEvent = "draft:changed"
	// simEventWait bounds how long a scenario waits for the event a pick
	// must produce. The hub's change detector polls twice a second, so a
	// wait this long only ever ends in a real failure.
	simEventWait = 10 * time.Second
	// simEventSettle bounds the extra look for a better match after a
	// coalesced event arrived. It is short because the only event that can
	// still be in flight was broadcast by the same detector tick.
	simEventSettle = 250 * time.Millisecond
	// simEventReads caps how many events one wait examines.
	simEventReads = 4
)

// simLeague is one seated league on one child server: the commissioner
// (seatless, named by COMMISSIONER_EMAILS) plus one bot per seat.
type simLeague struct {
	child   *simChild
	commish *draft.Bot
	bots    []*draft.Bot
}

// seatLeagueWith primes the commissioner first, then claims every seat.
// Order matters: once the last seat is claimed /join renders the closed
// state, with no form and therefore no csrf_token, so a commissioner who
// primed later would never get a token. Each manager marks itself ready,
// and reports presence when presence is true.
func seatLeagueWith(t *testing.T, child *simChild, presence bool) *simLeague {
	t.Helper()
	commish := draft.New(child.URL, "commish@sim.test", "Commissioner")
	if err := commish.Prime(); err != nil {
		t.Fatalf("prime commissioner: %v", err)
	}
	bots := make([]*draft.Bot, 0, len(simTeamNames))
	for index, teamName := range simTeamNames {
		email := fmt.Sprintf("manager%d@sim.test", index+1)
		bot := draft.New(child.URL, email, teamName+" Manager")
		if err := bot.Prime(); err != nil {
			t.Fatalf("prime %s: %v", email, err)
		}
		if err := bot.Join(teamName); err != nil {
			t.Fatalf("join %s as %q: %v", email, teamName, err)
		}
		// Join reads viewer_team_id back from the server. An empty seat here
		// means the claim did not stick, and every later action would post a
		// blank team_id.
		if bot.TeamID == "" {
			t.Fatalf("join %s left TeamID empty", email)
		}
		if err := bot.ToggleReady(); err != nil {
			t.Fatalf("ready %s: %v", email, err)
		}
		if presence {
			if err := bot.Presence(); err != nil {
				t.Fatalf("presence %s: %v", email, err)
			}
		}
		bots = append(bots, bot)
	}
	return &simLeague{child: child, commish: commish, bots: bots}
}

func seatLeague(t *testing.T, child *simChild) *simLeague {
	t.Helper()
	return seatLeagueWith(t, child, true)
}

// byTeam returns the bot that holds seat id.
func (l *simLeague) byTeam(id string) *draft.Bot {
	for _, bot := range l.bots {
		if bot.TeamID == id {
			return bot
		}
	}
	return nil
}

// pickOnClock makes one eligible pick for whichever seat is on the clock.
// It returns the state it read before the pick and the instant the pick
// request went out, which is the start of every latency sample.
func (l *simLeague) pickOnClock(t *testing.T) (draft.DraftState, time.Time) {
	t.Helper()
	state, err := l.commish.State()
	if err != nil {
		t.Fatalf("read draft state: %v", err)
	}
	bot := l.byTeam(state.OnClockID)
	if bot == nil {
		t.Fatalf("pick %d: no bot holds the on-clock seat %q", state.PickNumber, state.OnClockID)
	}
	// NextPick reads draft_eligible for the on-clock seat, so it already
	// respects the roster rule that rejects a pick leaving a starter slot
	// unfillable.
	playerID, err := bot.NextPick()
	if err != nil {
		t.Fatalf("pick %d for %s: %v", state.PickNumber, bot.Email, err)
	}
	sent := time.Now()
	result, err := bot.MakePick(playerID)
	if err != nil {
		t.Fatalf("pick %d for %s: %v", state.PickNumber, bot.Email, err)
	}
	if !result.OK {
		t.Fatalf("pick %d for %s rejected player %s: %s (player_id: %s)",
			state.PickNumber, bot.Email, playerID, result.Message, result.FieldErrors["player_id"])
	}
	return state, sent
}

// simSocket is one manager's hub socket plus the goroutine that reads it.
// A dedicated reader is required, not a convenience: gorilla/websocket
// records the first read error permanently, so one expired read deadline
// on the connection itself would break every later read. The goroutine
// therefore never sets a short deadline, and a caller waits on the channel
// instead of on the connection.
type simSocket struct {
	email  string
	conn   *websocket.Conn
	events chan draft.HubEvent
}

// newSimSocket starts the reader. It stamps each event's arrival instant
// as the event is received, not as a scenario consumes it, so a latency
// sample measures the hub and not the scenario's own loop.
func newSimSocket(email string, conn *websocket.Conn) *simSocket {
	socket := &simSocket{email: email, conn: conn, events: make(chan draft.HubEvent, 256)}
	go func() {
		defer close(socket.events)
		for {
			// The deadline is long enough that only a real stall reaches
			// it; the scenario's own bound is simEventWait below.
			event, err := draft.ReadEvent(conn, 5*time.Minute)
			if err != nil {
				return
			}
			socket.events <- event
		}
	}()
	return socket
}

// next returns the socket's next event, or false on a timeout or a closed
// connection.
func (s *simSocket) next(timeout time.Duration) (draft.HubEvent, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case event, open := <-s.events:
		return event, open
	case <-timer.C:
		return draft.HubEvent{}, false
	}
}

// openSockets dials one draft-hub socket per bot and drains the hub's
// __welcome frame. Each dial passes the fingerprint read at that moment as
// its since token, so the hub finds the client already current and queues
// no join-time repair event that a later read would mistake for a pick.
func openSockets(t *testing.T, l *simLeague) []*simSocket {
	t.Helper()
	sockets := make([]*simSocket, 0, len(l.bots))
	for _, bot := range l.bots {
		since, err := bot.Fingerprint()
		if err != nil {
			t.Fatalf("fingerprint %s: %v", bot.Email, err)
		}
		conn, err := bot.Socket(since)
		if err != nil {
			t.Fatalf("socket %s: %v", bot.Email, err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		socket := newSimSocket(bot.Email, conn)
		event, ok := socket.next(simEventWait)
		if !ok {
			t.Fatalf("no welcome event for %s", bot.Email)
		}
		if event.Event != "__welcome" {
			t.Fatalf("first event for %s = %q, want __welcome", bot.Email, event.Event)
		}
		sockets = append(sockets, socket)
	}
	return sockets
}

// simEventFingerprint reads a draft:changed payload's fingerprint.
func simEventFingerprint(t *testing.T, event draft.HubEvent) string {
	t.Helper()
	var payload struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		t.Fatalf("decode %s payload: %v", simDraftChangedEvent, err)
	}
	return payload.Fingerprint
}

// awaitChange waits for the event the pick just made. want is the
// fingerprint read straight after the pick returned, and an event carrying
// it is the exact match this returns at once. The detector polls twice a
// second, so one tick can instead publish a later generation that folds a
// presence change over the pick; such an event is kept and returned only
// after a short look for a better one. An event carrying the pre-pick
// fingerprint is stale and never accepted.
func awaitChange(t *testing.T, socket *simSocket, before, want string) draft.HubEvent {
	t.Helper()
	var coalesced draft.HubEvent
	found := false
	for read := 1; read <= simEventReads; read++ {
		timeout := simEventWait
		if found {
			timeout = simEventSettle
		}
		event, ok := socket.next(timeout)
		if !ok {
			if found {
				return coalesced
			}
			t.Fatalf("%s saw no %s within %s", socket.email, simDraftChangedEvent, timeout)
		}
		if event.Event != simDraftChangedEvent {
			continue
		}
		fingerprint := simEventFingerprint(t, event)
		if fingerprint == before {
			continue // a stale generation from before this pick
		}
		if fingerprint == want {
			return event
		}
		coalesced, found = event, true
	}
	if found {
		return coalesced
	}
	t.Fatalf("%s saw no %s carrying a new fingerprint within %d events",
		socket.email, simDraftChangedEvent, simEventReads)
	return draft.HubEvent{}
}

// simSnakeTeamIndex returns the seat index that owns pick number, counting
// from one, under a snake order over teams seats.
func simSnakeTeamIndex(number, teams int) int {
	index := (number - 1) % teams
	if round := (number-1)/teams + 1; round%2 == 0 {
		index = teams - 1 - index
	}
	return index
}

// assertSnakeOrder checks every recorded pick against the server's own team
// order, which /test/draft reports in draft order.
func assertSnakeOrder(t *testing.T, state draft.DraftState) {
	t.Helper()
	order := make([]string, 0, len(state.Teams))
	for _, team := range state.Teams {
		id, _ := team["id"].(string)
		order = append(order, id)
	}
	if len(order) == 0 {
		t.Fatal("the draft state reported no teams")
	}
	for index, pick := range state.Picks {
		number := draft.PickNumber(pick)
		if number < 1 {
			t.Fatalf("picks[%d] carries no pick number (%v); the snake seat is undefined", index, pick["number"])
		}
		want := order[simSnakeTeamIndex(number, len(order))]
		if got := draft.PickTeamID(pick); got != want {
			t.Fatalf("pick %d landed on %s, want %s in snake order", number, got, want)
		}
	}
}

// simPercentile returns the value at the given fraction of sorted samples.
func simPercentile(sorted []time.Duration, fraction float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)) * fraction)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

// TestSimFullDraftOverHTTP runs a complete eight-manager snake draft
// against a real server process. Every pick travels over HTTP, and every
// manager's WebSocket must observe every pick. The latency bound is one
// second because the hub's change detector polls twice a second; a typed
// event would tighten it.
func TestSimFullDraftOverHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	startedAt := time.Now()
	child := startSimChild(t, "")
	league := seatLeague(t, child)
	if err := league.commish.StartDraft(); err != nil {
		// draftStartReadiness (internal/league/admin.go) refuses a pool that
		// is not labelled live, cached, stale, or degraded, and refuses a
		// pool smaller than teams x rounds. GRIDIRON_TEST_POOL=offline-live
		// answers the first; the offline pool's size answers the second.
		t.Fatalf("start draft: %v", err)
	}
	sockets := openSockets(t, league)

	latencies := make([]time.Duration, 0, 1024)
	picks := 0
	// coalesced counts the events that carried a generation later than the
	// pick's own, which is what one detector tick folding a presence change
	// over the pick looks like from here.
	coalesced := 0
	for {
		state, err := league.commish.State()
		if err != nil {
			t.Fatalf("read draft state: %v", err)
		}
		if state.Complete {
			break
		}
		if !state.Started {
			t.Fatal("the draft reports itself not started after draft-start")
		}
		before, err := league.commish.Fingerprint()
		if err != nil {
			t.Fatalf("read fingerprint: %v", err)
		}
		_, sent := league.pickOnClock(t)
		picks++
		// Read straight after the pick landed: this is the generation the
		// detector's next tick should publish.
		want, err := league.commish.Fingerprint()
		if err != nil {
			t.Fatalf("read fingerprint after pick %d: %v", picks, err)
		}
		for _, socket := range sockets {
			event := awaitChange(t, socket, before, want)
			if simEventFingerprint(t, event) != want {
				coalesced++
			}
			latencies = append(latencies, event.At.Sub(sent))
		}
		if picks > 400 {
			t.Fatal("the draft did not complete within 400 picks")
		}
	}

	final, err := league.commish.State()
	if err != nil {
		t.Fatalf("read final draft state: %v", err)
	}
	if final.Rounds < 1 {
		t.Fatalf("the draft state reported %d rounds", final.Rounds)
	}
	if len(final.Teams) != len(simTeamNames) {
		t.Fatalf("the league holds %d seats, want %d", len(final.Teams), len(simTeamNames))
	}
	wantPicks := len(final.Teams) * final.Rounds
	if len(final.Picks) != wantPicks {
		t.Fatalf("the draft recorded %d picks, want %d", len(final.Picks), wantPicks)
	}
	if picks != wantPicks {
		t.Fatalf("the harness sent %d picks, want %d", picks, wantPicks)
	}
	assertSnakeOrder(t, final)

	sort.Slice(latencies, func(a, b int) bool { return latencies[a] < latencies[b] })
	p50 := simPercentile(latencies, 0.50)
	p99 := simPercentile(latencies, 0.99)
	wall := time.Since(startedAt)
	t.Logf("picks=%d rounds=%d observations=%d coalesced=%d p50=%s p99=%s wall=%s",
		picks, final.Rounds, len(latencies), coalesced, p50, p99, wall)
	if p99 > time.Second {
		t.Fatalf("hub p99 = %s, want <= 1s", p99)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
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
	// simSocketReadWait is the reader goroutine's own deadline. It is long
	// on purpose: gorilla/websocket keeps the first read error forever, so
	// a deadline that expires would break the socket for the rest of the
	// run. The scenario's real bound is simEventWait.
	simSocketReadWait = 5 * time.Minute
)

// simLeague is one seated league on one child server: the commissioner
// (seatless, named by COMMISSIONER_EMAILS) plus one bot per seat.
type simLeague struct {
	commish *draft.Bot
	bots    []*draft.Bot
}

// seatLeagueWith primes the commissioner first, then claims every seat.
// Order matters: once the last seat is claimed /join renders the closed
// state, with no form and therefore no csrf_token, so a commissioner who
// primed later would never get a token. Each manager marks itself ready,
// and reports presence when presence is true.
//
// presence is a parameter, not a constant: a NOT SEEN scenario needs seats
// that never sent one heartbeat, which is what presence=false produces.
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
	return &simLeague{commish: commish, bots: bots}
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
// request went out, which is the start of every latency sample. The
// pre-pick state is returned, not discarded, because a token scenario needs
// the current and previous pick tokens as they stood before the pick.
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
	done   chan struct{}

	stopOnce sync.Once
	mu       sync.Mutex
	readErr  error
}

// newSimSocket starts the reader. It stamps each event's arrival instant
// as the event is received, not as a scenario consumes it, so a latency
// sample measures the hub and not the scenario's own loop.
func newSimSocket(email string, conn *websocket.Conn) *simSocket {
	socket := &simSocket{
		email:  email,
		conn:   conn,
		events: make(chan draft.HubEvent, 256),
		done:   make(chan struct{}),
	}
	go func() {
		defer close(socket.events)
		for {
			// The deadline is long enough that only a real stall reaches
			// it; the scenario's own bound is simEventWait below.
			event, err := draft.ReadEvent(conn, simSocketReadWait)
			if err != nil {
				socket.setReadErr(err)
				return
			}
			// A t.Fatalf stops the consumer where it stands, so a plain
			// send would leave this goroutine blocked on a full channel for
			// the rest of the run. done is the exit the reader always has.
			select {
			case socket.events <- event:
			case <-socket.done:
				return
			}
		}
	}()
	return socket
}

// stop releases the reader: closing the connection ends a blocked read, and
// closing done ends a blocked send. Safe to call more than once.
func (s *simSocket) stop() {
	s.stopOnce.Do(func() {
		close(s.done)
		_ = s.conn.Close()
	})
}

func (s *simSocket) setReadErr(err error) {
	s.mu.Lock()
	s.readErr = err
	s.mu.Unlock()
}

// lastReadErr reports why the reader stopped, or nil while it still runs.
// A failure message quotes it so a dead socket reads differently from a
// hub that simply sent nothing.
func (s *simSocket) lastReadErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readErr
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
		socket := newSimSocket(bot.Email, conn)
		t.Cleanup(socket.stop)
		event, ok := socket.next(simEventWait)
		if !ok {
			t.Fatalf("no welcome event for %s (reader stopped: %v)", bot.Email, socket.lastReadErr())
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

// awaitChange waits for the event the pick just made and reports how many
// events it discarded as older than the pick. sent is the instant the pick
// request went out: an event that arrived before it was queued by an
// earlier generation and cannot report this pick, so it is dropped rather
// than timed, which is what keeps a latency sample from going negative.
//
// want is the fingerprint read straight after the pick returned, and an
// event carrying it is the exact match this returns at once. The detector
// polls twice a second, so one tick can instead publish a later generation
// that folds a presence change over the pick; such an event is kept and
// returned only after a short look for a better one. An event carrying the
// pre-pick fingerprint is stale and never accepted.
func awaitChange(t *testing.T, socket *simSocket, sent time.Time, before, want string) (draft.HubEvent, int) {
	t.Helper()
	var coalesced draft.HubEvent
	found := false
	dropped := 0
	for read := 1; read <= simEventReads; read++ {
		timeout := simEventWait
		if found {
			timeout = simEventSettle
		}
		event, ok := socket.next(timeout)
		if !ok {
			if found {
				return coalesced, dropped
			}
			t.Fatalf("%s saw no %s within %s (reader stopped: %v)",
				socket.email, simDraftChangedEvent, timeout, socket.lastReadErr())
		}
		if event.Event != simDraftChangedEvent {
			continue
		}
		if event.At.Before(sent) {
			dropped++
			continue // queued before this pick went out
		}
		fingerprint := simEventFingerprint(t, event)
		if fingerprint == before {
			continue // a stale generation from before this pick
		}
		if fingerprint == want {
			return event, dropped
		}
		coalesced, found = event, true
	}
	if found {
		return coalesced, dropped
	}
	t.Fatalf("%s saw no %s carrying a new fingerprint within %d events (reader stopped: %v)",
		socket.email, simDraftChangedEvent, simEventReads, socket.lastReadErr())
	return draft.HubEvent{}, dropped
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
	// over the pick looks like from here. dropped counts the events that
	// arrived before their pick went out, which awaitChange discards.
	coalesced := 0
	dropped := 0
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
			event, skipped := awaitChange(t, socket, sent, before, want)
			dropped += skipped
			if simEventFingerprint(t, event) != want {
				coalesced++
			}
			latencies = append(latencies, event.At.Sub(sent))
		}
		// The league's own shape sets the cap; the margin only keeps a
		// runaway loop from running to the test timeout.
		if maxPicks := len(state.Teams)*state.Rounds + 40; picks > maxPicks {
			t.Fatalf("the draft did not complete within %d picks", maxPicks)
		}
	}

	final, err := league.commish.State()
	if err != nil {
		t.Fatalf("read final draft state: %v", err)
	}
	// The server's own completion rule is the truth; the arithmetic below
	// is the cross-check that it agrees with the league's shape.
	if !final.Complete {
		t.Fatal("the loop ended but the draft does not report itself complete")
	}
	if final.Rounds < 1 {
		t.Fatalf("the draft state reported %d rounds", final.Rounds)
	}
	if len(final.Teams) != len(simTeamNames) {
		t.Fatalf("the league holds %d seats, want %d", len(final.Teams), len(simTeamNames))
	}
	wantPicks := final.Rounds * len(final.Teams)
	if len(final.Picks) != wantPicks {
		t.Fatalf("the draft recorded %d picks, want %d", len(final.Picks), wantPicks)
	}
	if picks != wantPicks {
		t.Fatalf("the harness sent %d picks, want %d", picks, wantPicks)
	}
	assertSnakeOrder(t, final)

	sort.Slice(latencies, func(a, b int) bool { return latencies[a] < latencies[b] })
	// A negative sample would mean an event that predates its own pick was
	// timed as that pick's, which awaitChange's sent filter exists to stop.
	if len(latencies) > 0 && latencies[0] < 0 {
		t.Fatalf("a hub latency sample is negative (%s); an event older than its pick was timed", latencies[0])
	}
	p50 := simPercentile(latencies, 0.50)
	p99 := simPercentile(latencies, 0.99)
	wall := time.Since(startedAt)
	t.Logf("picks=%d rounds=%d observations=%d coalesced=%d dropped=%d p50=%s p99=%s wall=%s",
		picks, final.Rounds, len(latencies), coalesced, dropped, p50, p99, wall)
	if p99 > time.Second {
		t.Fatalf("hub p99 = %s, want <= 1s", p99)
	}
}

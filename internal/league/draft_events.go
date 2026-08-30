package league

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// DraftEvent is one typed message for the draft-live hub. Payload is a JSON
// object; its top-level keys double as live-bind maps for the room.
type DraftEvent struct {
	Name       string
	Generation uint64
	Payload    map[string]any
}

// draftEventQueueSize bounds the sink dispatch backlog. A full queue means
// the drain goroutine (or the hub sink it calls) has fallen behind; emitDraft
// drops the event rather than block the caller's own request or ticker
// goroutine, and counts the drop in draftDropped so the next draft:state
// repair can report it (see draftFullBinds's dropped_events field).
const draftEventQueueSize = 256

// SetDraftEventSink installs the hub adapter and starts the two goroutines
// that serialize dispatch and repair. internal/league cannot import
// app/draft, so the hub adapter registers itself here at boot, next to
// SetPlayerSource.
//
// emitDraft assigns each event's generation and pushes it onto the queue
// under draftEmitMu, so wire order always matches generation order among
// regular events; the repair path (emitDraftEventBlocking) delivers
// through its own single-slot repairQueue instead (P2 perf fix,
// 2026-08-30 review), so it can never contend with a regular producer for
// draftEmitMu or for queue capacity — a repair carries a full resync, not
// a delta, so its delivery need not interleave with queue in generation
// order. draftEventDrain below — never the caller's own request or ticker
// goroutine — pays for fn's cost (the fingerprint stamp, the JSON marshal,
// and the websocket fan-out inside hub.Broadcast). A second call replaces
// the sink and restarts both goroutines against a fresh queue, repairQueue,
// and signal; StopDraftEvents (wired into AppRuntime.Close) is the only
// other way to stop them.
func (s *Service) SetDraftEventSink(fn func(DraftEvent)) {
	s.poolMu.Lock()
	if s.draftQueueCancel != nil {
		s.draftQueueCancel()
	}
	queue := make(chan DraftEvent, draftEventQueueSize)
	repairQueue := make(chan DraftEvent, 1)
	signal := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	s.draftQueue = queue
	s.draftRepairQueue = repairQueue
	s.draftRepairSignal = signal
	s.draftQueueCancel = cancel
	s.poolMu.Unlock()
	go draftEventDrain(ctx, queue, repairQueue, fn)
	go s.draftRepairLoop(ctx, signal)
}

// StopDraftEvents halts the goroutines SetDraftEventSink started, if any.
// main.go's AppRuntime.Close calls it at shutdown; safe to call when no
// sink was ever installed, and safe to call more than once.
func (s *Service) StopDraftEvents() {
	s.poolMu.Lock()
	cancel := s.draftQueueCancel
	s.draftQueue = nil
	s.draftRepairQueue = nil
	s.draftRepairSignal = nil
	s.draftQueueCancel = nil
	s.poolMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// draftEventDrain is the single consumer: it delivers every queued event to
// fn off the caller's own goroutine, from either channel as it arrives.
// repairQueue is its own single-slot channel (P2 perf fix, 2026-08-30
// review) rather than a second write into queue, precisely so a slow drain
// stalls emitDraftEventBlocking's own send — never a concurrent producer's
// non-blocking emitDraftEvent, which no longer contends with it for
// draftEmitMu either (see emitDraftEventBlocking). Wire order between queue
// and repairQueue is not preserved (select picks whichever is ready), which
// is fine: a repair is a full resync, not a delta that must land relative
// to any one regular event. It exits once ctx is canceled; any event still
// buffered in either channel at that point is simply never delivered
// (shutdown, not a bug).
func draftEventDrain(ctx context.Context, queue, repairQueue chan DraftEvent, fn func(DraftEvent)) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-repairQueue:
			if fn != nil {
				fn(event)
			}
		case event := <-queue:
			if fn != nil {
				fn(event)
			}
		}
	}
}

// draftRepairLoop is the one goroutine that answers emitDraft's drop
// signal with a coalesced draft:state: the room's regions listen to
// draft:state alongside the typed events (page.gsx's data-gosx-region-on),
// so one full resync repairs whatever a drop lost. signal is buffered to
// exactly one slot, so a burst of drops that lands while this loop is busy
// building and enqueueing the previous repair (or itself gets dropped, in
// which case this loop simply waits for the next signal rather than
// retrying) coalesces into at most one more repair, never one per dropped
// event. It runs on its own goroutine for the same reason draftEventDrain
// does: the snapshot and bind work below must never land on a caller's own
// request or ticker goroutine.
func (s *Service) draftRepairLoop(ctx context.Context, signal chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-signal:
			now := s.clock()
			state := s.store.Snapshot()
			payload := s.draftFullBinds(state, now)
			payload["started"] = state.DraftStarted
			payload["complete"] = draftComplete(state)
			payload["repair"] = true
			// R2 (Task 6 review): the repair itself must never be dropped —
			// it is the one message that resyncs whatever a prior drop
			// already lost, so silently dropping it too would leave a
			// client stuck stale until its own reconnect repair fires.
			// blockOnFull (below) waits for queue room instead of the
			// drop-and-count path every other emitDraft caller uses.
			s.emitDraftEventBlocking(ctx, "draft:state", payload)
		}
	}
}

// emitDraft assigns the next generation and enqueues payload for the drain
// goroutine; see emitDraftEvent.
func (s *Service) emitDraft(name string, payload map[string]any) {
	s.emitDraftEvent(name, payload, true)
}

// emitDraftEvent is emitDraft's shared core, never blocking: draftEmitMu
// serializes "assign the generation" with "push onto the queue" as one
// step, so two producers can never enqueue out of generation order, and a
// full queue drops the event (and counts it) rather than stall the caller.
// signalOnDrop controls whether that drop also pings draftRepairSignal
// (also never blocking); see draftRepairLoop for why its own call passes
// false.
func (s *Service) emitDraftEvent(name string, payload map[string]any, signalOnDrop bool) {
	s.draftEmitMu.Lock()
	s.poolMu.Lock()
	queue := s.draftQueue
	signal := s.draftRepairSignal
	s.poolMu.Unlock()
	if queue == nil {
		s.draftEmitMu.Unlock()
		return
	}
	generation := s.draftGeneration.Add(1)
	payload["generation"] = generation
	payload["at"] = s.clock().UTC().Format(time.RFC3339)
	event := DraftEvent{Name: name, Generation: generation, Payload: payload}
	select {
	case queue <- event:
	default:
		s.draftDropped.Add(1)
		if signalOnDrop && signal != nil {
			select {
			case signal <- struct{}{}:
			default:
			}
		}
	}
	s.draftEmitMu.Unlock()
}

// emitDraftEventBlocking is draftRepairLoop's own emit path (R2, Task 6
// review): unlike emitDraftEvent, a full repair channel never drops this
// one — it blocks until draftEventDrain frees a slot, or ctx is canceled
// (shutdown/StopDraftEvents).
//
// P2 fix (2026-08-30 review): draftEmitMu is held only long enough to
// assign the generation, the same ordering step emitDraftEvent's own doc
// comment describes — it is released BEFORE the potentially-blocking send,
// so a slow drain stalls only this goroutine (draftRepairLoop), never a
// concurrent producer's own emitDraftEvent (MakePick, clockTick,
// AdminForceAutopick, RecordPresence). The send itself goes to
// draftRepairQueue, a channel no other producer ever writes to, so this
// call cannot race a regular emitDraftEvent for queue capacity either.
func (s *Service) emitDraftEventBlocking(ctx context.Context, name string, payload map[string]any) {
	s.draftEmitMu.Lock()
	s.poolMu.Lock()
	repairQueue := s.draftRepairQueue
	s.poolMu.Unlock()
	if repairQueue == nil {
		s.draftEmitMu.Unlock()
		return
	}
	generation := s.draftGeneration.Add(1)
	payload["generation"] = generation
	payload["at"] = s.clock().UTC().Format(time.RFC3339)
	event := DraftEvent{Name: name, Generation: generation, Payload: payload}
	s.draftEmitMu.Unlock()
	select {
	case repairQueue <- event:
	case <-ctx.Done():
	}
}

// draftTeamCount is the denominator every draft completion and pick-count
// calculation shares: the same team count draftComplete (roster.go) and the
// store's own MakePick/AutoPick caps use — len(defaultTeams()), never the
// possibly-trimmed s.Teams(). A draft:* payload's total must always agree
// with the store's own completion rule, or a client could show "120 of 120"
// while the server still disagrees that the draft is complete.
func draftTeamCount() int { return len(defaultTeams()) }

// pickColumn is teamID's column in draft order, 1-based. It trusts the
// pick's own recorded team rather than recomputing who was on the clock for
// a pick number, which stays correct even when a payload is built from a
// snapshot a concurrent, later commit has already raced ahead of.
func pickColumn(order []string, teamID string) int {
	ids := order
	if len(ids) == 0 {
		ids = defaultTeamIDs()
	}
	for index, id := range ids {
		if id == teamID {
			return index + 1
		}
	}
	return 1
}

// pickSlot is the pick's position within its round, 1-based.
func pickSlot(teams, number int) int { return (number-1)%teams + 1 }

func snakeDirection(teams, number int) string {
	if pickRound(teams, number)%2 == 0 {
		return "←"
	}
	return "→"
}

// timeToPickSeconds is the elapsed time before picks[index]: since the
// previous pick, or since the draft start for pick one. Read-time only;
// the room labels it "Time to pick".
func timeToPickSeconds(state PersistedState, index int) int {
	if index < 0 || index >= len(state.Picks) {
		return 0
	}
	from := state.DraftStartedAt
	if index > 0 {
		from = state.Picks[index-1].MadeAt
	}
	used := int(state.Picks[index].MadeAt.Sub(from).Seconds())
	if used < 0 || from.IsZero() {
		return 0
	}
	return used
}

// pickIndexByNumber returns the position of the pick numbered number within
// state.Picks, or -1 when absent. A concurrent commit can add a later pick
// to the snapshot before a payload is built from it, so a caller describing
// one specific pick must locate it by number rather than assume it sits at
// len(state.Picks)-1.
func pickIndexByNumber(state PersistedState, number int) int {
	for index, pick := range state.Picks {
		if pick.Number == number {
			return index
		}
	}
	return -1
}

// clockBinds keeps the spec field names for draft:clock.
func (s *Service) clockBinds(state PersistedState, now time.Time) map[string]any {
	clock := s.clockView(state, now)
	remaining, _ := clock["remaining_seconds"].(int)
	return map[string]any{
		"state": clock["state"], "deadline": clock["deadline"], "effective_deadline": clock["effective_deadline"],
		"duration_sec": clock["duration_seconds"], "remaining_sec": remaining, "remaining_label": clock["remaining_label"],
		"duration_label": clock["duration_label"], "paused": state.ClockPaused, "reason": clock["reason"],
	}
}

func (s *Service) emitDraftClock(state PersistedState) {
	now := s.clock()
	binds := s.clockBinds(state, now)
	payload := make(map[string]any, len(binds)+1)
	for key, value := range binds {
		payload[key] = value
	}
	payload["clock"] = binds
	s.emitDraft("draft:clock", payload)
}

// yourPickBinds returns, per seat, how many picks remain before that seat is
// next on the clock: 0 means the seat is on the clock right now. A seat with
// no future turn left (the draft is complete, or its last pick already
// happened) reports in: -1 and onclock: false.
func (s *Service) yourPickBinds(state PersistedState) map[string]any {
	out := map[string]any{}
	total := draftTeamCount() * CurrentDraftRounds()
	next := len(state.Picks) + 1
	for _, team := range s.Teams() {
		in := -1
		for number := next; number <= total; number++ {
			if teamOnClock(state.DraftOrder, number) == team.ID {
				in = number - next
				break
			}
		}
		label := "your pick in " + strconv.Itoa(in)
		if in < 0 {
			label = "no more picks"
		}
		out[team.ID] = map[string]any{"in": in, "label": label, "onclock": in == 0}
	}
	return out
}

// roomBinds is the room-wide readiness and presence summary: how many seats
// are HERE right now, how many are checked in Ready, how many run Autopick,
// and the claimed-manager denominator (draftSeatCounts).
func (s *Service) roomBinds(state PersistedState, now time.Time) map[string]any {
	ready, managers := s.draftSeatCounts(state)
	here, auto := 0, 0
	for _, team := range s.Teams() {
		if label, _, _ := s.teamPresence(state, team.ID, now); label == "here" {
			here++
		}
		if state.Autopick[team.ID] {
			auto++
		}
	}
	return map[string]any{"here": here, "ready": ready, "auto": auto, "managers": managers}
}

// onClockBinds resolves the seat due to pick right now, or a blank record
// once the draft is complete (teamID == "").
func (s *Service) onClockBinds(state PersistedState, teamID string) map[string]any {
	if teamID == "" {
		return map[string]any{"team_id": "", "name": "", "abbreviation": "", "tone": ""}
	}
	team := s.teamMap(s.teamView(state, teamID))
	return map[string]any{"team_id": team["id"], "name": team["name"], "abbreviation": team["abbreviation"], "tone": team["tone"]}
}

// seatBinds is the one-seat readiness snapshot draft:seat carries.
func (s *Service) seatBinds(state PersistedState, teamID string, now time.Time) map[string]any {
	label, _, _ := s.teamPresence(state, teamID, now)
	return map[string]any{
		"team_id": teamID, "presence": label, "ready": state.Ready[teamID], "auto": state.Autopick[teamID],
	}
}

// draftBoardBinds builds the picked-only cell/cellpos/player/queue maps: at
// most teams x rounds entries, never the whole pool.
func (s *Service) draftBoardBinds(state PersistedState) (cell, cellpos, player, queue map[string]any) {
	cell, cellpos, player, queue = map[string]any{}, map[string]any{}, map[string]any{}, map[string]any{}
	pool := s.pool()
	for _, pick := range state.Picks {
		round := strconv.Itoa(pick.Round)
		column := strconv.Itoa(pickColumn(state.DraftOrder, pick.TeamID))
		p := pool.byID[pick.PlayerID]
		roundCell, _ := cell[round].(map[string]any)
		if roundCell == nil {
			roundCell = map[string]any{}
			cell[round] = roundCell
		}
		roundCell[column] = p.Name
		roundPos, _ := cellpos[round].(map[string]any)
		if roundPos == nil {
			roundPos = map[string]any{}
			cellpos[round] = roundPos
		}
		roundPos[column] = p.Position
		player[pick.PlayerID] = map[string]any{"taken": true}
		queue[pick.PlayerID] = map[string]any{"taken": true}
	}
	return
}

// draftLiveTail bundles the four binds every draft:* payload shares beyond
// its own subject (a pick, an undo, or the whole board): yourpick, room,
// onclock (for onClockID, "" once the draft completes), and the
// pick-progress summary for pick number next.
func (s *Service) draftLiveTail(state PersistedState, now time.Time, onClockID string, next int) map[string]any {
	teams := activeTeamCount(state.DraftOrder)
	total := draftTeamCount() * CurrentDraftRounds()
	return map[string]any{
		"yourpick": s.yourPickBinds(state),
		"room":     s.roomBinds(state, now),
		"onclock":  s.onClockBinds(state, onClockID),
		"pick": map[string]any{
			"number": next, "round": pickRound(teams, next), "direction": snakeDirection(teams, next),
			"made": len(state.Picks), "total": total,
		},
	}
}

// draftFullBinds is the whole live-bind object minus the started/complete
// flags: cell/cellpos/player/queue for picked IDs only, clock, yourpick,
// room, onclock, pick, and the dropped-event counter. DraftLiveView and
// every draft:state emission share it, so a stale reconnect and a
// lifecycle transition carry the same shape.
func (s *Service) draftFullBinds(state PersistedState, now time.Time) map[string]any {
	cell, cellpos, player, queue := s.draftBoardBinds(state)
	complete := draftComplete(state)
	next := len(state.Picks) + 1
	onClockID := ""
	if !complete {
		onClockID = teamOnClock(state.DraftOrder, next)
	}
	payload := map[string]any{
		"cell": cell, "cellpos": cellpos, "player": player, "queue": queue,
		"clock": s.clockBinds(state, now),
		// dropped_events surfaces emitDraft's queue-full counter: a client
		// that sees this rise between two draft:state messages missed real
		// events and should treat this repair as authoritative, not a delta.
		"dropped_events": s.draftDropped.Load(),
	}
	for key, value := range s.draftLiveTail(state, now, onClockID, next) {
		payload[key] = value
	}
	return payload
}

// DraftLiveView is the full room state for a stale reconnect's repair and
// /draft/live.json: every cell/cellpos, player/queue for picked IDs only,
// clock, yourpick, room, onclock, pick, started, and complete. r is
// currently unused (the view carries no viewer-specific bind), kept so a
// future personalization does not change the call sites that already pass
// the request.
func (s *Service) DraftLiveView(r *http.Request) map[string]any {
	now := s.clock()
	state := s.store.Snapshot()
	view := s.draftFullBinds(state, now)
	view["started"] = state.DraftStarted
	view["complete"] = draftComplete(state)
	return view
}

// emitDraftState announces a lifecycle transition (start, reset, or
// completion): the started/complete flags plus the full board and clock
// binds, so a client that missed intervening events can resynchronize from
// this one message.
func (s *Service) emitDraftState(state PersistedState, now time.Time, started, complete bool) {
	payload := s.draftFullBinds(state, now)
	payload["started"] = started
	payload["complete"] = complete
	s.emitDraft("draft:state", payload)
}

// maybeEmitDraftComplete emits draft:state{complete:true} the instant a
// commit crosses an incomplete draft into a complete one. before is the
// snapshot taken prior to the commit; after is the post-commit snapshot.
//
// This exists because Store.MakePick already zeroes the clock fields on the
// pick that completes the draft (the "final pick" branch), so clockTick's
// own leftover-clock-clear branch — which is what used to emit this — never
// finds dirty clock fields to clear and never runs.
//
// draftCompleteEmitted is the actual single-emit guarantee: each call site
// (MakePick, clockTick's autopick, AdminForceAutopick) reads its own before
// snapshot before the store call and its own after snapshot afterward, on
// its own goroutine, so two such reads can race each other independently of
// the store's own per-pick serialization. The CompareAndSwap makes "was the
// completion draft:state already sent" atomic across every caller, not just
// provably-unreachable-today reasoning about the store's guards.
// AdminResetDraft and AdminUndoPick (when an undo reopens the final slot)
// clear it, so a later completion can emit again.
func (s *Service) maybeEmitDraftComplete(before, after PersistedState, now time.Time) {
	if draftComplete(before) || !draftComplete(after) {
		return
	}
	if !s.draftCompleteEmitted.CompareAndSwap(false, true) {
		return
	}
	s.emitDraftState(after, now, true, true)
}

// draftPickPayload builds draft:pick from the post-commit snapshot. Every
// field describing the pick itself is grounded in pick.Number, never in
// state.Picks' length: a concurrent commit (an autopick racing a manual
// pick, say) can add a later pick to state before this payload is built,
// and state.Picks' last entry would then describe the wrong pick.
func (s *Service) draftPickPayload(state PersistedState, pick DraftPick, now time.Time) map[string]any {
	player := s.pool().byID[pick.PlayerID]
	teams := activeTeamCount(state.DraftOrder)
	column := pickColumn(state.DraftOrder, pick.TeamID)
	total := draftTeamCount() * CurrentDraftRounds()
	next := pick.Number + 1
	nextTeam := ""
	if pick.Number < total {
		nextTeam = teamOnClock(state.DraftOrder, next)
	}
	clock := s.clockBinds(state, now)
	round := strconv.Itoa(pick.Round)
	col := strconv.Itoa(column)
	payload := map[string]any{
		"number": pick.Number, "round": pick.Round, "slot": pickSlot(teams, pick.Number), "column": column,
		"team_id": pick.TeamID, "player_id": pick.PlayerID, "player_name": player.Name,
		"position": player.Position, "nfl_team": player.NFLTeam,
		"is_auto": pick.MadeBy == "auto", "is_commissioner": pick.MadeBy == "commissioner",
		"clock_used_sec": timeToPickSeconds(state, pickIndexByNumber(state, pick.Number)),
		"next_team_id":   nextTeam, "next_deadline": clock["effective_deadline"],
		"picks_made": pick.Number, "picks_total": total,
		"cell":    map[string]any{round: map[string]any{col: player.Name}},
		"cellpos": map[string]any{round: map[string]any{col: player.Position}},
		"player":  map[string]any{pick.PlayerID: map[string]any{"taken": true}},
		"queue":   map[string]any{pick.PlayerID: map[string]any{"taken": true}},
		"clock":   clock,
	}
	for key, value := range s.draftLiveTail(state, now, nextTeam, next) {
		payload[key] = value
	}
	return payload
}

// emitDraftUndo builds draft:undo from the removed pick (read from the
// pre-undo snapshot) and the reopened slot (read from the post-undo
// snapshot): the cell reverts to its open-slot pick label, never a blank
// string, so the board never shows an ambiguous empty cell.
func (s *Service) emitDraftUndo(state PersistedState, removed DraftPick, now time.Time) {
	round := strconv.Itoa(removed.Round)
	column := strconv.Itoa(pickColumn(state.DraftOrder, removed.TeamID))
	label := pickLabel(removed.Number, draftTeamCount())
	clock := s.clockBinds(state, now)
	next := len(state.Picks) + 1
	nextTeam := ""
	if !draftComplete(state) {
		nextTeam = teamOnClock(state.DraftOrder, next)
	}
	payload := map[string]any{
		"number": removed.Number, "restored_deadline": clock["effective_deadline"],
		"cell":    map[string]any{round: map[string]any{column: label}},
		"cellpos": map[string]any{round: map[string]any{column: ""}},
		"player":  map[string]any{removed.PlayerID: map[string]any{"taken": false}},
		"queue":   map[string]any{removed.PlayerID: map[string]any{"taken": false}},
		"clock":   clock,
	}
	for key, value := range s.draftLiveTail(state, now, nextTeam, next) {
		payload[key] = value
	}
	s.emitDraft("draft:undo", payload)
}

// emitPresenceTransitions runs once per clockTick: one draft:seat per seat
// whose presence label changed since the last tick. Bounded by one
// teamPresence read per seat per second; a steady room emits nothing.
func (s *Service) emitPresenceTransitions(state PersistedState, now time.Time) {
	for _, team := range s.Teams() {
		label, _, _ := s.teamPresence(state, team.ID, now)
		s.poolMu.Lock()
		if s.lastPresence == nil {
			s.lastPresence = map[string]string{}
		}
		previous, seen := s.lastPresence[team.ID]
		s.lastPresence[team.ID] = label
		s.poolMu.Unlock()
		if seen && previous != label {
			s.emitDraft("draft:seat", s.seatBinds(state, team.ID, now))
		}
	}
}

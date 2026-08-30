package league

import (
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

// SetDraftEventSink installs the one function every committed draft change
// calls exactly once. internal/league cannot import app/draft, so the hub
// adapter registers itself here at boot, next to SetPlayerSource.
func (s *Service) SetDraftEventSink(fn func(DraftEvent)) {
	s.poolMu.Lock()
	s.draftSink = fn
	s.poolMu.Unlock()
}

func (s *Service) emitDraft(name string, payload map[string]any) {
	s.poolMu.Lock()
	sink := s.draftSink
	s.poolMu.Unlock()
	if sink == nil {
		return
	}
	generation := s.draftGeneration.Add(1)
	payload["generation"] = generation
	payload["at"] = s.clock().UTC().Format(time.RFC3339)
	sink(DraftEvent{Name: name, Generation: generation, Payload: payload})
}

// pickColumn is the team column of pick number in draft order, 1-based.
func pickColumn(order []string, number int) int {
	ids := order
	if len(ids) == 0 {
		ids = defaultTeamIDs()
	}
	team := teamOnClock(order, number)
	for index, id := range ids {
		if id == team {
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
	payload := s.clockBinds(state, now)
	payload["clock"] = s.clockBinds(state, now)
	s.emitDraft("draft:clock", payload)
}

// yourPickBinds returns, per seat, how many picks remain before that seat is
// next on the clock: 0 means the seat is on the clock right now. A seat with
// no future turn left (the draft is complete, or its last pick already
// happened) reports in: -1 and onclock: false.
func (s *Service) yourPickBinds(state PersistedState) map[string]any {
	out := map[string]any{}
	total := len(s.Teams()) * CurrentDraftRounds()
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
		column := strconv.Itoa(pickColumn(state.DraftOrder, pick.Number))
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

// draftFullBinds is the whole live-bind object minus the started/complete
// flags: cell/cellpos/player/queue for picked IDs only, clock, yourpick,
// room, onclock, and pick. DraftLiveView and every draft:state emission
// share it, so a stale reconnect and a lifecycle transition carry the same
// shape.
func (s *Service) draftFullBinds(state PersistedState, now time.Time) map[string]any {
	cell, cellpos, player, queue := s.draftBoardBinds(state)
	complete := draftComplete(state)
	next := len(state.Picks) + 1
	onClockID := ""
	if !complete {
		onClockID = teamOnClock(state.DraftOrder, next)
	}
	teams := activeTeamCount(state.DraftOrder)
	total := len(s.Teams()) * CurrentDraftRounds()
	return map[string]any{
		"cell": cell, "cellpos": cellpos, "player": player, "queue": queue,
		"clock":    s.clockBinds(state, now),
		"yourpick": s.yourPickBinds(state),
		"room":     s.roomBinds(state, now),
		"onclock":  s.onClockBinds(state, onClockID),
		"pick": map[string]any{
			"number": next, "round": pickRound(teams, next), "direction": snakeDirection(teams, next),
			"made": len(state.Picks), "total": total,
		},
	}
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

// draftPickPayload builds draft:pick from the post-commit snapshot.
func (s *Service) draftPickPayload(state PersistedState, pick DraftPick, now time.Time) map[string]any {
	player := s.pool().byID[pick.PlayerID]
	teams := activeTeamCount(state.DraftOrder)
	column := pickColumn(state.DraftOrder, pick.Number)
	total := len(s.Teams()) * CurrentDraftRounds()
	next := len(state.Picks) + 1
	nextTeam := ""
	if len(state.Picks) < total {
		nextTeam = teamOnClock(state.DraftOrder, next)
	}
	clock := s.clockBinds(state, now)
	round := strconv.Itoa(pick.Round)
	col := strconv.Itoa(column)
	return map[string]any{
		"number": pick.Number, "round": pick.Round, "slot": pickSlot(teams, pick.Number), "column": column,
		"team_id": pick.TeamID, "player_id": pick.PlayerID, "player_name": player.Name,
		"position": player.Position, "nfl_team": player.NFLTeam,
		"is_auto": pick.MadeBy == "auto", "is_commissioner": pick.MadeBy == "commissioner",
		"clock_used_sec": timeToPickSeconds(state, len(state.Picks)-1),
		"next_team_id":   nextTeam, "next_deadline": clock["effective_deadline"],
		"picks_made": len(state.Picks), "picks_total": total,
		"cell":     map[string]any{round: map[string]any{col: player.Name}},
		"cellpos":  map[string]any{round: map[string]any{col: player.Position}},
		"player":   map[string]any{pick.PlayerID: map[string]any{"taken": true}},
		"queue":    map[string]any{pick.PlayerID: map[string]any{"taken": true}},
		"clock":    clock,
		"yourpick": s.yourPickBinds(state),
		"room":     s.roomBinds(state, now),
		"onclock":  s.onClockBinds(state, nextTeam),
		"pick":     map[string]any{"number": next, "round": pickRound(teams, next), "direction": snakeDirection(teams, next), "made": len(state.Picks), "total": total},
	}
}

// emitDraftUndo builds draft:undo from the removed pick (read from the
// pre-undo snapshot) and the reopened slot (read from the post-undo
// snapshot): the cell reverts to its open-slot pick label, never a blank
// string, so the board never shows an ambiguous empty cell.
func (s *Service) emitDraftUndo(state PersistedState, removed DraftPick, now time.Time) {
	teams := activeTeamCount(state.DraftOrder)
	round := strconv.Itoa(removed.Round)
	column := strconv.Itoa(pickColumn(state.DraftOrder, removed.Number))
	label := pickLabel(removed.Number, len(s.Teams()))
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
		"pick":    map[string]any{"number": next, "round": pickRound(teams, next), "direction": snakeDirection(teams, next), "made": len(state.Picks), "total": len(s.Teams()) * CurrentDraftRounds()},
		"onclock": s.onClockBinds(state, nextTeam),
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

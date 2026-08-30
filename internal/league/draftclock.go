package league

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	// DefaultPickClock, MinPickClock, and MaxPickClock bound the pick-clock
	// duration. PICK_CLOCK and the commissioner's ClockDurationSec override
	// both clamp into [MinPickClock, MaxPickClock].
	DefaultPickClock = 90 * time.Second
	MinPickClock     = 10 * time.Second
	MaxPickClock     = 10 * time.Minute

	// AutopickGrace: delay after arming before an Autopick-toggled seat
	// fires. Covers fingerprint propagation (one poll period) and gives the
	// manager a beat to cancel a mistaken toggle.
	AutopickGrace = 3 * time.Second

	// RestartGrace: deadline reset applied when the server boots past an
	// expired deadline. Bounded, so a crash loop cannot stall the draft by
	// more than this much per boot.
	RestartGrace = 30 * time.Second

	// NotSeenClock caps the deadline for a seat whose manager has never sent
	// one heartbeat this process lifetime (presenceStateSince's not_seen
	// bucket). Twenty seconds covers one heartbeat poll (PollPeriod) plus a
	// page load and a snap decision, so a manager who lands on the room at
	// that exact instant can still act, while meaningfully shortening the
	// default clock for a seat nobody has ever opened. AWAY and IDLE never
	// use this constant: a tab must poll at least once to read either state,
	// so a backgrounded tab is never mistaken for a no-show.
	NotSeenClock = 20 * time.Second

	// NotSeenBootGrace is the minimum process uptime before a NOT SEEN seat
	// may have its deadline shortened. presenceStateSince floors every
	// last-seen instant at the tracker's startedAt, so every seat reads NOT
	// SEEN for a moment right after a restart. This grace withholds the cap
	// until real heartbeats have had time to arrive and reclassify
	// genuinely-present seats, so a restart mid-draft never shortens every
	// seat's clock at once.
	NotSeenBootGrace = 2 * time.Minute

	// clockTickPeriod is StartDraftClock's enforcement-loop interval.
	clockTickPeriod = 1 * time.Second
)

// clampPickClock clamps d to [MinPickClock, MaxPickClock].
func clampPickClock(d time.Duration) time.Duration {
	if d < MinPickClock {
		return MinPickClock
	}
	if d > MaxPickClock {
		return MaxPickClock
	}
	return d
}

// StartDraftClock runs the pick-clock enforcement loop. Call once from
// main.go. The loop stops when ctx is canceled (server shutdown). Before
// the loop starts, it applies the restart-recovery rule (section 8.1 of the
// pick-clock spec) once, against the clock at boot.
//
// The loop ticks every second and calls clockTick(s.clock()). All decision
// logic lives in clockTick, which tests drive directly with a fake clock;
// no goroutine, no sleeps, in the test suite.
func (s *Service) StartDraftClock(ctx context.Context) {
	s.bootRecoverClock(s.clock())
	go func() {
		ticker := time.NewTicker(clockTickPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.clockTick(s.clock())
			}
		}
	}()
}

// bootRecoverClock applies the restart-fairness rule: a deadline left in
// the past when the server boots gets a bounded fresh deadline instead of
// an instant auto-pick that would punish the on-clock manager for an
// outage. A future deadline or a paused/unarmed clock is untouched.
func (s *Service) bootRecoverClock(now time.Time) {
	state := s.store.Snapshot()
	if !state.DraftStarted || state.ClockPaused || state.ClockDeadline.IsZero() {
		return
	}
	if state.ClockDeadline.After(now) {
		return
	}
	grace := RestartGrace
	if duration := s.pickClock(state); duration < grace {
		grace = duration
	}
	if err := s.store.ArmClock(now.Add(grace)); err != nil {
		log.Printf("draft clock: restart recovery failed: %v", err)
		return
	}
	s.emitDraftClock(s.store.Snapshot())
}

// clockTick is the whole enforcement decision for one instant, pure over a
// store snapshot, presence, and now. StartDraftClock's ticker calls it once
// a second; tests call it directly with simulated instants.
func (s *Service) clockTick(now time.Time) {
	state := s.store.Snapshot()
	totalPicks := len(defaultTeams()) * CurrentDraftRounds()
	if !state.DraftStarted {
		return
	}

	// 1-2. Draft complete: clear a leftover deadline once, then idle.
	if len(state.Picks) >= totalPicks {
		if !state.ClockDeadline.IsZero() || state.ClockPaused || state.ClockRemainingSec != 0 {
			if err := s.store.ClearClock(); err != nil {
				log.Printf("draft clock: clear on completion failed: %v", err)
			} else {
				s.emitDraftState(s.store.Snapshot(), now, true, true)
			}
		}
		return
	}

	// 3. Paused: the timer and auto-pick stop; picks stay allowed through
	// the manual path.
	if state.ClockPaused {
		return
	}

	// 4-5. The explicit start normally arms the first deadline atomically.
	// If a later pick/reset path leaves an open draft unarmed, re-arm it.
	if state.ClockDeadline.IsZero() {
		if err := s.store.ArmClock(now.Add(s.pickClock(state))); err != nil {
			log.Printf("draft clock: arm failed: %v", err)
		} else {
			s.emitDraftClock(s.store.Snapshot())
		}
		return
	}

	// N5: notify an AWAY on-clock manager, once per absence episode (spec
	// section 3, N5). Evaluated here so it fires every tick regardless of
	// how close the deadline is — early reach is the point; see the design
	// spec's "timing honesty" note. Guarded internally by notifyReady, so
	// this is a no-op when notifications are not wired.
	s.evalOnTheClock(state, now)
	s.emitPresenceTransitions(state, now)

	// 6. Not yet due.
	effective, reason := s.effectiveDeadline(state, now)
	if now.Before(effective) {
		return
	}

	// 7. Choose a player; an empty pool pauses the clock rather than spin.
	number := len(state.Picks) + 1
	teamID := teamOnClock(state.DraftOrder, number)
	playerID, ok := s.autopickChoice(state, teamID)
	if !ok {
		log.Printf("draft clock: no undrafted candidate for %s at pick %d; pausing", teamID, number)
		if err := s.store.PauseClock(now); err != nil {
			log.Printf("draft clock: pause on empty pool failed: %v", err)
		}
		return
	}

	// 8. Fire the auto-pick, racing safely against a human pick or a
	// commissioner action via the deadlineSeen token.
	nextDeadline := time.Time{}
	if number < totalPicks {
		nextDeadline = now.Add(s.pickClock(state))
	}
	pick, err := s.store.AutoPick(teamID, playerID, "auto", number, state.ClockDeadline, now, nextDeadline)
	if err != nil {
		if !errors.Is(err, errStaleAutoPick) {
			log.Printf("draft clock: auto-pick failed: %v", err)
		}
		return
	}
	s.emitDraft("draft:pick", s.draftPickPayload(s.store.Snapshot(), pick, now))
	// N6: notify the seat's manager that a pick fired on their behalf,
	// skipping a manager who was CONNECTED at pick time (spec section 3,
	// N6). state is the pre-fire snapshot already read above, matching the
	// world the auto-pick decision itself saw.
	s.notifyAutopickMade(state, pick, reason, now)
}

// effectiveDeadline returns the instant the auto-pick may fire and the
// reason label ("clock", "autopick", or "not_seen"). Presence is otherwise
// observational only: a disconnect, hidden tab, or process restart never
// shortens a live pick. The sole exception is NOT SEEN — a seat whose
// manager has never sent one heartbeat this process lifetime — which caps
// the deadline at NotSeenClock once the process has run past
// NotSeenBootGrace. AWAY and IDLE, which a backgrounded tab can read while
// its manager is fully engaged elsewhere, never shorten anything. An
// explicit AUTO toggle keeps first claim on the shortened deadline: the
// switch below evaluates it before NOT SEEN, so a commissioner's explicit
// authority is never displaced by an observational signal.
func (s *Service) effectiveDeadline(state PersistedState, now time.Time) (time.Time, string) {
	deadline := state.ClockDeadline
	duration := s.pickClock(state)
	armAt := deadline.Add(-duration)
	number := len(state.Picks) + 1
	teamID := teamOnClock(state.DraftOrder, number)

	effective := deadline
	reason := "clock"
	switch {
	case state.Autopick[teamID]:
		if candidate := armAt.Add(AutopickGrace); candidate.Before(effective) {
			effective = candidate
			reason = "autopick"
		}
	case s.teamNeverSeen(state, teamID, now):
		if candidate := armAt.Add(NotSeenClock); candidate.Before(effective) {
			effective = candidate
			reason = "not_seen"
		}
	}
	return effective, reason
}

// teamNeverSeen reports whether every operator assigned to teamID has never
// sent a single heartbeat this process lifetime, and the process has run
// past NotSeenBootGrace so that reading is trustworthy rather than a
// restart artifact. Mirrors presenceStateSince's not_seen bucket exactly
// (see teamPresence, which applies the same per-key classification for the
// UI): a seat with any co-manager who has ever appeared is not NOT SEEN. An
// unclaimed seat (no assigned manager) is never NOT SEEN in this sense —
// there is no manager who has failed to appear. Called by effectiveDeadline
// on every tick, so a seat's first-ever heartbeat clears this on the very
// next tick; nothing here is memoized past that heartbeat.
func (s *Service) teamNeverSeen(state PersistedState, teamID string, now time.Time) bool {
	if now.Sub(s.presence.startedAt) < NotSeenBootGrace {
		return false
	}
	keys := s.presenceKeysForTeam(state, teamID)
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		seenAt, seen := s.presence.seen(key)
		if presenceStateSince(seenAt, seen, now, s.presence.startedAt) != "not_seen" {
			return false
		}
	}
	return true
}

// autopickChoice resolves the player an auto-pick would select for teamID:
// first the seat's Big Board, walked in order and skipping any ID that is
// already picked, does not resolve in the pool, or would breach the
// league's optional Limits knob or would leave too few future picks to fill
// every required starter slot; then best-available ADP order (the pool's
// own draft order), with the same filters. If every remaining candidate
// would breach only a soft Limits cap, the second pass ignores that cap but
// never ignores starter viability. ok is false when no undrafted candidate
// can finish a legal roster — the clock pauses for commissioner attention
// instead of auto-drafting an unusable team.
func (s *Service) autopickChoice(state PersistedState, teamID string) (string, bool) {
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	pool := s.pool()
	fits := func(playerID string) bool {
		_, _, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, nil)
		return !breach && draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID)
	}
	viable := func(playerID string) bool {
		return draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID)
	}
	key := s.boardKeyForTeam(state, teamID)
	for _, id := range state.Boards[key] {
		if picked[id] {
			continue
		}
		if _, ok := pool.byID[id]; ok && fits(id) {
			return id, true
		}
	}
	for _, player := range pool.players {
		if !picked[player.ID] && fits(player.ID) {
			return player.ID, true
		}
	}
	// Fallback: ignore Limits rather than stall the draft.
	for _, id := range state.Boards[key] {
		if picked[id] {
			continue
		}
		if _, ok := pool.byID[id]; ok && viable(id) {
			return id, true
		}
	}
	for _, player := range pool.players {
		if !picked[player.ID] && viable(player.ID) {
			return player.ID, true
		}
	}
	return "", false
}

// draftCandidateKeepsRosterViable is the hard legality boundary shared by
// manual picks, AUTO, commissioner picks, and the Draft Room button state.
// A manager may spend early bench depth however they want, but once only N
// picks remain, at most N required starter slots may still be unfillable.
// Flexible slots are handled by maximum bipartite matching, so an RB already
// assigned conceptually to FLEX can be reassigned to RB when that produces a
// more complete starting shape.
func draftCandidateKeepsRosterViable(state PersistedState, pool map[string]Player, teamID, candidateID string) bool {
	candidate, ok := pool[candidateID]
	if !ok {
		return false
	}
	players := make([]Player, 0, CurrentDraftRounds())
	pickCount := 0
	for _, pick := range state.Picks {
		if pick.TeamID != teamID {
			continue
		}
		pickCount++
		if player, exists := pool[pick.PlayerID]; exists {
			players = append(players, player)
		}
	}
	pickCount++
	players = append(players, candidate)
	remaining := CurrentDraftRounds() - pickCount
	if remaining < 0 {
		return false
	}
	preset := CurrentRoster()
	missing := preset.Starters() - maximumDraftStarterFill(players, preset)
	return missing <= remaining
}

// maximumDraftStarterFill returns the largest number of starting slots the
// supplied roster can fill. This is a small augmenting-path matcher (at most
// 25 players/slots under config validation), which is clearer and safer than
// position-count arithmetic once FLEX and SUPERFLEX overlap fixed slots.
func maximumDraftStarterFill(players []Player, preset RosterPreset) int {
	slots := lineupSlots(preset)
	matchedPlayer := make([]int, len(slots))
	for index := range matchedPlayer {
		matchedPlayer[index] = -1
	}
	var assign func(int, []bool) bool
	assign = func(playerIndex int, seen []bool) bool {
		for slotIndex, slot := range slots {
			if seen[slotIndex] || !slot.Def.Fits(players[playerIndex].Position) {
				continue
			}
			seen[slotIndex] = true
			if matchedPlayer[slotIndex] == -1 || assign(matchedPlayer[slotIndex], seen) {
				matchedPlayer[slotIndex] = playerIndex
				return true
			}
		}
		return false
	}
	filled := 0
	for playerIndex := range players {
		if assign(playerIndex, make([]bool, len(slots))) {
			filled++
		}
	}
	return filled
}

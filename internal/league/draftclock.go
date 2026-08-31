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
	DefaultPickClock = 120 * time.Second
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

	// 1-2. Draft complete: clear a leftover deadline once, then idle. In
	// normal operation MakePick and clockTick's own autopick branch below
	// already zero the clock fields on the pick that completes the draft
	// (and emit draft:state themselves, via maybeEmitDraftComplete), so this
	// branch's condition is false right after a real completion and it does
	// nothing; it exists as a defensive fallback for a state that reaches
	// "complete" with the clock fields still dirty some other way (a
	// restored backup, a rounds/roster-shape change that retroactively
	// completes an in-progress draft).
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

	// Presence keeps updating every tick from here on, regardless of the
	// pick clock's own paused/armed state below: a manager's HERE/IDLE/AWAY
	// status is independent of whether the pick clock itself is running, so
	// pausing the clock must not also freeze the room's presence chips.
	s.emitPresenceTransitions(state, now)

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
	snapshot := s.store.Snapshot()
	s.emitDraft("draft:pick", s.draftPickPayload(snapshot, pick, now))
	s.maybeEmitDraftComplete(state, snapshot, now)
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
// league's optional Limits knob, would leave too few future picks to fill
// every required starter slot, or is blocked by the league-wide scarcity
// guard below; then best-available HOUSE order (houserank.go's
// pool.byHouse — the format-aware replacement-value ranking under the
// league's active roster preset, NOT the pool's market-ADP draft order),
// with the same filters. If every remaining candidate would breach only a
// soft Limits cap, the second pass ignores that cap but keeps both starter
// viability and the scarcity guard. ok is false when no undrafted candidate
// can finish a legal roster — the clock pauses for commissioner attention
// instead of auto-drafting an unusable team. As a last resort, if the
// scarcity guard alone leaves zero viable candidates (every remaining
// legal player is guard-blocked — a state the guard's own math should
// never reach, since it never blocks a position no seat still needs), a
// third pass drops only the guard and keeps starter viability, so a
// stalled clock is never caused by this guard itself. Only autopick's own
// selection order reads pool.byHouse; the board display, the commissioner
// force-pick, and every other "best available" consumer keep reading
// pool.players/byADP (market ADP) untouched.
func (s *Service) autopickChoice(state PersistedState, teamID string) (string, bool) {
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	pool := s.pool()
	preset := CurrentRoster()
	otherTeamIDs := make([]string, 0, len(s.Teams()))
	for _, team := range s.Teams() {
		if team.ID != teamID {
			otherTeamIDs = append(otherTeamIDs, team.ID)
		}
	}
	// scarceCache memoizes positionScarcityBlocksCandidate per real
	// position for this one autopickChoice call: state, picked, and the
	// rosters it reads are fixed for the whole call, so every candidate
	// at the same position shares one answer — a handful of positions,
	// not one bipartite-matching pass per pool row.
	scarceCache := make(map[string]bool, len(housePositionOrder))
	scarce := func(position string) bool {
		if blocked, cached := scarceCache[position]; cached {
			return blocked
		}
		blocked := positionScarcityBlocksCandidate(state, pool, picked, preset, teamID, position, otherTeamIDs)
		scarceCache[position] = blocked
		return blocked
	}
	fits := func(playerID string) bool {
		player, ok := pool.byID[playerID]
		if !ok {
			return false
		}
		_, _, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, nil)
		return !breach && draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID) && !scarce(player.Position)
	}
	viable := func(playerID string) bool {
		player, ok := pool.byID[playerID]
		if !ok {
			return false
		}
		return draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID) && !scarce(player.Position)
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
	for _, player := range pool.byHouse {
		if !picked[player.ID] && fits(player.ID) {
			return player.ID, true
		}
	}
	// Fallback: ignore Limits rather than stall the draft. The scarcity
	// guard survives this fallback — a soft Limits cap and league-wide
	// starvation protection are independent knobs, and relaxing Limits is
	// never a reason to also let a bench pick duplicate the one scarce
	// specialist a peer seat still needs.
	for _, id := range state.Boards[key] {
		if picked[id] {
			continue
		}
		if _, ok := pool.byID[id]; ok && viable(id) {
			return id, true
		}
	}
	for _, player := range pool.byHouse {
		if !picked[player.ID] && viable(player.ID) {
			return player.ID, true
		}
	}
	// Last resort: drop only the scarcity guard, keep starter viability.
	// Reached only when literally no legal candidate survives it — the
	// guard's own predicate should never produce that (it only blocks a
	// position once at least one OTHER seat still needs it, so someone
	// downstream can always legally take the alternative), but a stalled
	// clock is a worse outcome than one guard miss, so this pass exists
	// as the documented, narrow relief valve rather than a silent stall.
	unguardedViable := func(playerID string) bool {
		return draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID)
	}
	for _, id := range state.Boards[key] {
		if picked[id] {
			continue
		}
		if _, ok := pool.byID[id]; ok && unguardedViable(id) {
			return id, true
		}
	}
	for _, player := range pool.byHouse {
		if !picked[player.ID] && unguardedViable(player.ID) {
			return player.ID, true
		}
	}
	return "", false
}

// teamDraftedPlayers resolves teamID's currently drafted players against
// pool, in pick order, and the raw pick count for that team (including any
// pick whose player no longer resolves in pool). Shared by
// draftCandidateKeepsRosterViable and the scarcity guard below, so both
// count a team's roster the exact same way.
func teamDraftedPlayers(state PersistedState, pool map[string]Player, teamID string) (players []Player, pickCount int) {
	for _, pick := range state.Picks {
		if pick.TeamID != teamID {
			continue
		}
		pickCount++
		if player, exists := pool[pick.PlayerID]; exists {
			players = append(players, player)
		}
	}
	return players, pickCount
}

// teamCoversPositionRequirement reports whether players (a team's current
// roster, from teamDraftedPlayers) already fills every starter slot
// position is eligible for — its own dedicated slot count plus any
// FLEX/SUPERFLEX slot that also accepts it. It reuses
// maximumDraftStarterFill, the same bipartite matcher
// draftCandidateKeepsRosterViable calls, against one hypothetical extra
// player at position rather than inventing a parallel per-position
// counting scheme: if that hypothetical player cannot raise the team's
// maximum starter fill, no additional real player at position could
// either, so the requirement is already covered. A position absent from
// every slot (Starters()'s FLEX/SUPERFLEX included) is vacuously always
// covered — there is no starter requirement for it to leave open.
func teamCoversPositionRequirement(players []Player, preset RosterPreset, position string) bool {
	before := maximumDraftStarterFill(players, preset)
	hypothetical := make([]Player, len(players), len(players)+1)
	copy(hypothetical, players)
	hypothetical = append(hypothetical, Player{Position: position})
	after := maximumDraftStarterFill(hypothetical, preset)
	return after <= before
}

// undraftedPositionSupply counts pool's undrafted players at position —
// the scarcity guard's supply side. Reads pool.players (the active,
// annotated pool the board and house order both derive from), not the
// byID map, so a legacy fixture ID that only survives in byID for
// historical-lookup purposes (see buildPool) is never counted as supply
// that could reach a future pick.
func undraftedPositionSupply(players []Player, picked map[string]bool, position string) int {
	supply := 0
	for _, player := range players {
		if player.Position == position && !picked[player.ID] {
			supply++
		}
	}
	return supply
}

// positionScarcityBlocksCandidate is the league-wide starvation guard
// (adversarial review finding, 2026-08-30): HOUSE order (houserank.go)
// clusters same-position players together by VORP, so a seat spending a
// spare bench pick under house order can legally take a SECOND scarce
// specialist while a later seat in the same draft has not yet drafted its
// first — the VORP model has no notion of "someone else needs this more."
// This guard refuses a candidate at position when BOTH:
//
//  1. teamID's own requirement for position is already covered
//     (teamCoversPositionRequirement) — one more player there could not
//     raise teamID's own starter fill, so this specific pick is pure bench
//     depth, not a need.
//  2. The pool's remaining undrafted supply at position would not stretch
//     to cover every OTHER active seat that has not yet covered its own
//     requirement for position (each checked with the identical
//     teamCoversPositionRequirement predicate) — so taking one now could
//     leave a peer seat unable to fill a required starter slot legally at
//     all.
//
// Both conditions read only roster counts, never board contents or pick
// order, so the guard is symmetric across every seat and independent of
// which seat happens to be on the clock. A position with no starter slot
// at all (FLEX/SUPERFLEX absorption included) is vacuously covered for
// every seat, so the guard never fires for it.
func positionScarcityBlocksCandidate(state PersistedState, pool playerPool, picked map[string]bool, preset RosterPreset, teamID, position string, otherTeamIDs []string) bool {
	ownPlayers, _ := teamDraftedPlayers(state, pool.byID, teamID)
	if !teamCoversPositionRequirement(ownPlayers, preset, position) {
		return false
	}
	stillMissing := 0
	for _, other := range otherTeamIDs {
		otherPlayers, _ := teamDraftedPlayers(state, pool.byID, other)
		if !teamCoversPositionRequirement(otherPlayers, preset, position) {
			stillMissing++
		}
	}
	if stillMissing == 0 {
		return false
	}
	return undraftedPositionSupply(pool.players, picked, position) <= stillMissing
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
	players, pickCount := teamDraftedPlayers(state, pool, teamID)
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

package league

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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

// isSpecialistPosition reports whether position is one of the three
// specialist positions (K, DST, P) the owner's autopick directive defers
// behind every hole-filling and bench best-player-available (BPA) skill
// pick (owner instruction, 2026-08-31: "auto pick late should pick BPA
// ... according to remaining holes"). QB, RB, WR, and TE are the only
// non-specialist positions; anything else (there is no fourth kind today)
// is treated as specialist-class too, so autopickHouseWalk's Pass C never
// silently drops an unrecognized position. Used only by autopickHouseWalk
// and its three passes below — every other consumer of housePositionOrder
// keeps its existing, unsplit meaning.
func isSpecialistPosition(position string) bool {
	return position != "QB" && position != "RB" && position != "WR" && position != "TE"
}

// housePassOrder returns the first pool.byHouse player that is undrafted,
// matches want, and passes filter, or ok=false when none does. Shared by
// autopickHouseWalk's three passes, so hole-filling BPA, bench BPA, and
// the specialist pass all walk house order (best VORP first) identically,
// differing only in which players want admits.
func housePassOrder(pool playerPool, picked map[string]bool, filter func(string) bool, want func(Player) bool) (string, bool) {
	for _, player := range pool.byHouse {
		if picked[player.ID] || !want(player) {
			continue
		}
		if filter(player.ID) {
			return player.ID, true
		}
	}
	return "", false
}

// autopickHouseWalk resolves one house-ordered pass of autopickChoice
// under filter (fits, viable, or unguardedViable — whichever legality
// boundary the caller is currently walking) as three ordered sub-passes,
// the owner's refinement to pure VORP order (2026-08-31 directive):
//
//   - Pass A — hole-filling BPA: house order over non-specialists
//     (QB/RB/WR/TE) whose addition would raise teamID's own maximum
//     starter fill (fillsHole, backed by the same maximumDraftStarterFill
//     bipartite matcher draftCandidateKeepsRosterViable and
//     positionScarcityBlocksCandidate already share). This is the best
//     player available among the positions the roster actually still
//     needs, regardless of that candidate's raw VORP rank against a
//     covered position.
//   - Pass B — bench BPA: once no hole-filling non-specialist remains,
//     house order over every non-specialist regardless of hole status —
//     once no skill starter slot is left open, spending a pick on more
//     skill depth is fine, and house order's own VORP ranking is the
//     right tie-break for it.
//   - Pass C — specialists (K/DST/P): reached only once no non-specialist
//     at all still passes filter. Pure VORP house order places the first
//     kicker in a realistic ~182-player pool around house rank 45 — early
//     enough that a board-less seat would draft its first kicker around
//     round 6 of a 17-round draft — which is not what "best player
//     available" should mean for a position every seat needs exactly
//     once. Passes A and B already exhaust every non-specialist that is
//     still a legal, useful pick; a specialist reaching this pass is by
//     construction a real hole (the viability boundary baked into every
//     filter this walk runs under forces skill picks to stop being legal
//     once the remaining picks must cover the unfilled specialist slots).
//
// The three passes share one house-order walk (housePassOrder) rather
// than three hand-rolled loops, so every future filter this function is
// called with automatically gets the same split.
func autopickHouseWalk(pool playerPool, picked map[string]bool, filter func(string) bool, fillsHole func(position string) bool) (string, bool) {
	holeFiller := func(player Player) bool { return !isSpecialistPosition(player.Position) && fillsHole(player.Position) }
	if id, ok := housePassOrder(pool, picked, filter, holeFiller); ok {
		return id, true
	}
	nonSpecialist := func(player Player) bool { return !isSpecialistPosition(player.Position) }
	if id, ok := housePassOrder(pool, picked, filter, nonSpecialist); ok {
		return id, true
	}
	specialist := func(player Player) bool { return isSpecialistPosition(player.Position) }
	return housePassOrder(pool, picked, filter, specialist)
}

// autopickChoice resolves the player an auto-pick would select for teamID:
// first the seat's Big Board, walked in order and skipping any ID that is
// already picked, does not resolve in the pool, or would breach the
// league's optional Limits knob, would leave too few future picks to fill
// every required starter slot, or is blocked by the league-wide scarcity
// guard below; then best-available HOUSE order (houserank.go's
// pool.byHouse — the format-aware replacement-value ranking under the
// league's active roster preset, NOT the pool's market-ADP draft order),
// with the same filters, split into autopickHouseWalk's three
// hole-filling/bench/specialist passes (owner directive, 2026-08-31). If
// every remaining candidate would breach only a soft Limits cap, the
// second pass ignores that cap but keeps both starter viability and the
// scarcity guard. ok is false when no undrafted candidate can finish a
// legal roster — the clock pauses for commissioner attention instead of
// auto-drafting an unusable team. As a last resort, if the scarcity guard
// alone leaves zero viable candidates (every remaining legal player is
// guard-blocked — a state the guard's own math should never reach, since
// it never blocks a position no seat still needs), a third pass drops
// only the guard and keeps starter viability, so a stalled clock is never
// caused by this guard itself — this pass keeps autopickHouseWalk's
// hole-filling/bench/specialist split too, but never the scarcity guard.
// Only autopick's own selection order reads pool.byHouse; the board
// display, the commissioner force-pick, and every other "best available"
// consumer keep reading pool.players/byADP (market ADP) untouched.
func (s *Service) autopickChoice(state PersistedState, teamID string) (string, bool) {
	return autopickChoiceWith(state, s.pool(), s.Teams(), s.boardKeyForTeam(state, teamID), teamID)
}

// autopickChoiceWith is autopickChoice's store-free core (practice draft,
// practice.go): every Service-owned input — the pool, the team list, and
// the seat's board key — arrives as an argument, so the same strategy can
// run for a sandbox seat exactly as it runs for the real clock. The
// wrapper above is what clockTick calls; nothing about its behavior moved.
func autopickChoiceWith(state PersistedState, pool playerPool, teams []Team, boardKey, teamID string) (string, bool) {
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	preset := CurrentRoster()
	otherTeamIDs := make([]string, 0, len(teams))
	for _, team := range teams {
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
	// holeCache memoizes fillsHole per real position for this one call,
	// the same reasoning as scarceCache: teamID's own drafted roster
	// (ownPlayers) is fixed for the whole call, so every candidate at the
	// same position shares one maximumDraftStarterFill comparison.
	ownPlayers, _ := teamDraftedPlayers(state, pool.byID, teamID)
	holeCache := make(map[string]bool, len(housePositionOrder))
	fillsHole := func(position string) bool {
		if hole, cached := holeCache[position]; cached {
			return hole
		}
		hole := !teamCoversPositionRequirement(ownPlayers, preset, position)
		holeCache[position] = hole
		return hole
	}
	// nextPickNumber/currentRound/totalRounds (Wave D, owner debrief
	// 2026-09-06): the per-position cap below is round-aware (K/P/DST
	// lift in the final rounds), and the board value guard below compares
	// a board entry's own house rank against the pick it would spend.
	nextPickNumber := len(state.Picks) + 1
	currentRound := pickRound(activeTeamCount(state.DraftOrder), nextPickNumber)
	totalRounds := CurrentDraftRounds()
	// capCache memoizes autopickPositionCapBlocksCandidate per position,
	// the same reasoning as scarceCache/holeCache: ownPlayers, preset, and
	// the round are fixed for the whole call.
	capCache := make(map[string]bool, len(housePositionOrder))
	capBlocked := func(position string) bool {
		if blocked, cached := capCache[position]; cached {
			return blocked
		}
		blocked := autopickPositionCapBlocksCandidate(ownPlayers, preset, position, currentRound, totalRounds)
		capCache[position] = blocked
		return blocked
	}
	fits := func(playerID string) bool {
		player, ok := pool.byID[playerID]
		if !ok {
			return false
		}
		_, _, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, nil)
		return !breach && draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID) && !scarce(player.Position) && !capBlocked(player.Position)
	}
	viable := func(playerID string) bool {
		player, ok := pool.byID[playerID]
		if !ok {
			return false
		}
		return draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID) && !scarce(player.Position) && !capBlocked(player.Position)
	}
	key := boardKey
	for _, id := range state.Boards[key] {
		if picked[id] {
			continue
		}
		player, ok := pool.byID[id]
		if !ok || !fits(id) {
			continue
		}
		// Board value guard (owner debrief 2026-09-06: a manager's Big
		// Board #1, house rank ~140, was autopicked at pick 16 overall).
		// Skip this board entry — fall to the NEXT board entry, then to
		// house order below — only when the house order can offer some
		// OTHER candidate that fills a starter hole this entry does not;
		// see autopickBoardEntryFailsValueGuard's own doc comment for why
		// that second condition keeps the guard from second-guessing a
		// board that has nothing better to redirect to.
		if autopickBoardEntryFailsValueGuard(player, nextPickNumber, pool, picked, fits, fillsHole) {
			log.Printf("autopick: %s skipped board entry %s (%s, house rank %d) at pick %d — more than %d ranks below the pick, and house order offers a needed alternative",
				teamID, player.ID, player.Position, player.HouseRank, nextPickNumber, autopickBoardValueGuardMargin)
			continue
		}
		return id, true
	}
	if id, ok := autopickHouseWalk(pool, picked, fits, fillsHole); ok {
		return id, true
	}
	// Fallback: ignore Limits rather than stall the draft. The scarcity
	// guard and the position cap survive this fallback — a soft Limits
	// cap is an independent knob from either, and relaxing Limits is
	// never a reason to also let a bench pick duplicate the one scarce
	// specialist a peer seat still needs, or blow past the position cap.
	for _, id := range state.Boards[key] {
		if picked[id] {
			continue
		}
		player, ok := pool.byID[id]
		if !ok || !viable(id) {
			continue
		}
		if autopickBoardEntryFailsValueGuard(player, nextPickNumber, pool, picked, viable, fillsHole) {
			log.Printf("autopick: %s skipped board entry %s (%s, house rank %d) at pick %d — more than %d ranks below the pick, and house order offers a needed alternative",
				teamID, player.ID, player.Position, player.HouseRank, nextPickNumber, autopickBoardValueGuardMargin)
			continue
		}
		return id, true
	}
	if id, ok := autopickHouseWalk(pool, picked, viable, fillsHole); ok {
		return id, true
	}
	// Last resort: drop the scarcity guard AND the position cap, keep
	// starter viability. Reached only when literally no legal candidate
	// survives them — neither guard's own predicate should ever produce
	// that on its own (the scarcity guard only blocks a position once at
	// least one OTHER seat still needs it, so someone downstream can
	// always legally take the alternative; the position cap only blocks a
	// position teamID already holds a legal roster's worth of), but a
	// stalled clock is a worse outcome than one guard miss, so this pass
	// exists as the documented, narrow relief valve rather than a silent
	// stall. No board value guard here either, for the same reason.
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
	if id, ok := autopickHouseWalk(pool, picked, unguardedViable, fillsHole); ok {
		return id, true
	}
	return "", false
}

// autopickPositionCapDefault is the flat per-team cap AUTOPICK enforces
// for K, P, and DST (Wave D, owner debrief after the 2026-09-06 draft: one
// team ended with six quarterbacks — the QB/RB/WR/TE cap below covers
// that; the K/P/DST cap covers the same unchecked-hoarding shape for the
// specialist positions, which house order's own VORP model has no notion
// of "enough" for either). A specialist bench stash is a real, if rare,
// late-draft need — see autopickPositionCap's own final-rounds carve-out.
const autopickPositionCapDefault = 1

// autopickPositionCapFinalRounds is how many rounds from the very end of
// the draft the K/P/DST flat cap stops applying.
const autopickPositionCapFinalRounds = 2

// autopickPositionCap resolves the roster-shape-derived cap AUTOPICK
// enforces for position, given the draft's current round and total round
// count. applies is false when no cap governs position at all right now
// (K/P/DST in the final autopickPositionCapFinalRounds rounds) — the
// caller then never blocks a candidate at position on this guard.
//
// K, P, and DST share the flat autopickPositionCapDefault cap through the
// rest of the draft. Every other position's cap is the SUM of every
// preset starter slot position is eligible for (slotTable's own Eligible
// list, lineup.go — position's own dedicated slot plus any FLEX/
// SUPERFLEX slot that also accepts it: gridiron-house's QB is eligible
// for QB and SUPERFLEX, giving a cap of 1+1+1(bench)=3) plus one bench
// buffer — the same "starters this position could occupy" count
// teamCoversPositionRequirement (above) already uses for hole-filling,
// so the cap and the hole-filling logic agree on what "a starter at this
// position" means. This also keeps every position's own cap sum at or
// above the preset's own Total() roster size (verified for every shipped
// preset by TestAutopickPositionCapsSumCoverEveryPresetsRosterSize,
// houserank_test.go's own sibling suite): a team can always find some
// legal, in-cap position for all Total() of its picks, so
// autopickChoiceWith's own last-resort "drop the cap" pass (this file,
// above) is a true rarity — a genuine supply shortage, never an
// arithmetic certainty the cap itself created.
func autopickPositionCap(preset RosterPreset, position string, currentRound, totalRounds int) (limit int, applies bool) {
	switch position {
	case "K", "P", "DST":
		if totalRounds-currentRound < autopickPositionCapFinalRounds {
			return 0, false
		}
		return autopickPositionCapDefault, true
	default:
		eligible := 0
		for _, slot := range slotTable {
			if slot.Fits(position) {
				eligible += preset.Slots[slot.Key]
			}
		}
		return eligible + 1, true
	}
}

// autopickPositionCapBlocksCandidate reports whether teamID (represented
// here by ownPlayers, its already-drafted roster) already holds
// autopickPositionCap(position)'s own limit worth of position. Scoped to
// AUTOPICK's own selection order only (autopickChoiceWith/
// autopickHouseWalk, both above) — a manager's own manual pick (MakePick,
// store.go) is never subject to it; a human choosing a sixth QB on
// purpose is a decision, not a defect.
func autopickPositionCapBlocksCandidate(ownPlayers []Player, preset RosterPreset, position string, currentRound, totalRounds int) bool {
	limit, applies := autopickPositionCap(preset, position, currentRound, totalRounds)
	if !applies {
		return false
	}
	count := 0
	for _, player := range ownPlayers {
		if player.Position == position {
			count++
		}
	}
	return count >= limit
}

// autopickBoardValueGuardMargin bounds how far below the CURRENT pick a
// board-first candidate's own house rank may sit before
// autopickBoardEntryFailsValueGuard prefers to redirect autopick toward
// house order's own next real need instead (owner debrief 2026-09-06: a
// manager's Big Board #1, house rank ~140, was autopicked at his second
// selection, pick 16 overall).
const autopickBoardValueGuardMargin = 24

// autopickBoardEntryFailsValueGuard reports whether a board-first
// candidate should be skipped in favor of house order's own next pick:
// both candidate's own house rank sits more than
// autopickBoardValueGuardMargin ranks below pickNumber (the pick this
// candidate would be spent at), AND house order can offer some OTHER
// candidate that fills a starter hole this one does not. A player with no
// house rank at all (HouseRank 0 — a zero-Projection player, houserank.go)
// never fails this guard: there is no rank to compare against a pick
// number.
//
// The second condition keeps the guard narrow on purpose: without it,
// autopick would routinely bump a manager's own late-round sleeper for
// whatever ranks "better," second-guessing the board with no roster
// benefit at all. The guard exists to redirect autopick toward a real,
// still-open starter need, never to run its own private best-player-
// available pass over a manager's own ranking. filter is whichever
// legality boundary the caller is currently walking (fits or viable, the
// two board-first passes that call this) — never unguardedViable's last
// resort, which takes this guard's own doc comment's advice and skips it
// entirely rather than risk a stall.
func autopickBoardEntryFailsValueGuard(candidate Player, pickNumber int, pool playerPool, picked map[string]bool, filter func(string) bool, fillsHole func(string) bool) bool {
	if candidate.HouseRank <= 0 || candidate.HouseRank-pickNumber <= autopickBoardValueGuardMargin {
		return false
	}
	// holeFiller excludes candidate itself: house order's own walk below
	// starts at rank 1 and virtually always finds a BETTER, distinct
	// candidate before ever reaching candidate's own (far worse) rank —
	// but this exclusion keeps that a guarantee, not a coincidence of
	// walk order, so the guard can never answer "yes, redirect" by
	// finding no one but the very candidate it is evaluating.
	holeFiller := func(player Player) bool {
		return player.ID != candidate.ID && !isSpecialistPosition(player.Position) && fillsHole(player.Position)
	}
	_, ok := housePassOrder(pool, picked, filter, holeFiller)
	return ok
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
	blocked, _, _ := positionScarcityDetail(state, pool, picked, preset, teamID, position, otherTeamIDs)
	return blocked
}

// positionScarcityDetail is positionScarcityBlocksCandidate's own
// computation, exposed with the raw supply/stillMissing counts (rules-
// audit item 3) so a caller that must explain WHY a pick is blocked —
// MakePick's validation message, see positionScarcityMessage — reads the
// exact numbers the guard itself decided on, rather than recomputing them
// separately and risking the two drifting apart. supply and stillMissing
// are both 0 when blocked is false and the guard never reached the supply
// count (an uncovered-requirement short-circuit, matching
// positionScarcityBlocksCandidate's own early returns).
func positionScarcityDetail(state PersistedState, pool playerPool, picked map[string]bool, preset RosterPreset, teamID, position string, otherTeamIDs []string) (blocked bool, supply, stillMissing int) {
	ownPlayers, _ := teamDraftedPlayers(state, pool.byID, teamID)
	if !teamCoversPositionRequirement(ownPlayers, preset, position) {
		return false, 0, 0
	}
	for _, other := range otherTeamIDs {
		otherPlayers, _ := teamDraftedPlayers(state, pool.byID, other)
		if !teamCoversPositionRequirement(otherPlayers, preset, position) {
			stillMissing++
		}
	}
	if stillMissing == 0 {
		return false, 0, 0
	}
	supply = undraftedPositionSupply(pool.players, picked, position)
	return supply <= stillMissing, supply, stillMissing
}

// positionScarcityMessage renders positionScarcityDetail's counts as the
// plain-language validation text a manual pick sees when MakePick refuses
// a scarcity-blocked candidate (rules-audit item 3: before this, only
// autopick ever consulted the guard, so a manager could freely hoard a
// scarce position — 12 punters drafted onto 8 seats while a peer still
// needed a first one — with no in-app refusal at all). It reuses
// lineupPositionPlainName (lineup.go) for the same spelled-out noun the
// lineup pages already name a position by, so a manager who has never
// seen the roster abbreviation table still understands the refusal.
func positionScarcityMessage(position string, supply, stillMissing int) string {
	noun := strings.ToLower(position)
	if plain, ok := lineupPositionPlainName[position]; ok {
		noun = plain
	}
	verb := "is"
	if supply != 1 {
		noun = pluralizePositionNoun(noun)
		verb = "are"
	}
	return fmt.Sprintf("Only %d %s %s left for %d teams that still need one; pick another position first.", supply, noun, verb, stillMissing)
}

// pluralizePositionNoun pluralizes a lineupPositionPlainName noun for
// positionScarcityMessage. A noun that already ends in "s" — today only
// "defense/special teams" — is left unchanged rather than getting a
// second "s", since it already reads as plural in context ("Only 2
// defense/special teams are left...").
func pluralizePositionNoun(noun string) string {
	if strings.HasSuffix(noun, "s") {
		return noun
	}
	return noun + "s"
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

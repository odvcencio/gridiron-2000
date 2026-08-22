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
	}
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
	// N6: notify the seat's manager that a pick fired on their behalf,
	// skipping a manager who was CONNECTED at pick time (spec section 3,
	// N6). state is the pre-fire snapshot already read above, matching the
	// world the auto-pick decision itself saw.
	s.notifyAutopickMade(state, pick, reason, now)
}

// effectiveDeadline returns the instant the auto-pick may fire and the
// reason label ("clock" or "autopick"). Presence is observational only:
// a disconnect, hidden tab, or process restart never shortens a live pick.
func (s *Service) effectiveDeadline(state PersistedState, now time.Time) (time.Time, string) {
	deadline := state.ClockDeadline
	duration := s.pickClock(state)
	armAt := deadline.Add(-duration)
	number := len(state.Picks) + 1
	teamID := teamOnClock(state.DraftOrder, number)

	effective := deadline
	reason := "clock"
	if state.Autopick[teamID] {
		if candidate := armAt.Add(AutopickGrace); candidate.Before(effective) {
			effective = candidate
			reason = "autopick"
		}
	}
	return effective, reason
}

// autopickChoice resolves the player an auto-pick would select for teamID:
// first the seat's Big Board, walked in order and skipping any ID that is
// already picked, does not resolve in the pool, or would breach the
// league's optional Limits knob (mirrors the board-panel filter in
// DraftData); then best-available ADP order (the pool's own draft order),
// same Limits filter. If every remaining candidate would breach some
// limit — a degenerate config, should not happen in practice — the
// second pass falls back to ignoring Limits entirely rather than pausing
// the draft indefinitely; a live draft finishing is more important than a
// soft cap. ok is false only when neither source has any candidate at all
// — an empty or exhausted pool (section 8.6).
func (s *Service) autopickChoice(state PersistedState, teamID string) (string, bool) {
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	pool := s.pool()
	fits := func(playerID string) bool {
		_, _, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, nil)
		return !breach
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
		if _, ok := pool.byID[id]; ok {
			return id, true
		}
	}
	for _, player := range pool.players {
		if !picked[player.ID] {
			return player.ID, true
		}
	}
	return "", false
}

// Roster-ops ticker: the daily waiver-processing run (roster-ops spec
// section 5.4) plus trade execution and expiry (section 6.1's
// T-exec/T-expire steps).
package league

import (
	"context"
	"log"
	"time"
)

// rosterOpsTickPeriod is StartRosterOps's evaluation interval — the exact
// StartNotifier shape (notifications.go), a 60-second resolution that is
// exact enough against a once-daily run.
const rosterOpsTickPeriod = 60 * time.Second

// StartRosterOps runs the 60-second roster-ops evaluation loop. Call it
// once from main.go, beside StartDraftClock. Unlike StartNotifier, this
// loop always starts: state mutations (waiver processing, and WP-R5's
// trade execution/expiry) must run with mail wired or not — only the send
// step at the end of rosterOpsTick is itself notifyReady-gated, matching
// every other notify hook (section 5.4's "why a separate ticker" note).
// All decision logic lives in rosterOpsTick, which tests drive directly
// with a fake clock; no goroutine, no sleeps, in the test suite.
func (s *Service) StartRosterOps(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(rosterOpsTickPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.rosterOpsTick(s.clock())
			}
		}
	}()
}

// rosterOpsTick is the whole time-driven roster-ops decision for one
// instant (section 5.4), evaluated in order: the waiver run predicate,
// trade execution and expiry (section 6.1's T-exec/T-expire steps), then
// the healed-IR lifecycle (SK IR rule).
func (s *Service) rosterOpsTick(now time.Time) {
	s.evalWaiverRun(now)
	s.evalTradeTick(now)
	s.evalHealedIR(now)
}

// ---------------------------------------------------------------------
// Healed-IR lifecycle (roster-ops SK spec, 2026-08-18 owner decision): a
// healed IR player must be activated (with a corresponding drop) before
// his NFL team's next kickoff. Unresolved at kickoff, the platform
// auto-cuts him — a typed auto-drop transaction — and he becomes
// waiver-eligible the following week, never an instant free agent.
// ---------------------------------------------------------------------

// healedIRWarnWindow is the due window before an IR occupant's deadline
// kickoff the warning notification fires in — the N18 lineup-warning
// precedent's own window (notifications.go: "due window
// [firstKickoff(week)-24h, firstKickoff(week)]"), reused here rather than
// inventing a second one.
const healedIRWarnWindow = 24 * time.Hour

// deferredClearsAt resolves an auto-cut player's clear instant (SK IR
// rule: "waiver-eligible THE FOLLOWING WEEK, not an instant free agent").
// Unlike a manager's own drop (clearsAt's clear_days formula), an
// auto-cut always defers to the first daily waiver-processing run at or
// after the following NFL week's earliest kickoff — never sooner, even
// when clear_days would resolve faster. When no game anchors "the
// following week" (an empty or exhausted schedule mirror), this falls
// back to one week (7 days) past droppedAt, then the standard first-run
// rule, so the mechanism still terminates against a thin test schedule.
func deferredClearsAt(cfg Config, games []GameInfo, droppedAt time.Time) time.Time {
	week := lineupCurrentWeekAt(games, droppedAt)
	if first, ok := firstKickoff(games, week+1); ok {
		return firstRunAtOrAfter(cfg, first)
	}
	return firstRunAtOrAfter(cfg, droppedAt.Add(7*24*time.Hour))
}

// evalHealedIR evaluates every team's IR occupants on every roster-ops
// tick: an occupant whose injury designation no longer qualifies
// (irEligible, zones.go) is "healed." Inside the due window before his
// NFL team's next kickoff, a warning notification fires (N19, once per
// week per player — recordAndSend's ledger); at or after that kickoff,
// unresolved, the platform auto-cuts him (AutoCutHealedIR, one atomic
// persist). A nil injury source (never wired) makes this whole
// evaluation a no-op — IR eligibility cannot be reconfirmed, so nothing
// is ever declared healed. Idempotent by construction: AutoCutHealedIR
// removes the zone assignment it just cut, so a repeat tick's fresh
// snapshot never re-sees an already-cut player.
func (s *Service) evalHealedIR(now time.Time) {
	source := s.injuryDesignationSource()
	if source == nil {
		return
	}
	state := s.store.Snapshot()
	if len(state.RosterZones) == 0 {
		return
	}
	games := s.schedule()
	pool := s.pool()
	week := lineupCurrentWeekAt(games, now)
	for teamID, zones := range state.RosterZones {
		for playerID, za := range zones {
			if za.Zone != zoneIR {
				continue
			}
			player, ok := pool.byID[playerID]
			if !ok {
				continue
			}
			if irEligible(source, player) {
				continue // still qualifies; stays parked on IR
			}
			kickoff, ok := nextKickoffForTeam(games, player.NFLTeam, now)
			if !ok {
				continue // no scheduled game to anchor a deadline against
			}
			if now.Before(kickoff.Add(-healedIRWarnWindow)) {
				continue // not due yet
			}
			if now.Before(kickoff) {
				s.notifyHealedIRWarning(state, teamID, player, week, kickoff, now)
				continue
			}
			s.autoCutHealedIR(teamID, player, week, now)
		}
	}
}

// autoCutHealedIR appends the SK IR rule's typed auto-drop transaction and
// clears the zone assignment, one atomic Store call.
func (s *Service) autoCutHealedIR(teamID string, player Player, week int, now time.Time) {
	if _, err := s.store.AutoCutHealedIR(teamID, transactionPlayerFromPlayer(player), s.cfg.Season, week, now); err != nil {
		log.Printf("roster ops: AutoCutHealedIR(%s, %s) failed: %v", teamID, player.ID, err)
	}
}

// evalTradeTick implements section 6.1's T-exec and T-expire steps: every
// open offer past its 7-day age or the trade deadline expires; every
// accepted offer past AcceptedAt + review_hours attempts execution
// (Store.ExecuteTradeOffer — the same routine ApproveTrade's early
// execution calls). An offer a community veto has already resolved never
// reaches this switch in "accepted" state (Store.FileTradeVetoOffer's doc
// comment), so "vetoes below threshold" (section 6.1's T-exec note) needs
// no separate check here. Each offer resolves through its own Store call,
// one lock and one persist per offer — trades are rare enough that a
// shared per-tick persist (ProcessWaivers's batching) buys nothing.
func (s *Service) evalTradeTick(now time.Time) {
	state := s.store.Snapshot()
	if len(state.TradeOffers) == 0 {
		return
	}
	games := s.schedule()
	pool := s.pool()
	starterCount, rosterCap := tradeRosterBounds()
	for _, offer := range state.TradeOffers {
		switch offer.Status {
		case TradeStatusOpen:
			if _, err := s.store.ExpireTradeOffer(offer.ID, s.cfg, now); err != nil {
				log.Printf("roster ops: ExpireTradeOffer(%s) failed: %v", offer.ID, err)
			}
		case TradeStatusAccepted:
			reviewDeadline := offer.AcceptedAt.Add(time.Duration(s.cfg.Trades.ReviewHours) * time.Hour)
			if now.Before(reviewDeadline) {
				continue
			}
			txn, err := s.store.ExecuteTradeOffer(offer.ID, s.cfg, games, pool.byID, now, starterCount, rosterCap)
			if err != nil {
				s.notifyTradeFailed(offer)
				continue
			}
			s.notifyTradeExecuted(offer, txn)
		}
	}
}

// evalWaiverRun implements section 5.4 step 1: resolve nextRun from
// WaiversProcessedThrough and cfg.Waivers.ProcessTime; a zero
// WaiversProcessedThrough baselines to now without processing (no
// retroactive runs on a fresh or migrated state, the notifyLastPruneAt
// boot precedent inverted); otherwise, once now reaches nextRun, run
// Store.ProcessWaivers and fire N14 for every resolved claim.
func (s *Service) evalWaiverRun(now time.Time) {
	state := s.store.Snapshot()
	cfg := s.cfg

	if state.WaiversProcessedThrough.IsZero() {
		if err := s.store.BaselineWaiversProcessedThrough(now); err != nil {
			log.Printf("roster ops: baseline WaiversProcessedThrough failed: %v", err)
		}
		return
	}

	nextRun := firstRunStrictlyAfter(cfg, state.WaiversProcessedThrough)
	if now.Before(nextRun) {
		return
	}

	games := s.schedule()
	pool := s.pool()
	rosterCap := CurrentRoster().Total()
	results, err := s.store.ProcessWaivers(nextRun, cfg, games, pool.byID, rosterCap)
	if err != nil {
		log.Printf("roster ops: ProcessWaivers failed: %v", err)
		return
	}
	for _, result := range results {
		s.notifyWaiverResult(result, nextRun)
	}
}

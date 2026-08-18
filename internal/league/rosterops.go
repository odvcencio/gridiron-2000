// Roster-ops ticker: the daily waiver-processing run (roster-ops spec
// section 5.4). Trade execution and expiry (section 6.1's T-exec/T-expire
// steps) join rosterOpsTick in WP-R5; this file only ever drives waivers
// today.
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
// then (WP-R5) trade execution and expiry.
func (s *Service) rosterOpsTick(now time.Time) {
	s.evalWaiverRun(now)
	// TODO(WP-R5): trade execution (T-exec) and expiry (T-expire), section
	// 6.1 — the same tick, evaluated after waivers.
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

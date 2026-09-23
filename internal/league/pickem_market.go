package league

import (
	"context"
	"log"
	"time"

	"gridiron-2000/internal/loopguard"
)

const pickemFallbackThursdayHour = 20
const pickemFallbackThursdayMinute = 15

const pickemMarketSyncPeriod = time.Minute

// pickemWeekMarketLock returns the shared weekly market freeze boundary.
// The first Thursday kickoff is authoritative. A schedule without a
// Thursday game falls back to 8:15 PM Eastern on the preceding Thursday.
// Zero-kickoff/TBA games do not participate in boundary discovery.
func pickemWeekMarketLock(games []GameInfo, eastern *time.Location) time.Time {
	if eastern == nil {
		eastern, _ = time.LoadLocation("America/New_York")
	}
	var earliest, firstThursday time.Time
	for _, game := range games {
		if game.Kickoff.IsZero() {
			continue
		}
		local := game.Kickoff.In(eastern)
		if earliest.IsZero() || local.Before(earliest) {
			earliest = local
		}
		if local.Weekday() == time.Thursday && (firstThursday.IsZero() || local.Before(firstThursday)) {
			firstThursday = local
		}
	}
	if !firstThursday.IsZero() {
		return firstThursday
	}
	if earliest.IsZero() {
		return time.Time{}
	}
	daysBack := (int(earliest.Weekday()) - int(time.Thursday) + 7) % 7
	if daysBack == 0 {
		daysBack = 7
	}
	thursday := earliest.AddDate(0, 0, -daysBack)
	return time.Date(thursday.Year(), thursday.Month(), thursday.Day(), pickemFallbackThursdayHour, pickemFallbackThursdayMinute, 0, 0, eastern)
}

func pickemMarketLock(game GameInfo, weekLock time.Time) time.Time {
	if game.Kickoff.IsZero() {
		return time.Time{}
	}
	if weekLock.IsZero() || game.Kickoff.Before(weekLock) {
		return game.Kickoff
	}
	return weekLock
}

func marketFromGame(game GameInfo, lockAt time.Time) PickemMarket {
	return PickemMarket{
		Week: game.Week, Kickoff: game.Kickoff, Away: game.Away, Home: game.Home, LockAt: lockAt,
		LineTenths: game.SpreadLineTenths, LinePresent: game.SpreadLinePresent,
		ObservedAt: game.SourceObservedAt.UTC(), SourceUpdatedAt: game.SourceUpdatedAt.UTC(),
		SourceURL: game.SourceURL, SourceProvenance: game.SourceProvenance,
	}
}

func marketObservationEligible(game GameInfo, at time.Time) bool {
	return game.SpreadLinePresent && !game.SourceObservedAt.IsZero() && !game.SourceObservedAt.After(at)
}

// ReconcilePickemMarkets refreshes moving market candidates and freezes each
// contest line exactly once. At or after lock, only an observation made at or
// before the boundary is eligible. If neither the durable candidate nor the
// current feed qualifies, the game becomes NO LINE/VOID rather than silently
// becoming straight-up or accepting a late price.
func (s *Store) ReconcilePickemMarkets(now time.Time, games []GameInfo, eastern *time.Location) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	weekGames := make(map[int][]GameInfo)
	for _, game := range games {
		weekGames[game.Week] = append(weekGames[game.Week], game)
	}
	changed := false
	for _, game := range games {
		weekLock := pickemWeekMarketLock(weekGames[game.Week], eastern)
		lockAt := pickemMarketLock(game, weekLock)
		if lockAt.IsZero() {
			continue
		}
		current, exists := s.state.PickemMarkets[game.ID]
		if exists && (current.Frozen || current.Void) {
			continue
		}
		incomingEligibleNow := marketObservationEligible(game, now)
		if now.Before(lockAt) {
			if incomingEligibleNow && (!exists || current.ObservedAt.IsZero() || !game.SourceObservedAt.Before(current.ObservedAt)) {
				s.state.PickemMarkets[game.ID] = marketFromGame(game, lockAt)
				changed = true
			}
			continue
		}

		candidate := current
		haveCandidate := exists && current.LinePresent && !current.ObservedAt.IsZero() && !current.ObservedAt.After(lockAt)
		if marketObservationEligible(game, lockAt) && (!haveCandidate || !game.SourceObservedAt.Before(current.ObservedAt)) {
			candidate = marketFromGame(game, lockAt)
			haveCandidate = true
		}
		if haveCandidate {
			candidate.LockAt = lockAt
			candidate.Frozen = true
			candidate.FrozenAt = now.UTC()
			candidate.Void = false
			candidate.VoidReason = ""
		} else {
			candidate = marketFromGame(game, lockAt)
			candidate.LinePresent = false
			candidate.Frozen = false
			candidate.Void = true
			candidate.VoidReason = "no eligible market line before lock"
			candidate.FrozenAt = now.UTC()
		}
		s.state.PickemMarkets[game.ID] = candidate
		changed = true
	}
	if !changed {
		return nil
	}
	return s.persistLocked(colPickemMarkets)
}

// pickemMarketTick is the testable, single-pass market synchronizer. It
// records moving candidates before the boundary and performs the exact-once
// freeze/void transition at or after it, then backfills any legacy pick's
// missing entry timestamp (BackfillPickemEnteredAt).
//
// Both used to also run inline on GET (DashboardData, ActionCenterData,
// PickemData, pickemHomeSummary — audit item 10, 2026-09-23): a page load
// silently wrote to the store, with its error discarded (`_ =`). They now
// run only here, on this function's own ticker (StartPickemMarketSync),
// with every error logged instead of swallowed. A GET handler reads
// whatever this tick last reconciled — at most pickemMarketSyncPeriod
// stale, the same staleness the ticker already promised while a page was
// closed.
func (s *Service) pickemMarketTick(now time.Time) error {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		eastern = time.UTC
	}
	games := s.schedule()
	if err := s.store.ReconcilePickemMarkets(now, games, eastern); err != nil {
		return err
	}
	return s.store.BackfillPickemEnteredAt(games)
}

// StartPickemMarketSync starts the schedule-to-contest-line lifecycle. The
// immediate pass protects a restart close to the boundary; the short ticker
// keeps the durable candidate current between nflverse refreshes.
func (s *Service) StartPickemMarketSync(ctx context.Context) {
	if err := s.pickemMarketTick(s.clock()); err != nil {
		log.Printf("pickem market sync: %v", err)
	}
	go func() {
		ticker := time.NewTicker(pickemMarketSyncPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// loopguard.Tick: a panic in one pass must not end pickem
				// market sync for the rest of the process's life (audit
				// item 12); the next tick still runs.
				loopguard.Tick("pickemMarketSync", 0, func() {
					if err := s.pickemMarketTick(s.clock()); err != nil {
						log.Printf("pickem market sync: %v", err)
					}
				})
			}
		}
	}()
}

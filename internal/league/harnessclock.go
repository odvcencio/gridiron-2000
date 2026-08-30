package league

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// SetClockForTest overrides the now hook: when set, clock() reads the
// harness clock instead of the package-test now field. Harness only: it is
// ignored when APP_ENV=production so a leaked flag can never move a live
// league's time. Pass nil to clear the override and fall back to now (or,
// absent that, time.Now()).
func (s *Service) SetClockForTest(now func() time.Time) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return
	}
	if now == nil {
		s.testNow.Store(nil)
		return
	}
	s.testNow.Store(&now)
}

// ClockForTest reads the service's current instant through the same seam as
// every production call site. Unlike SetClockForTest, it is not guarded by
// APP_ENV: it always returns clock()'s result, so it works in every
// environment.
func (s *Service) ClockForTest() time.Time { return s.clock() }

// SeedStarterForTest directly records a pick and pins it into teamID's
// week starting slot, bypassing the draft-pick clock and the SetLineup
// service wrapper's kickoff lock entirely (Store.MakePick,
// Store.SetLineupSlot — the same two-call sequence live_state_test.go
// uses from inside this package). It exists so a render fixture outside
// this package (app/matchups/page_render_test.go's "live" fixture) can
// seat a real starter without running an actual draft — the same
// "ForTest" seam SetClockForTest/ClockForTest already expose. Harness
// only: it is refused when APP_ENV=production, matching
// SetClockForTest's own guard. store.draftLifecycleBypass is the same
// private escape hatch newTestService flips directly for in-package
// tests (service_test.go); Store.MakePick still enforces the draft-order
// "on the clock" check unconditionally, so callers must still make picks
// in the league's real snake order (team-1, then team-2, ...). The flag
// is restored to its prior value on return, so back-to-back calls (or a
// caller that had already set its own bypass) never leave the store
// permanently unguarded after this one call returns.
func (s *Service) SeedStarterForTest(teamID string, week int, slot, playerID string) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return fmt.Errorf("SeedStarterForTest is refused in production")
	}
	previousBypass := s.store.draftLifecycleBypass
	s.store.draftLifecycleBypass = true
	defer func() { s.store.draftLifecycleBypass = previousBypass }()
	now := s.clock()
	if _, err := s.store.MakePick(teamID, playerID, "manager", now, time.Time{}); err != nil {
		return err
	}
	return s.store.SetLineupSlot(teamID, week, slot, playerID, now)
}

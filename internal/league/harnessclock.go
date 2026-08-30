package league

import (
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

package league

import (
	"os"
	"strings"
	"time"
)

// SetClockForTest replaces the service clock. Harness only: it is ignored
// when APP_ENV=production so a leaked flag can never move a live league's
// time. Pass nil to restore the real clock. It is the exported twin of the
// s.now assignment package tests use.
func (s *Service) SetClockForTest(now func() time.Time) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return
	}
	s.testNow.Store(&now)
}

// ClockForTest reports the service's current time through the same seam.
func (s *Service) ClockForTest() time.Time { return s.clock() }

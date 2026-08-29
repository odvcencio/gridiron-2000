package league

import (
	"testing"
	"time"
)

func TestSetClockForTestDrivesServiceClock(t *testing.T) {
	service := newTestService(t, false)
	fixed := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	service.SetClockForTest(func() time.Time { return fixed })
	if got := service.clock(); !got.Equal(fixed) {
		t.Fatalf("clock() = %v, want %v", got, fixed)
	}
	if got := service.ClockForTest(); !got.Equal(fixed) {
		t.Fatalf("ClockForTest() = %v, want %v", got, fixed)
	}
	service.SetClockForTest(nil)
	if got := service.clock(); got.Equal(fixed) {
		t.Fatalf("clearing the test clock did not restore the real clock: %v", got)
	}
}

func TestSetClockForTestIsIgnoredInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	service := newTestService(t, false)
	service.SetClockForTest(func() time.Time { return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) })
	if got := service.clock(); got.Year() == 2000 {
		t.Fatalf("production service accepted a test clock: %v", got)
	}
}

package league

import (
	"testing"
	"time"
)

func TestSetClockForTestDrivesServiceClock(t *testing.T) {
	t.Setenv("APP_ENV", "test")
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

func TestSetClockForTestTakesPrecedenceOverNow(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	service := newTestService(t, false)
	nowValue := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	harnessValue := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return nowValue }

	service.SetClockForTest(func() time.Time { return harnessValue })
	if got := service.clock(); !got.Equal(harnessValue) {
		t.Fatalf("clock() = %v, want harness value %v", got, harnessValue)
	}

	service.SetClockForTest(nil)
	if got := service.clock(); !got.Equal(nowValue) {
		t.Fatalf("clock() = %v, want now value %v after clearing the harness clock", got, nowValue)
	}
}

func TestSetClockForTestAppEnvGuard(t *testing.T) {
	cases := []struct {
		name    string
		appEnv  string
		ignored bool
	}{
		{name: "production", appEnv: "production", ignored: true},
		{name: "mixed case Production", appEnv: "Production", ignored: true},
		{name: "padded production", appEnv: " production ", ignored: true},
		{name: "staging", appEnv: "staging", ignored: false},
		{name: "unset", appEnv: "", ignored: false},
	}
	sentinel := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("APP_ENV", testCase.appEnv)
			service := newTestService(t, false)
			service.SetClockForTest(func() time.Time { return sentinel })
			got := service.clock()
			gotSentinel := got.Equal(sentinel)
			if testCase.ignored && gotSentinel {
				t.Fatalf("APP_ENV=%q: expected SetClockForTest to be ignored, got %v", testCase.appEnv, got)
			}
			if !testCase.ignored && !gotSentinel {
				t.Fatalf("APP_ENV=%q: expected SetClockForTest to be accepted, got %v", testCase.appEnv, got)
			}
		})
	}
}

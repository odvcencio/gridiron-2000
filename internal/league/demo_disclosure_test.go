package league

import (
	"net/http"
	"testing"
)

// TestMutatingSurfacesExposeDemoModeTruthfully guards wave-6 item 6: the
// REHEARSAL MODE disclosure only rendered on /, /admin, and /draft. /board,
// /locker, and /trades (this agent's ownership) now render their own copy
// of it from this same top-level demo_mode key, matching the convention
// admin.go and notification_settings.go already use.
func TestMutatingSurfacesExposeDemoModeTruthfully(t *testing.T) {
	for _, demo := range []bool{true, false} {
		service := newTestService(t, demo)
		request, _ := http.NewRequest(http.MethodGet, "/", nil)

		if got := service.BoardData(request)["demo_mode"]; got != demo {
			t.Errorf("BoardData demo_mode = %v, want %v", got, demo)
		}
		if got := service.LockerData(request)["demo_mode"]; got != demo {
			t.Errorf("LockerData demo_mode = %v, want %v", got, demo)
		}
		if got := service.TradesData(request)["demo_mode"]; got != demo {
			t.Errorf("TradesData demo_mode = %v, want %v", got, demo)
		}
	}
}

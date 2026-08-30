// Package liveaccess is the one authorization rule every live-update
// socket in this app shares: the draft-live hub (app/draft/live.go) and
// the scores-live hub (app/matchups/live.go) both gate their WebSocket
// upgrade through SignedInOrDemo, rather than each keeping its own copy
// (round-2 review of commit 917cf4f, finding 4).
package liveaccess

import (
	"net/http"

	"gridiron-2000/internal/league"
)

// SignedInOrDemo reports whether request may open a live-update
// connection for service: true when the league is in demo/rehearsal mode
// (every visitor is treated as an authenticated member), or when the
// request maps to a signed-in league member through
// league.Service.CurrentUser. A nil service never authorizes anything.
func SignedInOrDemo(service *league.Service) func(*http.Request) bool {
	return func(request *http.Request) bool {
		if service == nil {
			return false
		}
		if service.DemoMode() {
			return true
		}
		_, signedIn := service.CurrentUser(request)
		return signedIn
	}
}

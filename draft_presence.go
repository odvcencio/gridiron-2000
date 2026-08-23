package main

import (
	"net/http"
	"time"

	"m31labs.dev/gosx/server"
)

const (
	leagueVersionEndpoint  = "/api/league/version"
	leaguePresenceEndpoint = "/api/league/presence"
)

// leagueHeartbeatEndpoint keeps the single GoSX body marker honest. The
// version heartbeat is a global synchronization primitive, while presence is
// an attendance claim and therefore belongs only to an open Draft Room. The
// Draft Room's fragment polls continue to carry its live room/workspace state.
func leagueHeartbeatEndpoint(requestPath string) string {
	path := requestPath
	for len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	if path == "/draft" {
		return leaguePresenceEndpoint
	}
	return leagueVersionEndpoint
}

type leaguePresenceRecorder interface {
	RecordPresence(*http.Request, time.Time)
}

// registerLeagueHeartbeatAPIs deliberately keeps the two heartbeat effects
// in separate handlers. A version poll must remain side-effect free: every
// authenticated page except Draft Room uses it for state synchronization,
// and treating those background requests as attendance would make the draft
// clock claim managers who are not in the room.
func registerLeagueHeartbeatAPIs(app *server.App, recorder leaguePresenceRecorder, fingerprint func() string) {
	app.API("GET "+leaguePresenceEndpoint, func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		recorder.RecordPresence(ctx.Request, time.Now())
		return map[string]any{"ok": true}, nil
	})
	app.API("GET "+leagueVersionEndpoint, func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return map[string]any{"fingerprint": fingerprint()}, nil
	})
}

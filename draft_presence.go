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
// Draft Room's live hub continues to carry its room/workspace state.
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
//
// now stamps every recorded heartbeat. It must be the league service's own
// clock (league.Default().ClockForTest, which is time.Now() unless a
// harness build has overridden it) rather than a bare time.Now(): the draft
// clock's NOT-SEEN/AWAY/IDLE classification in internal/league/draftclock.go
// reads presence against that same clock, so a harness run that advances
// the service clock must advance presence with it, or a bot's own heartbeat
// reads back as stale the instant the clock moves.
func registerLeagueHeartbeatAPIs(app *server.App, recorder leaguePresenceRecorder, fingerprint func() string, now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	app.API("GET "+leaguePresenceEndpoint, func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		recorder.RecordPresence(ctx.Request, now())
		return map[string]any{"ok": true}, nil
	})
	app.API("GET "+leagueVersionEndpoint, func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return map[string]any{"fingerprint": fingerprint()}, nil
	})
}

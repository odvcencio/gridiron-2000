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

// leagueHeartbeatEndpoint keeps the single GoSX body marker honest. It must
// return leaguePresenceEndpoint on /draft (an attendance claim; nowhere else
// belongs there) and "" everywhere else, never leagueVersionEndpoint: the
// client's heartbeat ping (gosx#216, client/runtime/host/navigation.ts
// pingHeartbeat) is fire-and-forget by contract — it discards both a
// network failure and a 2xx body alike, so it can never itself perform
// version-fingerprint state sync. That sync is data-gosx-revalidate-src's
// job (every page's own <main> declares it where freshness matters), which
// actually reads the response and swaps the document. Before this endpoint
// was route-aware, every non-draft page's heartbeat pointed at
// leagueVersionEndpoint too, so a route that also declared
// data-gosx-revalidate-src="/api/league/version" fired two identical GETs
// to that URL every 4s tick — one that did the real work (revalidate) and
// one whose response the runtime always threw away (heartbeat). Returning
// "" here (which BuildApp's layout must treat as "omit the body marker
// entirely," not as an empty-string src) removes that duplicate and, on the
// handful of routes with no revalidate poll at all, removes a ping that
// never had a client-visible effect to begin with.
func leagueHeartbeatEndpoint(requestPath string) string {
	path := requestPath
	for len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	if path == "/draft" {
		return leaguePresenceEndpoint
	}
	return ""
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

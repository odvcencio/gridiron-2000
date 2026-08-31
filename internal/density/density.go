// Package density carries the viewer's data-density preference (P1-6,
// UI pass 2026-08-30): comfortable (the default, >=13px data text
// everywhere) or compact (the pre-UI-pass 12px size, for a manager who
// wants more rows on screen).
//
// It rides the existing signed session cookie (session.Store) rather than
// a bespoke cookie or a durable per-manager record. The league's own
// per-identity preference store (league.Service.SetNotificationPreference)
// requires a signed-in Google identity and is read-only in demo mode; the
// density preference has to apply to every viewer, signed in or not, so
// it does not fit that path. The session store already backs every
// request's CSRF token (session.Token) and is read/written the same way
// here: Set marks it dirty, and the session middleware (app.Use(sessions.
// Middleware), app_build.go) commits it back as part of the response the
// same request that called Set already produces.
package density

import (
	"net/http"

	"m31labs.dev/gosx/session"
)

// sessionKey is the session.Store key that holds the preference.
const sessionKey = "density"

// Compact and Comfortable are the two legal preference values. Comfortable
// is also the zero value: an empty or unrecognized session value reads
// back as Comfortable, never as an error.
const (
	Compact     = "compact"
	Comfortable = "comfortable"
)

// Value reads the viewer's density preference off the request's session,
// defaulting to Comfortable when there is no session or no preference on
// it yet.
func Value(r *http.Request) string {
	if r == nil {
		return Comfortable
	}
	if store := session.Current(r); store != nil {
		if store.String(sessionKey) == Compact {
			return Compact
		}
	}
	return Comfortable
}

// IsCompact reports whether the viewer asked for the compact density.
func IsCompact(r *http.Request) bool {
	return Value(r) == Compact
}

// Set persists value onto the request's current session so every later
// page the viewer loads carries it, until they change it again. An
// unrecognized value is stored as Comfortable rather than rejected: the
// only two buttons the settings page renders submit Compact or
// Comfortable literally, so anything else reaching here is already a
// malformed request, not a real third state to preserve.
func Set(r *http.Request, value string) {
	if r == nil {
		return
	}
	store := session.Current(r)
	if store == nil {
		return
	}
	if value != Compact {
		value = Comfortable
	}
	store.Set(sessionKey, value)
}

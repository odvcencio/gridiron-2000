package main

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/server"
)

// isLoopbackRemote reports whether r's RemoteAddr is a loopback address
// (127.0.0.0/8 or ::1). Shared by testRoutesLoopbackOnly below and
// harnessProvider in app_build.go: harnessProvider runs inside
// authManager.Middleware, which executes before app.Mount's own routing —
// and therefore before testRoutesLoopbackOnly ever runs — so a non-loopback
// request carrying X-Test-User would otherwise register a member via
// EnsureMember before the /test/* route it was headed to ever got a chance
// to answer 403.
func isLoopbackRemote(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// testRoutesLoopbackOnly rejects any request whose RemoteAddr is not a
// loopback address. main.go binds the HTTP server to every interface
// (":"+rt.Port), so without this guard the /test/* surface would be
// reachable from any host that can reach the process's port, not just the
// machine running it. It is applied to every route this file mounts.
func testRoutesLoopbackOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemote(r) {
			http.Error(w, "harness routes are loopback-only", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// mountTestRoutes adds the harness-only surface. BuildApp mounts it only
// when cfg.TestAuth is true, which AppConfig.validate refuses outside a
// local environment. Every route answers GET only, which keeps GET-safe
// CSRF middleware (session.Protect checks only unsafe methods) out of the
// harness path entirely — GET is a convenience, not an authorization
// control. The routes' real guards are GRIDIRON_TEST_AUTH (checked by
// BuildApp before this is ever called) and testRoutesLoopbackOnly above.
//
// It returns a restore func that clears the harness clock override
// installed below (lazily, on the first /test/clock request — a harness
// build that never calls /test/clock never touches the process-wide
// league clock at all). AppRuntime.Close calls the returned func; it is
// safe to call more than once and safe to call even when /test/clock was
// never hit — it only calls SetClockForTest(nil) when installed is
// actually true. installed is a plain bool under mu, not a sync.Once,
// specifically so a /test/clock request after a Close reinstalls the
// override instead of being permanently disarmed: not installed ->
// installed -> closed -> installed again.
func mountTestRoutes(app *server.App, service *league.Service, authManager *auth.Manager) func() {
	var mu sync.Mutex
	offset := time.Duration(0)
	var fixed *time.Time
	installed := false
	installClock := func() {
		mu.Lock()
		already := installed
		installed = true
		mu.Unlock()
		if already {
			return
		}
		service.SetClockForTest(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			if fixed != nil {
				return *fixed
			}
			return time.Now().Add(offset)
		})
	}
	restoreClock := func() {
		mu.Lock()
		wasInstalled := installed
		installed = false
		mu.Unlock()
		if wasInstalled {
			service.SetClockForTest(nil)
		}
	}

	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	app.Mount("GET /test/clock", testRoutesLoopbackOnly(func(w http.ResponseWriter, r *http.Request) {
		installClock()
		query := r.URL.Query()
		resetting := query.Get("reset") == "1"
		// Parse both parameters before mutating any state. A malformed
		// advance must not leave a well-formed set applied halfway: the
		// request is all-or-nothing, so a 400 always means the clock did
		// not move at all.
		var newFixed *time.Time
		settingFixed := false
		if set := query.Get("set"); set != "" {
			at, err := time.Parse(time.RFC3339, set)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			newFixed = &at
			settingFixed = true
		}
		var advanceBy time.Duration
		advancing := false
		if advance := query.Get("advance"); advance != "" {
			d, err := time.ParseDuration(advance)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			advanceBy = d
			advancing = true
		}
		mu.Lock()
		// reset clears first, so "reset=1&advance=10s" composes as "back
		// to wall time, then 10s ahead of it"; set (if also given) then
		// overrides reset's clear; advance always applies last, on top of
		// whichever base the two above left behind.
		if resetting {
			fixed = nil
			offset = 0
		}
		if settingFixed {
			fixed = newFixed
		}
		if advancing {
			if fixed != nil {
				moved := fixed.Add(advanceBy)
				fixed = &moved
			} else {
				offset += advanceBy
			}
		}
		mu.Unlock()
		writeJSON(w, map[string]any{"now": service.ClockForTest().UTC()})
	}))
	app.Mount("GET /test/draft", testRoutesLoopbackOnly(func(w http.ResponseWriter, r *http.Request) {
		data := service.DraftDataReadOnly(r)
		draft, _ := data["draft"].(map[string]any)
		viewer, _ := data["viewer"].(map[string]any)
		viewerTeamID, _ := viewer["team_id"].(string)
		writeJSON(w, map[string]any{
			"started":     draft["started"],
			"complete":    draft["complete"],
			"pick_number": data["pick_number"],
			"on_clock_id": data["on_clock_id"],
			// viewer_team_id is the only reliable way a bot learns its own
			// seat: actingTeam (internal/league/service.go) ignores a
			// submitted team_id form field and derives the acting seat
			// from the signed-in identity's own membership record.
			"viewer_team_id":      viewerTeamID,
			"clock":               data["clock"],
			"current_pick_token":  data["current_pick_token"],
			"previous_pick_token": data["previous_pick_token"],
			"picks":               data["picks"],
			"available":           data["available"],
			"teams":               data["teams"],
		})
	}))
	// /test/signin lets a browser (which cannot set X-Test-User) become a
	// manager: it signs the session in through the same path the Google
	// callback uses, then redirects to `to` (default /draft).
	app.Mount("GET /test/signin", testRoutesLoopbackOnly(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.URL.Query().Get("user"))
		if raw == "" {
			http.Error(w, "user=email|name is required", http.StatusBadRequest)
			return
		}
		email, name, _ := strings.Cut(raw, "|")
		email = strings.TrimSpace(email)
		if name = strings.TrimSpace(name); name == "" {
			name = email
		}
		if email == "" {
			http.Error(w, "user=email|name is required", http.StatusBadRequest)
			return
		}
		// Mirror main.go's googleCallbackHandlerWithMembership (~763-816)
		// exactly, minus its EmailAllowed admission gate: canonicalize the
		// identity, sign the session in, then bind a pending co-manager
		// invite if this email has one — falling back to the ordinary
		// seatless EnsureMember only when it does not, exactly as the
		// callback's own BindCoManagerOnSignIn/EnsureMember branch does.
		// EmailAllowed is deliberately NOT checked here: GRIDIRON_TEST_AUTH
		// (BuildApp's gate on ever mounting this route) and
		// testRoutesLoopbackOnly above are this route's admission control,
		// not the league's invite policy.
		user := service.CanonicalUser(auth.User{ID: email, Email: email, Name: name})
		if strings.TrimSpace(user.ID) == "" {
			user.ID = user.Email
		}
		if !authManager.SignIn(r, user) {
			http.Error(w, "sign-in failed", http.StatusInternalServerError)
			return
		}
		if _, bound, err := service.BindCoManagerOnSignIn(user.Email, user.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if !bound {
			if _, err := service.EnsureMember(user.Email, user.Name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		to := r.URL.Query().Get("to")
		// Reject anything that is not a same-origin absolute path: an empty
		// value, a scheme-relative "//host" target, and a backslash, which a
		// browser normalizes to "/" and which therefore turns "/\evil.example"
		// into the same open redirect as "//evil.example".
		if to == "" || !strings.HasPrefix(to, "/") || strings.HasPrefix(to, "//") || strings.ContainsRune(to, '\\') {
			to = "/draft"
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
	}))
	return restoreClock
}

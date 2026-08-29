package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/server"
)

// mountTestRoutes adds the harness-only surface. BuildApp mounts it only
// when cfg.TestAuth is true, which AppConfig.validate refuses outside a
// local environment. Every route is GET so no CSRF token is needed.
func mountTestRoutes(app *server.App, service *league.Service, authManager *auth.Manager) {
	var mu sync.Mutex
	offset := time.Duration(0)
	var fixed *time.Time
	service.SetClockForTest(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		if fixed != nil {
			return *fixed
		}
		return time.Now().Add(offset)
	})
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	app.Mount("GET /test/clock", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
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
	app.Mount("GET /test/draft", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := service.DraftDataReadOnly(r)
		draft, _ := data["draft"].(map[string]any)
		writeJSON(w, map[string]any{
			"started":             draft["started"],
			"complete":            draft["complete"],
			"pick_number":         data["pick_number"],
			"on_clock_id":         data["on_clock_id"],
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
	app.Mount("GET /test/signin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		// Canonicalize through the same identity-alias boundary the Google
		// callback applies (main.go's googleCallbackHandlerWithMembership,
		// via league.Service.CanonicalUser) before registering membership or
		// signing the session in, so a harness principal lands on the exact
		// email a real sign-in would land on.
		user := service.CanonicalUser(auth.User{ID: email, Email: email, Name: name})
		if strings.TrimSpace(user.ID) == "" {
			user.ID = user.Email
		}
		if _, err := service.EnsureMember(user.Email, user.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !authManager.SignIn(r, user) {
			http.Error(w, "sign-in failed", http.StatusInternalServerError)
			return
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
}

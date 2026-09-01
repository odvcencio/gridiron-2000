package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/league"
)

// setupTokenIdleTimeout is the design's boot-printed setup token's idle
// window (design section 3.3): the bound session idles out after 60 minutes
// without a wizard action, and the process then mints and prints a fresh
// token. A token never outlives the process.
const setupTokenIdleTimeout = 60 * time.Minute

// setupTokenEncoding matches the invite-link/magic-link token alphabet
// (internal/league's setupTokenEncoding): lowercase, unpadded base32.
var setupTokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// setupTokenGuard holds the process's one live setup token in memory only
// (design section 3.3: "Storage: SHA-256 of the token in process memory
// only. Never persisted, never logged after mint."). A successful claim
// binds an opaque epoch value into the claiming request's encrypted
// session; every later /setup request must present that same epoch to stay
// authorized, so an idle re-mint (or a restart, which simply discards the
// whole process and its guard) invalidates every session that has not
// proven it holds the current mint.
type setupTokenGuard struct {
	mu           sync.Mutex
	now          func() time.Time
	print        func(rawToken string)
	idleTimeout  time.Duration
	hash         string
	epoch        string
	claimed      bool
	lastActivity time.Time
}

// newSetupTokenGuard mints the first token and returns a guard ready to
// serve /setup requests. print is called once per mint (including every
// idle re-mint) with the raw token, exactly the moment it is safe to show
// it — see printSetupTokenBanner.
func newSetupTokenGuard(idleTimeout time.Duration, now func() time.Time, print func(string)) (*setupTokenGuard, error) {
	if now == nil {
		now = time.Now
	}
	if print == nil {
		print = func(string) {}
	}
	g := &setupTokenGuard{now: now, print: print, idleTimeout: idleTimeout}
	if err := g.remintLocked(); err != nil {
		return nil, err
	}
	return g, nil
}

func randomSetupToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate setup token: %w", err)
	}
	return strings.ToLower(setupTokenEncoding.EncodeToString(raw)), nil
}

func hashSetupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// remintLocked mints a fresh token and epoch, discards any prior claim, and
// prints the new token. Callers hold g.mu.
func (g *setupTokenGuard) remintLocked() error {
	raw, err := randomSetupToken(32)
	if err != nil {
		return err
	}
	epoch, err := randomSetupToken(16)
	if err != nil {
		return err
	}
	g.hash = hashSetupToken(raw)
	g.epoch = epoch
	g.claimed = false
	g.lastActivity = g.now()
	g.print(raw)
	return nil
}

// maybeExpireLocked re-mints when a claimed token has idled past
// idleTimeout. Callers hold g.mu.
func (g *setupTokenGuard) maybeExpireLocked() {
	if g.claimed && g.idleTimeout > 0 && g.now().Sub(g.lastActivity) > g.idleTimeout {
		_ = g.remintLocked()
	}
}

// Claim attempts to bind candidate as the current setup session. ok is true
// exactly once per mint, for the first caller to present the correct raw
// token; epoch is then the value the caller must store in its encrypted
// session and echo back on every later request (via Authorized). already is
// true when candidate is correct but a different session already claimed
// this mint — the "already claimed" truthful page.
func (g *setupTokenGuard) Claim(candidate string) (epoch string, ok bool, already bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maybeExpireLocked()
	if !league.ConstantTimeTokenEqual(hashSetupToken(candidate), g.hash) {
		return "", false, false
	}
	if g.claimed {
		return "", false, true
	}
	g.claimed = true
	g.lastActivity = g.now()
	return g.epoch, true, false
}

// Authorized reports whether sessionEpoch still matches the guard's current,
// unexpired claim.
func (g *setupTokenGuard) Authorized(sessionEpoch string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maybeExpireLocked()
	return g.claimed && sessionEpoch != "" && sessionEpoch == g.epoch
}

// Touch records a wizard action, resetting the 60-minute idle clock.
func (g *setupTokenGuard) Touch() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.claimed {
		g.lastActivity = g.now()
	}
}

// setupRateLimiter is a small fixed-window-plus-lockout limiter (design
// section 3.3: "5 token attempts per minute per IP, then a 30-second
// lockout. Defense in depth against log noise, not entropy."). It is
// intentionally simple: this gates a boot-time console token, not a
// production authentication surface.
type setupRateLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	limit   int
	window  time.Duration
	lockout time.Duration
	state   map[string]*setupRateLimiterEntry
}

type setupRateLimiterEntry struct {
	windowStart time.Time
	count       int
	lockedUntil time.Time
}

func newSetupRateLimiter(limit int, window, lockout time.Duration, now func() time.Time) *setupRateLimiter {
	if now == nil {
		now = time.Now
	}
	return &setupRateLimiter{now: now, limit: limit, window: window, lockout: lockout, state: map[string]*setupRateLimiterEntry{}}
}

// Allow reports whether key (an IP address) may make another attempt right
// now, and records the attempt when it does.
func (l *setupRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry, ok := l.state[key]
	if !ok {
		entry = &setupRateLimiterEntry{windowStart: now}
		l.state[key] = entry
	}
	if now.Before(entry.lockedUntil) {
		return false
	}
	// A just-expired lockout always starts a fresh window: without this, a
	// lockout shorter than the window (30s lockout, 1-minute window) would
	// leave the stale over-limit count in place and re-lock on the very
	// next attempt, turning a 30-second lockout into an effectively
	// indefinite one.
	if !entry.lockedUntil.IsZero() || now.Sub(entry.windowStart) > l.window {
		entry.windowStart = now
		entry.count = 0
		entry.lockedUntil = time.Time{}
	}
	entry.count++
	if entry.count > l.limit {
		entry.lockedUntil = now.Add(l.lockout)
		return false
	}
	return true
}

// setupRequestIP extracts the bare host from r.RemoteAddr for the rate
// limiter key. Gridiron does not trust X-Forwarded-For anywhere else in
// this codebase (test_routes.go's isLoopbackRemote reads RemoteAddr
// directly too), so this stays consistent rather than trusting a
// client-supplied header for a security decision.
func setupRequestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// printSetupTokenBanner is the Jupyter/Grafana-style boot banner (design
// section 3.3): stdout and the log, both the bare token and the full
// tokenized URL. baseURL is best-effort (empty when unknown); the token
// itself is always shown so copy-paste still works over a bare host:port.
func printSetupTokenBanner(baseURL, token string) {
	line := fmt.Sprintf("SETUP: open %s/setup and enter token %s", strings.TrimSuffix(baseURL, "/"), token)
	if baseURL == "" {
		line = fmt.Sprintf("SETUP: open /setup and enter token %s", token)
	}
	fmt.Println(line)
	if baseURL != "" {
		fmt.Printf("SETUP: full link %s/setup?token=%s\n", strings.TrimSuffix(baseURL, "/"), token)
	}
	log.Println(line)
}

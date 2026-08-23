package league

import (
	"sync"
	"time"
)

const (
	// PollPeriod documents the client heartbeat: main.go's SetLayout
	// callback pings the route-scoped /api/league/presence endpoint every
	// PollPeriod while the Draft Room is open via
	// data-gosx-heartbeat-interval (gosx#216). Keep the two in sync.
	PollPeriod = 4 * time.Second

	// PresenceConnectedWithin: three poll periods. One lost poll plus
	// network latency stays CONNECTED. Two consecutive losses do not.
	PresenceConnectedWithin = 12 * time.Second

	// PresenceIdleWithin: a hidden tab stops polling (document.hidden
	// guard). Sixty seconds covers a quick tab switch or phone lock. Past
	// it, the manager is AWAY for observation and notification copy only.
	PresenceIdleWithin = 60 * time.Second
)

// presenceTracker records the last poll time per viewer key. In-memory
// only: a persisted timestamp would poison StateFingerprint (it would
// change on every poll, forcing every client to revalidate every 4
// seconds), force disk churn on every heartbeat, and be actively
// misleading after a restart (stale "seen" beats an honest "never seen").
// This assumes one server process; a multi-replica deploy would need a
// shared presence store.
//
// The zero value is ready to use: mu and lastSeen work with no
// initialization (lastSeen lazily allocates on first write), and startedAt
// reads as the zero time, which floors every key to "away" until a real
// poll arrives — the same honest-empty behavior newPresenceTracker gives
// explicitly at process start.
type presenceTracker struct {
	mu        sync.Mutex
	lastSeen  map[string]time.Time // viewer key -> last poll time
	startedAt time.Time            // process start; floor for cap math
}

// newPresenceTracker returns a tracker floored at startedAt (normally
// time.Now() at process start; tests pass a fake instant).
func newPresenceTracker(startedAt time.Time) presenceTracker {
	return presenceTracker{lastSeen: map[string]time.Time{}, startedAt: startedAt}
}

// record stores key's last-seen time. An empty key (anonymous, non-demo) is
// a no-op.
func (p *presenceTracker) record(key string, now time.Time) {
	if key == "" {
		return
	}
	p.mu.Lock()
	if p.lastSeen == nil {
		p.lastSeen = map[string]time.Time{}
	}
	p.lastSeen[key] = now
	p.mu.Unlock()
}

// seen returns key's last-seen time and whether it has ever been recorded.
func (p *presenceTracker) seen(key string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	seenAt, ok := p.lastSeen[key]
	return seenAt, ok
}

// presenceState classifies a seen last-seen instant against now: here
// (silence at or under 12s), idle (at or under 60s), or away (older).
// Call presenceStateSince when NOT SEEN must remain distinct from an
// observed-but-old heartbeat.
func presenceState(lastSeen time.Time, now time.Time) string {
	if lastSeen.IsZero() {
		return "away"
	}
	age := now.Sub(lastSeen)
	switch {
	case age <= PresenceConnectedWithin:
		return "here"
	case age <= PresenceIdleWithin:
		return "idle"
	default:
		return "away"
	}
}

// presenceStateSince preserves the operationally important distinction
// between an operator who has never heartbeated since this process started
// and one who was seen and then went quiet. A restart intentionally resets
// that observation window: it must never manufacture a stale "present"
// claim from persisted data.
func presenceStateSince(lastSeen time.Time, seen bool, now, startedAt time.Time) string {
	if !seen || lastSeen.IsZero() || lastSeen.Before(startedAt) {
		return "not_seen"
	}
	return presenceState(lastSeen, now)
}

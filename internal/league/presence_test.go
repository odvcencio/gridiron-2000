package league

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// newPresenceTestService builds a Service with a fake clock and a presence
// tracker floored at start, following the direct-construction pattern
// service_test.go already uses. demo controls whether every seat shares the
// "demo-guest" presence key.
func newPresenceTestService(t *testing.T, demo bool, start time.Time) (*Service, *time.Time) {
	t.Helper()
	clock := start
	svc := &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		feed:     newLiveFeed(nil),
		draftAt:  start.Add(-time.Hour),
		demoMode: demo,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		cfg:      DefaultConfig(),
		presence: newPresenceTracker(start),
		now:      func() time.Time { return clock },
	}
	return svc, &clock
}

// TestPresenceStateTransitions checks the boundaries named in the pick-clock
// spec: 0s/12s/12.1s/60s/60.1s of silence map to
// connected/connected/idle/idle/away, an unseen key is away, and a fresh
// ping restores connected.
func TestPresenceStateTransitions(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	if got := presenceState(time.Time{}, start); got != "away" {
		t.Errorf("unseen key = %q, want away", got)
	}

	cases := []struct {
		age  time.Duration
		want string
	}{
		{0, "connected"},
		{12 * time.Second, "connected"},
		{12*time.Second + 100*time.Millisecond, "idle"},
		{60 * time.Second, "idle"},
		{60*time.Second + 100*time.Millisecond, "away"},
	}
	for _, tc := range cases {
		lastSeen := start
		now := start.Add(tc.age)
		if got := presenceState(lastSeen, now); got != tc.want {
			t.Errorf("age %v: presenceState = %q, want %q", tc.age, got, tc.want)
		}
	}

	// A fresh ping restores connected from any prior state, including away.
	longSilence := start.Add(10 * time.Minute)
	if got := presenceState(longSilence, longSilence); got != "connected" {
		t.Errorf("a ping at now = %q, want connected", got)
	}
}

// TestRecordPresenceRespectsViewerKey checks the public wiring: a demo
// request records under the shared "demo-guest" key (every seat's presence
// moves together), and an anonymous, non-demo request — no viewer key —
// is a no-op, not a panic.
func TestRecordPresenceRespectsViewerKey(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	request, _ := http.NewRequest(http.MethodGet, "/api/league/version", nil)

	demo, _ := newPresenceTestService(t, true, start)
	demo.RecordPresence(request, start)
	if seenAt, ok := demo.presence.seen("demo-guest"); !ok || !seenAt.Equal(start) {
		t.Fatalf("demo-guest not recorded: seenAt=%v ok=%v", seenAt, ok)
	}

	live, _ := newPresenceTestService(t, false, start)
	live.RecordPresence(request, start) // anonymous, non-demo: no key, no-op
	if _, ok := live.presence.seen(""); ok {
		t.Fatal("an empty viewer key must never be recorded")
	}
}

// TestPresenceDigestStableBetweenTransitions checks that the digest string
// is identical while every team's presence bucket is unchanged, and changes
// when exactly one team's bucket flips.
func TestPresenceDigestStableBetweenTransitions(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)

	member, _, err := service.store.AssignMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	service.presence.record(member.Email, start)

	state := service.store.Snapshot()
	digest1 := service.presenceDigest(state, start)

	// Two seconds later, still within the CONNECTED window: no bucket
	// changed, so the digest must be byte-identical.
	*clock = start.Add(2 * time.Second)
	digest2 := service.presenceDigest(state, *clock)
	if digest1 != digest2 {
		t.Fatalf("digest changed with no bucket transition:\n%q\n%q", digest1, digest2)
	}

	// Past the CONNECTED window: exactly one team (team-1, a@example.com's
	// seat) flips to idle, so the digest must change.
	*clock = start.Add(20 * time.Second)
	digest3 := service.presenceDigest(state, *clock)
	if digest3 == digest2 {
		t.Fatalf("digest did not change on an idle transition: %q", digest3)
	}
}

// TestFingerprintChangesOnPresenceTransition checks that StateFingerprint
// changes when a tracked seat's presence bucket transitions (connected ->
// idle here), holding the persisted state and pool version fixed.
func TestFingerprintChangesOnPresenceTransition(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)
	member, _, err := service.store.AssignMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	service.presence.record(member.Email, start)

	before := service.StateFingerprint(1)
	*clock = start.Add(13 * time.Second) // past PresenceConnectedWithin
	after := service.StateFingerprint(1)
	if before == after {
		t.Fatal("fingerprint did not change on a connected -> idle presence transition")
	}
}

// TestFingerprintChangesOnClockEvents checks that every clock event named
// in the pick-clock spec's test plan — arm, a pick's clock reset, pause,
// resume, extend, and a duration set — changes the fingerprint.
func TestFingerprintChangesOnClockEvents(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)

	fp := service.StateFingerprint(1)
	step := func(label string, mutate func()) {
		t.Helper()
		mutate()
		next := service.StateFingerprint(1)
		if next == fp {
			t.Fatalf("%s: fingerprint did not change", label)
		}
		fp = next
	}

	step("arm", func() {
		if err := service.store.ArmClock(start.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
	})
	step("pick resets the clock", func() {
		team := teamOnClock(nil, 1)
		if _, err := service.store.MakePick(team, "p-01", "manager", *clock, clock.Add(90*time.Second)); err != nil {
			t.Fatal(err)
		}
	})
	step("pause", func() {
		if err := service.store.PauseClock(*clock); err != nil {
			t.Fatal(err)
		}
	})
	step("resume", func() {
		if err := service.store.ResumeClock(*clock, DefaultPickClock); err != nil {
			t.Fatal(err)
		}
	})
	step("extend", func() {
		if err := service.store.ExtendClock(*clock, 30*time.Second); err != nil {
			t.Fatal(err)
		}
	})
	step("set duration", func() {
		if err := service.store.SetClockDuration(45); err != nil {
			t.Fatal(err)
		}
	})
}

// TestFingerprintStableAcrossQuietSeconds checks the negative case the
// bucketed digest exists to guarantee: two calls five simulated seconds
// apart, with no clock event and no presence bucket transition in between,
// produce an identical fingerprint. Sub-bucket presence noise (a poll that
// refreshes lastSeen but does not cross a threshold) must not perturb it
// either.
func TestFingerprintStableAcrossQuietSeconds(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newPresenceTestService(t, false, start)
	member, _, err := service.store.AssignMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	service.presence.record(member.Email, start)

	before := service.StateFingerprint(1)
	*clock = start.Add(5 * time.Second)
	after := service.StateFingerprint(1)
	if before != after {
		t.Fatalf("fingerprint changed across quiet seconds:\n%q\n%q", before, after)
	}

	// Sub-bucket presence noise: a fresh poll within the CONNECTED window
	// still refreshes lastSeen, but the bucket does not change, so the
	// digest — and the fingerprint — must not change either.
	service.presence.record(member.Email, *clock)
	afterNoise := service.StateFingerprint(1)
	if afterNoise != after {
		t.Fatalf("fingerprint changed on sub-bucket presence noise:\n%q\n%q", after, afterNoise)
	}
}

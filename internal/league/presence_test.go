package league

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newPresenceTestService builds a Service with a fake clock and a presence
// tracker floored at start, following the direct-construction pattern
// service_test.go already uses. Demo gives its single synthetic viewer the
// first rehearsal seat only.
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
	svc.store.draftLifecycleBypass = true
	return svc, &clock
}

// TestPresenceStateTransitions checks the observable boundaries: 0s/12s/
// 12.1s/60s/60.1s of silence map to HERE/HERE/IDLE/IDLE/AWAY. NOT SEEN is
// tested separately because it needs the tracker's seen bit.
func TestPresenceStateTransitions(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	if got := presenceState(time.Time{}, start); got != "away" {
		t.Errorf("unseen key = %q, want away", got)
	}

	cases := []struct {
		age  time.Duration
		want string
	}{
		{0, "here"},
		{12 * time.Second, "here"},
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
	if got := presenceState(longSilence, longSilence); got != "here" {
		t.Errorf("a ping at now = %q, want here", got)
	}
	if got := presenceStateSince(time.Time{}, false, start, start); got != "not_seen" {
		t.Errorf("unseen key = %q, want not_seen", got)
	}
	if got := presenceStateSince(start.Add(-time.Minute), true, start, start); got != "not_seen" {
		t.Errorf("pre-restart heartbeat = %q, want not_seen", got)
	}
}

// TestRecordPresenceRespectsViewerKey checks the public wiring: a demo
// request records under the "demo-guest" key, and an anonymous, non-demo
// request — no viewer key — is a no-op, not a panic.
func TestRecordPresenceRespectsViewerKey(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	request, _ := http.NewRequest(http.MethodGet, "/api/league/presence", nil)

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

func TestDemoPresenceDoesNotAttendEveryUnclaimedSeat(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, _ := newPresenceTestService(t, true, start)
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	service.RecordPresence(request, start)

	teams := service.draftTeamMaps(service.store.Snapshot(), "team-1")
	if got := teams[0]["presence"]; got != "here" {
		t.Fatalf("rehearsal seat presence = %v, want here", got)
	}
	if got := teams[0]["manager"]; got != "REHEARSAL SEAT" {
		t.Fatalf("rehearsal seat manager = %v", got)
	}
	if got := teams[1]["presence"]; got != "unclaimed" {
		t.Fatalf("second unclaimed seat presence = %v, want unclaimed", got)
	}
	if got := teams[1]["operator_count"]; got != 0 {
		t.Fatalf("second unclaimed seat operator_count = %v, want 0", got)
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

func TestTeamPresenceAggregatesPrimaryAndCoManager(t *testing.T) {
	start := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, _ := newPresenceTestService(t, false, start)
	primary, _, err := service.store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager(primary.TeamID, "co@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := service.BindCoManagerOnSignIn("co@example.com", "Co"); err != nil || !bound {
		t.Fatalf("bind co-manager: bound=%v err=%v", bound, err)
	}
	service.presence.record("primary@example.com", start.Add(-2*time.Minute))
	service.presence.record("co@example.com", start)
	state := service.store.Snapshot()
	label, detail, _ := service.teamPresence(state, primary.TeamID, start)
	if label != "here" {
		t.Fatalf("aggregate label = %q, want here (%s)", label, detail)
	}
	if !strings.Contains(detail, "1 of 2") {
		t.Fatalf("aggregate detail = %q, want operator count", detail)
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

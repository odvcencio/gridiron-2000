package main

import (
	"testing"
	"time"
)

func newTestClock(start time.Time) (now func() time.Time, advance func(time.Duration)) {
	current := start
	return func() time.Time { return current }, func(d time.Duration) { current = current.Add(d) }
}

func TestSetupTokenGuardSingleClaim(t *testing.T) {
	var printed []string
	now, advance := newTestClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	guard, err := newSetupTokenGuard(setupTokenIdleTimeout, now, func(token string) { printed = append(printed, token) })
	if err != nil {
		t.Fatal(err)
	}
	if len(printed) != 1 {
		t.Fatalf("printed %d tokens at mint, want 1", len(printed))
	}
	token := printed[0]

	epoch1, ok, already := guard.Claim(token)
	if !ok || already || epoch1 == "" {
		t.Fatalf("first claim: ok=%v already=%v epoch=%q, want ok=true already=false non-empty epoch", ok, already, epoch1)
	}
	if !guard.Authorized(epoch1) {
		t.Fatal("the claiming epoch must be authorized immediately")
	}

	// A second presentation of the same raw token, from any session, must
	// fail with "already claimed" — never mint a second valid epoch.
	epoch2, ok, already := guard.Claim(token)
	if ok || !already || epoch2 != "" {
		t.Fatalf("second claim: ok=%v already=%v epoch=%q, want ok=false already=true empty epoch", ok, already, epoch2)
	}

	// A wrong token is neither claimed nor "already claimed": it is simply
	// invalid, and must not disturb the real claim.
	_, ok, already = guard.Claim("not-the-real-token")
	if ok || already {
		t.Fatalf("wrong token: ok=%v already=%v, want both false", ok, already)
	}
	if !guard.Authorized(epoch1) {
		t.Fatal("an unrelated wrong-token attempt must not invalidate the real claim")
	}
	if guard.Authorized("some-other-epoch") {
		t.Fatal("a session with the wrong epoch must never be authorized")
	}

	_ = advance // used below
}

func TestSetupTokenGuardIdleReMint(t *testing.T) {
	var printed []string
	now, advance := newTestClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	guard, err := newSetupTokenGuard(60*time.Minute, now, func(token string) { printed = append(printed, token) })
	if err != nil {
		t.Fatal(err)
	}
	first := printed[0]
	epoch, ok, _ := guard.Claim(first)
	if !ok {
		t.Fatal("claim should succeed")
	}
	if !guard.Authorized(epoch) {
		t.Fatal("freshly claimed epoch must be authorized")
	}

	// Idle for over an hour with no Touch: the guard must re-mint and the
	// old epoch must stop being authorized.
	advance(61 * time.Minute)
	if guard.Authorized(epoch) {
		t.Fatal("an idle-expired epoch must no longer be authorized")
	}
	if len(printed) != 2 {
		t.Fatalf("printed %d tokens, want 2 (initial mint + idle re-mint)", len(printed))
	}
	if printed[1] == first {
		t.Fatal("the re-minted token must differ from the original")
	}

	// The fresh token can be claimed again.
	newEpoch, ok, already := guard.Claim(printed[1])
	if !ok || already {
		t.Fatalf("claiming the re-minted token: ok=%v already=%v, want ok=true already=false", ok, already)
	}
	if newEpoch == epoch {
		t.Fatal("the re-minted epoch must differ from the original")
	}
}

func TestSetupTokenGuardTouchResetsIdleClock(t *testing.T) {
	var printed []string
	now, advance := newTestClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	guard, err := newSetupTokenGuard(60*time.Minute, now, func(token string) { printed = append(printed, token) })
	if err != nil {
		t.Fatal(err)
	}
	epoch, ok, _ := guard.Claim(printed[0])
	if !ok {
		t.Fatal("claim should succeed")
	}
	advance(45 * time.Minute)
	guard.Touch()
	advance(45 * time.Minute)
	if !guard.Authorized(epoch) {
		t.Fatal("a touched session should not idle out after only 45 more minutes")
	}
}

func TestSetupRateLimiterLockout(t *testing.T) {
	now, advance := newTestClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	limiter := newSetupRateLimiter(5, time.Minute, 30*time.Second, now)
	for i := 0; i < 5; i++ {
		if !limiter.Allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed within the 5/minute budget", i+1)
		}
	}
	if limiter.Allow("1.2.3.4") {
		t.Fatal("the 6th attempt within a minute should be locked out")
	}
	// A different IP is unaffected.
	if !limiter.Allow("5.6.7.8") {
		t.Fatal("a different IP must not share the lockout")
	}
	advance(31 * time.Second)
	if !limiter.Allow("1.2.3.4") {
		t.Fatal("the lockout should have expired after 30 seconds")
	}
}

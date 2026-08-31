package main

// harnessSensitiveEnv lists the variables that an exported value in the
// developer's shell must never carry into a test. Each one arms something
// real: a Tank01 key starts a poller, a mail key opens a transport, and a
// COMMISSIONER_HQ_* value opts the build into the v1 provider and then
// fails on an incomplete identity.
//
// Both harness paths clear this one list: hermeticEnv (app_build_test.go)
// for an in-process build, and simChildEnv (sim_child_test.go) for a child
// server process. The two lists are NOT otherwise identical. hermeticEnv
// also clears GRIDIRON_TEST_AUTH and GRIDIRON_TEST_POOL, because an
// in-process test decides for itself whether the harness surface exists;
// the child sets both on purpose, so it must not clear them.
var harnessSensitiveEnv = []string{
	"TANK01_API_KEY",
	"TANK01_BASE_URL",
	"RESEND_API_KEY",
	"SMTP_HOST",
	"COMMISSIONER_HQ_LEAGUE_ID",
	"COMMISSIONER_HQ_PROVIDER_KEY_ID",
	"COMMISSIONER_HQ_PROVIDER_SECRET",
	"COMMISSIONER_HQ_PROVIDER_SECRET_FILE",
	"COMMISSIONER_HQ_PROVIDER_ADDR",
	"COMMISSIONER_HQ_V1_REGISTRY_FILE",
	"COMMISSIONER_HQ_PEERS",
	"COMMISSIONER_HQ_TOKEN",
	// An exported LIVE_SCORING_ENABLED=true (or a poll cadence/budget
	// tuned for production) must not start a real live poller inside a
	// test process either — the same reasoning as TANK01_API_KEY above.
	// buildLiveScoring also forces the poller off whenever the fantasy
	// pool itself is unauthenticated, but that is a second, independent
	// guard, not a reason to skip clearing these (round-2 review of
	// commit cdeb7f2, finding 1).
	"LIVE_SCORING_ENABLED",
	"LIVE_POLL_INTERVAL",
	"LIVE_SCOREBOARD_INTERVAL",
	"LIVE_BOX_BASELINE",
	"LIVE_BOX_FAST",
	"LIVE_DAILY_BUDGET",
	"LIVE_MAX_INFLIGHT",
	"LIVE_REPLAY_FIXTURE",
	// LIVE_REPLAY_ALLOW_PRODUCTION overrides liveScoringInputs's APP_ENV
	// gate on replay mode (the Stable Kernel rehearsal's override for a
	// deployed environment). An exported "true" in the developer's shell
	// must not silently widen what a test process is allowed to wire.
	"LIVE_REPLAY_ALLOW_PRODUCTION",
}

// harnessSensitiveEnvWith returns the shared list plus extra, as a new
// slice. A caller must never append to harnessSensitiveEnv itself: a spare
// capacity would let one caller's extra key land in another's view of the
// shared list.
func harnessSensitiveEnvWith(extra ...string) []string {
	out := make([]string, 0, len(harnessSensitiveEnv)+len(extra))
	out = append(out, harnessSensitiveEnv...)
	return append(out, extra...)
}

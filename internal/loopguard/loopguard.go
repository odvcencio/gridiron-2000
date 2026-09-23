// Package loopguard gives every long-running background loop (a poller,
// ticker, or scheduler) one shared, tested way to survive a panic in a
// single iteration: recover, log the panic and its stack, wait a short
// backoff, then let the caller's own loop try again on its next tick.
//
// Before this package existed, no background loop in this codebase
// recovered from a panic at all (production audit, 2026-09-23,
// "## Correctness" item 12): one malformed box score, one bad stat row,
// or any other single-iteration bug silently ended the whole poller,
// ticker, or scheduler for the rest of the process's life — on game day,
// with a single replica, that is a crash loop, not a graceful degrade.
package loopguard

import (
	"context"
	"log"
	"runtime/debug"
	"time"
)

// DefaultBackoff is how long Tick sleeps after recovering from a panic
// when the caller passes backoff <= 0. It exists so a panic that recurs
// on every tick cannot spin the process in a tight loop while still
// logging every occurrence.
const DefaultBackoff = time.Second

// Tick runs fn and recovers any panic it raises, logging name, the panic
// value, and a stack trace, then sleeping backoff (or DefaultBackoff when
// backoff <= 0) before returning. A caller wraps its own loop body's
// per-iteration work in Tick so one bad iteration cannot end the loop —
// the next scheduled tick still fires normally. Tick does not sleep at
// all when fn returns without panicking.
//
// ctx makes the post-panic backoff cancellation-aware: the sleep ends
// early the moment ctx is done, so a panic during shutdown cannot hold
// the process up for the full backoff (buckbot review, PR #68, finding
// 001 — the caller's own ctx.Done() case, one select branch up, would
// otherwise not run again until the backoff elapses). Pass
// context.Background() when the caller has no context of its own to
// offer; the sleep then always runs its full course, exactly as before
// this parameter existed.
func Tick(ctx context.Context, name string, backoff time.Duration, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			if backoff <= 0 {
				backoff = DefaultBackoff
			}
			log.Printf("%s: recovered from panic: %v\n%s", name, r, debug.Stack())
			timer := time.NewTimer(backoff)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
			}
		}
	}()
	fn()
}

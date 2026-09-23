package loopguard

import (
	"bytes"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestTickRecoversAndTheLoopKeepsRunning is the audit's exact demand
// (2026-09-23, "## Correctness" item 12, "Test with an injected panic:
// the loop keeps running"): a background loop that wraps each iteration
// in Tick must survive one iteration panicking and still run its next
// iteration normally.
func TestTickRecoversAndTheLoopKeepsRunning(t *testing.T) {
	var iterations atomic.Int32
	runIteration := func(i int) {
		iterations.Add(1)
		if i == 1 {
			panic("simulated malformed box score")
		}
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// Three simulated ticks, one panicking in the middle — exactly the
	// shape a real ticker loop's body takes.
	for i := 0; i < 3; i++ {
		func(i int) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("iteration %d: panic escaped Tick instead of being recovered: %v", i, r)
				}
			}()
			Tick("test-loop", time.Microsecond, func() { runIteration(i) })
		}(i)
	}

	if got := iterations.Load(); got != 3 {
		t.Fatalf("iterations = %d, want 3 (the loop must keep running after the panic)", got)
	}
	if !strings.Contains(buf.String(), "test-loop") || !strings.Contains(buf.String(), "simulated malformed box score") {
		t.Fatalf("panic was not logged with the loop name and panic value: log=%q", buf.String())
	}
}

// TestTickSleepsTheBackoffOnlyAfterAPanic proves Tick does not slow down
// the common, non-panicking case, and does apply the requested backoff
// after a recovered panic (so a panic that recurs every tick cannot spin
// the process in a tight crash loop).
func TestTickSleepsTheBackoffOnlyAfterAPanic(t *testing.T) {
	log.SetOutput(discardWriter{})
	defer log.SetOutput(os.Stderr)

	start := time.Now()
	Tick("no-panic", time.Hour, func() {})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Tick slept %v with no panic; it must only back off after recovering one", elapsed)
	}

	start = time.Now()
	Tick("panics", 30*time.Millisecond, func() { panic("boom") })
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("Tick returned after %v, want at least the 30ms backoff following a recovered panic", elapsed)
	}
}

// TestTickDefaultsBackoffWhenNonPositive proves a caller that does not
// pick its own backoff (backoff <= 0) still gets one, rather than
// spinning immediately.
func TestTickDefaultsBackoffWhenNonPositive(t *testing.T) {
	log.SetOutput(discardWriter{})
	defer log.SetOutput(os.Stderr)

	start := time.Now()
	Tick("zero-backoff", 0, func() { panic("boom") })
	if elapsed := time.Since(start); elapsed <= 0 {
		t.Fatalf("Tick with backoff<=0 returned immediately after a panic; want DefaultBackoff applied")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

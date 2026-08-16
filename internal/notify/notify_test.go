package notify

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestQueue wires a Queue with an injectable clock: sleep calls are
// recorded on a channel instead of blocking in wall time, so throttle and
// retry timing tests are deterministic and never sleep for real (spec
// section 9: "no sleeps in tests").
func newTestQueue(send Sender, logf func(string, ...any)) (*Queue, chan time.Duration) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	sleeps := make(chan time.Duration, 32)
	q := New(send, logf)
	q.now = func() time.Time { return time.Unix(0, 0) }
	q.sleep = func(ctx context.Context, d time.Duration) bool { sleeps <- d; return true }
	return q, sleeps
}

func newSyncLogger() (func(string, ...any), func() []string) {
	var mu sync.Mutex
	var logs []string
	logf := func(format string, args ...any) {
		mu.Lock()
		logs = append(logs, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), logs...)
	}
	return logf, snapshot
}

// TestQueueFIFOOrder checks that messages are delivered in the order they
// were enqueued (spec section 9, test 19).
func TestQueueFIFOOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	done := make(chan struct{}, 8)
	send := func(m Message) error {
		mu.Lock()
		order = append(order, m.Key)
		mu.Unlock()
		done <- struct{}{}
		return nil
	}
	q, _ := newTestQueue(send, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keys := []string{"a", "b", "c", "d", "e"}
	for _, k := range keys {
		q.Enqueue(Message{Key: k})
	}
	q.Start(ctx)
	for range keys {
		<-done
	}

	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(order, keys) {
		t.Fatalf("delivery order = %v, want %v", order, keys)
	}
}

// TestQueueThrottlesBetweenSends checks that the worker requests exactly
// sendThrottle between messages (spec section 9, test 19).
func TestQueueThrottlesBetweenSends(t *testing.T) {
	done := make(chan struct{}, 4)
	send := func(Message) error { done <- struct{}{}; return nil }
	q, sleeps := newTestQueue(send, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.Enqueue(Message{Key: "a"})
	q.Start(ctx)
	<-done
	if d := <-sleeps; d != sendThrottle {
		t.Fatalf("throttle sleep = %v, want %v", d, sendThrottle)
	}
}

// TestQueueRetriesOnceThenDrops checks the retry-once policy: a failing
// send is retried after retryDelay, exactly once; a second failure is
// logged and the message is dropped (spec section 9, test 19).
func TestQueueRetriesOnceThenDrops(t *testing.T) {
	var calls int
	var mu sync.Mutex
	send := func(m Message) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("boom")
	}
	logf, logs := newSyncLogger()
	q, sleeps := newTestQueue(send, logf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start before Enqueue: the queue is already running, so Enqueue's
	// one-time "not started" warning (finding m2) never fires here, and
	// the log assertion below stays exact.
	q.Start(ctx)
	q.Enqueue(Message{Key: "x", To: "a@example.com"})

	if d := <-sleeps; d != retryDelay {
		t.Fatalf("first sleep = %v, want retryDelay %v", d, retryDelay)
	}
	if d := <-sleeps; d != sendThrottle {
		t.Fatalf("second sleep = %v, want sendThrottle %v", d, sendThrottle)
	}

	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Fatalf("send call count = %d, want 2 (one attempt, one retry)", gotCalls)
	}

	got := logs()
	if len(got) != 1 || !strings.Contains(got[0], "send failed twice") ||
		!strings.Contains(got[0], "key=x") || !strings.Contains(got[0], "to=a@example.com") {
		t.Fatalf("logs = %v, want exactly one \"send failed twice\" line naming the key and recipient", got)
	}
}

// TestQueueLastErrorSurfaces checks that a failed send's error and instant
// are visible through LastError, for the admin mail panel (spec section
// 9, test 19; spec section 6.6).
func TestQueueLastErrorSurfaces(t *testing.T) {
	fixed := time.Unix(555, 0)
	send := func(Message) error { return errors.New("transport is down") }
	sleeps := make(chan time.Duration, 8)
	q := New(send, func(string, ...any) {})
	q.sleep = func(ctx context.Context, d time.Duration) bool { sleeps <- d; return true }
	q.now = func() time.Time { return fixed }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if msg, at := q.LastError(); msg != "" || !at.IsZero() {
		t.Fatalf("LastError before any send = (%q, %v), want zero value", msg, at)
	}

	q.Enqueue(Message{Key: "x"})
	q.Start(ctx)
	<-sleeps // retryDelay
	<-sleeps // sendThrottle, after the second failed attempt

	msg, at := q.LastError()
	if !strings.Contains(msg, "transport is down") {
		t.Fatalf("LastError message = %q, want it to carry the send error", msg)
	}
	if !at.Equal(fixed) {
		t.Fatalf("LastError time = %v, want %v", at, fixed)
	}
}

// TestQueueEnqueueDropsWhenFull checks that a full queue drops the newest
// message and logs it, without blocking the caller (spec section 9, test
// 19; spec section 6.1). Enqueue's select-with-default is non-blocking by
// construction, so no worker needs to run and no wait is needed to prove
// this call returns. The queue is deliberately never started, so every
// call also exercises the "not started" warning (finding m2); that
// warning logs exactly once, alongside the queue-full line.
func TestQueueEnqueueDropsWhenFull(t *testing.T) {
	logf, logs := newSyncLogger()
	q := New(func(Message) error { return nil }, logf)

	for i := 0; i < queueCapacity; i++ {
		q.Enqueue(Message{Key: fmt.Sprintf("k%d", i)})
	}
	if got := len(q.queue); got != queueCapacity {
		t.Fatalf("queue depth after filling = %d, want %d", got, queueCapacity)
	}

	q.Enqueue(Message{Key: "overflow", To: "over@example.com"})

	got := logs()
	unstartedWarnings, queueFullLines := 0, 0
	for _, line := range got {
		switch {
		case strings.Contains(line, "before Start"):
			unstartedWarnings++
		case strings.Contains(line, "queue full"):
			if !strings.Contains(line, "key=overflow") || !strings.Contains(line, "to=over@example.com") {
				t.Errorf("queue-full line missing the dropped message's key/to: %q", line)
			}
			queueFullLines++
		}
	}
	if unstartedWarnings != 1 {
		t.Fatalf("logs = %v, want exactly one \"not started\" warning across the whole burst", got)
	}
	if queueFullLines != 1 {
		t.Fatalf("logs = %v, want exactly one queue-full line naming the dropped message", got)
	}
	if len(got) != 2 {
		t.Fatalf("logs = %v, want exactly 2 lines total (one warning, one drop)", got)
	}
	if got := len(q.queue); got != queueCapacity {
		t.Fatalf("queue depth after overflow = %d, want unchanged %d", got, queueCapacity)
	}
}

// TestQueueNeverDeliversWithoutStart checks the notify-owned half of
// test-plan item 17 (disabled transport): a Queue whose worker was never
// started makes zero delivery attempts and leaves LastError at its zero
// value, no matter how many messages are enqueued. main.go's wiring
// relies on exactly this: when mailer.Config.Enabled() is false, it skips
// Queue.Start and logs the spec's exact line once at startup (spec
// section 6.6). The league package's own event-hook and StartNotifier
// short-circuit — the "no ledger writes" half of item 17 — lands with the
// trigger logic itself (WP-E3) and is out of this package's scope.
func TestQueueNeverDeliversWithoutStart(t *testing.T) {
	var calls int
	var mu sync.Mutex
	send := func(Message) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}
	q, _ := newTestQueue(send, nil)

	q.Enqueue(Message{Key: "a"})
	q.Enqueue(Message{Key: "b"})

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 0 {
		t.Fatalf("send was called %d times without Start ever running", got)
	}
	if msg, at := q.LastError(); msg != "" || !at.IsZero() {
		t.Fatalf("LastError = (%q, %v), want the zero value", msg, at)
	}
	if got := len(q.queue); got != 2 {
		t.Fatalf("queue depth = %d, want 2 (both messages still queued, undelivered)", got)
	}
}

// TestQueueDepth checks that Depth reports the number of messages queued
// and not yet delivered: zero on a fresh queue, growing with Enqueue, and
// dropping back to zero once the worker drains it (spec section 6.6's
// admin mail-panel queue_depth field).
func TestQueueDepth(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	send := func(Message) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return nil
	}
	q, _ := newTestQueue(send, nil)
	if got := q.Depth(); got != 0 {
		t.Fatalf("Depth on a fresh queue = %d, want 0", got)
	}

	q.Enqueue(Message{Key: "a"})
	q.Enqueue(Message{Key: "b"})
	if got := q.Depth(); got != 2 {
		t.Fatalf("Depth after two enqueues = %d, want 2", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	<-started // the worker has dequeued "a" and is blocked delivering it
	if got := q.Depth(); got != 1 {
		t.Fatalf("Depth while one message is in flight = %d, want 1 (the other still queued)", got)
	}
	close(release)
}

// TestNewDefaultsNilLogf checks that New tolerates a nil logf so callers
// are not forced to pass a no-op function.
func TestNewDefaultsNilLogf(t *testing.T) {
	q := New(func(Message) error { return nil }, nil)
	if q.logf == nil {
		t.Fatal("New(send, nil) left logf nil; Enqueue would panic on a full queue")
	}
	// Must not panic.
	q.logf("test message %d", 1)
}

// --- Finding m2: a never-started queue must not buffer silently ----------

// TestQueueEnqueueWarnsOnceWhenNeverStarted checks that Enqueue logs a
// warning the first time a message arrives before Start has run, and does
// not repeat it for every later pre-start message in the same burst
// (finding m2): the message still buffers (Start may be moments away), but
// the operator now has a signal instead of silence.
func TestQueueEnqueueWarnsOnceWhenNeverStarted(t *testing.T) {
	logf, logs := newSyncLogger()
	q := New(func(Message) error { return nil }, logf)

	q.Enqueue(Message{Key: "a"})
	q.Enqueue(Message{Key: "b"})
	q.Enqueue(Message{Key: "c"})

	got := logs()
	count := 0
	for _, line := range got {
		if strings.Contains(line, "before Start") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("\"before Start\" warning logged %d times, want exactly 1: %v", count, got)
	}
	if got := len(q.queue); got != 3 {
		t.Fatalf("queue depth = %d, want 3 (still buffered, not dropped)", got)
	}
}

// TestQueueEnqueueNoWarningAfterStart checks that the pre-start warning
// never fires once Start has run, even for the very first message.
func TestQueueEnqueueNoWarningAfterStart(t *testing.T) {
	logf, logs := newSyncLogger()
	done := make(chan struct{}, 1)
	q := New(func(Message) error { done <- struct{}{}; return nil }, logf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.Start(ctx)
	q.Enqueue(Message{Key: "a"})
	<-done

	for _, line := range logs() {
		if strings.Contains(line, "before Start") {
			t.Fatalf("unexpected \"before Start\" warning after Start ran: %v", logs())
		}
	}
}

// --- Finding M3: Permanent errors skip the retry --------------------------

// TestPermanentWrapsAndIsDetectable checks that Permanent's wrapped error
// is detectable via errors.Is(err, ErrPermanent) and still unwraps to the
// original error, and that Permanent(nil) returns nil.
func TestPermanentWrapsAndIsDetectable(t *testing.T) {
	base := errors.New("boom")
	wrapped := Permanent(base)
	if !errors.Is(wrapped, ErrPermanent) {
		t.Error("Permanent(err) must be detectable via errors.Is(err, ErrPermanent)")
	}
	if !errors.Is(wrapped, base) {
		t.Error("Permanent(err) must still unwrap to the original error")
	}
	if Permanent(nil) != nil {
		t.Error("Permanent(nil) must return nil")
	}
}

// TestQueueSkipsRetryOnPermanentError checks that deliver does not retry a
// Permanent-wrapped failure: exactly one send attempt, no retryDelay
// sleep, and a distinct log line naming the key (finding M3, spec section
// 6.3: at-most-once is preferred for a transport that can fail after
// already accepting the message).
func TestQueueSkipsRetryOnPermanentError(t *testing.T) {
	var calls int
	var mu sync.Mutex
	send := func(Message) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return Permanent(errors.New("smtp boom"))
	}
	logf, logs := newSyncLogger()
	q, sleeps := newTestQueue(send, logf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.Start(ctx)
	q.Enqueue(Message{Key: "x", To: "a@example.com"})

	if d := <-sleeps; d != sendThrottle {
		t.Fatalf("sleep after a permanent failure = %v, want the throttle sleep (no retry wait)", d)
	}

	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("send call count = %d, want 1 (no retry for a permanent error)", gotCalls)
	}

	found := false
	for _, line := range logs() {
		if strings.Contains(line, "permanent") && strings.Contains(line, "key=x") {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs = %v, want a permanent-failure line naming the key", logs())
	}
}

// --- Finding m1: bounded drain and a context-aware retry wait ------------

// TestQueueDrainReturnsZeroWhenAlreadyEmpty checks that Drain returns
// immediately on an idle, never-started queue.
func TestQueueDrainReturnsZeroWhenAlreadyEmpty(t *testing.T) {
	q, _ := newTestQueue(func(Message) error { return nil }, nil)
	if got := q.Drain(50 * time.Millisecond); got != 0 {
		t.Fatalf("Drain on an empty, never-started queue = %d, want 0", got)
	}
}

// TestQueueDrainWaitsForInFlightDelivery checks that Drain blocks while a
// message is mid-delivery and returns 0 once that delivery completes,
// rather than treating "not in the channel buffer" as "done" (finding m1).
func TestQueueDrainWaitsForInFlightDelivery(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	send := func(Message) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return nil
	}
	q, _ := newTestQueue(send, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	q.Enqueue(Message{Key: "a"})
	<-started // the worker is mid-delivery; Depth() alone would already read 0 here

	done := make(chan int, 1)
	go func() { done <- q.Drain(time.Second) }()

	select {
	case <-done:
		t.Fatal("Drain returned before the in-flight message finished delivering")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if got := <-done; got != 0 {
		t.Fatalf("Drain after delivery completes = %d, want 0", got)
	}
}

// TestQueueDrainTimesOutAndLogsRemaining checks that Drain gives up at its
// timeout, logs the remaining count, and returns it, rather than blocking
// forever on a message that is never going to finish (finding m1).
func TestQueueDrainTimesOutAndLogsRemaining(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) }) // let the blocked goroutine exit after the test
	send := func(Message) error { <-block; return nil }
	logf, logs := newSyncLogger()
	q, _ := newTestQueue(send, logf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	q.Enqueue(Message{Key: "a"})

	got := q.Drain(50 * time.Millisecond)
	if got != 1 {
		t.Fatalf("Drain remaining = %d, want 1", got)
	}
	found := false
	for _, line := range logs() {
		if strings.Contains(line, "drain timed out") {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs = %v, want a drain-timed-out line", logs())
	}
}

// TestDeliverRetryWaitRespectsContextCancellation checks that a canceled
// context cuts the retry-delay wait short: deliver makes exactly one send
// attempt (no retry) and logs that shutdown interrupted the wait (finding
// m1). This drives deliver directly with the real, context-aware sleeper
// (not the test's recording fake) so the actual cancellation plumbing is
// exercised, not just the seam.
func TestDeliverRetryWaitRespectsContextCancellation(t *testing.T) {
	var calls int
	var mu sync.Mutex
	send := func(Message) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("boom")
	}
	logf, logs := newSyncLogger()
	q := New(send, logf)
	// q.sleep stays realSleep (the production seam); only ctx is canceled
	// early, well inside the 30s retryDelay, so this test finishes fast.
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	q.deliver(ctx, Message{Key: "x", To: "a@example.com"})

	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("send call count = %d, want 1 (ctx canceled during the retry wait, no second attempt)", gotCalls)
	}
	found := false
	for _, line := range logs() {
		if strings.Contains(line, "shutdown during retry wait") && strings.Contains(line, "key=x") {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs = %v, want a shutdown-during-retry-wait line naming the key", logs())
	}
}

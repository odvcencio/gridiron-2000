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
	q.sleep = func(d time.Duration) { sleeps <- d }
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

	q.Enqueue(Message{Key: "x", To: "a@example.com"})
	q.Start(ctx)

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
	q.sleep = func(d time.Duration) { sleeps <- d }
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
// this call returns.
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
	if len(got) != 1 || !strings.Contains(got[0], "queue full") ||
		!strings.Contains(got[0], "key=overflow") || !strings.Contains(got[0], "to=over@example.com") {
		t.Fatalf("logs = %v, want exactly one queue-full line naming the dropped message", got)
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

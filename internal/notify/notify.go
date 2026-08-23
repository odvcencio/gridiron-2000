// Package notify is the delivery layer for the notification email system:
// a bounded queue, one worker goroutine, a send throttle, and a
// retry-once policy (design spec section 6.1). It is generic and imports
// nothing from league or mailer; the league package renders messages with
// emailkit, records the idempotency ledger, then enqueues here.
package notify

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// queueCapacity bounds the FIFO. 256 covers a full league-wide send burst
// forty times over (spec section 6.1); Enqueue on a full queue drops the
// message and logs it rather than blocking the caller.
const queueCapacity = 256

const (
	// sendThrottle is the worker's pause between messages, keeping the
	// send rate under Resend's documented default of 2 req/s.
	sendThrottle = 600 * time.Millisecond
	// retryDelay is the pause before a failed send's one retry.
	retryDelay = 30 * time.Second
)

// ErrPermanent is the sentinel errors.Is detects on an error Permanent has
// wrapped (finding M3). Callers should not compare it directly; call
// errors.Is(err, ErrPermanent).
var ErrPermanent = errors.New("notify: permanent delivery error")

// permanentError marks a delivery error Queue.deliver must not retry.
type permanentError struct{ err error }

func (p *permanentError) Error() string        { return p.err.Error() }
func (p *permanentError) Unwrap() error        { return p.err }
func (p *permanentError) Is(target error) bool { return target == ErrPermanent }

// Permanent marks err as one Queue.deliver must not retry (design spec
// section 6.3; finding M3): some transports can fail after the message was
// already accepted (for example SMTP's DATA/QUIT sequence), so retrying
// blindly risks a duplicate. A Sender wraps only the failures its own
// transport cannot safely retry — a transport protected by an
// idempotency key (Resend) should leave its errors unwrapped, since those
// stay safely retryable. Permanent(nil) returns nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// Message is one fully rendered email plus its ledger key.
type Message struct {
	Key      string // idempotency key; also the Resend Idempotency-Key
	Category string // catalog category, for tags and logs
	To       string
	Subject  string
	Text     string
	HTML     string
}

// Sender delivers one message. The league package wires mailer.Config's
// send call here.
type Sender func(Message) error

// EnqueueResult reports what happened when a message was offered to the
// bounded queue. Queued means the message was accepted into the FIFO; Dropped
// means the FIFO was full and the message was not accepted. Delivery is still
// asynchronous, so Queued is deliberately not a delivery receipt.
type EnqueueResult string

const (
	EnqueueQueued  EnqueueResult = "queued"
	EnqueueDropped EnqueueResult = "dropped"
)

// Accepted reports whether the message entered the queue.
func (r EnqueueResult) Accepted() bool { return r == EnqueueQueued }

// sleeper pauses for d or returns early when ctx is done, reporting which
// happened (true: the full duration elapsed; false: ctx ended the wait
// early). New wires the real, context-aware implementation; tests inject a
// fake that records the requested duration without actually blocking
// (spec section 9: "no sleeps in tests"). Both the throttle and the
// retry-once delay call it, so a bounded shutdown drain (finding m1) can
// cut either wait short instead of leaving deliver stuck in an up-to-30s
// retry sleep after the caller has given up waiting.
type sleeper func(ctx context.Context, d time.Duration) bool

// realSleep is the production sleeper: it blocks for d or until ctx is
// done, whichever comes first.
func realSleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Queue is a bounded FIFO with one worker goroutine. Enqueue never blocks:
// a full queue drops the message and logs it. The worker sleeps
// sendThrottle between messages and retries a failed send once, after
// retryDelay, unless the error is Permanent (finding M3), in which case it
// does not retry at all. A second failure logs and drops. The idempotency
// ledger entry is written by the caller before Enqueue (spec section 6.3),
// so a dropped message is never silently resent later — it is simply
// gone, and the log line is the only record.
type Queue struct {
	send Sender
	logf func(string, ...any)

	queue     chan Message
	startOnce sync.Once
	// started reports whether Start has run. Enqueue reads it to log a
	// one-time warning when a message arrives before the worker exists
	// (finding m2): silent, unbounded buffering with zero operator
	// visibility until the queue eventually filled was the complaint: the
	// message still buffers (a queue that is about to Start must not lose
	// it), but the operator now finds out immediately, not 256 messages
	// later.
	started         atomic.Bool
	warnedUnstarted atomic.Bool
	// outstanding counts every message Enqueue has successfully buffered
	// but deliver has not yet finished with (including any retry wait). It
	// is incremented in the same call that sends onto the channel and
	// decremented only after delivery completes, so there is no window
	// where a message is neither "in the channel" nor "counted in
	// flight" — the gap len(queue)+inFlight would have between a channel
	// receive and a separate increment. Drain (finding m1) polls it to
	// know when every already-enqueued message has been accounted for.
	outstanding atomic.Int32

	mu          sync.Mutex
	lastError   string
	lastErrorAt time.Time

	// now and sleep are the injectable clock seam (mirrors league.Service's
	// now field): tests replace them with a fake so the throttle and retry
	// timing are deterministic and no test sleeps in wall-clock time.
	now   func() time.Time
	sleep sleeper
}

// New builds a Queue. send delivers one message; logf records drops and
// failures (the caller typically wires log.Printf).
func New(send Sender, logf func(string, ...any)) *Queue {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Queue{
		send:  send,
		logf:  logf,
		queue: make(chan Message, queueCapacity),
		now:   time.Now,
		sleep: realSleep,
	}
}

// Start launches the worker goroutine once. Call it once from main.go,
// like fantasy.Service.Start (internal/fantasy/service.go:95-102). The
// worker stops when ctx is canceled.
func (q *Queue) Start(ctx context.Context) {
	q.startOnce.Do(func() {
		q.started.Store(true)
		go q.run(ctx)
	})
}

// Enqueue adds m to the queue without blocking. A full queue drops m and
// logs it; the caller's persisted ledger entry (written before Enqueue)
// already exists, so the message is never retried after a restart — the
// drop is final, by design (spec section 6.1, 6.3). A message enqueued
// before Start has run still buffers (a queue about to start must not
// lose it), but the first such message logs a one-time warning so a
// queue that never starts is not silently invisible (finding m2).
func (q *Queue) Enqueue(m Message) EnqueueResult {
	if !q.started.Load() {
		if !q.warnedUnstarted.Swap(true) {
			q.logf("notify: message enqueued before Start; queued sends will not be delivered until the worker starts")
		}
	}
	select {
	case q.queue <- m:
		q.outstanding.Add(1)
		return EnqueueQueued
	default:
		q.logf("notify: queue full (capacity %d); dropping key=%s to=%s", queueCapacity, m.Key, m.To)
		return EnqueueDropped
	}
}

// LastError reports the most recent transport failure and when it was
// recorded, for the admin mail panel (spec section 6.6). It reports a
// zero time when no failure has happened yet.
func (q *Queue) LastError() (string, time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.lastError, q.lastErrorAt
}

// Depth reports the number of messages currently queued and not yet
// delivered, for the admin mail panel's queue_depth field (spec section
// 6.6). WP-E2 deliberately deferred this accessor until a caller needed
// it; WP-E3's mail-health card is that caller.
func (q *Queue) Depth() int {
	return len(q.queue)
}

// Drain blocks until every already-enqueued message has been delivered or
// dropped, or until timeout elapses, whichever comes first. Call it once
// after canceling the notify worker's context and after the HTTP server
// has stopped accepting new requests, so a graceful shutdown does not
// silently abandon a message the ledger already marked "sent" (spec
// section 6.3; finding m1). It logs and returns the number of messages
// still outstanding when the deadline hits; a queue that drains naturally
// returns 0 and logs nothing.
func (q *Queue) Drain(timeout time.Duration) int {
	const pollInterval = 10 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		remaining := int(q.outstanding.Load())
		if remaining == 0 {
			return 0
		}
		if !time.Now().Before(deadline) {
			q.logf("notify: shutdown drain timed out with %d message(s) still undelivered", remaining)
			return remaining
		}
		time.Sleep(pollInterval)
	}
}

func (q *Queue) recordError(err error) {
	q.mu.Lock()
	q.lastError = err.Error()
	q.lastErrorAt = q.now()
	q.mu.Unlock()
}

// run is the one worker goroutine: dequeue, deliver (with its retry),
// throttle, repeat. It exits when ctx is canceled.
func (q *Queue) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-q.queue:
			q.deliver(ctx, m)
			q.outstanding.Add(-1)
			select {
			case <-ctx.Done():
				return
			default:
			}
			q.sleep(ctx, sendThrottle)
		}
	}
}

// deliver sends m, retrying once after retryDelay on failure, unless the
// error is Permanent (finding M3), in which case it does not retry at
// all. A second failure is logged and the message is dropped; the first
// failure alone still updates LastError so the admin panel reflects it
// even if the retry then succeeds. The retry wait itself respects ctx
// cancellation (finding m1): a shutdown drain that gives up waiting can
// cut a message's retry short instead of holding it up to 30s past the
// drain deadline.
func (q *Queue) deliver(ctx context.Context, m Message) {
	err := q.send(m)
	if err == nil {
		return
	}
	q.recordError(err)
	if errors.Is(err, ErrPermanent) {
		q.logf("notify: send failed (permanent, no retry): key=%s to=%s err=%v", m.Key, m.To, err)
		return
	}
	if !q.sleep(ctx, retryDelay) {
		q.logf("notify: shutdown during retry wait: key=%s to=%s left undelivered", m.Key, m.To)
		return
	}
	err = q.send(m)
	if err != nil {
		q.recordError(err)
		q.logf("notify: send failed twice: key=%s to=%s err=%v", m.Key, m.To, err)
	}
}

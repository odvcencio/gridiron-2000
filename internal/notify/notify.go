// Package notify is the delivery layer for the notification email system:
// a bounded queue, one worker goroutine, a send throttle, and a
// retry-once policy (design spec section 6.1). It is generic and imports
// nothing from league or mailer; the league package renders messages with
// emailkit, records the idempotency ledger, then enqueues here.
package notify

import (
	"context"
	"sync"
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

// Queue is a bounded FIFO with one worker goroutine. Enqueue never blocks:
// a full queue drops the message and logs it. The worker sleeps
// sendThrottle between messages and retries a failed send once, after
// retryDelay; a second failure logs and drops. The idempotency ledger
// entry is written by the caller before Enqueue (spec section 6.3), so a
// dropped message is never silently resent later — it is simply gone,
// and the log line is the only record.
type Queue struct {
	send Sender
	logf func(string, ...any)

	queue     chan Message
	startOnce sync.Once

	mu          sync.Mutex
	lastError   string
	lastErrorAt time.Time

	// now and sleep are the injectable clock seam (mirrors league.Service's
	// now field): tests replace them with a fake so the throttle and retry
	// timing are deterministic and no test sleeps in wall-clock time.
	now   func() time.Time
	sleep func(time.Duration)
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
		sleep: time.Sleep,
	}
}

// Start launches the worker goroutine once. Call it once from main.go,
// like fantasy.Service.Start (internal/fantasy/service.go:95-102). The
// worker stops when ctx is canceled.
func (q *Queue) Start(ctx context.Context) {
	q.startOnce.Do(func() {
		go q.run(ctx)
	})
}

// Enqueue adds m to the queue without blocking. A full queue drops m and
// logs it; the caller's persisted ledger entry (written before Enqueue)
// already exists, so the message is never retried after a restart — the
// drop is final, by design (spec section 6.1, 6.3).
func (q *Queue) Enqueue(m Message) {
	select {
	case q.queue <- m:
	default:
		q.logf("notify: queue full (capacity %d); dropping key=%s to=%s", queueCapacity, m.Key, m.To)
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
			q.deliver(m)
			select {
			case <-ctx.Done():
				return
			default:
			}
			q.sleep(sendThrottle)
		}
	}
}

// deliver sends m, retrying once after retryDelay on failure. A second
// failure is logged and the message is dropped; the first failure alone
// still updates LastError so the admin panel reflects it even if the
// retry then succeeds.
func (q *Queue) deliver(m Message) {
	err := q.send(m)
	if err == nil {
		return
	}
	q.recordError(err)
	q.sleep(retryDelay)
	err = q.send(m)
	if err != nil {
		q.recordError(err)
		q.logf("notify: send failed twice: key=%s to=%s err=%v", m.Key, m.To, err)
	}
}

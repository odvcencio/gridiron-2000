package league

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/notify"
)

func assignNotificationMembers(t *testing.T, svc *Service, emails ...string) {
	t.Helper()
	for _, email := range emails {
		if _, _, err := svc.store.AssignMember(email, strings.ToUpper(email[:1])); err != nil {
			t.Fatal(err)
		}
	}
}

func assertReceiptNoPII(t *testing.T, receipt NotificationReceipt, emails ...string) {
	t.Helper()
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range emails {
		if strings.Contains(string(encoded), email) {
			t.Fatalf("receipt JSON contains recipient PII %q: %s", email, encoded)
		}
	}
}

func testReceiptAnnouncement(id string, now time.Time) Announcement {
	return Announcement{ID: id, Body: "A commissioner note.", PostedAt: now, PostedBy: "The Commissioner"}
}

func TestNotificationReceiptTransportState(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	t.Run("disabled transport", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
		assignNotificationMembers(t, svc, "disabled-a@example.com", "disabled-b@example.com")
		svc.SetNotifier(svc.notifyQueue, false)

		got := svc.notifyAnnouncement(testReceiptAnnouncement("ann-disabled", now))
		if got.Requested != 2 || !got.TransportDisabled || got.Queued != 0 ||
			got.PreferenceSuppressed != 0 || got.AlreadyRecorded != 0 {
			t.Fatalf("disabled receipt = %+v, want requested=2 and delivery off without ledger/send counts", got)
		}
		if len(svc.store.Snapshot().SentLog) != 0 || svc.notifyQueue.Depth() != 0 {
			t.Fatalf("disabled transport touched ledger/queue: sent=%d depth=%d",
				len(svc.store.Snapshot().SentLog), svc.notifyQueue.Depth())
		}
		assertReceiptNoPII(t, got, "disabled-a@example.com", "disabled-b@example.com")
	})

	t.Run("not wired", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
		assignNotificationMembers(t, svc, "unwired@example.com")
		svc.SetNotifier(nil, false)

		got := svc.notifyAnnouncement(testReceiptAnnouncement("ann-unwired", now))
		if got.Requested != 1 || !got.TransportNotWired || got.Queued != 0 {
			t.Fatalf("not-wired receipt = %+v, want requested=1 and delivery not wired", got)
		}
		if len(svc.store.Snapshot().SentLog) != 0 {
			t.Fatal("not-wired transport wrote a ledger entry")
		}
	})
}

func TestNotificationReceiptSuppressionAndQueue(t *testing.T) {
	now := time.Date(2026, 8, 23, 13, 0, 0, 0, time.UTC)

	t.Run("all suppressed", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
		assignNotificationMembers(t, svc, "suppressed-a@example.com", "suppressed-b@example.com")
		for _, email := range []string{"suppressed-a@example.com", "suppressed-b@example.com"} {
			if err := svc.store.SetNotifyPref(email, categoryBroadcast, false); err != nil {
				t.Fatal(err)
			}
		}

		got := svc.notifyAnnouncement(testReceiptAnnouncement("ann-suppressed", now))
		if got.Requested != 2 || got.PreferenceSuppressed != 2 || got.Queued != 0 {
			t.Fatalf("all-suppressed receipt = %+v, want requested=2 suppressed=2 queued=0", got)
		}
		if svc.notifyQueue.Depth() != 0 || len(svc.store.Snapshot().SentLog) != 0 {
			t.Fatal("suppressed recipients reached the queue or ledger")
		}
		assertReceiptNoPII(t, got, "suppressed-a@example.com", "suppressed-b@example.com")
	})

	t.Run("mixed queued and suppressed", func(t *testing.T) {
		svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
		assignNotificationMembers(t, svc, "queued@example.com", "suppressed@example.com")
		if err := svc.store.SetNotifyPref("suppressed@example.com", categoryBroadcast, false); err != nil {
			t.Fatal(err)
		}

		got := svc.notifyAnnouncement(testReceiptAnnouncement("ann-mixed", now))
		if got.Requested != 2 || got.Queued != 1 || got.PreferenceSuppressed != 1 {
			t.Fatalf("mixed receipt = %+v, want requested=2 queued=1 suppressed=1", got)
		}
		if svc.notifyQueue.Depth() != 1 || sentLogCount(svc.store.Snapshot(), "broadcast:") != 1 {
			t.Fatalf("mixed outcome queue/ledger = depth %d / %d, want 1 / 1",
				svc.notifyQueue.Depth(), sentLogCount(svc.store.Snapshot(), "broadcast:"))
		}
		assertReceiptNoPII(t, got, "queued@example.com", "suppressed@example.com")
	})
}

func TestNotificationReceiptDuplicateLedger(t *testing.T) {
	now := time.Date(2026, 8, 23, 14, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
	assignNotificationMembers(t, svc, "duplicate@example.com")
	announcement := testReceiptAnnouncement("ann-duplicate", now)

	first := svc.notifyAnnouncement(announcement)
	second := svc.notifyAnnouncement(announcement)
	if first.Requested != 1 || first.Queued != 1 {
		t.Fatalf("first receipt = %+v, want requested=1 queued=1", first)
	}
	if second.Requested != 1 || second.AlreadyRecorded != 1 || second.Queued != 0 {
		t.Fatalf("duplicate receipt = %+v, want requested=1 already_recorded=1 queued=0", second)
	}
	if svc.notifyQueue.Depth() != 1 || sentLogCount(svc.store.Snapshot(), "broadcast:") != 1 {
		t.Fatalf("duplicate changed queue/ledger: depth=%d ledger=%d",
			svc.notifyQueue.Depth(), sentLogCount(svc.store.Snapshot(), "broadcast:"))
	}
	assertReceiptNoPII(t, second, "duplicate@example.com")
}

func TestNotificationReceiptQueueFullDropKeepsLedger(t *testing.T) {
	now := time.Date(2026, 8, 23, 15, 0, 0, 0, time.UTC)
	queue := notify.New(func(notify.Message) error { return nil }, func(string, ...any) {})
	for i := 0; i < 10000; i++ {
		if queue.Enqueue(notify.Message{Key: "prefill"}) == notify.EnqueueDropped {
			break
		}
		if i == 9999 {
			t.Fatal("queue never reported a drop while filling")
		}
	}
	depth := queue.Depth()
	svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
	svc.SetNotifier(queue, true)
	assignNotificationMembers(t, svc, "dropped@example.com")

	announcement := testReceiptAnnouncement("ann-dropped", now)
	got := svc.notifyAnnouncement(announcement)
	if got.Requested != 1 || got.QueueDrops != 1 || got.Queued != 0 {
		t.Fatalf("queue-full receipt = %+v, want requested=1 dropped=1 queued=0", got)
	}
	if queue.Depth() != depth {
		t.Fatalf("queue depth changed after drop: got %d want %d", queue.Depth(), depth)
	}
	if _, ok := svc.store.Snapshot().SentLog[keyBroadcast(announcement.ID, "dropped@example.com")]; !ok {
		t.Fatal("queue-full drop lost the durable at-most-once ledger entry")
	}
	assertReceiptNoPII(t, got, "dropped@example.com")
}

func TestNotificationReceiptLedgerFailure(t *testing.T) {
	now := time.Date(2026, 8, 23, 16, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
	assignNotificationMembers(t, svc, "ledger-error@example.com")
	failThisStorePersist(svc.store)

	got := svc.notifyAnnouncement(testReceiptAnnouncement("ann-ledger-error", now))
	if got.Requested != 1 || got.LedgerFailures != 1 || got.Queued != 0 {
		t.Fatalf("ledger-error receipt = %+v, want requested=1 ledger_failures=1 queued=0", got)
	}
	if svc.notifyQueue.Depth() != 0 || len(svc.store.Snapshot().SentLog) != 0 {
		t.Fatal("ledger failure reached the queue or committed a sent entry")
	}
	assertReceiptNoPII(t, got, "ledger-error@example.com")
}

func TestDraftOrderNotificationReceipt(t *testing.T) {
	now := time.Date(2026, 8, 23, 17, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(time.Hour), now)
	svc.demoMode = true
	assignNotificationMembers(t, svc, "order-queued@example.com", "order-suppressed@example.com")
	if err := svc.store.SetNotifyPref("order-suppressed@example.com", categoryDraftReminders, false); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	scheduleCreated, got, err := svc.AdminRandomizeDraftOrder(request, "")
	if err != nil {
		t.Fatal(err)
	}
	if !scheduleCreated || got.Requested != 2 || got.Queued != 1 || got.PreferenceSuppressed != 1 {
		t.Fatalf("draft-order result/receipt = created=%v %+v, want created=true requested=2 queued=1 suppressed=1",
			scheduleCreated, got)
	}
	if svc.notifyQueue.Depth() != 1 {
		t.Fatalf("draft-order queue depth = %d, want 1", svc.notifyQueue.Depth())
	}
	assertReceiptNoPII(t, got, "order-queued@example.com", "order-suppressed@example.com")
}

func TestAdminPostAnnouncementReturnsNotificationReceipt(t *testing.T) {
	svc := newAnnouncementAdminService(t)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	assignNotificationMembers(t, svc, "announcement@example.com")

	_, got, err := svc.AdminPostAnnouncement(request, "A note.", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requested != 1 || got.Queued != 1 || got.PreferenceSuppressed != 0 {
		t.Fatalf("admin announcement receipt = %+v, want requested=1 queued=1", got)
	}
	assertReceiptNoPII(t, got, "announcement@example.com")
}

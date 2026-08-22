package league

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/notify"
)

// TestPostAnnouncementCapAndOrder pins the "newest first, cap 20" rule
// (league-announcements spec): posting a 21st announcement drops the
// oldest.
func TestPostAnnouncementCapAndOrder(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 21; i++ {
		if _, err := store.PostAnnouncement(fmt.Sprintf("note %d", i), "commish@example.com", base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got := store.Snapshot().Announcements
	if len(got) != announcementCap {
		t.Fatalf("len(Announcements) = %d, want %d (cap)", len(got), announcementCap)
	}
	if got[0].Body != "note 20" {
		t.Fatalf("got[0].Body = %q, want %q (newest first)", got[0].Body, "note 20")
	}
	if got[len(got)-1].Body != "note 1" {
		t.Fatalf("got[len-1].Body = %q, want %q (note 0 dropped past the cap)", got[len(got)-1].Body, "note 1")
	}
}

// TestPostAnnouncementRejectsEmptyAndOversize pins the exact validation
// messages: an empty (or whitespace-only) body, and a body over 500 runes.
// Exactly 500 runes is still accepted.
func TestPostAnnouncementRejectsEmptyAndOversize(t *testing.T) {
	store := newTestStore(t)

	_, err := store.PostAnnouncement("   ", "commish@example.com", time.Now())
	if err == nil {
		t.Fatal("empty body accepted")
	}
	if got := err.Error(); got != "announcement text is required" {
		t.Fatalf("error = %q, want the exact empty-body message", got)
	}

	oversize := strings.Repeat("x", announcementBodyMaxRunes+1)
	_, err = store.PostAnnouncement(oversize, "commish@example.com", time.Now())
	if err == nil {
		t.Fatal("oversize body accepted")
	}
	if got := err.Error(); got != "announcements must be 500 characters or fewer" {
		t.Fatalf("error = %q, want the exact oversize message", got)
	}

	exact := strings.Repeat("x", announcementBodyMaxRunes)
	if _, err := store.PostAnnouncement(exact, "commish@example.com", time.Now()); err != nil {
		t.Fatalf("exactly %d runes must be accepted: %v", announcementBodyMaxRunes, err)
	}
}

// TestDeleteAnnouncement checks per-item delete and its idempotent-no-op
// precedent for an unknown ID (matches ReleaseBadge's shape).
func TestDeleteAnnouncement(t *testing.T) {
	store := newTestStore(t)
	a, err := store.PostAnnouncement("Hello, league.", "commish@example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteAnnouncement("unknown-id"); err != nil {
		t.Fatalf("deleting an unknown id must be a harmless no-op: %v", err)
	}
	if got := len(store.Snapshot().Announcements); got != 1 {
		t.Fatalf("no-op delete changed the count: %d", got)
	}

	if err := store.DeleteAnnouncement(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Snapshot().Announcements); got != 0 {
		t.Fatalf("delete left %d announcements, want 0", got)
	}
}

// TestStateFingerprintChangesOnAnnouncementPostAndDelete mirrors
// TestStateFingerprintChangesOnBadgeClaimAndRelease (badge_test.go):
// Announcements lives directly in PersistedState, so StateFingerprint's
// whole-state hash already covers it — no separate |announce: suffix is
// needed the way the out-of-state blitz/avatar data needs one.
func TestStateFingerprintChangesOnAnnouncementPostAndDelete(t *testing.T) {
	service := newTestService(t, false)
	before := service.StateFingerprint(1)

	a, err := service.store.PostAnnouncement("Hello, league.", "commish@example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	afterPost := service.StateFingerprint(1)
	if before == afterPost {
		t.Fatal("StateFingerprint did not change after PostAnnouncement")
	}

	if err := service.store.DeleteAnnouncement(a.ID); err != nil {
		t.Fatal(err)
	}
	afterDelete := service.StateFingerprint(1)
	if afterPost == afterDelete {
		t.Fatal("StateFingerprint did not change after DeleteAnnouncement")
	}
}

// TestNotifyAnnouncementRespectsPrefsAndIdempotency checks the N11 email
// hook (notifications.go): a member's own broadcast preference gates the
// send, and firing the same announcement twice sends only once (the
// FirstSend ledger, keyed by keyBroadcast(announcement ID, email)).
func TestNotifyAnnouncementRespectsPrefsAndIdempotency(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(48*time.Hour), now)

	if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.store.AssignMember("b@example.com", "B"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetNotifyPref("b@example.com", categoryBroadcast, false); err != nil {
		t.Fatal(err)
	}

	a := Announcement{ID: "ann-testfixed01", Body: "Season starts soon.", PostedAt: now, PostedBy: "commish@example.com"}
	svc.notifyAnnouncement(a)

	state := svc.store.Snapshot()
	if _, sent := state.SentLog[keyBroadcast(a.ID, "a@example.com")]; !sent {
		t.Fatal("member a (default broadcast pref = on) must be recorded as sent")
	}
	if _, sent := state.SentLog[keyBroadcast(a.ID, "b@example.com")]; sent {
		t.Fatal("member b opted out of broadcast; must not be recorded as sent")
	}
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("queue depth = %d, want 1 (only member a)", got)
	}

	// Idempotency: firing the same announcement again must not re-send.
	svc.notifyAnnouncement(a)
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("queue depth after a repeat fire = %d, want still 1 (idempotent)", got)
	}
}

// newAnnouncementAdminService builds a demo-mode (commissioner-granted)
// Service with a wired-but-unstarted notify queue, for exercising
// AdminPostAnnouncement's commissioner gate and email-toggle wiring end to
// end (newNotifyTestService's "queue never Started" pattern).
func newAnnouncementAdminService(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t, true) // demo mode grants commissioner
	queue := notify.New(func(notify.Message) error { return nil }, func(string, ...any) {})
	svc.SetNotifier(queue, true)
	return svc
}

// TestAdminPostAnnouncementProvenanceAndEmailToggle checks
// AdminPostAnnouncement end to end: PostedBy provenance, trimming, and
// that the "also email the league" toggle is the only thing that enqueues
// mail.
func TestAdminPostAnnouncementProvenanceAndEmailToggle(t *testing.T) {
	svc := newAnnouncementAdminService(t)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}

	a, err := svc.AdminPostAnnouncement(request, "  Draft starts Saturday.  ", false)
	if err != nil {
		t.Fatal(err)
	}
	if a.Body != "Draft starts Saturday." {
		t.Fatalf("body = %q, want trimmed", a.Body)
	}
	// The test request carries no signed-in user; commissionerProvenance
	// falls back to its demo/no-name branch.
	if a.PostedBy != "The Commissioner" {
		t.Fatalf("PostedBy = %q, want %q", a.PostedBy, "The Commissioner")
	}
	if got := svc.notifyQueue.Depth(); got != 0 {
		t.Fatalf("also_email=false must not enqueue; depth = %d", got)
	}

	if _, err := svc.AdminPostAnnouncement(request, "Second note.", true); err != nil {
		t.Fatal(err)
	}
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("also_email=true must enqueue once per seated member; depth = %d, want 1", got)
	}
}

// TestAdminPostAnnouncementRequiresCommissioner mirrors
// TestAdminSetRosterShapeRequiresCommissioner.
func TestAdminPostAnnouncementRequiresCommissioner(t *testing.T) {
	svc := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := svc.AdminPostAnnouncement(request, "Hello.", false); err == nil {
		t.Fatal("a non-commissioner request must be rejected")
	}
	if err := svc.AdminDeleteAnnouncement(request, "some-id"); err == nil {
		t.Fatal("a non-commissioner request must be rejected")
	}
}

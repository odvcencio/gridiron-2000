package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPostLockerTopLevelAndFlatReply(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	top, err := store.PostLocker("", "Welcome to the Locker Room.", "primary@example.com", "Primary", "team-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if top.ParentID != "" || top.ID == "" {
		t.Fatalf("top-level post = %+v", top)
	}

	reply, err := store.PostLocker(top.ID, "Good to be here.", "co@example.com", "Co-Manager", "team-2", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reply.ParentID != top.ID {
		t.Fatalf("reply.ParentID = %q, want %q", reply.ParentID, top.ID)
	}

	got := store.Snapshot().LockerPosts
	if len(got) != 2 {
		t.Fatalf("len(LockerPosts) = %d, want 2", len(got))
	}
}

func TestPostLockerRejectsNestedReplyBeyondOneLevel(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	top, err := store.PostLocker("", "Top post.", "primary@example.com", "Primary", "team-1", now)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := store.PostLocker(top.ID, "First reply.", "co@example.com", "Co-Manager", "team-2", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PostLocker(reply.ID, "Reply to a reply.", "primary@example.com", "Primary", "team-1", now.Add(2*time.Minute)); err == nil {
		t.Fatal("a reply to a reply was accepted")
	} else if !strings.Contains(err.Error(), "one level") {
		t.Fatalf("error = %q, want the one-level message", err.Error())
	}
}

func TestPostLockerRejectsUnknownParent(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.PostLocker("does-not-exist", "A reply to nothing.", "primary@example.com", "Primary", "team-1", time.Now()); err == nil {
		t.Fatal("a reply to an unknown parent was accepted")
	}
}

func TestPostLockerValidatesBodyLength(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	if _, err := store.PostLocker("", "   ", "primary@example.com", "Primary", "team-1", now); err == nil {
		t.Fatal("an empty (whitespace-only) body was accepted")
	}

	exact := strings.Repeat("x", lockerBodyMaxRunes)
	if _, err := store.PostLocker("", exact, "primary@example.com", "Primary", "team-1", now); err != nil {
		t.Fatalf("exactly %d runes was rejected: %v", lockerBodyMaxRunes, err)
	}

	oversize := strings.Repeat("x", lockerBodyMaxRunes+1)
	if _, err := store.PostLocker("", oversize, "primary@example.com", "Primary", "team-1", now.Add(time.Second)); err == nil {
		t.Fatal("a body over the rune limit was accepted")
	}
}

func TestPostLockerRateLimitsSixPerMinutePerIdentity(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	for i := 0; i < lockerPostRateLimit; i++ {
		if _, err := store.PostLocker("", "post", "primary@example.com", "Primary", "team-1", now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("post %d within the limit failed: %v", i, err)
		}
	}
	if _, err := store.PostLocker("", "post", "primary@example.com", "Primary", "team-1", now.Add(6*time.Second)); err == nil {
		t.Fatal("the seventh post inside one minute was accepted")
	}
	// A different identity is never throttled by another identity's log.
	if _, err := store.PostLocker("", "post", "co@example.com", "Co-Manager", "team-2", now.Add(6*time.Second)); err != nil {
		t.Fatalf("a different identity's post was refused: %v", err)
	}
	// Once the window rolls forward, the original identity may post again.
	if _, err := store.PostLocker("", "post", "primary@example.com", "Primary", "team-1", now.Add(90*time.Second)); err != nil {
		t.Fatalf("a post after the rate window rolled forward was refused: %v", err)
	}
}

func TestRemoveLockerPostTombstonesAndIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	post, err := store.PostLocker("", "Removable post.", "primary@example.com", "Primary", "team-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveLockerPost(post.ID, "author", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot().LockerPosts[0]
	if got.Body != "" || got.RemovedAt.IsZero() || got.RemovedByRole != "author" {
		t.Fatalf("removed post = %+v, want a cleared-body tombstone", got)
	}
	if got.AuthorEmail != "primary@example.com" {
		t.Fatalf("removal cleared authorship provenance: %+v", got)
	}

	// Removing an already-removed post, or an unknown ID, is a harmless
	// no-op (DeleteAnnouncement/ReleaseBadge's precedent).
	if err := store.RemoveLockerPost(post.ID, "commissioner", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("re-removing an already-removed post errored: %v", err)
	}
	if store.Snapshot().LockerPosts[0].RemovedByRole != "author" {
		t.Fatal("a second removal overwrote the original removal's audit metadata")
	}
	if err := store.RemoveLockerPost("unknown-id", "commissioner", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("removing an unknown ID errored: %v", err)
	}
}

func TestLockerGenerationAdvancesOnPostAndRemoval(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	before := store.LockerGeneration()
	post, err := store.PostLocker("", "post", "primary@example.com", "Primary", "team-1", now)
	if err != nil {
		t.Fatal(err)
	}
	afterPost := store.LockerGeneration()
	if afterPost <= before {
		t.Fatalf("generation after post = %d, want > %d", afterPost, before)
	}
	if err := store.RemoveLockerPost(post.ID, "author", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := store.LockerGeneration(); got <= afterPost {
		t.Fatalf("generation after removal = %d, want > %d", got, afterPost)
	}
}

// lockerRequestFor reuses internal/league's own withPublicEntryRequest
// helper (public_entry_test.go, service is unused there) to drive fn
// through a request the auth middleware has already attached email's
// identity to, so Service.CurrentUser resolves it exactly as a real
// signed-in request would.
func lockerRequestFor(t *testing.T, email string, fn func(*http.Request)) {
	t.Helper()
	withPublicEntryRequest(t, nil, email, fn)
}

func TestServicePostLockerPostRequiresAdmission(t *testing.T) {
	service := newTestService(t, false)
	member, _, err := service.store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}

	lockerRequestFor(t, "unadmitted@example.com", func(r *http.Request) {
		if _, err := service.PostLockerPost(r, "", "hello"); err == nil {
			t.Fatal("an un-admitted signed-in identity posted successfully")
		} else if !strings.Contains(err.Error(), "admission") {
			t.Fatalf("error = %q, want an admission message", err.Error())
		}
	})
	lockerRequestFor(t, "primary@example.com", func(r *http.Request) {
		post, err := service.PostLockerPost(r, "", "hello, league")
		if err != nil {
			t.Fatalf("an admitted seated identity failed to post: %v", err)
		}
		if post.AuthorTeamID != member.TeamID {
			t.Fatalf("post.AuthorTeamID = %q, want %q", post.AuthorTeamID, member.TeamID)
		}
	})
}

func TestServicePostLockerPostAllowsSeatlessMember(t *testing.T) {
	service := newTestService(t, false)
	if _, _, err := service.store.EnsureMember("seatless@example.com", "Seatless"); err != nil {
		t.Fatal(err)
	}
	lockerRequestFor(t, "seatless@example.com", func(r *http.Request) {
		post, err := service.PostLockerPost(r, "", "seatless voices count too")
		if err != nil {
			t.Fatalf("an admitted seatless identity failed to post: %v", err)
		}
		if post.AuthorTeamID != "" {
			t.Fatalf("post.AuthorTeamID = %q, want empty for a seatless member", post.AuthorTeamID)
		}
	})
}

func TestServicePostLockerPostDemoModeIsReadOnly(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodGet, "/locker", nil)
	if _, err := service.PostLockerPost(request, "", "demo post"); err == nil {
		t.Fatal("demo mode accepted a Locker Room post")
	} else if !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("error = %q, want a read-only message", err.Error())
	}
}

func TestServiceRemoveLockerPostAuthorOrCommissioner(t *testing.T) {
	service := newTestService(t, false)
	if _, _, err := service.store.AssignMember("author@example.com", "Author"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("other@example.com", "Other"); err != nil {
		t.Fatal(err)
	}

	var postID string
	lockerRequestFor(t, "author@example.com", func(r *http.Request) {
		post, err := service.PostLockerPost(r, "", "removable")
		if err != nil {
			t.Fatal(err)
		}
		postID = post.ID
	})

	lockerRequestFor(t, "other@example.com", func(r *http.Request) {
		if err := service.RemoveLockerPost(r, postID); err == nil {
			t.Fatal("a non-author, non-commissioner identity removed another member's post")
		}
	})

	t.Setenv("COMMISSIONER_EMAILS", "commish@example.com")
	lockerRequestFor(t, "commish@example.com", func(r *http.Request) {
		if err := service.RemoveLockerPost(r, postID); err != nil {
			t.Fatalf("the commissioner failed to remove another member's post: %v", err)
		}
	})
	got := service.store.Snapshot().LockerPosts[0]
	if got.RemovedByRole != "commissioner" || got.Body != "" {
		t.Fatalf("removed post = %+v", got)
	}
}

func TestServiceRemoveLockerPostDemoModeIsReadOnly(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodGet, "/locker", nil)
	if err := service.RemoveLockerPost(request, "any-id"); err == nil {
		t.Fatal("demo mode accepted a Locker Room removal")
	} else if !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("error = %q, want a read-only message", err.Error())
	}
}

func TestLockerDataPaginationOrderingAndTruthfulEmptyState(t *testing.T) {
	service := newTestService(t, false)
	member, _, err := service.store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	team := member.TeamID

	emptyRequest := httptest.NewRequest(http.MethodGet, "/locker", nil)
	empty := service.LockerData(emptyRequest)
	if empty["has_posts"] != false || empty["posts_empty"] != true {
		t.Fatalf("empty board data = %+v, want a truthful empty state", empty)
	}

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	base := 60
	for i := 0; i < base; i++ {
		if _, err := service.store.PostLocker("", "post", "primary@example.com", "Primary", team, now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatalf("seed post %d: %v", i, err)
		}
	}
	first := service.LockerData(emptyRequest)
	posts, ok := first["posts"].([]LockerPostView)
	if !ok || len(posts) != poolPageSize {
		t.Fatalf("first page post count = %d (ok=%v), want %d", len(posts), ok, poolPageSize)
	}
	if first["posts_count"] != base || first["pages"] != 2 {
		t.Fatalf("pagination = count:%v pages:%v, want count:%d pages:2", first["posts_count"], first["pages"], base)
	}
	// Newest first: the 60th seeded post (index 59, the latest timestamp)
	// must lead the first page.
	if posts[0].Body != "post" {
		t.Fatalf("first page did not carry seeded posts: %+v", posts[0])
	}

	secondPageRequest := httptest.NewRequest(http.MethodGet, "/locker?page=2", nil)
	second := service.LockerData(secondPageRequest)
	secondPosts, ok := second["posts"].([]LockerPostView)
	if !ok || len(secondPosts) != base-poolPageSize {
		t.Fatalf("second page post count = %d (ok=%v), want %d", len(secondPosts), ok, base-poolPageSize)
	}
}

func TestLockerDataRepliesNestOneLevelOldestFirstUnderTheirParent(t *testing.T) {
	service := newTestService(t, false)
	member, _, err := service.store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	team := member.TeamID
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	top, err := service.store.PostLocker("", "top", "primary@example.com", "Primary", team, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.PostLocker(top.ID, "second reply", "primary@example.com", "Primary", team, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.PostLocker(top.ID, "first reply", "primary@example.com", "Primary", team, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/locker", nil)
	data := service.LockerData(request)
	posts, ok := data["posts"].([]LockerPostView)
	if !ok || len(posts) != 1 {
		t.Fatalf("posts = %#v", data["posts"])
	}
	if len(posts[0].Replies) != 2 {
		t.Fatalf("len(Replies) = %d, want 2", len(posts[0].Replies))
	}
	if posts[0].Replies[0].Body != "first reply" || posts[0].Replies[1].Body != "second reply" {
		t.Fatalf("replies = %+v, want oldest-first order", posts[0].Replies)
	}
	if len(posts[0].Replies[0].Replies) != 0 {
		t.Fatal("a reply itself carried nested replies")
	}
}

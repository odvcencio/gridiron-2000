package league

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// TestSetLockerReactionTogglesPerMemberAndSurvivesReload pins the store
// contract for Locker Room reactions: one reaction per member per emoji
// per post, toggled on and off, refused for an unknown post or an emoji
// outside the fixed set, and persisted through the additive kv set.
func TestSetLockerReactionTogglesPerMemberAndSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	post, err := store.PostLocker("", "Who wants Derrick Henry?", "kernel@example.com", "Kernel", "team-1", false, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLockerReaction(post.ID, "🔥", "zoe@example.com", true, now); err != nil {
		t.Fatalf("react: %v", err)
	}
	if err := store.SetLockerReaction(post.ID, "🔥", "ash@example.com", true, now); err != nil {
		t.Fatalf("second member react: %v", err)
	}
	if err := store.SetLockerReaction(post.ID, "🍕", "zoe@example.com", true, now); err == nil {
		t.Fatal("an emoji outside the fixed set was accepted")
	}
	if err := store.SetLockerReaction("no-such-post", "🔥", "zoe@example.com", true, now); err == nil {
		t.Fatal("a reaction on an unknown post was accepted")
	}
	if got := len(store.Snapshot().LockerReactions[post.ID]["🔥"]); got != 2 {
		t.Fatalf("🔥 reactions = %d, want 2", got)
	}
	if err := store.SetLockerReaction(post.ID, "🔥", "zoe@example.com", false, now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLockerReaction(post.ID, "👍", "zoe@example.com", false, now); err != nil {
		t.Fatalf("removing a reaction never made must be a no-op, got %v", err)
	}
	reloaded := NewStore(path).Snapshot()
	if got := reloaded.LockerReactions[post.ID]["🔥"]; len(got) != 1 || got["ash@example.com"].IsZero() {
		t.Fatalf("reactions after reload = %+v, want ash's 🔥 only", got)
	}
}

// TestReactToLockerPostRendersCountsAndTheViewersOwnMark pins the service
// and view contract: a signed-in member toggles a reaction, every post
// view carries the fixed emoji set with counts, and the viewer's own
// reaction reads as theirs (Mine) with the toggle value flipped.
func TestReactToLockerPostRendersCountsAndTheViewersOwnMark(t *testing.T) {
	svc := newTestService(t, false)
	if _, _, err := svc.store.AssignMember("kernel@example.com", "Kernel Panic"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.store.AssignMember("zoe@example.com", "Zoe"); err != nil {
		t.Fatal(err)
	}
	now := svc.clock()
	post, err := svc.store.PostLocker("", "Trade block is open.", "kernel@example.com", "Kernel Panic", "team-1", false, now)
	if err != nil {
		t.Fatal(err)
	}
	withPublicEntryRequest(t, svc, "zoe@example.com", func(r *http.Request) {
		if _, err := svc.ReactToLockerPost(r, post.ID, "👍", true); err != nil {
			t.Fatalf("react: %v", err)
		}
		data := svc.LockerData(r)
		posts, _ := data["posts"].([]LockerPostView)
		if len(posts) != 1 || !posts[0].CanReact {
			t.Fatalf("posts = %+v, want one reactable post", posts)
		}
		reactions := posts[0].Reactions
		if len(reactions) != 3 {
			t.Fatalf("reactions = %+v, want the fixed set of three", reactions)
		}
		thumbs := reactions[0]
		if thumbs.Emoji != "👍" || thumbs.Count != 1 || !thumbs.Mine || thumbs.ToggleValue != "0" {
			t.Fatalf("👍 view = %+v, want count 1, mine, toggle off", thumbs)
		}
		if reactions[1].Count != 0 || reactions[1].Mine || reactions[1].ToggleValue != "1" {
			t.Fatalf("🔥 view = %+v, want count 0, not mine, toggle on", reactions[1])
		}
		if _, err := svc.ReactToLockerPost(r, post.ID, "👍", false); err != nil {
			t.Fatal(err)
		}
		again := svc.LockerData(r)
		posts, _ = again["posts"].([]LockerPostView)
		if posts[0].Reactions[0].Count != 0 || posts[0].Reactions[0].Mine {
			t.Fatalf("after unreacting: %+v", posts[0].Reactions[0])
		}
	})
}

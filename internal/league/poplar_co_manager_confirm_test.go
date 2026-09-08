package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCoManagerWelcomeFlashNamesThePrimaryNotTheInvitee is F6, moved here
// (Decision 3, J5 F11) from main.go's own former coManagerWelcomeFlash: the
// welcome sentence must credit the seat's primary manager, never the
// invitee who just joined.
func TestCoManagerWelcomeFlashNamesThePrimaryNotTheInvitee(t *testing.T) {
	got := CoManagerWelcomeFlash("Caleb's Corn Dogs", "Priya Anand Fixture")
	want := "You're co-managing Caleb's Corn Dogs alongside its primary manager, Priya Anand Fixture."
	if got != want {
		t.Fatalf("CoManagerWelcomeFlash = %q, want %q", got, want)
	}
}

func TestCoManagerWelcomeFlashFallsBackWhenNoPrimaryName(t *testing.T) {
	got := CoManagerWelcomeFlash("Caleb's Corn Dogs", "")
	want := "You're co-managing Caleb's Corn Dogs alongside its primary manager, the primary manager."
	if got != want {
		t.Fatalf("CoManagerWelcomeFlash = %q, want %q", got, want)
	}
}

// TestHasPendingCoManagerInviteDoesNotConsume is Decision 3 (J5 F11): a
// read of "is there a pending co-manager invite for this identity" must
// never bind the seat itself — main.go's sign-in callback needs to ask
// this question without the side effect BindCoManagerOnSignIn carries.
func TestHasPendingCoManagerInviteDoesNotConsume(t *testing.T) {
	service := newTestService(t, false)
	primary, _, err := service.store.AssignMember("primary@example.com", "Primary Manager")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager(primary.TeamID, "invitee@example.com"); err != nil {
		t.Fatal(err)
	}

	before := service.store.Snapshot()
	if !service.HasPendingCoManagerInvite("invitee@example.com") {
		t.Fatal("HasPendingCoManagerInvite = false, want true for a freshly invited email")
	}
	after := service.store.Snapshot()
	if len(after.CoInvites) != len(before.CoInvites) {
		t.Fatalf("HasPendingCoManagerInvite consumed the invite: before %v, after %v", before.CoInvites, after.CoInvites)
	}
	if service.HasPendingCoManagerInvite("nobody@example.com") {
		t.Fatal("HasPendingCoManagerInvite = true for an email with no invite")
	}
}

// TestConfirmCoManagerJoinBindsOnlyTheSignedInIdentity is Decision 3's
// own explicit-confirm contract: the seat binds only when the signed-in
// identity itself calls this, never as a side effect of anything else.
func TestConfirmCoManagerJoinBindsOnlyTheSignedInIdentity(t *testing.T) {
	service := newTestService(t, false)
	primary, _, err := service.store.AssignMember("primary@example.com", "Primary Manager")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager(primary.TeamID, "invitee@example.com"); err != nil {
		t.Fatal(err)
	}

	// Not signed in at all: refuses, does not bind.
	unauth := httptest.NewRequest(http.MethodPost, "/", nil)
	if _, err := service.ConfirmCoManagerJoin(unauth); err == nil {
		t.Fatal("ConfirmCoManagerJoin with no session = nil error, want a refusal")
	}
	if !service.HasPendingCoManagerInvite("invitee@example.com") {
		t.Fatal("the invite must still be pending after an unauthenticated attempt")
	}

	// A signed-in identity with no pending invite of its own: refuses.
	withPublicEntryRequest(t, service, "bystander@example.com", func(r *http.Request) {
		if _, err := service.ConfirmCoManagerJoin(r); err == nil {
			t.Error("ConfirmCoManagerJoin for an identity with no pending invite = nil error, want a refusal")
		}
	})
	if !service.HasPendingCoManagerInvite("invitee@example.com") {
		t.Fatal("a bystander's failed attempt must not touch the real invitee's own pending invite")
	}

	var member Member
	var bindErr error
	withPublicEntryRequest(t, service, "invitee@example.com", func(r *http.Request) {
		member, bindErr = service.ConfirmCoManagerJoin(r)
	})
	if bindErr != nil {
		t.Fatalf("ConfirmCoManagerJoin for the invited identity = %v, want success", bindErr)
	}
	if member.TeamID != primary.TeamID || member.Role != "co" {
		t.Fatalf("ConfirmCoManagerJoin bound = %+v, want team %q role co", member, primary.TeamID)
	}
	if service.HasPendingCoManagerInvite("invitee@example.com") {
		t.Fatal("invite is still pending after a successful join")
	}

	// A second call for the same identity, now bound: no pending invite
	// left, so it refuses rather than silently doing nothing.
	var secondErr error
	withPublicEntryRequest(t, service, "invitee@example.com", func(r *http.Request) {
		_, secondErr = service.ConfirmCoManagerJoin(r)
	})
	if secondErr == nil {
		t.Fatal("a second ConfirmCoManagerJoin call = nil error, want a refusal (nothing pending)")
	}
	if !strings.Contains(strings.ToLower(secondErr.Error()), "pending") {
		t.Errorf("second-call error = %q, want it to say nothing is pending", secondErr.Error())
	}
}

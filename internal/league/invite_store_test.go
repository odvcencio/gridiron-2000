package league

import (
	"sync"
	"testing"
	"time"
)

// TestMintInviteLinkWithAdmissionAlsoRecordsPlainInvite is the design's
// "extends AddInvite; one action records the invite AND mints the link"
// contract (section 6.2): a Tier 0 mint must also admit the email through
// the same state.Invites list Service.EmailAllowed reads, or a freshly
// minted link would admit nobody.
func TestMintInviteLinkWithAdmissionAlsoRecordsPlainInvite(t *testing.T) {
	store := newSetupTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if store.Invited("newcomer@example.com") {
		t.Fatal("email should not be invited before minting")
	}
	if _, _, err := store.MintInviteLinkWithAdmission("newcomer@example.com", "commish@example.com", 0, now); err != nil {
		t.Fatal(err)
	}
	if !store.Invited("newcomer@example.com") {
		t.Fatal("MintInviteLinkWithAdmission must also record the plain admission invite")
	}
}

// TestMintInviteLinkAloneDoesNotRecordPlainInvite documents the narrower
// entry point's contract: it mints a link only, for a caller that already
// manages admission separately.
func TestMintInviteLinkAloneDoesNotRecordPlainInvite(t *testing.T) {
	store := newSetupTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if _, _, err := store.MintInviteLink("already-admitted@example.com", "commish@example.com", 0, now); err != nil {
		t.Fatal(err)
	}
	if store.Invited("already-admitted@example.com") {
		t.Fatal("MintInviteLink alone must not record a plain admission invite")
	}
}

func TestSetupDraftRoundTrip(t *testing.T) {
	store := newSetupTestStore(t)
	if _, found, err := store.LoadSetupDraft(); err != nil || found {
		t.Fatalf("fresh store: found=%v err=%v, want found=false", found, err)
	}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := store.SaveSetupDraft([]byte(`{"a":1}`), []byte(`{"identity":"done"}`), now); err != nil {
		t.Fatalf("SaveSetupDraft: %v", err)
	}
	got, found, err := store.LoadSetupDraft()
	if err != nil || !found {
		t.Fatalf("found=%v err=%v, want found=true", found, err)
	}
	if string(got.DraftJSON) != `{"a":1}` || string(got.StepStatusJSON) != `{"identity":"done"}` {
		t.Fatalf("LoadSetupDraft() = %+v, unexpected payload", got)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, now)
	}
	// A second save replaces the single row rather than appending.
	later := now.Add(time.Minute)
	if err := store.SaveSetupDraft([]byte(`{"a":2}`), []byte(`{"identity":"done","teams":"current"}`), later); err != nil {
		t.Fatalf("SaveSetupDraft (2nd): %v", err)
	}
	got2, found2, err := store.LoadSetupDraft()
	if err != nil || !found2 {
		t.Fatalf("found=%v err=%v after 2nd save", found2, err)
	}
	if string(got2.DraftJSON) != `{"a":2}` {
		t.Fatalf("2nd DraftJSON = %s, want replaced value", got2.DraftJSON)
	}
}

func TestMintInviteLinkSupersedesEarlierUnusedLink(t *testing.T) {
	store := newSetupTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	token1, link1, err := store.MintInviteLink("Manager@Example.com", "commish@example.com", 0, now)
	if err != nil {
		t.Fatalf("first mint: %v", err)
	}
	if link1.Email != "manager@example.com" {
		t.Fatalf("email not lowercased: %q", link1.Email)
	}
	wantExpiry := now.Add(DefaultInviteLinkTTL)
	if !link1.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("ExpiresAt = %v, want default TTL %v", link1.ExpiresAt, wantExpiry)
	}
	token2, link2, err := store.MintInviteLink("manager@example.com", "commish@example.com", 0, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if token1 == token2 {
		t.Fatalf("second mint returned the same raw token")
	}
	// The first link is now revoked; the second is still unused.
	first, ok, err := store.LookupInviteLinkByToken(token1)
	if err != nil || !ok {
		t.Fatalf("lookup token1: ok=%v err=%v", ok, err)
	}
	if state := first.StateAt(now.Add(time.Minute)); state != InviteLinkRevoked {
		t.Fatalf("token1 state = %s, want revoked", state)
	}
	second, ok, err := store.LookupInviteLinkByToken(token2)
	if err != nil || !ok {
		t.Fatalf("lookup token2: ok=%v err=%v", ok, err)
	}
	if state := second.StateAt(now.Add(time.Minute)); state != InviteLinkUnused {
		t.Fatalf("token2 state = %s, want unused", state)
	}
	if link2.ID == first.ID {
		t.Fatalf("expected a distinct row for the second mint")
	}
}

func TestLookupInviteLinkByTokenUnknownTokenIsGenericallyInvalid(t *testing.T) {
	store := newSetupTestStore(t)
	link, ok, err := store.LookupInviteLinkByToken("no-such-token-ever-minted")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatalf("unknown token resolved to a link: %+v", link)
	}
}

func TestConsumeInviteLinkSingleUse(t *testing.T) {
	store := newSetupTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	token, _, err := store.MintInviteLink("manager@example.com", "commish@example.com", 0, now)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	ok, err := store.ConsumeInviteLink(token, "manager@example.com", now.Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("first consume: ok=%v err=%v, want ok=true", ok, err)
	}
	// A replay of the exact same token updates zero rows.
	ok, err = store.ConsumeInviteLink(token, "manager@example.com", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("replay consume returned an error instead of ok=false: %v", err)
	}
	if ok {
		t.Fatalf("replay consume succeeded a second time; single-use is broken")
	}
	link, found, err := store.LookupInviteLinkByToken(token)
	if err != nil || !found {
		t.Fatalf("lookup after consume: found=%v err=%v", found, err)
	}
	if state := link.StateAt(now.Add(3 * time.Minute)); state != InviteLinkConsumed {
		t.Fatalf("state after consume = %s, want consumed", state)
	}
	if link.ConsumedEmail != "manager@example.com" {
		t.Fatalf("ConsumedEmail = %q, want manager@example.com", link.ConsumedEmail)
	}
}

// TestConsumeInviteLinkConcurrentRaceExactlyOneWinner is the design's
// explicit acceptance criterion for slice 3: "invite link replay updates
// zero rows under a concurrent-consume test." Many goroutines race to
// consume the exact same token; the single conditional UPDATE under the
// store's one-writer SQLite pool must let exactly one succeed.
func TestConsumeInviteLinkConcurrentRaceExactlyOneWinner(t *testing.T) {
	store := newSetupTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	token, _, err := store.MintInviteLink("manager@example.com", "commish@example.com", 0, now)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	const attempts = 32
	var wg sync.WaitGroup
	var successCount, errorCount int
	var mu sync.Mutex
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			ok, err := store.ConsumeInviteLink(token, "manager@example.com", now.Add(time.Minute))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errorCount++
				return
			}
			if ok {
				successCount++
			}
		}()
	}
	wg.Wait()
	if errorCount != 0 {
		t.Fatalf("%d concurrent consume attempts returned an error", errorCount)
	}
	if successCount != 1 {
		t.Fatalf("successCount = %d, want exactly 1 winner among %d concurrent attempts", successCount, attempts)
	}
}

func TestConsumeInviteLinkRefusesExpiredRevokedAndUnknown(t *testing.T) {
	store := newSetupTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	expiredToken, _, err := store.MintInviteLink("expired@example.com", "commish@example.com", time.Minute, now)
	if err != nil {
		t.Fatalf("mint expired: %v", err)
	}
	if ok, err := store.ConsumeInviteLink(expiredToken, "expired@example.com", now.Add(time.Hour)); err != nil || ok {
		t.Fatalf("expired consume: ok=%v err=%v, want ok=false", ok, err)
	}

	revokedToken, revokedLink, err := store.MintInviteLink("revoked@example.com", "commish@example.com", 0, now)
	if err != nil {
		t.Fatalf("mint revoked: %v", err)
	}
	if err := store.RevokeInviteLink(revokedLink.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if ok, err := store.ConsumeInviteLink(revokedToken, "revoked@example.com", now.Add(2*time.Minute)); err != nil || ok {
		t.Fatalf("revoked consume: ok=%v err=%v, want ok=false", ok, err)
	}

	if ok, err := store.ConsumeInviteLink("never-issued", "nobody@example.com", now); err != nil || ok {
		t.Fatalf("unknown token consume: ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestInviteLinksForEmailAndInviteLinksListing(t *testing.T) {
	store := newSetupTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if _, _, err := store.MintInviteLink("a@example.com", "commish@example.com", 0, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MintInviteLink("b@example.com", "commish@example.com", 0, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MintInviteLink("a@example.com", "commish@example.com", 0, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	forA, err := store.InviteLinksForEmail("a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(forA) != 2 {
		t.Fatalf("InviteLinksForEmail(a) = %d rows, want 2 (superseded + current)", len(forA))
	}
	all, err := store.InviteLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("InviteLinks() = %d rows, want 3", len(all))
	}
}

func TestConstantTimeTokenEqual(t *testing.T) {
	if !ConstantTimeTokenEqual("abc", "abc") {
		t.Fatal("equal tokens reported unequal")
	}
	if ConstantTimeTokenEqual("abc", "abd") {
		t.Fatal("unequal tokens reported equal")
	}
	if ConstantTimeTokenEqual("abc", "abcd") {
		t.Fatal("different-length tokens reported equal")
	}
}

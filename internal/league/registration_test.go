// Tests for the registration/membership wave: the two signup paths, the
// dual-status dashboard, co-managers, and the domain gate.
package league

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------
// Build item 1 — sign-in creates membership only
// ---------------------------------------------------------------------

// TestSignInMembershipIsSeatless pins main.go's sign-in sequence (build
// item 1: AssignManager's auto-seating retires): BindCoManagerOnSignIn
// with no pending invite reports bound == false, and the EnsureMember
// fallback it is paired with records membership without a team seat.
// Neither call takes an *http.Request — both are exactly what main.go's
// callback now runs, in order.
func TestSignInMembershipIsSeatless(t *testing.T) {
	service := newTestService(t, false)
	member, bound, err := service.BindCoManagerOnSignIn("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if bound {
		t.Fatal("no co-invite is pending; bound must be false")
	}
	member, err = service.EnsureMember("a@example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if member.TeamID != "" {
		t.Fatalf("sign-in must not claim a seat: %+v", member)
	}
}

// ---------------------------------------------------------------------
// Build item 2 — fantasy signup atomicity
// ---------------------------------------------------------------------

func admitSeatlessForClaim(t *testing.T, service *Service, email string) {
	t.Helper()
	if _, err := service.EnsureMember(email, "Admitted Manager"); err != nil {
		t.Fatalf("admit %s: %v", email, err)
	}
}

// TestClaimFantasySeatAtomic checks the happy path: one claimFantasySeat
// call claims a seat, sets the team name, and claims the badge motif —
// all three, in one call.
func TestClaimFantasySeatAtomic(t *testing.T) {
	service := newTestService(t, false)
	admitSeatlessForClaim(t, service, "a@example.com")
	team, err := service.claimFantasySeat("a@example.com", "A", "The Rebrand", "wolf")
	if err != nil {
		t.Fatal(err)
	}
	if team.Name != "The Rebrand" {
		t.Fatalf("team name = %q, want %q", team.Name, "The Rebrand")
	}
	member, ok := service.store.MemberByEmail("a@example.com")
	if !ok || member.TeamID != team.ID {
		t.Fatalf("member not bound to claimed team: %+v", member)
	}
	motif, claimed := service.store.BadgeClaim(team.ID)
	if !claimed || motif != "wolf" {
		t.Fatalf("badge claim = %q, %v; want wolf, true", motif, claimed)
	}
}

func TestClaimFantasySeatRejectsUnrecordedSignedInIdentityWithoutMutation(t *testing.T) {
	service := newTestService(t, false)
	before := service.store.Snapshot()
	var claimErr error
	withPublicEntryRequest(t, service, "unrecorded@example.com", func(r *http.Request) {
		_, claimErr = service.ClaimFantasySeat(r, "Unrecorded Team", "wolf")
	})
	if claimErr == nil || !strings.Contains(strings.ToLower(claimErr.Error()), "admission") ||
		!strings.Contains(strings.ToLower(claimErr.Error()), "ask the commissioner") {
		t.Fatalf("unrecorded claim error = %v", claimErr)
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("unrecorded claim mutated state\nbefore: %#v\n after: %#v", before, after)
	}
}

func TestClaimFantasySeatRejectsSeatedAndReleasedStaleSessionsWithoutMutation(t *testing.T) {
	service := newTestService(t, false)
	const email = "returning@example.com"
	admitSeatlessForClaim(t, service, email)
	var claimed Team
	var claimErr error
	withPublicEntryRequest(t, service, email, func(r *http.Request) {
		claimed, claimErr = service.ClaimFantasySeat(r, "Returning Team", "wolf")
	})
	if claimErr != nil {
		t.Fatal(claimErr)
	}

	seatedBefore := service.store.Snapshot()
	withPublicEntryRequest(t, service, email, func(r *http.Request) {
		_, claimErr = service.ClaimFantasySeat(r, "Second Team", "rocket")
	})
	if claimErr == nil || claimErr.Error() != "you already hold a team seat" {
		t.Fatalf("seated claim error = %v", claimErr)
	}
	if seatedAfter := service.store.Snapshot(); !reflect.DeepEqual(seatedBefore, seatedAfter) {
		t.Fatalf("seated claim mutated state\nbefore: %#v\n after: %#v", seatedBefore, seatedAfter)
	}

	if err := service.store.ReleaseSeat(claimed.ID); err != nil {
		t.Fatal(err)
	}
	releasedBefore := service.store.Snapshot()
	withPublicEntryRequest(t, service, email, func(r *http.Request) {
		_, claimErr = service.ClaimFantasySeat(r, "Stale Session Team", "rocket")
	})
	if claimErr == nil || !strings.Contains(strings.ToLower(claimErr.Error()), "admission") {
		t.Fatalf("released-seat stale-session claim error = %v", claimErr)
	}
	if releasedAfter := service.store.Snapshot(); !reflect.DeepEqual(releasedBefore, releasedAfter) {
		t.Fatalf("released-seat claim mutated state\nbefore: %#v\n after: %#v", releasedBefore, releasedAfter)
	}
}

func TestClaimFantasySeatPendingCoInviteAlwaysWinsAdmissionCheck(t *testing.T) {
	for _, withSeatlessMember := range []bool{false, true} {
		name := "without seatless member"
		if withSeatlessMember {
			name = "with seatless member"
		}
		t.Run(name, func(t *testing.T) {
			service := newTestService(t, false)
			primary, _, err := service.store.AssignMember("primary@example.com", "Primary")
			if err != nil {
				t.Fatal(err)
			}
			const pendingEmail = "pending@example.com"
			if withSeatlessMember {
				admitSeatlessForClaim(t, service, pendingEmail)
			}
			if err := service.store.InviteCoManager(primary.TeamID, pendingEmail); err != nil {
				t.Fatal(err)
			}
			before := service.store.Snapshot()
			var claimErr error
			withPublicEntryRequest(t, service, pendingEmail, func(r *http.Request) {
				_, claimErr = service.ClaimFantasySeat(r, "Competing Team", "rocket")
			})
			if claimErr == nil || !strings.Contains(strings.ToLower(claimErr.Error()), "pending co-manager") ||
				!strings.Contains(claimErr.Error(), service.TeamLabel(primary.TeamID)) {
				t.Fatalf("pending claim error = %v", claimErr)
			}
			if after := service.store.Snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatalf("pending claim mutated state\nbefore: %#v\n after: %#v", before, after)
			}
		})
	}
}

// TestClaimFantasySeatRejectsWhenFull fills every seat, then checks the
// next signup is turned away with the league's own ErrLeagueFull and its
// existing admitted seatless record remains unchanged.
func TestClaimFantasySeatRejectsWhenFull(t *testing.T) {
	service := newTestService(t, false)
	for _, team := range defaultTeams() {
		if _, _, err := service.store.AssignMember(team.ID+"@example.com", team.ID); err != nil {
			t.Fatalf("seed seat for %s: %v", team.ID, err)
		}
	}
	admitSeatlessForClaim(t, service, "late@example.com")
	before := service.store.Snapshot()
	_, err := service.claimFantasySeat("late@example.com", "Late", "Latecomers", "wolf")
	if !errors.Is(err, ErrLeagueFull) {
		t.Fatalf("err = %v, want ErrLeagueFull", err)
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("full-league rejection mutated state\nbefore: %#v\n after: %#v", before, after)
	}
}

// TestClaimFantasySeatRejectsTakenMotifAndRollsBack claims "wolf" for one
// team directly, then drives a second signup at the same motif through
// claimFantasySeat: it must fail with the exact "already claimed by"
// message and must not leave the second seat claimed (the atomic
// rollback contract in claimFantasySeat's doc comment).
func TestClaimFantasySeatRejectsTakenMotifAndRollsBack(t *testing.T) {
	service := newTestService(t, false)
	admitSeatlessForClaim(t, service, "b@example.com")
	if err := service.store.ClaimBadge("team-1", "wolf"); err != nil {
		t.Fatal(err)
	}
	before := claimedSeatCount(service.store.Snapshot().Members)

	_, err := service.claimFantasySeat("b@example.com", "B", "Second Signup", "wolf")
	if err == nil {
		t.Fatal("a taken motif must be rejected")
	}
	want := "that badge is already claimed by " + service.teamByID("team-1").Name
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
	if member, ok := service.store.MemberByEmail("b@example.com"); !ok || member.TeamID != "" {
		t.Fatalf("a rejected signup must preserve the admitted seatless member: %+v", member)
	}
	if got := claimedSeatCount(service.store.Snapshot().Members); got != before {
		t.Fatalf("claimed seat count = %d, want %d (rollback must release the seat)", got, before)
	}
}

// TestClaimFantasySeatRaceOneMotifTwoSignups drives two concurrent
// signups at the same motif and checks exactly one wins: the store's
// ClaimBadge call (not the best-effort pre-check) is the authoritative
// race referee, so this exercises the real concurrent path, not just the
// pre-check's fast rejection.
func TestClaimFantasySeatRaceOneMotifTwoSignups(t *testing.T) {
	service := newTestService(t, false)
	var wg sync.WaitGroup
	results := make([]error, 2)
	emails := []string{"racer-a@example.com", "racer-b@example.com"}
	for _, email := range emails {
		admitSeatlessForClaim(t, service, email)
	}
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := service.claimFantasySeat(emails[i], "Racer", "Racer Team", "wolf")
			results[i] = err
		}(i)
	}
	wg.Wait()

	wins, losses := 0, 0
	for _, err := range results {
		if err == nil {
			wins++
			continue
		}
		losses++
		var taken *badgeTakenError
		if !errors.As(err, &taken) {
			t.Fatalf("loser error = %v, want a badgeTakenError", err)
		}
		assertClaimValidationField(t, err, ClaimFieldMotif)
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("wins=%d losses=%d, want exactly one of each", wins, losses)
	}
	if got := claimedSeatCount(service.store.Snapshot().Members); got != 1 {
		t.Fatalf("claimed seat count after the race = %d, want 1 (the loser's seat must roll back)", got)
	}
}

// TestClaimFantasySeatAfterSeatlessSignIn is the real-world precondition
// regression test: every signed-in member now starts as an EnsureMember
// seatless record (build item 1) before they ever reach /join, so
// claimFantasySeat's AssignMember call must upgrade that existing
// seatless Member in place — not short-circuit and hand back the
// still-seatless record unchanged (the bug this test caught: signup
// silently no-opped for anyone who had ever signed in first). Driving the
// public wrapper mirrors the OAuth callback's persisted membership followed
// by the real /join action.
func TestClaimFantasySeatAfterSeatlessSignIn(t *testing.T) {
	service := newTestService(t, false)
	if _, err := service.EnsureMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	var team Team
	var claimErr error
	withPublicEntryRequest(t, service, "a@example.com", func(r *http.Request) {
		team, claimErr = service.ClaimFantasySeat(r, "Post-Signin Franchise", "wolf")
	})
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	member, ok := service.store.MemberByEmail("a@example.com")
	if !ok || member.TeamID == "" {
		t.Fatalf("signup after sign-in must claim a seat: %+v", member)
	}
	if team.Name != "Post-Signin Franchise" {
		t.Fatalf("team name = %q, want %q", team.Name, "Post-Signin Franchise")
	}
}

// TestClaimFantasySeatRequiresTeamName and TestClaimFantasySeatRequiresKnownMotif
// pin the two field-validation rejections a bad form submission hits
// before any store write.
func TestClaimFantasySeatRequiresTeamName(t *testing.T) {
	service := newTestService(t, false)
	admitSeatlessForClaim(t, service, "a@example.com")
	if _, err := service.claimFantasySeat("a@example.com", "A", "   ", "wolf"); err == nil {
		t.Fatal("a blank team name must be rejected")
	} else {
		assertClaimValidationField(t, err, ClaimFieldTeamName)
	}
}

func TestClaimFantasySeatRequiresKnownMotif(t *testing.T) {
	service := newTestService(t, false)
	admitSeatlessForClaim(t, service, "a@example.com")
	if _, err := service.claimFantasySeat("a@example.com", "A", "A Team", "not-a-motif"); !errors.Is(err, ErrBadgeUnknownMotif) {
		t.Fatalf("err = %v, want ErrBadgeUnknownMotif", err)
	} else {
		assertClaimValidationField(t, err, ClaimFieldMotif)
	}
}

// ---------------------------------------------------------------------
// Build item 3 — dashboard dual-status cards
// ---------------------------------------------------------------------

// TestFantasyCardBranches drives DashboardData's fantasy_card through its
// three branches (seated / unseated-open / full) without forging auth: an
// unauthenticated request against a non-demo service reads has_seat ==
// false through Viewer's own signed_in==false branch (has_seat ==
// s.demoMode), which is exactly the seatless shape this card renders for.
func TestFantasyCardBranches(t *testing.T) {
	t.Run("unseated with open seats", func(t *testing.T) {
		service := newTestService(t, false)
		request, _ := http.NewRequest(http.MethodGet, "/", nil)
		data := service.DashboardData(context.Background(), request)
		card, ok := data["fantasy_card"].(map[string]any)
		if !ok {
			t.Fatalf("fantasy_card missing or wrong shape: %+v", data["fantasy_card"])
		}
		if card["has_seat"] != false || card["league_full"] != false {
			t.Fatalf("fantasy_card = %+v, want unseated+open", card)
		}
		if card["open_seats"] != len(defaultTeams()) {
			t.Fatalf("open_seats = %v, want %d", card["open_seats"], len(defaultTeams()))
		}
	})

	t.Run("full league", func(t *testing.T) {
		service := newTestService(t, false)
		for _, team := range defaultTeams() {
			if _, _, err := service.store.AssignMember(team.ID+"@example.com", team.ID); err != nil {
				t.Fatalf("seed seat for %s: %v", team.ID, err)
			}
		}
		request, _ := http.NewRequest(http.MethodGet, "/", nil)
		data := service.DashboardData(context.Background(), request)
		card := data["fantasy_card"].(map[string]any)
		if card["league_full"] != true || card["open_seats"] != 0 {
			t.Fatalf("fantasy_card = %+v, want full", card)
		}
	})

	t.Run("seated (demo mode)", func(t *testing.T) {
		service := newTestService(t, true)
		request, _ := http.NewRequest(http.MethodGet, "/", nil)
		data := service.DashboardData(context.Background(), request)
		card := data["fantasy_card"].(map[string]any)
		if card["has_seat"] != true {
			t.Fatalf("fantasy_card = %+v, want has_seat true", card)
		}
		if _, ok := card["team"].(map[string]any); !ok {
			t.Fatalf("seated fantasy_card missing team: %+v", card)
		}
	})
}

// ---------------------------------------------------------------------
// Build item 4 — co-manager full lifecycle
// ---------------------------------------------------------------------

// TestCoManagerFullLifecycle drives invite → first sign-in binds → both
// act → one-co limit → detach → release clears both, at the store/service
// layer (co-manager binding takes email/name directly, never an
// *http.Request, so this needs no forged auth).
func TestCoManagerFullLifecycle(t *testing.T) {
	service := newTestService(t, false)
	primary, _, err := service.store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	teamID := primary.TeamID

	// Invite.
	if err := service.store.InviteCoManager(teamID, "co@example.com"); err != nil {
		t.Fatal(err)
	}
	if !service.store.Invited("co@example.com") {
		t.Fatal("InviteCoManager must add the invite-list entry")
	}

	// First sign-in binds.
	member, bound, err := service.BindCoManagerOnSignIn("co@example.com", "Co")
	if err != nil {
		t.Fatal(err)
	}
	if !bound || member.TeamID != teamID || member.Role != "co" {
		t.Fatalf("co bind = %+v, bound=%v; want TeamID=%s Role=co bound=true", member, bound, teamID)
	}

	// Both act: teamMembers surfaces both, memberForTeam still resolves
	// the primary deterministically.
	both := teamMembers(service.store.Snapshot().Members, teamID)
	if len(both) != 2 || both[0].Role != "" || both[1].Role != "co" {
		t.Fatalf("teamMembers = %+v, want [primary, co]", both)
	}
	if resolved := memberForTeam(service.store.Snapshot().Members, teamID); resolved.Email != "primary@example.com" {
		t.Fatalf("memberForTeam = %+v, want the primary", resolved)
	}

	// One-co limit, exact message.
	err = service.store.InviteCoManager(teamID, "second-co@example.com")
	if err == nil || err.Error() != "this seat already has a co-manager; detach them first" {
		t.Fatalf("second invite err = %v, want the one-co-limit message", err)
	}

	// Detach.
	if err := service.store.DetachCoManager(teamID); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.store.MemberByEmail("co@example.com"); ok {
		t.Fatal("detach must remove the co Member")
	}
	if members := teamMembers(service.store.Snapshot().Members, teamID); len(members) != 1 {
		t.Fatalf("teamMembers after detach = %+v, want just the primary", members)
	}

	// Re-invite and bind again, then release the seat: both must clear.
	if err := service.store.InviteCoManager(teamID, "co2@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := service.BindCoManagerOnSignIn("co2@example.com", "Co2"); err != nil || !bound {
		t.Fatalf("second co bind: bound=%v err=%v", bound, err)
	}
	if err := service.store.ReleaseSeat(teamID); err != nil {
		t.Fatal(err)
	}
	state := service.store.Snapshot()
	if _, ok := state.Members["primary@example.com"]; ok {
		t.Fatal("seat release must clear the primary")
	}
	if _, ok := state.Members["co2@example.com"]; ok {
		t.Fatal("seat release must clear the co-manager too")
	}
}

// TestDetachCoManagerByCommissionerInterventionRecordsEvent checks the
// wave-2 commissioner-console audit trail: DetachCoManager is shared by
// both the seat's own primary manager and the commissioner
// (canManageCoManager), so only a genuine commissioner intervention — the
// commissioner acting on a seat that is not their own — is a
// commissioner-gated mutation worth a durable row.
func TestDetachCoManagerByCommissionerInterventionRecordsEvent(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("COMMISSIONER_EMAILS", "boss-detach@example.com")
	primary, err := service.AssignManager("primary-detach@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	teamID := primary.TeamID
	primaryRequest := commissionerRequest("primary-detach@example.com", "Primary")
	if err := service.InviteCoManager(primaryRequest, teamID, "co-detach@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.BindCoManagerOnSignIn("co-detach@example.com", "Co"); err != nil {
		t.Fatal(err)
	}

	commissioner := commissionerRequest("boss-detach@example.com", "Boss")
	if err := service.DetachCoManager(commissioner, teamID); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "seat.co_detach" || events[0].Refs.TeamID != teamID {
		t.Fatalf("commissioner events = %+v, want one seat.co_detach row for %s", events, teamID)
	}
}

// TestDetachCoManagerBySelfDoesNotRecordCommissionerEvent checks the other
// half of the same seam: the seat's own primary manager detaching their
// own co-manager through the identical DetachCoManager call must not
// produce a commissioner-console audit row — it is ordinary self-service,
// not a commissioner-gated mutation, even though canManageCoManager's own
// authorization check happens to allow both actors.
func TestDetachCoManagerBySelfDoesNotRecordCommissionerEvent(t *testing.T) {
	service := newTestService(t, false)
	primary, err := service.AssignManager("primary-self-detach@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	teamID := primary.TeamID
	primaryRequest := commissionerRequest("primary-self-detach@example.com", "Primary")
	if err := service.InviteCoManager(primaryRequest, teamID, "co-self-detach@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.BindCoManagerOnSignIn("co-self-detach@example.com", "Co"); err != nil {
		t.Fatal(err)
	}

	if err := service.DetachCoManager(primaryRequest, teamID); err != nil {
		t.Fatal(err)
	}
	if got := service.store.Snapshot().CommissionerEvents; len(got) != 0 {
		t.Fatalf("commissioner events = %+v, want none for a self-service co-detach", got)
	}
}

// TestInviteCoManagerRejectsAlreadySeatedEmail checks that a person who
// already operates a seat (primary or co, any team) cannot also be
// invited as a co-manager elsewhere.
func TestInviteCoManagerRejectsAlreadySeatedEmail(t *testing.T) {
	service := newTestService(t, false)
	if _, _, err := service.store.AssignMember("team1@example.com", "T1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("team2@example.com", "T2"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager("team-2", "team1@example.com"); err == nil {
		t.Fatal("inviting an already-seated email must be rejected")
	}
}

// TestCoManagerNotificationsReachBoth checks the recipient-resolution fix
// (registration wave, build item 4): a team-scoped notification (N15,
// trade offer) reaches both the primary and the bound co-manager, each
// under their own idempotency key.
func TestCoManagerNotificationsReachBoth(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	if err := svc.store.InviteCoManager("team-2", "co-team2@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := svc.BindCoManagerOnSignIn("co-team2@example.com", "Co"); err != nil || !bound {
		t.Fatalf("co bind: bound=%v err=%v", bound, err)
	}

	offerID := proposeFixtureOffer(t, svc)
	state := svc.store.Snapshot()
	offer := state.TradeOffers[0]
	svc.notifyTradeOffer(offer)

	sent := svc.store.Snapshot().SentLog
	if sent[keyTradeOffer(offerID, "team-2@example.com")].IsZero() {
		t.Fatal("N15 must still reach the primary")
	}
	if sent[keyTradeOffer(offerID, "co-team2@example.com")].IsZero() {
		t.Fatal("N15 must also reach the bound co-manager")
	}
}

// ---------------------------------------------------------------------
// Build item 5 — domain gate
// ---------------------------------------------------------------------

// TestEmailAllowedDomainGate checks that a configured membership domain
// admits any matching email without an invite-list entry, while a
// non-matching email still needs one (the invite list works alongside
// the domain gate, not instead of it).
func TestEmailAllowedDomainGate(t *testing.T) {
	service := newTestService(t, false)
	service.cfg.Membership.AllowedDomain = "example.net"
	// A non-empty invite list keeps EmailAllowed off its "both lists
	// empty means wide open" fallback, so the domain gate is the thing
	// actually under test here.
	if err := service.store.AddInvite("someone-else@example.com"); err != nil {
		t.Fatal(err)
	}
	if !service.EmailAllowed("new-hire@example.net") {
		t.Fatal("a matching domain must be admitted without an invite")
	}
	if service.EmailAllowed("outsider@example.com") {
		t.Fatal("a non-matching, non-invited email must still be rejected")
	}
	if !service.EmailAllowed("someone-else@example.com") {
		t.Fatal("the invite list must still work alongside the domain gate")
	}
}

// TestValidateMembershipDomain pins the config-validation rules (build
// item 5): empty is valid (no gate), an "@" address is rejected, and a
// malformed domain is rejected. A valid bare domain — the flagship
// omits this block; a config like a "SK" deployment's would set
// example.com — passes.
func TestValidateMembershipDomain(t *testing.T) {
	if err := validateMembership(MembershipBlock{}); err != nil {
		t.Fatalf("empty allowed_domain must validate: %v", err)
	}
	if err := validateMembership(MembershipBlock{AllowedDomain: "example.com"}); err != nil {
		t.Fatalf("a valid bare domain must validate: %v", err)
	}
	if err := validateMembership(MembershipBlock{AllowedDomain: "someone@example.com"}); err == nil {
		t.Fatal("an email address must be rejected")
	}
	if err := validateMembership(MembershipBlock{AllowedDomain: "not a domain"}); err == nil {
		t.Fatal("a malformed domain must be rejected")
	}
}

// ---------------------------------------------------------------------
// Build item 6 — seatless /team fix
// ---------------------------------------------------------------------

// TestTeamDataSeatlessHonestState checks that a seatless viewer of /team
// gets the honest no-franchise state, not team-1's roster (the flagged
// paper cut). An unauthenticated request against a non-demo service
// reaches Viewer's has_seat == false branch with no forged auth needed.
func TestTeamDataSeatlessHonestState(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/team", nil)
	data := service.TeamData(request)
	if data["has_seat"] != false {
		t.Fatalf("has_seat = %v, want false", data["has_seat"])
	}
	if _, leaked := data["team"]; leaked {
		t.Fatalf("a seatless /team view must not carry a team roster: %+v", data)
	}
	card, ok := data["fantasy_card"].(map[string]any)
	if !ok {
		t.Fatalf("seatless /team view must carry the signup CTA data: %+v", data)
	}
	if card["has_seat"] != false {
		t.Fatalf("fantasy_card.has_seat = %v, want false", card["has_seat"])
	}
}

func assertClaimValidationField(t *testing.T, err error, want ClaimField) {
	t.Helper()
	var validation *ClaimValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("err = %T %v, want *ClaimValidationError", err, err)
	}
	if validation.Field != want {
		t.Fatalf("claim validation field = %q, want %q", validation.Field, want)
	}
}

func TestClaimFantasySeatValidationAttributionCoversBadgeStates(t *testing.T) {
	tests := []struct {
		name         string
		motif        string
		seedTaken    bool
		identityLock bool
		wantIs       error
	}{
		{name: "empty", motif: ""},
		{name: "unknown", motif: "not-a-motif", wantIs: ErrBadgeUnknownMotif},
		{name: "taken", motif: "wolf", seedTaken: true, wantIs: ErrBadgeTaken},
		{name: "identity lock", motif: "wolf", identityLock: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := newTestService(t, false)
			admitSeatlessForClaim(t, service, "claimant@example.com")
			if tc.seedTaken {
				if err := service.store.ClaimBadge("team-1", tc.motif); err != nil {
					t.Fatal(err)
				}
			}
			if tc.identityLock {
				service.store.mu.Lock()
				service.store.identityUnhealthy = true
				service.store.mu.Unlock()
			}
			_, err := service.claimFantasySeat("claimant@example.com", "A", "Claimant Team", tc.motif)
			if err == nil {
				t.Fatal("claim should fail")
			}
			if tc.identityLock {
				if err.Error() != identityUnavailableCopy {
					t.Fatalf("identity lock error = %v, want %q", err, identityUnavailableCopy)
				}
				return
			}
			assertClaimValidationField(t, err, ClaimFieldMotif)
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("err = %v, want errors.Is(..., %v)", err, tc.wantIs)
			}
		})
	}
}

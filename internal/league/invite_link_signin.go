// Tier 0 invite-link consume, the runtime (CONFIGURED-state) half of
// invite_store.go (setup-wizard design section 6.2). Minting happens in the
// wizard (direct Store access, before any Service exists) and, in a later
// slice, from /admin; this file is the read/consume surface every anonymous
// /auth/invite/{token} visitor reaches through Service.
package league

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// LookupInviteLinkByToken performs the read-only lookup GET /auth/invite/
// {token} needs to render its typed confirmation page before anything is
// consumed (design section 6.2). No commissioner check: an anonymous
// visitor presenting the raw token is exactly who this route is for.
func (s *Service) LookupInviteLinkByToken(token string) (InviteLink, bool, error) {
	return s.store.LookupInviteLinkByToken(token)
}

// ConsumeInviteLink atomically claims token, exactly once (Store.
// ConsumeInviteLink's single conditional UPDATE). The caller then runs the
// shared completeSignIn chain (main.go) with the link's target email.
func (s *Service) ConsumeInviteLink(token, consumedEmail string, now time.Time) (bool, error) {
	return s.store.ConsumeInviteLink(token, consumedEmail, now)
}

// MintInviteLinkForTest mints a Tier 0 invite link (with admission — see
// Store.MintInviteLinkWithAdmission) directly against the service's store,
// bypassing the commissioner-authorized /admin mint action (a later slice)
// that does not exist yet. It exists so a caller outside this package can
// prove the invite-link consume path (main.go's /auth/invite/{token})
// admits and binds identically to the Google callback path, without
// duplicating Store internals in a cross-package test. Harness/test only:
// refused when APP_ENV=production, matching SetClockForTest/
// SeedStarterForTest's existing "ForTest" guard.
func (s *Service) MintInviteLinkForTest(email string) (token string, err error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return "", fmt.Errorf("MintInviteLinkForTest is refused in production")
	}
	token, _, err = s.store.MintInviteLinkWithAdmission(email, "test", 0, time.Now())
	return token, err
}

// MemberByEmailForTest looks up a member by canonical email. Test-only:
// exists so a cross-package test can observe a sign-in's resulting
// membership (team, role) without reaching into Store internals.
func (s *Service) MemberByEmailForTest(email string) (Member, bool) {
	return s.store.MemberByEmail(s.identityResolver.Resolve(email))
}

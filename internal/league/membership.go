package league

import "strings"

// MembershipPostureMode is the effective initial-admission mode exposed by
// public copy. It intentionally has no invitation addresses or member data:
// callers can safely pass the value to a public page without leaking policy
// PII.
type MembershipPostureMode string

const (
	MembershipPostureOpenAfterSignIn MembershipPostureMode = "open_after_sign_in"
	MembershipPostureDomainOrInvite  MembershipPostureMode = "domain_or_invite"
	MembershipPostureInviteOnly      MembershipPostureMode = "invite_only"
)

// MembershipPosture is the typed, PII-free effective membership contract.
// Domain is retained for the private admission resolver. Public callers must
// use Label and Detail, which deliberately never disclose it. Invitation
// addresses never cross this boundary; only their source presence is exposed.
type MembershipPosture struct {
	Mode                MembershipPostureMode
	Domain              string
	HasInvitationSource bool
}

// ResolveMembershipPosture derives the public admission posture from the
// configured domain and whether either explicit invitation source (the
// environment allowlist or stored invitations) has at least one entry.
// A configured domain always closes the open-setup fallback, even when its
// invitation source is empty.
func ResolveMembershipPosture(domain string, hasInvitationSource bool) MembershipPosture {
	domain = normalizeMembershipDomain(domain)
	mode := MembershipPostureInviteOnly
	if domain != "" {
		mode = MembershipPostureDomainOrInvite
	} else if !hasInvitationSource {
		mode = MembershipPostureOpenAfterSignIn
	}
	return MembershipPosture{
		Mode:                mode,
		Domain:              domain,
		HasInvitationSource: hasInvitationSource,
	}
}

// Label is the short public posture label shared by PublicEntry, Guide, and
// Rules + Scoring. It never includes an invitation address.
func (p MembershipPosture) Label() string {
	switch p.Mode {
	case MembershipPostureDomainOrInvite:
		return "DOMAIN OR INVITE"
	case MembershipPostureInviteOnly:
		return "INVITE-ONLY"
	default:
		return "OPEN AFTER SIGN-IN"
	}
}

// Detail is the public explanation paired with Label. It deliberately names no invited email addresses, domains, or counts.
func (p MembershipPosture) Detail() string {
	switch p.Mode {
	case MembershipPostureDomainOrInvite:
		return "Accounts at the configured work domain or with an explicit invitation may enter."
	case MembershipPostureInviteOnly:
		return "Only accounts with an explicit invitation may enter. Ask the commissioner for access."
	default:
		return "Any authenticated Google account may enter while league setup is open."
	}
}

// IsOpenAfterSignIn reports whether the no-domain/no-invitation setup fallback
// is active.
func (p MembershipPosture) IsOpenAfterSignIn() bool {
	return p.Mode == MembershipPostureOpenAfterSignIn
}

// IsDomainOrInvite reports whether the configured domain and explicit invite
// sources are additive admission paths.
func (p MembershipPosture) IsDomainOrInvite() bool {
	return p.Mode == MembershipPostureDomainOrInvite
}

// IsInviteOnly reports whether an explicit invitation source is required and
// no configured domain path exists.
func (p MembershipPosture) IsInviteOnly() bool {
	return p.Mode == MembershipPostureInviteOnly
}

func normalizeMembershipDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

func membershipDomainMatches(email, domain string) bool {
	if strings.Count(email, "@") != 1 {
		return false
	}
	at := strings.IndexByte(email, '@')
	return at > 0 && email[at+1:] == domain
}

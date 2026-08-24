package league

import (
	"net/http"
	"strings"
	"testing"
)

func TestMembershipPostureDecisionTable(t *testing.T) {
	const domain = "example.com"
	const invite = "guest@example.com"
	tests := []struct {
		name          string
		configured    string
		invitations   bool
		mode          MembershipPostureMode
		label         string
		mustNotReveal []string
	}{
		{
			name:          "no domain and no invitation source is open",
			mode:          MembershipPostureOpenAfterSignIn,
			label:         "OPEN AFTER SIGN-IN",
			mustNotReveal: []string{domain, invite},
		},
		{
			name:          "no domain with invitations is invite only",
			invitations:   true,
			mode:          MembershipPostureInviteOnly,
			label:         "INVITE-ONLY",
			mustNotReveal: []string{domain, invite},
		},
		{
			name:          "domain with no invitations is still gated",
			configured:    domain,
			mode:          MembershipPostureDomainOrInvite,
			label:         "DOMAIN OR INVITE",
			mustNotReveal: []string{domain, invite},
		},
		{
			name:          "domain and invitations are additive",
			configured:    domain,
			invitations:   true,
			mode:          MembershipPostureDomainOrInvite,
			label:         "DOMAIN OR INVITE",
			mustNotReveal: []string{domain, invite},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			posture := ResolveMembershipPosture(tt.configured, tt.invitations)
			if posture.Mode != tt.mode {
				t.Fatalf("mode = %q, want %q", posture.Mode, tt.mode)
			}
			if posture.Label() != tt.label {
				t.Fatalf("label = %q, want %q", posture.Label(), tt.label)
			}
			publicCopy := posture.Label() + " " + posture.Detail()
			for _, secret := range tt.mustNotReveal {
				if strings.Contains(publicCopy, secret) || strings.Contains(publicCopy, "@") {
					t.Fatalf("public posture copy disclosed %q: %q", secret, publicCopy)
				}
			}
		})
	}
}

func TestEmailAllowedMembershipDecisionTable(t *testing.T) {
	tests := []struct {
		name         string
		domain       string
		envInvite    string
		storedInvite string
		allowed      string
		wantMode     MembershipPostureMode
		alsoAllowed  []string
		rejected     []string
	}{
		{
			name:     "open fallback",
			allowed:  "anyone@example.com",
			wantMode: MembershipPostureOpenAfterSignIn,
			rejected: nil,
		},
		{
			name:     "domain with zero invites rejects outsider",
			domain:   "example.net",
			allowed:  "colleague@example.net",
			wantMode: MembershipPostureDomainOrInvite,
			rejected: []string{"outsider@example.com"},
		},
		{
			name:        "domain or environment invitation",
			domain:      "example.net",
			envInvite:   "guest@example.com",
			allowed:     "colleague@example.net",
			alsoAllowed: []string{"guest@example.com"},
			rejected:    []string{"outsider@example.com"},
			wantMode:    MembershipPostureDomainOrInvite,
		},
		{
			name:         "domain or stored invitation",
			domain:       "example.net",
			storedInvite: "stored@example.com",
			allowed:      "colleague@example.net",
			alsoAllowed:  []string{"stored@example.com"},
			rejected:     []string{"outsider@example.com"},
			wantMode:     MembershipPostureDomainOrInvite,
		},
		{
			name:      "invite only",
			envInvite: "guest@example.com",
			allowed:   "guest@example.com",
			wantMode:  MembershipPostureInviteOnly,
			rejected:  []string{"outsider@example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, false)
			service.cfg.Membership.AllowedDomain = tt.domain
			t.Setenv("LEAGUE_ALLOWED_EMAILS", tt.envInvite)
			if tt.storedInvite != "" {
				if err := service.store.AddInvite(tt.storedInvite); err != nil {
					t.Fatal(err)
				}
			}
			posture := service.MembershipPosture()
			if posture.Mode != tt.wantMode {
				t.Fatalf("posture mode = %q, want %q", posture.Mode, tt.wantMode)
			}
			if !service.EmailAllowed(tt.allowed) {
				t.Fatalf("%s should be admitted", tt.allowed)
			}
			for _, email := range tt.alsoAllowed {
				if !service.EmailAllowed(email) {
					t.Errorf("%s should be admitted", email)
				}
			}
			for _, email := range tt.rejected {
				if service.EmailAllowed(email) {
					t.Errorf("%s should be rejected", email)
				}
			}
		})
	}
}

func TestMembershipPosturePreservesPersistedMembers(t *testing.T) {
	service := newTestService(t, false)
	service.cfg.Membership.AllowedDomain = "example.net"
	const returning = "returning@example.com"
	if _, err := service.EnsureMember(returning, "Returning Manager"); err != nil {
		t.Fatal(err)
	}
	if service.EmailAllowed(returning) != true {
		t.Fatal("persisted member should remain admitted after policy closes")
	}
}

func TestCommissionerAliasAdmissionPrecedesRawPolicy(t *testing.T) {
	service := newTestService(t, false)
	resolver := testIdentityResolver(t)
	service.identityResolver = resolver
	service.store.identityResolver = resolver
	service.cfg.Membership.AllowedDomain = "example.net"
	t.Setenv("COMMISSIONER_EMAILS", identityCanonicalEmail)
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "")

	for _, email := range []string{identityCanonicalEmail, identityAliasEmail} {
		if !service.EmailAllowed(email) {
			t.Errorf("commissioner identity %q should be admitted across configured domain", email)
		}
	}
	for _, email := range []string{"other@example.com", "outsider@example.com"} {
		if service.EmailAllowed(email) {
			t.Errorf("unrelated identity %q received commissioner bypass", email)
		}
	}
}

func TestMembershipPosturePublicProjectionIsPIIFree(t *testing.T) {
	service := newTestService(t, false)
	service.cfg.Membership.AllowedDomain = "example.net"
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "guest@example.com")
	if err := service.store.AddInvite("stored@example.com"); err != nil {
		t.Fatal(err)
	}

	posture := service.MembershipPosture()
	publicCopy := posture.Label() + " " + posture.Detail()
	for _, secret := range []string{"example.net", "guest@example.com", "stored@example.com"} {
		if strings.Contains(publicCopy, secret) || strings.Contains(publicCopy, "@") {
			t.Fatalf("posture copy disclosed %q: %q", secret, publicCopy)
		}
	}

	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	entry := service.PublicEntryView(request)
	if entry.MembershipLabel != posture.Label() || entry.MembershipDetail != posture.Detail() {
		t.Fatalf("public entry posture = %#v, want %#v", entry, posture)
	}
	if strings.Contains(entry.Detail, posture.Detail()) {
		t.Fatal("generic entry detail should not duplicate dedicated membership detail")
	}
	rules := service.rulesMembershipMap(service.store.Snapshot())
	for key, value := range rules {
		if text, ok := value.(string); ok {
			for _, secret := range []string{"example.net", "guest@example.com", "stored@example.com"} {
				if strings.Contains(text, secret) || strings.Contains(text, "@") {
					t.Fatalf("rules[%q] disclosed %q: %q", key, secret, text)
				}
			}
		}
	}
	if _, leaked := rules["domain"]; leaked {
		t.Fatal("rules projection should not expose configured domain")
	}
}

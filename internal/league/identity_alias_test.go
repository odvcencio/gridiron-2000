package league

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/identity"
	"m31labs.dev/gosx/auth"
)

const (
	identityAliasEmail     = "commissioner@example.com"
	identityCanonicalEmail = "commissioner.alias@example.org"
)

func testIdentityResolver(t *testing.T) identity.Resolver {
	t.Helper()
	resolver, err := identity.New(map[string]string{
		identityAliasEmail: identityCanonicalEmail,
	})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func TestIdentityAliasStoreCanonicalizesOwnershipAndPreservesAdmission(t *testing.T) {
	resolver := testIdentityResolver(t)
	statePath := filepath.Join(t.TempDir(), "league-state.json")
	older := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	newer := older.Add(time.Minute)
	legacy := PersistedState{
		SchemaVersion: currentSchemaVersion,
		Members: map[string]Member{
			identityAliasEmail: {
				TeamID: "team-1",
				Name:   "Alias Name",
				Email:  identityAliasEmail,
			},
			identityCanonicalEmail: {
				TeamID: "team-1",
				Name:   "Canonical Name",
				Email:  identityCanonicalEmail,
			},
		},
		Invites: []string{identityAliasEmail},
		Boards: map[string][]string{
			identityAliasEmail:     {"player-a"},
			identityCanonicalEmail: {"player-b", "player-a"},
		},
		Pickems: map[string]map[string]string{
			identityAliasEmail: {"game-1": "BUF"},
		},
		BlitzEntries: map[string]map[string]BlitzEntry{
			identityAliasEmail: {
				"pre2": {Players: []string{"player-a"}, UpdatedAt: older},
			},
			identityCanonicalEmail: {
				"pre2": {Players: []string{"player-a"}, UpdatedAt: newer},
			},
		},
		NotifyPrefs: map[string]map[string]bool{
			identityAliasEmail:     {categoryDraftLive: false},
			identityCanonicalEmail: {categoryDraftLive: false},
		},
		Announcements: []Announcement{{
			ID: "announcement-1", Body: "hello", PostedAt: older, PostedBy: identityAliasEmail,
		}},
		SentLog: map[string]time.Time{
			"broadcast:announcement-1:" + identityAliasEmail:     older,
			"broadcast:announcement-1:" + identityCanonicalEmail: newer,
		},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o640); err != nil {
		t.Fatal(err)
	}

	store := NewStoreWithIdentity(statePath, resolver)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("identity import failed: %v", err)
	}
	got := store.Snapshot()
	if _, ok := got.Members[identityAliasEmail]; ok {
		t.Fatal("alias member key survived migration")
	}
	member, ok := got.Members[identityCanonicalEmail]
	if !ok {
		t.Fatal("canonical member key missing")
	}
	if member.Name != "Canonical Name" || member.Email != identityCanonicalEmail {
		t.Fatalf("canonical member = %+v, want canonical record", member)
	}
	if board := got.Boards[identityCanonicalEmail]; len(board) != 2 ||
		board[0] != "player-b" || board[1] != "player-a" {
		t.Fatalf("merged board = %#v", board)
	}
	if got.Pickems[identityCanonicalEmail]["game-1"] != "BUF" {
		t.Fatalf("pick'ems = %#v", got.Pickems)
	}
	if got.BlitzEntries[identityCanonicalEmail]["pre2"].UpdatedAt != newer {
		t.Fatalf("Blitz entry = %#v, want latest duplicate", got.BlitzEntries)
	}
	pref, prefOK := got.NotifyPrefs[identityCanonicalEmail][categoryDraftLive]
	if !prefOK || pref {
		t.Fatal("notification preference changed during merge")
	}
	if got.Announcements[0].PostedBy != identityCanonicalEmail {
		t.Fatalf("announcement provenance = %q", got.Announcements[0].PostedBy)
	}
	if _, ok := got.SentLog["broadcast:announcement-1:"+identityAliasEmail]; ok {
		t.Fatal("alias idempotency key survived migration")
	}
	if got.SentLog["broadcast:announcement-1:"+identityCanonicalEmail] != older {
		t.Fatalf("sent log = %#v, want earliest collision timestamp", got.SentLog)
	}
	if len(got.Invites) != 1 || got.Invites[0] != identityAliasEmail {
		t.Fatalf("admission invites = %#v, want raw alias preserved", got.Invites)
	}
	if member, ok := store.MemberByEmail(identityAliasEmail); !ok || member.Email != identityCanonicalEmail {
		t.Fatalf("alias lookup = %+v, %v; want canonical member", member, ok)
	}

	restarted := NewStoreWithIdentity(statePath, resolver)
	t.Cleanup(func() { _ = restarted.Close() })
	if err := restarted.StartupError(); err != nil {
		t.Fatalf("restart after identity migration failed: %v", err)
	}
	if _, ok := restarted.Snapshot().Members[identityAliasEmail]; ok {
		t.Fatal("alias member key returned after restart")
	}
}

func TestIdentityAliasMigrationRejectsCrossTeamMemberCoInviteConflict(t *testing.T) {
	resolver := testIdentityResolver(t)
	statePath := filepath.Join(t.TempDir(), "league-state.json")
	raw, err := json.Marshal(PersistedState{
		SchemaVersion: currentSchemaVersion,
		Members: map[string]Member{
			identityCanonicalEmail: {TeamID: "team-1", Email: identityCanonicalEmail},
		},
		CoInvites: map[string]string{identityAliasEmail: "team-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	store := NewStoreWithIdentity(statePath, resolver)
	t.Cleanup(func() { _ = store.Close() })
	err = store.StartupError()
	if err == nil || !strings.Contains(err.Error(), "already owns team") {
		t.Fatalf("StartupError = %v, want fail-closed cross-team member/co-invite collision", err)
	}
}

func TestBindCoManagerRefusesCrossTeamIdentityOverwrite(t *testing.T) {
	resolver := testIdentityResolver(t)
	store := NewStoreWithIdentity("", resolver)
	t.Cleanup(func() { _ = store.Close() })
	store.state.Members[identityCanonicalEmail] = Member{
		TeamID: "team-1", Name: "Primary", Email: identityCanonicalEmail,
	}
	store.state.CoInvites[identityCanonicalEmail] = "team-2"

	got, bound, err := store.BindCoManager(identityAliasEmail, "Alias")
	if err == nil || bound {
		t.Fatalf("BindCoManager = %+v, bound=%v, err=%v; want a rejected cross-team overwrite", got, bound, err)
	}
	member, ok := store.MemberByEmail(identityCanonicalEmail)
	if !ok || member.TeamID != "team-1" || member.Role != "" {
		t.Fatalf("existing primary = %+v, %v; bind must not strip its team or role", member, ok)
	}
	if got := store.Snapshot().CoInvites[identityCanonicalEmail]; got != "team-2" {
		t.Fatalf("pending invite = %q, want team-2 preserved after rejected bind", got)
	}
}

func TestBindCoManagerSameTeamExistingCoIsIdempotent(t *testing.T) {
	resolver := testIdentityResolver(t)
	store := NewStoreWithIdentity("", resolver)
	t.Cleanup(func() { _ = store.Close() })
	existing := Member{TeamID: "team-1", Name: "Co", Email: identityCanonicalEmail, Role: "co"}
	store.state.Members[identityCanonicalEmail] = existing
	store.state.CoInvites[identityCanonicalEmail] = "team-1"

	got, bound, err := store.BindCoManager(identityAliasEmail, "New Name")
	if err != nil || !bound || got != existing {
		t.Fatalf("BindCoManager = %+v, bound=%v, err=%v; want existing co-manager idempotently", got, bound, err)
	}
	if _, pending := store.Snapshot().CoInvites[identityCanonicalEmail]; pending {
		t.Fatal("idempotent co-manager bind must consume the stale pending invite")
	}
}

func TestIdentityAliasMigrationFailsClosedOnSeatConflict(t *testing.T) {
	resolver := testIdentityResolver(t)
	statePath := filepath.Join(t.TempDir(), "league-state.json")
	raw, err := json.Marshal(PersistedState{
		SchemaVersion: currentSchemaVersion,
		Members: map[string]Member{
			identityAliasEmail:     {TeamID: "team-1", Email: identityAliasEmail},
			identityCanonicalEmail: {TeamID: "team-2", Email: identityCanonicalEmail},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	store := NewStoreWithIdentity(statePath, resolver)
	t.Cleanup(func() { _ = store.Close() })
	err = store.StartupError()
	if err == nil || !strings.Contains(err.Error(), "conflicting team seats") {
		t.Fatalf("StartupError = %v, want fail-closed seat conflict", err)
	}
}

func TestIdentityAliasStoreActionsUseCanonicalKeys(t *testing.T) {
	resolver := testIdentityResolver(t)
	store := NewStoreWithIdentity("", resolver)
	t.Cleanup(func() { _ = store.Close() })

	if _, _, err := store.EnsureMember(identityAliasEmail, "Gridiron Maintainer"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.MemberByEmail(identityCanonicalEmail); !ok {
		t.Fatal("EnsureMember did not canonicalize member key")
	}
	if err := store.BoardAdd(identityAliasEmail, "player-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPickem(identityAliasEmail, "game-1", "BUF"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNotifyPref(identityAliasEmail, categoryDraftLive, false); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	if _, ok := got.Boards[identityAliasEmail]; ok {
		t.Fatal("board alias key survived write")
	}
	if _, ok := got.Pickems[identityAliasEmail]; ok {
		t.Fatal("pick'em alias key survived write")
	}
	if _, ok := got.NotifyPrefs[identityAliasEmail]; ok {
		t.Fatal("notification alias key survived write")
	}
	if err := store.InviteCoManager("team-1", identityAliasEmail); err != nil {
		t.Fatal(err)
	}
	got = store.Snapshot()
	if got.CoInvites[identityCanonicalEmail] != "team-1" {
		t.Fatalf("co-invites = %#v", got.CoInvites)
	}
	if len(got.Invites) != 1 || got.Invites[0] != identityAliasEmail {
		t.Fatalf("invite policy = %#v, want raw alias", got.Invites)
	}
}

func TestIdentityAliasCommissionerAndAllowlistRemainDistinct(t *testing.T) {
	service := newTestService(t, false)
	resolver := testIdentityResolver(t)
	service.identityResolver = resolver
	t.Setenv("COMMISSIONER_EMAILS", identityCanonicalEmail)
	t.Setenv("LEAGUE_ALLOWED_EMAILS", identityCanonicalEmail)

	if !commissionerForEmail(t, service, identityAliasEmail) {
		t.Fatal("explicit alias should authorize the canonical commissioner")
	}
	if !service.EmailAllowed(identityCanonicalEmail) {
		t.Fatal("canonical allowlist identity should be admitted")
	}
	if service.EmailAllowed(identityAliasEmail) {
		t.Fatal("alias must not bypass the independent raw-email allowlist")
	}
	user := service.CanonicalUser(auth.User{ID: "google-subject", Email: identityAliasEmail})
	if user.Email != identityCanonicalEmail || user.ID != "google-subject" {
		t.Fatalf("canonical user = %+v, provider subject must remain stable", user)
	}
}

func TestIdentityAliasResolverRejectsChainsAndAmbiguity(t *testing.T) {
	if _, err := identity.New(map[string]string{
		"one@example.com": "two@example.com",
		"two@example.com": "three@example.com",
	}); err == nil {
		t.Fatal("alias chains must be rejected")
	}
	if _, err := identity.New(map[string]string{
		"alias@example.com": "one@example.com",
		"ALIAS@example.com": "two@example.com",
	}); err == nil {
		t.Fatal("case-folded conflicting aliases must be rejected")
	}
}

func TestIdentityAliasMigrationErrorIsOperationallyVisible(t *testing.T) {
	resolver := testIdentityResolver(t)
	statePath := filepath.Join(t.TempDir(), "league-state.json")
	raw, err := json.Marshal(PersistedState{
		SchemaVersion: currentSchemaVersion,
		CoInvites: map[string]string{
			identityAliasEmail:     "team-1",
			identityCanonicalEmail: "team-2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	store := NewStoreWithIdentity(statePath, resolver)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err == nil {
		t.Fatalf("StartupError = %v, want a visible co-invite conflict", err)
	}
	if !strings.Contains(store.StartupError().Error(), "conflicting teams") {
		t.Fatalf("StartupError = %v, want conflicting teams", store.StartupError())
	}
}

func TestIdentityAliasMigrationRejectsRolePickAndPreferenceConflicts(t *testing.T) {
	tests := []struct {
		name  string
		state PersistedState
		want  string
	}{
		{
			name: "member role",
			state: PersistedState{Members: map[string]Member{
				identityAliasEmail:     {Email: identityAliasEmail, TeamID: "team-1", Role: "co"},
				identityCanonicalEmail: {Email: identityCanonicalEmail, TeamID: "team-1"},
			}},
			want: "conflicting member roles",
		},
		{
			name: "pick'em",
			state: PersistedState{Pickems: map[string]map[string]string{
				identityAliasEmail:     {"game-1": "BUF"},
				identityCanonicalEmail: {"game-1": "MIA"},
			}},
			want: "conflicting picks",
		},
		{
			name: "notification preference",
			state: PersistedState{NotifyPrefs: map[string]map[string]bool{
				identityAliasEmail:     {categoryDraftLive: false},
				identityCanonicalEmail: {categoryDraftLive: true},
			}},
			want: "notification preference",
		},
	}
	resolver := testIdentityResolver(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizeState(&tt.state)
			_, err := reconcileIdentityState(&tt.state, resolver)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("reconcileIdentityState error = %v, want %q", err, tt.want)
			}
		})
	}
}

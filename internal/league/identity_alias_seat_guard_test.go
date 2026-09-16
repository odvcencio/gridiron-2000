package league

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The two regressions below come from one live incident (2026-09-16).
//
// One person held the league seat under a work address and signed in later
// with a personal address. Nothing connected the two, so the personal
// address behaved as a second person: it collected its own Pick'em entry —
// putting the same name on the leaderboard twice with the season record
// split between the rows — and it passed InviteCoManager's "already holds a
// team seat" guard, because that guard reads the Member record for the
// address it is handed and the personal address had no seat of its own.
//
// Both are the same defect: the seat guard and the Pick'em owner key are
// per-address, while a manager is a person. The identity resolver is the
// mechanism that closes it, so these tests pin the two behaviours a
// configured alias must produce.

func writeIdentitySeatGuardState(t *testing.T, state PersistedState) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "league-state.json")
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestInviteCoManagerRejectsAliasOfASeatedManager is the guard the incident
// asked for in plain words: a person who already manages a team cannot be
// invited to co-manage another one, even when the invite names their other
// email address.
func TestInviteCoManagerRejectsAliasOfASeatedManager(t *testing.T) {
	resolver := testIdentityResolver(t)
	path := writeIdentitySeatGuardState(t, PersistedState{
		SchemaVersion: currentSchemaVersion,
		Members: map[string]Member{
			identityCanonicalEmail: {
				TeamID: "team-1",
				Name:   "Seated Manager",
				Email:  identityCanonicalEmail,
			},
		},
	})

	store := NewStoreWithIdentity(path, resolver)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("store startup: %v", err)
	}

	err := store.InviteCoManager("team-2", identityAliasEmail)
	if err == nil {
		t.Fatal("inviting a seated manager's alias as co-manager was allowed; it must be refused")
	}
	if !strings.Contains(err.Error(), "already holds a team seat") {
		t.Fatalf("refusal = %q, want the seat-holder sentence", err.Error())
	}
	// The refusal must name the person, not the address that was typed:
	// the commissioner needs to see WHICH seat blocks the invite.
	if !strings.Contains(err.Error(), identityCanonicalEmail) {
		t.Fatalf("refusal = %q, want it to name the canonical seat holder %q", err.Error(), identityCanonicalEmail)
	}
	if pending, ok := store.Snapshot().CoInvites[identityCanonicalEmail]; ok {
		t.Fatalf("a refused invite still recorded a pending co-invite for team %q", pending)
	}
	if pending, ok := store.Snapshot().CoInvites[identityAliasEmail]; ok {
		t.Fatalf("a refused invite still recorded a pending co-invite under the alias for team %q", pending)
	}
}

// TestInviteCoManagerStillAllowsAnUnseatedAlias proves the guard above
// refuses for the right reason. An alias belonging to somebody with no seat
// at all is an ordinary, legal co-manager invite, and it must land on the
// canonical key so first sign-in from either address consumes it once.
func TestInviteCoManagerStillAllowsAnUnseatedAlias(t *testing.T) {
	resolver := testIdentityResolver(t)
	path := writeIdentitySeatGuardState(t, PersistedState{
		SchemaVersion: currentSchemaVersion,
		Members:       map[string]Member{},
	})

	store := NewStoreWithIdentity(path, resolver)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("store startup: %v", err)
	}

	if err := store.InviteCoManager("team-2", identityAliasEmail); err != nil {
		t.Fatalf("co-manager invite for an unseated alias = %v, want it allowed", err)
	}
	got := store.Snapshot()
	if got.CoInvites[identityCanonicalEmail] != "team-2" {
		t.Fatalf("pending co-invites = %#v, want the canonical key holding team-2", got.CoInvites)
	}
	// Admission policy stays on the raw address the commissioner typed —
	// an alias must never widen or rewrite the sign-in allowlist.
	if len(got.Invites) != 1 || got.Invites[0] != identityAliasEmail {
		t.Fatalf("admission invites = %#v, want the raw alias preserved", got.Invites)
	}
}

// TestIdentityAliasMergesSplitPickemEntrantIntoOneRow reproduces the live
// shape exactly: a seated canonical member, a seatless alias member, and
// one week of Pick'em picks under each key. pickemLeaderboard ranks one row
// per state.Pickems key, so two keys are two rows with the same name and
// half a record each. After migration there is one entrant holding both
// weeks.
func TestIdentityAliasMergesSplitPickemEntrantIntoOneRow(t *testing.T) {
	resolver := testIdentityResolver(t)
	weekOne := time.Date(2026, 9, 10, 0, 20, 0, 0, time.UTC)
	weekTwo := weekOne.Add(5 * 24 * time.Hour)
	path := writeIdentitySeatGuardState(t, PersistedState{
		SchemaVersion: currentSchemaVersion,
		Members: map[string]Member{
			identityCanonicalEmail: {
				TeamID: "team-1",
				Name:   "Split Entrant",
				Email:  identityCanonicalEmail,
			},
			// The seatless record the second sign-in left behind.
			identityAliasEmail: {
				Name:  "Split Entrant",
				Email: identityAliasEmail,
			},
		},
		Pickems: map[string]map[string]string{
			identityCanonicalEmail: {"2026_01_BUF_HOU": "HOU"},
			identityAliasEmail:     {"2026_02_CIN_HOU": "CIN"},
		},
		PickemEnteredAt: map[string]time.Time{
			identityCanonicalEmail: weekOne,
			identityAliasEmail:     weekTwo,
		},
	})

	store := NewStoreWithIdentity(path, resolver)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("store startup: %v", err)
	}
	got := store.Snapshot()

	if len(got.Pickems) != 1 {
		t.Fatalf("Pick'em entrants = %d (%#v), want exactly one row for one person", len(got.Pickems), got.Pickems)
	}
	picks := got.Pickems[identityCanonicalEmail]
	if picks["2026_01_BUF_HOU"] != "HOU" || picks["2026_02_CIN_HOU"] != "CIN" {
		t.Fatalf("merged picks = %#v, want both weeks under the canonical entrant", picks)
	}
	// The earliest entry instant wins, so the merged entrant keeps credit
	// for every week they were established for, not just the later one.
	if got.PickemEnteredAt[identityCanonicalEmail] != weekOne {
		t.Fatalf("entry instant = %v, want the earliest (%v)", got.PickemEnteredAt[identityCanonicalEmail], weekOne)
	}

	// The seatless duplicate must fold into the seated record rather than
	// compete with it, so the one remaining row still owns the team.
	if len(got.Members) != 1 {
		t.Fatalf("members = %#v, want the seatless duplicate merged away", got.Members)
	}
	member := got.Members[identityCanonicalEmail]
	if member.TeamID != "team-1" {
		t.Fatalf("merged member = %+v, want the team-1 seat preserved", member)
	}
}

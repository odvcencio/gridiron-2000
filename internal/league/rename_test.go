package league

import (
	"net/http/httptest"
	"testing"
)

// TestRenameTeamAuthority pins the self-service rename authority model
// (the canSetAvatar precedent): a demo-mode seat renames its own team;
// a request with no identity in non-demo mode is refused with the exact
// message; the store's 40-character cap applies on the manager path.
func TestRenameTeamAuthority(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest("POST", "/team", nil)

	team, err := service.RenameTeam(request, "team-1", "The Renamed Unit")
	if err != nil {
		t.Fatalf("demo own-seat rename: %v", err)
	}
	if team.Name != "The Renamed Unit" {
		t.Fatalf("rename did not apply: %q", team.Name)
	}

	locked := newTestService(t, false)
	if _, err := locked.RenameTeam(request, "team-1", "Hijack"); err == nil {
		t.Fatal("expected a no-identity rename to be refused")
	} else if err.Error() != "only the seat's manager or the commissioner can rename this team" {
		t.Fatalf("wrong refusal message: %q", err.Error())
	}

	long := make([]byte, 41)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := service.RenameTeam(request, "team-1", string(long)); err == nil {
		t.Fatal("expected the 40-character cap to apply")
	}
}

// TestRenameTeamRejectsBlankNameInsteadOfSilentlyErasingTheCustomOne is
// item 4's own regression test (2026-08-31 post-wave audit): a
// whitespace-only rename used to silently succeed as an implicit reset —
// "West 4 is set." — erasing a manager's custom name with no error and
// no indication the typed text never took. RenameTeam must now refuse it
// with a plain-language error and leave the existing custom name
// untouched; ResetTeamName is the explicit action that clears it.
func TestRenameTeamRejectsBlankNameInsteadOfSilentlyErasingTheCustomOne(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest("POST", "/team", nil)

	if _, err := service.RenameTeam(request, "team-1", "West 4"); err != nil {
		t.Fatalf("set the custom name: %v", err)
	}

	if _, err := service.RenameTeam(request, "team-1", "   "); err == nil {
		t.Fatal("a whitespace-only rename must be rejected, not treated as a reset")
	} else if err.Error() != "Enter a team name, or use Reset to the configured name" {
		t.Fatalf("wrong refusal message: %q", err.Error())
	}
	if got := service.teamView(service.store.Snapshot(), "team-1").Name; got != "West 4" {
		t.Fatalf("rejected blank rename erased the custom name: got %q, want \"West 4\" preserved", got)
	}

	team, err := service.ResetTeamName(request, "team-1")
	if err != nil {
		t.Fatalf("explicit reset: %v", err)
	}
	if team.Name == "West 4" {
		t.Fatalf("ResetTeamName did not clear the override: %q", team.Name)
	}
}

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

package login

import (
	"os"
	"strings"
	"testing"
)

// TestSignedInConsoleBranchesOnFantasySeat pins the onboarding handoff in
// the source template. The production build verifies the GSX branch syntax;
// this contract keeps a future copy refactor from restoring the old blank
// franchise label and misleading Team CTA for a signed-in seatless invitee.
func TestSignedInConsoleBranchesOnFantasySeat(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<If cond={data.viewer.has_seat}>`,
		`<If cond={data.viewer.has_seat == false}>`,
		`<strong>{data.viewer.team_name}</strong>`,
		`href="/team"`,
		`ACTIVE · NO FRANCHISE`,
		`href="/join"`,
		`Claim a franchise →`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("signed-in login console missing %q", want)
		}
	}
}

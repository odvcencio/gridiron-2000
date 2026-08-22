package team

import (
	"os"
	"strings"
	"testing"
)

func TestManagedTeamFormsCarryCSRFToken(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, action := range []string{"co-invite", "co-detach", "team-rename"} {
		formStart := strings.Index(source, `action={actionPath("`+action+`")}`)
		if formStart < 0 {
			t.Fatalf("managed %s form not found", action)
		}
		formEnd := strings.Index(source[formStart:], "</form>")
		if formEnd < 0 {
			t.Fatalf("managed %s form has no closing form tag", action)
		}
		form := source[formStart : formStart+formEnd]
		if !strings.Contains(form, `name="csrf_token" value={csrf.token}`) {
			t.Errorf("managed %s form has no csrf.token control", action)
		}
	}
}

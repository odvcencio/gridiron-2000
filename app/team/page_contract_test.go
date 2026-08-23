package team

import (
	"os"
	"path/filepath"
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

func TestTeamHeroAndBadgePickerKeepTheirWideLayoutStructure(t *testing.T) {
	stylesBytes, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, want := range []string{
		"grid-template-columns: minmax(0, 1fr) auto;",
		".badge-option-wrap,\n.badge-option-form {",
		".badge-option {\n  min-width: 0;\n  width: 100%;",
		".team-hero__identity > div {\n  min-width: 0;\n  width: min(100%, 48rem);",
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("team layout stylesheet missing regression contract %q", want)
		}
	}
}

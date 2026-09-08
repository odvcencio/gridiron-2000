package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIdentitySummaryFollowsOpenState pins J5 F26: the franchise identity
// disclosure's own summary action label must follow data.identity_expanded
// instead of a static "OPEN EDITOR" that never changes once the editor is
// open.
func TestIdentitySummaryFollowsOpenState(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<span class="team-identity-settings__summary-action mono">`,
		"<If cond={data.identity_expanded}>CLOSE EDITOR</If>",
		"<If cond={data.identity_expanded == false}>OPEN EDITOR</If>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("team-identity-settings summary missing %q", want)
		}
	}
	if strings.Contains(page, `<span class="team-identity-settings__summary-action mono">OPEN EDITOR</span>`) {
		t.Error("summary action label is still a static \"OPEN EDITOR\" string")
	}
}

// TestBadgeOptionLabelWrapsInsteadOfClamping pins J5 F26's other half: the
// badge grid's per-tile state label must wrap onto two lines rather than
// clamp to a single-line CSS ellipsis, so "AVAILABLE" and "TAKEN" stay
// distinguishable at a glance.
func TestBadgeOptionLabelWrapsInsteadOfClamping(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	start := strings.Index(css, "/* J5 F26: every badge tile's state label")
	if start < 0 {
		t.Fatal("styles.css is missing the J5 F26 badge-option label block")
	}
	block := css[start:]
	ruleStart := strings.Index(block, ".badge-option small {")
	if ruleStart < 0 {
		t.Fatal("badge-option label block missing its .badge-option small rule")
	}
	ruleEnd := strings.Index(block[ruleStart:], "}")
	if ruleEnd < 0 {
		t.Fatal("could not find the end of the .badge-option small override")
	}
	rule := block[ruleStart : ruleStart+ruleEnd]
	for _, want := range []string{"white-space: normal;", "overflow-wrap: break-word;"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".badge-option small override missing %q: %s", want, rule)
		}
	}
}

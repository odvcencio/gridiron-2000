package team

import (
	"os"
	"strings"
	"testing"
)

// TestUnknownLineupTargetRendersATeamNotFoundStateInsteadOfTheOwnTeamFallback
// pins J4 F10 (coordinator hand-off from hemlock): when a commissioner's
// ?team= request cannot be resolved, page.gsx must render a plain "team
// not found" state naming the value it looked for and linking back to
// the console's seat list — never the ordinary lineup grid, which would
// silently show the commissioner's own franchise as though that were
// what was asked for.
func TestUnknownLineupTargetRendersATeamNotFoundStateInsteadOfTheOwnTeamFallback(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		"<If cond={data.has_seat && data.lineup_target_unknown}>",
		`No team matches`,
		"data.lineup_target_unknown_value",
		`Pick a team from the console's seat list.`,
		`href="/admin#admin-seats"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page.gsx missing %q", want)
		}
	}
	// The ordinary seated content (the lineup grid, the notice stack, the
	// team hero) must not render while the target is unknown — a single
	// gate change on the existing has_seat branch, not a duplicated
	// template.
	if !strings.Contains(page, "<If cond={data.has_seat && data.lineup_target_unknown == false}>") {
		t.Error("page.gsx does not gate the ordinary seated content on lineup_target_unknown == false")
	}
}

// TestInterventionHeroNamesTargetManagerNotViewer pins J4 F32: during an
// intervention the hero's eyebrow reads the target team's own manager
// initials (hero_initials, internal/league), and the hero shows that
// manager's name (hero_manager_name) — not the signed-in commissioner's
// own identity, which the eyebrow printed unconditionally before this
// fix.
func TestInterventionHeroNamesTargetManagerNotViewer(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		"{data.hero_initials}",
		"<If cond={data.lineup_intervention}>",
		"{data.hero_manager_name}",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page.gsx missing %q", want)
		}
	}
	if strings.Contains(page, "{data.viewer.initials}") {
		t.Error("hero eyebrow still reads data.viewer.initials directly; it must read data.hero_initials so an intervention names the target manager")
	}
}

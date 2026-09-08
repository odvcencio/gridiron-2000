package players

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlayersOwnerNameIsVisibleNotOnlyInATitleAttribute is larch's title
// audit finding (wave 7b): a title="" attribute is only reachable on
// hover, which a touch device never gives, so the manager's full name
// used to be readable nowhere on a phone. The ROSTERED availability line
// inside each player's expandable stat-tip panel — reachable from every
// row that also shows the compact position-chip--locked badge — now
// prints the full owner name inline. The compact chip badge and the
// dense waiver-order strip stay abbreviation-only (expanding either
// would defeat their glance-at-a-row-of-many-teams purpose), but both
// now also carry aria-label so the name is not title-only there either.
//
// Item 1's root-cause fix (2026-09-02 route-crawl finding — rowan) made
// PlayerPoolRegion() and WaiverDeskRegion() the single source of this
// markup: Page() embeds them directly instead of hand-duplicating their
// content, so each of these markers now appears exactly once in
// page.gsx, not twice.
func TestPlayersOwnerNameIsVisibleNotOnlyInATitleAttribute(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if got := strings.Count(source, "ROSTERED · {player.owner_name} ({player.owner_abbr})"); got != 1 {
		t.Errorf("page.gsx has %d visible-owner-name ROSTERED lines, want 1 (PlayerPoolRegion())", got)
	}
	if got := strings.Count(source, `aria-label={"Rostered by " + player.owner_name}`); got != 1 {
		t.Errorf("page.gsx has %d position-chip--locked aria-labels, want 1 (PlayerPoolRegion())", got)
	}
	if got := strings.Count(source, "aria-label={slot.name}"); got != 1 {
		t.Errorf("page.gsx has %d waiver-order-strip aria-labels, want 1 (WaiverDeskRegion())", got)
	}
}

// TestPlayersOwnerChipNamesTheTeamAtPhoneWidth is J1 F23 (2026-09-04
// audit): asked "who has Jahmyr Gibbs", a phone manager saw only the
// division-style abbreviation ("OR2") on the compact owner chip — the
// full team name sat one tap deeper, reachable only via title/aria-label
// (hover- or screen-reader-only), never in front of a sighted phone
// user's own eyes. The chip now carries both an abbreviation span and a
// name span; public/styles.css swaps which one is visible at phone width
// (the chip already sits on its own full-width grid row there — see
// .pool-row--status's own nth-child rule — so there is room for the
// real name).
func TestPlayersOwnerChipNamesTheTeamAtPhoneWidth(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`class="owner-chip__abbr"`, "{player.owner_abbr}",
		`class="owner-chip__name"`, "{player.owner_name}",
		"owner-chip", "position-chip--locked",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx's owner chip is missing %q", want)
		}
	}

	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	cssSource := string(css)
	for _, want := range []string{".owner-chip__name", ".owner-chip__abbr"} {
		if !strings.Contains(cssSource, want) {
			t.Errorf("public/styles.css is missing a rule for %q", want)
		}
	}
}

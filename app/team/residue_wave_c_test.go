package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchupOrdinalTrimsToughestSuffix pins J5 F26 team-lineup residue's
// own view-model helper: the OPPONENT cell's chip shows the compact
// ordinal alone ("19th"), not the full "19th-toughest" phrase a 7rem
// column has never had room for.
func TestMatchupOrdinalTrimsToughestSuffix(t *testing.T) {
	cases := []struct{ chip, want string }{
		{"19th-toughest", "19th"},
		{"1st-toughest", "1st"},
		{"", ""},
	}
	for _, c := range cases {
		if got := matchupOrdinal(c.chip); got != c.want {
			t.Errorf("matchupOrdinal(%q) = %q, want %q", c.chip, got, c.want)
		}
	}
}

// TestPrepareTeamDataAddsMatchupOrdinalToStarterSlots covers the starter
// slot's own raw-map path (not the typed RosterCard path bench rows use):
// prepareTeamData must add matchup_ordinal to every starter slot map so
// page.gsx's <Each of={data.starters}> can read slot.matchup_ordinal.
func TestPrepareTeamDataAddsMatchupOrdinalToStarterSlots(t *testing.T) {
	data := map[string]any{
		"starters": []map[string]any{
			{"slot_id": "QB", "matchup_chip": "19th-toughest"},
			{"slot_id": "RB1"},
		},
	}
	out := prepareTeamData(data, nil)
	starters, ok := out["starters"].([]map[string]any)
	if !ok || len(starters) != 2 {
		t.Fatalf("prepareTeamData starters = %#v, want a 2-element slice", out["starters"])
	}
	if got := starters[0]["matchup_ordinal"]; got != "19th" {
		t.Errorf("starters[0].matchup_ordinal = %q, want %q", got, "19th")
	}
	if got := starters[1]["matchup_ordinal"]; got != "" {
		t.Errorf("starters[1].matchup_ordinal (no chip) = %q, want empty", got)
	}
}

// TestOpponentChipRendersOrdinalWithFullTextAsTitleTip pins the page.gsx
// markup shape for both the starter slot and the bench RosterRow: the
// matchup-chip span shows the ordinal field and carries the full
// provenance sentence as its title tip.
func TestOpponentChipRendersOrdinalWithFullTextAsTitleTip(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<span class="matchup-chip" data-matchup-tier={slot.matchup_tier} title={slot.matchup_detail}>{slot.matchup_ordinal}</span>`,
		`<span class="matchup-chip" data-matchup-tier={props.MatchupTier} title={props.MatchupDetail}>{props.MatchupOrdinal}</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page.gsx missing %q", want)
		}
	}
}

// TestLineupGridResidueDesktopCSS pins the PROJ/PTS label hide and the
// news-icon chevron fix (J5's team-lineup residue after pine, items 2/3)
// at the shared desktop breakpoint.
func TestLineupGridResidueDesktopCSS(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	start := strings.Index(css, "/* J0 team-lineup residue after pine")
	if start < 0 {
		t.Fatal("styles.css is missing the J0 team-lineup residue after pine block")
	}
	block := css[start:]
	// Season3 integration note (public/styles.css, this same block):
	// widened from "@media (width > 899px)" to "@media (width > 68.75rem)"
	// so the rail-band range (900-1100px) keeps the two-line phone rows
	// instead of a desktop grid-template-columns squeezing into a
	// narrower column there.
	mediaStart := strings.Index(block, "@media (width > 68.75rem) {")
	if mediaStart < 0 {
		t.Fatal("team-lineup residue block is missing its @media (width > 68.75rem) override")
	}
	media := block[mediaStart:]
	for _, want := range []string{
		".lineup-slot__proj small,",
		".lineup-slot__pts small {",
		"display: none;",
		".lineup-slot__player .stat-tip--news > .stat-tip__summary::after {",
		"content: none;",
		"grid-template-columns: 3.5rem minmax(14rem, 1fr) 6rem 6.5rem 6rem 2.75rem 2.75rem 6rem;",
	} {
		if !strings.Contains(media, want) {
			t.Errorf("team-lineup residue desktop block missing %q", want)
		}
	}
}

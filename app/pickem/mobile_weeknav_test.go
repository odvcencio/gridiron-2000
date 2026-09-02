package pickem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPickemWeekSelectSharesTheBoardButtonChipRecipe is item 9's own
// contract: /pickem's week <select> (page.gsx) now carries
// class="board-button" — the same recipe /matchups' WeekBrowser already
// gives its own week select (page.gsx's own doc comment on that
// component explains why both pages share the .pickem-weeknav class) —
// so every chip in the strip reaches the same 44px floor and visual
// treatment, and the shared wave 7b sticky/snap-scroll rule for
// .pickem-weeknav (pinned in app/matchups' own
// TestMatchupsWeekNavIsStickyAndSnapScrollsOnPhone) applies identically
// here.
func TestPickemWeekSelectSharesTheBoardButtonChipRecipe(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, `<select name="week" class="board-button" aria-label="Select week">`) {
		t.Error("page.gsx week select is missing class=\"board-button\"")
	}
	if !strings.Contains(source, `<div class="pickem-weeknav">`) {
		t.Error("page.gsx lost the shared .pickem-weeknav wrapper")
	}
}

// TestPickemPrimaryActionLinksToTheSlateWhenPickable is item 9's own
// primary_action contract: /pickem has no single "submit picks" form (each
// game posts its own small managed form), so the bar action is a link to
// the weekly slate section, set only when the viewer can actually pick.
func TestPickemPrimaryActionLinksToTheSlateWhenPickable(t *testing.T) {
	source, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{
		`if canPick, _ := data["can_pick"].(bool); canPick {`,
		`"href":  "#pickem-slate"`,
		`"kind":  "link"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("page.server.go missing primary_action contract %q", want)
		}
	}

	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `<section class="player-pool" id="pickem-slate">`) {
		t.Error("page.gsx is missing the #pickem-slate anchor the primary_action targets")
	}
}

// TestPickemRowIsACardOnPhone is item 9's own "two-line card" contract:
// the existing wave-1/wave-2 flex-wrap collapse already stacks kickoff +
// label above the market/buttons/status line at phone/tablet width; the
// wave 7b block adds the card framing (border-radius, spacing) so
// consecutive rows read as separate cards.
func TestPickemRowIsACardOnPhone(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	blockStart := strings.Index(css, "/* wave 7b — ash")
	if blockStart < 0 {
		t.Fatal("styles.css is missing the wave 7b — ash block")
	}
	block := css[blockStart:]
	if !strings.Contains(block, ".pickem-row {") || !strings.Contains(block, "border-radius: var(--radius-md);") {
		t.Error("wave 7b — ash block is missing the .pickem-row card treatment")
	}
}

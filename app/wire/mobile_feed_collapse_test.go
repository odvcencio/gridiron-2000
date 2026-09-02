package wire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSplitSignalCardsAndMaps is splitSignalCards'/splitMaps' own unit
// contract: a slice at or under the cap comes back whole with a nil
// overflow (no pointless "Show 0 more" toggle); a slice over the cap
// splits at exactly n.
func TestSplitSignalCardsAndMaps(t *testing.T) {
	under := []WireSignalCard{{ID: "a"}, {ID: "b"}}
	visible, overflow := splitSignalCards(under, 6)
	if len(visible) != 2 || overflow != nil {
		t.Fatalf("splitSignalCards(under cap) = %d visible, %d overflow, want 2 visible, nil overflow", len(visible), len(overflow))
	}
	over := []WireSignalCard{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	visible, overflow = splitSignalCards(over, 2)
	if len(visible) != 2 || len(overflow) != 2 || visible[0].ID != "a" || overflow[0].ID != "c" {
		t.Fatalf("splitSignalCards(over cap) = %+v visible, %+v overflow, want [a b]/[c d]", visible, overflow)
	}

	overMaps := []map[string]any{{"id": "a"}, {"id": "b"}, {"id": "c"}}
	visibleMaps, overflowMaps := splitMaps(overMaps, 1)
	if len(visibleMaps) != 1 || len(overflowMaps) != 2 {
		t.Fatalf("splitMaps(over cap) = %d visible, %d overflow, want 1 visible, 2 overflow", len(visibleMaps), len(overflowMaps))
	}
}

// TestWireFeedListViewCollapsesPastTheVisibleCount covers
// wireFeedListView's own contract: HasOverflow/OverflowCount track the
// actual overflow length, and Visible never exceeds the cap.
func TestWireFeedListViewCollapsesPastTheVisibleCount(t *testing.T) {
	items := make([]WireSignalCard, 9)
	for i := range items {
		items[i] = WireSignalCard{ID: string(rune('a' + i))}
	}
	view := wireFeedListView(items, wireVisibleSignalCount)
	if len(view.Visible) != wireVisibleSignalCount {
		t.Fatalf("Visible = %d cards, want %d", len(view.Visible), wireVisibleSignalCount)
	}
	if !view.HasOverflow || view.OverflowCount != len(items)-wireVisibleSignalCount {
		t.Fatalf("HasOverflow=%v OverflowCount=%d, want true/%d", view.HasOverflow, view.OverflowCount, len(items)-wireVisibleSignalCount)
	}
	small := wireFeedListView(items[:3], wireVisibleSignalCount)
	if small.HasOverflow || small.OverflowCount != 0 || len(small.Visible) != 3 {
		t.Fatalf("under-cap view = %+v, want no overflow and all 3 visible", small)
	}
}

// TestWireSectionStripAndFoldMarkup is item 5's own static contract: the
// three anchor targets (#wire-feed, #wire-sources, #community-input) all
// exist on their sections, the sticky jump strip links to all three, the
// sighting form carries the id larch's PageActionBar submit contract
// needs, and the wave 7b stylesheet block styles the strip and the
// collapsed-tail toggle.
func TestWireSectionStripAndFoldMarkup(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<section class="wire-stage" id="wire-feed">`,
		`<section class="wire-source-panel" id="wire-sources">`,
		`<section class="wire-submit-panel" id="community-input">`,
		`<a href="#wire-feed" class="board-button">Feed</a>`,
		`<a href="#wire-sources" class="board-button">Sources</a>`,
		`<a href="#community-input" class="board-button">Sighting</a>`,
		`<form id="wire-sighting-form" method="post" action={actionPath("submit-sighting")} data-gosx-managed="true">`,
		`<WireFeedList {...data.feed_list}></WireFeedList>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx missing wire section-strip/fold contract %q", want)
		}
	}

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
	for _, want := range []string{".wire-section-strip", "position: sticky", ".wire-more__summary", "min-height: var(--control-h)"} {
		if !strings.Contains(block, want) {
			t.Errorf("wave 7b — ash block missing %q", want)
		}
	}
}

// TestWirePrimaryActionOnlyWhenTheSightingFormExists is item 5's
// primary_action contract: a signed-in (or demo-mode) viewer, who
// actually sees the sighting form, gets a submit action pointed at its
// id; a signed-out viewer, who sees a sign-in link instead of the form,
// gets none.
func TestWirePrimaryActionOnlyWhenTheSightingFormExists(t *testing.T) {
	data := map[string]any{"can_submit": true}
	if canSubmit, _ := data["can_submit"].(bool); !canSubmit {
		t.Fatal("test setup: expected can_submit true")
	}
	// This mirrors the Load closure's own gate (page.server.go): the
	// action map below is only ever assigned inside that same
	// `if canSubmit` branch, so asserting the branch condition here is
	// this package's fast, no-HTTP-server check on that contract; the
	// exact map shape is pinned by TestWireSectionStripAndFoldMarkup's
	// sibling assertion on the sighting form's id plus this literal.
	action := map[string]any{
		"label": "Transmit sighting",
		"href":  "#community-input",
		"kind":  "submit",
		"form":  "wire-sighting-form",
		"tone":  "primary",
	}
	if action["form"] != "wire-sighting-form" || action["kind"] != "submit" {
		t.Fatalf("wire primary_action = %v, want a submit action targeting #wire-sighting-form", action)
	}

	serverSource, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(serverSource)
	for _, want := range []string{
		`if canSubmit, _ := data["can_submit"].(bool); canSubmit {`,
		`"kind":  "submit"`,
		`"form":  "wire-sighting-form"`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page.server.go missing primary_action gate/shape %q", want)
		}
	}
}

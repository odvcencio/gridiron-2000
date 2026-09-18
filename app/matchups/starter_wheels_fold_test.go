package matchups

import (
	"os"
	"strings"
	"testing"
)

// TestStarterWheelsFollowTheLineupOnAPhone pins the score-first fold
// contract for the featured card (wave 7b, ash item 1). The two starter
// wheels used to render inside .my-matchup__score, between the score and
// the first lineup row, which put that row at 887px on an 844px phone
// screen (TestBrowserAshJobInFoldAcrossOwnedRoutes). The wheels are now
// the summary header's own sibling, so a phone-width rule can order them
// after the lineup comparison while wider screens keep them under the
// score.
func TestStarterWheelsFollowTheLineupOnAPhone(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	cardStart := strings.Index(page, `<section class="my-matchup card"`)
	if cardStart < 0 {
		t.Fatal("page.gsx is missing the featured my-matchup card")
	}
	card := page[cardStart:]
	card = card[:strings.Index(card, "</section>")]
	headerEnd := strings.Index(card, "</header>")
	wheels := strings.Index(card, "<StarterProgress")
	pairs := strings.Index(card, `class="matchup-pairs-table"`)
	if headerEnd < 0 || wheels < 0 || pairs < 0 {
		t.Fatalf("featured card is missing its summary header, StarterProgress, or pairs table (header end %d, wheels %d, pairs %d)", headerEnd, wheels, pairs)
	}
	if !(headerEnd < wheels && wheels < pairs) {
		t.Fatalf("StarterProgress must sit between the summary header and the pairs table as their sibling: header end %d, wheels %d, pairs %d", headerEnd, wheels, pairs)
	}

	css, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(css)
	rule := strings.Index(styles, ".matchups-page .my-matchup > .starter-progress {")
	if rule < 0 {
		t.Fatal("styles.css never orders .my-matchup > .starter-progress on a phone")
	}
	block := styles[rule : rule+strings.Index(styles[rule:], "}")]
	if !strings.Contains(block, "order:") {
		t.Fatalf("phone rule for .my-matchup > .starter-progress sets no order: %s", block)
	}
	media := strings.LastIndex(styles[:rule], "@media (width <= 38rem)")
	if media < 0 {
		t.Fatal("the wheel order rule must live inside a phone-width (38rem) media block")
	}
	phoneBlock := styles[media:rule]
	if !strings.Contains(phoneBlock, ".matchups-page .my-matchup {") || !strings.Contains(phoneBlock, "flex-direction: column") {
		t.Fatal("the phone-width block must make .my-matchup a flex column before the order rule, or order has nothing to act on")
	}
}

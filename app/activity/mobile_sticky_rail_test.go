package activity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestActivityFilterRailIsStickyWithTopPaginationAndBackToTop is item 3's
// own contract: the team/search filter form is wrapped in the shared
// .pool-filter-rail (id="activity-filters", sticky under the fixed top
// bar at phone/tablet width — the same wave 7b rule /players' own rail
// uses), the search field carries phone keyboard hints, the pagination
// nav is duplicated at the top of the feed (hidden by default, shown only
// at phone/tablet width), and a "Back to filters" link follows the feed.
//
// Page() embeds ActivityRegion() directly (item 1's root-cause fix,
// 2026-09-02 route-crawl finding — rowan) instead of hand-duplicating its
// markup, so this markup — and each control's id — now has exactly one
// definition, not a "-sync-" duplicate.
func TestActivityFilterRailIsStickyWithTopPaginationAndBackToTop(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)

	if got := strings.Count(source, `<div class="pool-filter-rail" id="activity-filters">`); got != 1 {
		t.Errorf(`page.gsx has %d copies of the activity filter rail wrapper, want 1 (ActivityRegion())`, got)
	}
	if got := strings.Count(source, `<a class="access-link activity-back-to-top" href="#activity-filters">`); got != 1 {
		t.Errorf(`page.gsx has %d back-to-top links, want 1 (ActivityRegion())`, got)
	}
	if got := strings.Count(source, `class="pool-pagination pool-pagination--top"`); got != 1 {
		t.Errorf(`page.gsx has %d top-of-list pagination navs, want 1 (ActivityRegion())`, got)
	}
	if !strings.Contains(source, `<input id="activity-search" type="search" name="q" value={data.query} placeholder="Player, move, or team" inputmode="search" enterkeyhint="search" autocomplete="off"></input>`) {
		t.Error("page.gsx missing search-field contract for id=\"activity-search\"")
	}
	if strings.Contains(source, "activity-sync-search") {
		t.Error("page.gsx still carries a duplicate activity-sync-search id — one id per control")
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
	if !strings.Contains(block, ".pool-pagination--top {") {
		t.Error("wave 7b — ash block never styles .pool-pagination--top")
	}
	if !strings.Contains(block, ".activity-back-to-top") {
		t.Error("wave 7b — ash block never styles .activity-back-to-top")
	}
}

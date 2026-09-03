package activity

import (
	"os"
	"strings"
	"testing"
)

// TestActivityFeedSitsAboveThePlayoffContextBandAndPaginationHidesAtOnePage
// is item 8's own regression test (2026-09-02 audit): the masthead's
// decorative second paragraph and the playoff-context card together
// pushed the feed's first row below the fold at 390px — zero rows
// visible above it even on a 137-move league — and two "Page 1 / 1"
// navigations rendered around an empty feed. The playoff-context card
// now follows the feed instead of preceding it, the masthead drops the
// decorative paragraph, and both pagination navs render only once there
// is more than one page.
func TestActivityFeedSitsAboveThePlayoffContextBandAndPaginationHidesAtOnePage(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)

	feedIndex := strings.Index(source, `id="activity-feed-region"`)
	playoffIndex := strings.Index(source, `class="score-command playoff-truth-card"`)
	if feedIndex < 0 {
		t.Fatal("page.gsx is missing the activity feed region")
	}
	if playoffIndex < 0 {
		t.Fatal("page.gsx is missing the playoff-truth-card section")
	}
	if feedIndex > playoffIndex {
		t.Error("page.gsx renders the playoff-context card before the transaction feed, pushing it below the fold")
	}

	if strings.Contains(source, "Draft picks, waiver and free-agent moves, and trades — one permanent league record, newest first.") {
		t.Error("page.gsx still carries the decorative masthead paragraph that pushed the feed below the fold")
	}

	if got := strings.Count(source, `<If cond={data.pages > 1}>`); got != 2 {
		t.Errorf("page.gsx guards %d pagination navs with data.pages > 1, want 2 (top and bottom)", got)
	}
}

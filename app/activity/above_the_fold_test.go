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

// TestActivityHeroLinksShareOneRowUnderTheTimeLine is a coordinator
// follow-up to the J6 wave: on a phone, the hero card's own three
// .draft-clock-meta children (the league-time span, "Player pool →",
// "Team terminal →") used to wrap as two-then-one under
// justify-content: space-between, leaving "Team terminal →" alone on
// its own row with a large gap above it. The two links now share one
// wrapper, .activity-clock-links, so they read as a single row under
// the time line and wrap together — never split from each other — only
// when they do not both fit.
func TestActivityHeroLinksShareOneRowUnderTheTimeLine(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	timeIndex := strings.Index(source, `<span class="mono">League time · {data.timezone}</span>`)
	linksIndex := strings.Index(source, `<div class="activity-clock-links">`)
	playerPoolIndex := strings.Index(source, `<a href="/players" data-gosx-link>Player pool →</a>`)
	teamTerminalIndex := strings.Index(source, `<a href="/team" data-gosx-link>Team terminal →</a>`)
	if timeIndex < 0 {
		t.Fatal("page.gsx is missing the hero card's league-time line")
	}
	if linksIndex < 0 {
		t.Fatal(`page.gsx is missing the hero card's ".activity-clock-links" wrapper around Player pool and Team terminal`)
	}
	if playerPoolIndex < 0 || teamTerminalIndex < 0 {
		t.Fatal("page.gsx is missing the hero card's Player pool or Team terminal link")
	}
	if timeIndex > linksIndex {
		t.Error("the league-time line should render before the links row, not after it")
	}
	if linksIndex > playerPoolIndex || linksIndex > teamTerminalIndex {
		t.Error("Player pool and Team terminal should both render inside .activity-clock-links")
	}
	if playerPoolIndex > teamTerminalIndex {
		t.Error("Player pool should render before Team terminal within the shared links row")
	}
}

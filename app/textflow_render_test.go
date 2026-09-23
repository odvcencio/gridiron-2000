package app

import (
	"strings"
	"testing"
)

// TestHomeActionCenterTitleAndDetailRenderThroughTextBlock is the text-
// flow wave's render check (2026-09-05) for the Action Center card title
// (props.Label) and detail sentence (props.Detail): both now render
// through the GoSX TextBlock substrate instead of a plain <strong>/<span>,
// so a long title or detail clamps at a real line boundary instead of
// overflowing the fixed-height task card.
func TestHomeActionCenterTitleAndDetailRenderThroughTextBlock(t *testing.T) {
	body := renderAuthenticatedHomepage(t)
	if !strings.Contains(body, `home-action-center__task`) {
		t.Fatalf("fixture rendered no Action Center task to check: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("Action Center task title missing the maxLines=2 TextBlock clamp: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-max-lines="3"`) {
		t.Errorf("Action Center task detail missing the maxLines=3 TextBlock clamp: %s", body)
	}
}

// The practice card keeps a direct action without repeating the rules.
func TestHomePracticeCardOffersDirectAction(t *testing.T) {
	body := runHomeBootstrapFixture(t, "seated")
	if !strings.Contains(body, "Practice the draft room") {
		t.Fatalf("seated fixture did not render the practice invite card: %s", body)
	}
	if !strings.Contains(body, "Open the practice draft") {
		t.Errorf("practice card has no direct action: %s", body)
	}
}

// TestHomeStandingsAndMatchupPreviewNamesRenderThroughTextBlock covers
// the one-line team and manager names in standings and matchup previews.
func TestHomeStandingsAndMatchupPreviewNamesRenderThroughTextBlock(t *testing.T) {
	body := runHomepageStandingsFixture(t, "scored")
	if !strings.Contains(body, "standing-team") {
		t.Fatalf("scored fixture rendered no standings row to check: %s", body)
	}
	if !strings.Contains(body, "mini-team") {
		t.Fatalf("scored fixture rendered no matchup preview card to check: %s", body)
	}
	oneLineNames := strings.Count(body, `data-gosx-text-layout-max-lines="1"`)
	if oneLineNames < 4 {
		t.Errorf("expected at least 4 one-line TextBlock names (standing team+manager, matchup team+manager), got %d: %s", oneLineNames, body)
	}
}

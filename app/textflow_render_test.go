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

// TestHomePracticeCardCopyRendersThroughTextBlock covers the practice
// invite card's own copy sentence: it flows (no maxLines) through
// TextBlock rather than staying a bare <p>, so it is on the same
// substrate as every other converted surface even though this sentence's
// fixed length never needed a CSS clamp.
func TestHomePracticeCardCopyRendersThroughTextBlock(t *testing.T) {
	body := runHomeBootstrapFixture(t, "seated")
	if !strings.Contains(body, "Take a few picks on the clock") {
		t.Fatalf("seated fixture did not render the practice invite card: %s", body)
	}
	const marker = `data-gosx-text-layout-source="Take a few picks on the clock against the other seats, played by bots. Nothing you do there counts."`
	if !strings.Contains(body, marker) {
		t.Errorf("practice card copy did not render through TextBlock: %s", body)
	}
}

// TestHomeStandingsAndMatchupPreviewNamesRenderThroughTextBlock covers
// the Power Grid standing row (team + manager) and the matchup preview's
// MiniMatchup card (team + manager): both keep a one-line row contract,
// so both clamp at maxLines=1 with ellipsis instead of the retired
// .standing-team/.mini-team CSS ellipsis rules.
func TestHomeStandingsAndMatchupPreviewNamesRenderThroughTextBlock(t *testing.T) {
	body := runHomepageStandingsFixture(t, "scored")
	if !strings.Contains(body, "standing-team") {
		t.Fatalf("scored fixture rendered no standings row to check: %s", body)
	}
	if !strings.Contains(body, "mini-team") {
		t.Fatalf("scored fixture rendered no matchup preview card to check: %s", body)
	}
	if !strings.Contains(body, "commissioner-note") {
		t.Fatalf("scored fixture rendered no commissioner note banner to check: %s", body)
	}
	oneLineNames := strings.Count(body, `data-gosx-text-layout-max-lines="1"`)
	if oneLineNames < 4 {
		t.Errorf("expected at least 4 one-line TextBlock names (standing team+manager, matchup team+manager), got %d: %s", oneLineNames, body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-source="Scheduled time is the meeting point, not an auto-start. The commissioner randomizes draft order about one hour before the room opens. Draft order locks when the commissioner starts the draft."`) {
		t.Errorf("commissioner note banner text did not render through TextBlock: %s", body)
	}
}

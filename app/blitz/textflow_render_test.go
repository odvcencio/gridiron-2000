package blitz

import (
	"os"
	"strings"
	"testing"
)

// TestBlitzEntryAndChampionNamesSourceUsesTextBlock pins the textflow
// wave (2026-09-05): a leaderboard/archive row's own entry name
// (.pool-player strong, BlitzLeaderRow/BlitzArchiveRow) and the archive's
// own overall-champion line render through <TextBlock maxLines={1,2}>.
// .pool-player strong is a SHARED selector (public/styles.css ~2231:
// .mini-team, .standing-team, .score-team__name, .player-identity,
// .leader-row, .scout-row, .pick-row all reuse it) whose own white-
// space: nowrap + text-overflow: ellipsis clip is retired only for
// .blitz-page under the textflow comb — see that comb's own comment for
// why the shared selector itself stays pinned. Populating a real,
// scored blitz entry needs a full preseason feed fixture, so this pins
// the source template directly, the same style
// TestCommissionerCardTitleSourceUsesTextBlock (app/commissioner) uses.
func TestBlitzEntryAndChampionNamesSourceUsesTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis" text={props.Entry.name}`,
		`TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis">OVERALL CHAMPION: {data.archive.overall_champion}</TextBlock>`,
	} {
		if got := strings.Count(page, want); got == 0 {
			t.Errorf("blitz page.gsx missing textflow conversion %q", want)
		}
	}
	if got := strings.Count(page, `TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis" text={props.Entry.name}`); got != 2 {
		t.Errorf("blitz page.gsx has %d entry-name TextBlock conversions, want 2 (BlitzLeaderRow + BlitzArchiveRow)", got)
	}
}

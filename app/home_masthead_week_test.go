package app

import (
	"os"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestHomeMastheadWeekLabelNamesPreseasonOrTheWeek pins Decision 1 (J3
// F21, J5 F29, wave E): the home masthead h1 now names the page and the
// week ("Home · Week 1", or "Home · Preseason" before the schedule
// reaches game weeks) instead of leading with the stage's own slogan.
// homeMastheadWeekLabel reads data["live"] (Service.liveMap) — the one
// page-wide week answer commissionerSeatlessOverlay already reads above
// it in this file — so this never computes a second one.
func TestHomeMastheadWeekLabelNamesPreseasonOrTheWeek(t *testing.T) {
	cases := []struct {
		name string
		live map[string]any
		want string
	}{
		{"preseason state", map[string]any{"state": league.MatchupStatePreseason, "week": 1}, "Preseason"},
		{"regular season week 1", map[string]any{"state": league.MatchupStateScheduled, "week": 1}, "Week 1"},
		{"regular season week 9", map[string]any{"state": league.MatchupStateInProgress, "week": 9}, "Week 9"},
		{"final state", map[string]any{"state": league.MatchupStateFinal, "week": 14}, "Week 14"},
		{"no week yet", map[string]any{"state": "", "week": 0}, "Preseason"},
		{"nil live map", nil, "Preseason"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := homeMastheadWeekLabel(c.live); got != c.want {
				t.Errorf("homeMastheadWeekLabel(%+v) = %q, want %q", c.live, got, c.want)
			}
		})
	}
}

// TestHomeMastheadUrgentReadsAttentionMap pins the desktop masthead
// chip's source: the same data.league.attention map app/layout.gsx's
// rail-attention-chip already reads, so the rail and the masthead can
// never disagree about the urgent count.
func TestHomeMastheadUrgentReadsAttentionMap(t *testing.T) {
	count, has, label := homeMastheadUrgent(map[string]any{
		"attention": map[string]any{
			"urgent_count": 3,
			"has_items":    true,
			"chip_label":   "3 items need attention in the Action Center",
		},
	})
	if count != 3 || !has || label != "3 items need attention in the Action Center" {
		t.Fatalf("homeMastheadUrgent = (%d, %v, %q), want (3, true, the chip label)", count, has, label)
	}

	count, has, label = homeMastheadUrgent(map[string]any{})
	if count != 0 || has || label != "" {
		t.Fatalf("homeMastheadUrgent with no attention map = (%d, %v, %q), want the zero value", count, has, label)
	}
}

// TestHomeMastheadTemplateNamesPageAndWeek pins page.gsx's own shape:
// the h1 reads "Home · <week>" (never the bare stage slogan), the slogan
// (props.Heading) renders one line down as the lede sentence, the
// eyebrow above the h1 carries no "00 // " section number (Decision 2),
// and the desktop urgent chip reuses layout.gsx's own rail-attention-
// chip class so the two can never visually or semantically diverge.
func TestHomeMastheadTemplateNamesPageAndWeek(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<h1 id="home-action-center-heading">Home · {props.WeekLabel}</h1>`,
		`<p class="home-action-center__lede">{props.Heading}</p>`,
		`<span class="section-index">{props.StageLabel}</span>`,
		`class="rail-attention-chip home-action-center__urgent-chip"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("home page.gsx missing %q", want)
		}
	}
	if strings.Contains(page, `<span class="section-index">00 // {props.StageLabel}</span>`) {
		t.Error("home masthead eyebrow still carries the retired \"00 // \" section number")
	}
}

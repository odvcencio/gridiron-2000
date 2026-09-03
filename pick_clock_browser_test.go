package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// preDraftPickClockSelector names the pre-draft scheduled-window
// countdown, the one .pick-clock instance rendered with the "dhms"
// format (app/draft/page.gsx's DraftCommandBar) — "Xd HH:MM:SS", the
// element item 2 (comb — oleander) gave real room to.
const preDraftPickClockSelector = `[data-pick-clock][data-gosx-countdown-format="dhms"]`

// preDraftPickClockDHMSRE is the countdown's own live format
// (formatCountdownDHMS, gosx's navigation.ts): "Nd HH:MM:SS", days
// unpadded, the rest always two digits.
var preDraftPickClockDHMSRE = regexp.MustCompile(`^\d+d \d{2}:\d{2}:\d{2}$`)

type pickClockOverflowProbe struct {
	Text        string  `json:"text"`
	ScrollWidth float64 `json:"scrollWidth"`
	ClientWidth float64 `json:"clientWidth"`
	BoxWidth    float64 `json:"boxWidth"`
}

const pickClockOverflowScript = `(function(){
	var clock = document.querySelector(` + "`" + preDraftPickClockSelector + "`" + `);
	if (!clock) return {text: '', scrollWidth: -1, clientWidth: -1, boxWidth: -1};
	var box = clock.closest('.draft-command__clock') || clock.parentElement;
	return {
		text: clock.textContent,
		scrollWidth: clock.scrollWidth,
		clientWidth: clock.clientWidth,
		boxWidth: box ? box.getBoundingClientRect().width : -1
	};
})()`

func readPickClockOverflowProbe(t *testing.T, ctx context.Context) pickClockOverflowProbe {
	t.Helper()
	var probe pickClockOverflowProbe
	if err := chromedp.Run(ctx, chromedp.Evaluate(pickClockOverflowScript, &probe)); err != nil {
		t.Fatalf("read pick-clock overflow probe: %v", err)
	}
	return probe
}

// TestPreDraftPickClockFullyReadableAtPhoneWidths is item 2's own
// browser regression test (comb — oleander, 2026-09-02 audit). Before
// this fix, .draft-command__clock capped at a fixed 5rem (80px) box —
// sized for the D1/D2 fix's own worst case, the scheduled-window label
// PLUS digits — while the digits alone ("3d 13:26:42") needed ~111px,
// so the box's own overflow: hidden/text-overflow: ellipsis painted
// "3d 13…" and silently dropped the actual hours/minutes/seconds until
// the draft. min-width: max-content (this item's fix) keeps the box
// exactly as wide as its own content at every width the accessible
// baseline test matrix covers.
func TestPreDraftPickClockFullyReadableAtPhoneWidths(t *testing.T) {
	// The harness's own built-in reference league (LEAGUE_FILE="",
	// simChildBaseEnv) carries the neutral shipped placeholder draft
	// date (2099-01-01), more than 400 days out — DraftDatePublished
	// (service.go) refuses to publish a countdown that far away, so the
	// pre-draft pick-clock never renders at all under the harness's own
	// default league. DRAFT_AT (a documented league.json env override,
	// config.go) moves it to a real near-term date — 3 days, 13-ish
	// hours out, chosen to reproduce the exact "3d 13:26:42" shape
	// Yarrow's own evidence measured — so this test can actually reach
	// the element it is pinning.
	chrome := chromePath(t)
	root := browserAppRoot(t)
	draftAt := time.Now().UTC().Add(3*24*time.Hour + 13*time.Hour + 26*time.Minute + 42*time.Second)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "DRAFT_AT="+draftAt.Format(time.RFC3339))
	league := seatLeagueWith(t, child, true)
	ctx := newBrowserContext(t, chrome)
	bot := league.bots[0]

	for _, width := range []int64{360, 390, 430} {
		signInBrowserSeat(t, ctx, child, bot, "/draft", width, 800)

		if err := chromedp.Run(ctx, chromedp.WaitVisible(preDraftPickClockSelector, chromedp.ByQuery)); err != nil {
			t.Fatalf("pre-draft pick clock never appeared at %dpx: %v", width, err)
		}

		probe := readPickClockOverflowProbe(t, ctx)
		if probe.Text == "" {
			t.Fatalf("pre-draft pick clock rendered no text at %dpx", width)
		}
		if strings.Contains(probe.Text, "…") {
			t.Errorf("at %dpx the pick clock text was truncated with an ellipsis: %q", width, probe.Text)
		}
		if !preDraftPickClockDHMSRE.MatchString(strings.TrimSpace(probe.Text)) {
			t.Errorf("at %dpx the pick clock text %q does not match the full \"Nd HH:MM:SS\" format — some of it is missing", width, probe.Text)
		}
		if probe.ScrollWidth > probe.ClientWidth+1 {
			t.Errorf("at %dpx the pick clock overflows its own box: scrollWidth=%.1f clientWidth=%.1f (text %q)", width, probe.ScrollWidth, probe.ClientWidth, probe.Text)
		}
	}
}

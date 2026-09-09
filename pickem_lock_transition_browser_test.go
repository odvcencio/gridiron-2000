package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// pickemLockQAScheduleCSV is a small real schedule snapshot rather than an
// in-memory rendering fixture.  The first game starts Thursday evening, the
// next one starts Friday evening, and the remaining games provide the final
// ATS, push, void, and missed-loss states after the fixture clock advances.
const pickemLockQAScheduleCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score,spread_line\n" +
	"g-win,2026,REG,1,2026-09-10,19:00,BUF,,MIA,,3.5\n" +
	"g-loss,2026,REG,1,2026-09-11,19:00,KC,,DEN,,3.5\n" +
	"g-push,2026,REG,1,2026-09-12,13:00,NYJ,,NE,,0\n" +
	"g-void,2026,REG,1,2026-09-13,13:00,DAL,,PHI,,\n" +
	"g-missed,2026,REG,1,2026-09-13,16:00,SF,,SEA,,2.5\n"

// The updated source deliberately changes g-loss's line from +3.5 to +8.5
// after Thursday's market freeze.  Restarting the same child state with this
// snapshot proves the durable frozen line and provenance win over a later
// source observation, while the real scores make the final result readable.
const pickemLockQAFinalScheduleCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score,spread_line\n" +
	"g-win,2026,REG,1,2026-09-10,19:00,BUF,20,MIA,27,3.5\n" +
	"g-loss,2026,REG,1,2026-09-11,19:00,KC,24,DEN,20,8.5\n" +
	"g-push,2026,REG,1,2026-09-12,13:00,NYJ,17,NE,17,0\n" +
	"g-void,2026,REG,1,2026-09-13,13:00,DAL,17,PHI,20,\n" +
	"g-missed,2026,REG,1,2026-09-13,16:00,SF,24,SEA,20,2.5\n"

// A cached openstats schedule has to carry a non-zero source observation so
// the market candidate is eligible at the Thursday lock.  NewService loads
// this manifest even with OPEN_STATS_ENABLED=false; no upstream network call
// is involved in the browser acceptance.
const pickemLockQAScheduleManifest = `{
  "schema_version": 1,
  "season": 2026,
  "schedules": {
    "name": "schedules",
    "state": "ready",
    "license": "CC-BY-4.0",
    "rows": 5,
    "last_checked": "2026-09-09T14:00:00Z",
    "last_updated": "2026-09-09T14:00:00Z"
  }
}`

const pickemLockQAFinalScheduleManifest = `{
  "schema_version": 1,
  "season": 2026,
  "schedules": {
    "name": "schedules",
    "state": "ready",
    "license": "CC-BY-4.0",
    "rows": 5,
    "last_checked": "2026-09-14T00:00:00Z",
    "last_updated": "2026-09-14T00:00:00Z"
  }
}`

var (
	pickemLockQAStart = time.Date(2026, time.September, 9, 16, 0, 0, 0, time.UTC)
	pickemLockQAFinal = time.Date(2026, time.September, 14, 0, 0, 0, 0, time.UTC)
)

type pickemLockQAViewport struct {
	name   string
	width  int64
	height int64
}

type pickemLockQADOMMetrics struct {
	ScrollWidth int64    `json:"scrollWidth"`
	InnerWidth  int64    `json:"innerWidth"`
	Clipped     []string `json:"clipped"`
}

func writePickemLockQASnapshot(t *testing.T, root, games, manifest string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "games.csv"), []byte(games), 0o600); err != nil {
		t.Fatalf("write Pick'em lock schedule: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write Pick'em lock manifest: %v", err)
	}
}

func waitPickemLockQASlate(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#game-g-win`, chromedp.ByQuery)); err != nil {
		t.Fatalf("Pick'em slate did not render: %v", err)
	}
}

func refreshPickemLockQAPage(t *testing.T, ctx context.Context, child *simChild) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+"/pickem?week=1"),
		chromedp.WaitVisible(`#main-content`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("refresh Pick'em page: %v", err)
	}
	waitPickemLockQASlate(t, ctx)
}

// clickPickemLockQATeam performs a real browser click on the managed form's
// submit button.  The temporary attribute only gives chromedp a stable,
// team-specific selector; the form remains the app's own action and CSRF
// transport.
func clickPickemLockQATeam(t *testing.T, ctx context.Context, gameID, team string) {
	t.Helper()
	expression := fmt.Sprintf(`(function(){
  var row = document.getElementById("game-%s");
  if (!row) return false;
  document.querySelectorAll('[data-pickem-lock-qa-target]').forEach(function(e){e.removeAttribute('data-pickem-lock-qa-target');});
  var forms = row.querySelectorAll('form');
  for (var i = 0; i < forms.length; i++) {
    var input = forms[i].querySelector('input[name="team"]');
    var button = forms[i].querySelector('button[type="submit"]');
    if (input && button && input.value === %q) {
      button.scrollIntoView({block:"center", inline:"nearest"});
      button.setAttribute('data-pickem-lock-qa-target', '1');
      return true;
    }
  }
  return false;
})()`, gameID, team)
	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &found)); err != nil {
		t.Fatalf("find Pick'em %s button for %s: %v", gameID, team, err)
	}
	if !found {
		t.Fatalf("Pick'em %s has no active form for %s", gameID, team)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`[data-pickem-lock-qa-target="1"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click Pick'em %s for %s: %v", gameID, team, err)
	}
}

func pickemLockQAPickedTeam(t *testing.T, ctx context.Context, gameID string) string {
	t.Helper()
	expression := fmt.Sprintf(`(function(){
  var row = document.getElementById("game-%s");
  if (!row) return '';
  var forms = row.querySelectorAll('form');
  for (var i = 0; i < forms.length; i++) {
    var input = forms[i].querySelector('input[name="team"]');
    var marker = forms[i].querySelector('.pickem-your-pick');
    if (input && marker) return input.value;
  }
  var buttons = row.querySelectorAll('button');
  for (var j = 0; j < buttons.length; j++) {
    if (buttons[j].querySelector('.pickem-your-pick')) return buttons[j].textContent || '';
  }
  return '';
})()`, gameID)
	var picked string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &picked)); err != nil {
		t.Fatalf("read Pick'em selection for %s: %v", gameID, err)
	}
	return strings.TrimSpace(picked)
}

func waitPickemLockQAPicked(t *testing.T, ctx context.Context, gameID, want string) {
	t.Helper()
	deadline := time.Now().Add(browserRegionSwapWait)
	last := ""
	for time.Now().Before(deadline) {
		last = pickemLockQAPickedTeam(t, ctx, gameID)
		if last == want {
			return
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("Pick'em %s did not settle on %s within %s (last selection %q)", gameID, want, browserRegionSwapWait, last)
}

func assertPickemLockQAPersistedPick(t *testing.T, ctx context.Context, child *simChild, gameID, want string) {
	t.Helper()
	refreshPickemLockQAPage(t, ctx, child)
	if got := pickemLockQAPickedTeam(t, ctx, gameID); got != want {
		t.Fatalf("fresh Pick'em GET lost %s selection: got %q, want %q", gameID, got, want)
	}
}

func submitPickemLockQANativeTeam(t *testing.T, ctx context.Context, gameID string, formNumber int) {
	t.Helper()
	selector := fmt.Sprintf(`#game-%s .pickem-buttons form:nth-of-type(%d) button[type="submit"]`, gameID, formNumber)
	if err := chromedp.Run(ctx,
		chromedp.Click(selector, chromedp.NodeVisible),
		chromedp.WaitVisible(`.notice-stack .flash-message`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("native Pick'em submit for %s form %d: %v", gameID, formNumber, err)
	}
}

func openPickemLockQAMarketDetails(t *testing.T, ctx context.Context, gameID string) {
	t.Helper()
	selector := fmt.Sprintf(`#game-%s .pickem-market__provenance > summary`, gameID)
	if err := chromedp.Run(ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
		t.Fatalf("open Pick'em line details for %s: %v", gameID, err)
	}
}

func pickemLockQARowText(t *testing.T, ctx context.Context, gameID string) string {
	t.Helper()
	expression := fmt.Sprintf(`(function(){var e=document.getElementById("game-%s");return e ? (e.innerText || e.textContent || '') : '';})()`, gameID)
	var text string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &text)); err != nil {
		t.Fatalf("read Pick'em row %s: %v", gameID, err)
	}
	return strings.TrimSpace(text)
}

func assertPickemLockQARowContains(t *testing.T, ctx context.Context, gameID string, wants ...string) {
	t.Helper()
	text := pickemLockQARowText(t, ctx, gameID)
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("Pick'em row %s missing %q in %q", gameID, want, text)
		}
	}
}

func pickemLockQAHasActiveForm(t *testing.T, ctx context.Context, gameID string) bool {
	t.Helper()
	expression := fmt.Sprintf(`!!(function(){var e=document.getElementById("game-%s");return e && e.querySelector('form[action*="pickem-set"]');})()`, gameID)
	var active bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &active)); err != nil {
		t.Fatalf("read Pick'em form state for %s: %v", gameID, err)
	}
	return active
}

func assertPickemLockQAOverflow(t *testing.T, ctx context.Context, viewport pickemLockQAViewport, phase string) {
	t.Helper()
	expression := `(function(){
  var clipped = [];
  var width = window.innerWidth;
  document.querySelectorAll('button, a, input, select, summary').forEach(function(e){
    var style = getComputedStyle(e);
    if (style.display === 'none' || style.visibility === 'hidden') return;
    var r = e.getBoundingClientRect();
    if (r.width > 0 && (r.left < -1 || r.right > width + 1)) {
      clipped.push(e.tagName.toLowerCase() + (e.className ? '.' + String(e.className).split(' ').join('.') : ''));
    }
  });
  return {scrollWidth: document.documentElement.scrollWidth, innerWidth: width, clipped: clipped.slice(0, 8)};
})()`
	var metrics pickemLockQADOMMetrics
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &metrics)); err != nil {
		t.Fatalf("read Pick'em overflow at %s (%s): %v", phase, viewport.name, err)
	}
	if metrics.ScrollWidth > metrics.InnerWidth {
		t.Fatalf("Pick'em document overflows at %s (%s): scrollWidth=%d innerWidth=%d", phase, viewport.name, metrics.ScrollWidth, metrics.InnerWidth)
	}
	if len(metrics.Clipped) != 0 {
		t.Fatalf("Pick'em controls clipped at %s (%s): %v", phase, viewport.name, metrics.Clipped)
	}
}

func TestBrowserPickemLockTransitionAndFinalOutcomes(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	viewports := []pickemLockQAViewport{
		{name: "phone", width: 390, height: 844},
		{name: "landscape-phone", width: 844, height: 390},
		{name: "desktop", width: 1440, height: 900},
	}
	for _, viewport := range viewports {
		viewport := viewport
		t.Run(viewport.name, func(t *testing.T) {
			statsRoot := t.TempDir()
			writePickemLockQASnapshot(t, statsRoot, pickemLockQAScheduleCSV, pickemLockQAScheduleManifest)
			child := startSimChild(t, "", "GOSX_APP_ROOT="+root, "OPEN_STATS_ROOT="+statsRoot, "NFL_SEASON=2026")
			league := seatLeagueWith(t, child, true)
			setClockAbsolute(t, child.URL, pickemLockQAStart)
			ctx := newBrowserContext(t, chrome)
			bot := league.bots[0]

			signInBrowserSeat(t, ctx, child, bot, "/pickem?week=1", viewport.width, viewport.height)
			waitPickemLockQASlate(t, ctx)
			assertPickemLockQARowContains(t, ctx, "g-win", "CURRENT LINE", "BUF +3.5", "MIA -3.5")
			assertPickemLockQARowContains(t, ctx, "g-loss", "CURRENT LINE", "KC +3.5", "DEN -3.5")
			assertPickemLockQAOverflow(t, ctx, viewport, "pre-lock")

			// Submit three picks before the weekly Thursday market lock.  The
			// second one is deliberately changed after the market freezes but
			// before its own Friday kickoff, proving those are separate gates.
			clickPickemLockQATeam(t, ctx, "g-win", "MIA")
			waitPickemLockQAPicked(t, ctx, "g-win", "MIA")
			assertPickemLockQAPersistedPick(t, ctx, child, "g-win", "MIA")

			clickPickemLockQATeam(t, ctx, "g-loss", "KC")
			waitPickemLockQAPicked(t, ctx, "g-loss", "KC")
			assertPickemLockQAPersistedPick(t, ctx, child, "g-loss", "KC")

			clickPickemLockQATeam(t, ctx, "g-push", "NE")
			waitPickemLockQAPicked(t, ctx, "g-push", "NE")
			assertPickemLockQAPersistedPick(t, ctx, child, "g-push", "NE")

			// Friday 00:00 UTC is after g-win's Thursday 23:00 UTC kickoff,
			// while g-loss remains 23 hours away.  Its market is frozen at
			// Thursday kickoff, but its own pick form must still be editable.
			advanceClock(t, child.URL, 32*time.Hour)
			refreshPickemLockQAPage(t, ctx, child)
			if pickemLockQAHasActiveForm(t, ctx, "g-win") {
				t.Fatal("first Pick'em game still has an active form after kickoff lock")
			}
			if !pickemLockQAHasActiveForm(t, ctx, "g-loss") {
				t.Fatal("later Pick'em game lost its editable form before its own kickoff")
			}
			assertPickemLockQARowContains(t, ctx, "g-win", "FROZEN LINE", "LOCKED · IN PROGRESS")
			openPickemLockQAMarketDetails(t, ctx, "g-loss")
			assertPickemLockQARowContains(t, ctx, "g-loss", "FROZEN LINE", "KC +3.5", "DEN -3.5", "AS OF", "VEGAS MARKET VIA NFLVERSE")
			assertPickemLockQAOverflow(t, ctx, viewport, "Thursday-freeze")

			// Change the later game's pick before its own lock through the
			// ordinary no-JavaScript POST path.  This is a real browser form
			// submission, not a fetch or a synthetic state toggle; the flash
			// and row-fragment redirect prove the native round trip landed.
			nativeCtx := newBrowserContext(t, chrome)
			if err := chromedp.Run(nativeCtx, emulation.SetScriptExecutionDisabled(true)); err != nil {
				t.Fatalf("disable GoSX for native Pick'em change: %v", err)
			}
			signInBrowserSeat(t, nativeCtx, child, bot, "/pickem?week=1#game-g-loss", viewport.width, viewport.height)
			submitPickemLockQANativeTeam(t, nativeCtx, "g-loss", 2)
			var nativeLocation string
			if err := chromedp.Run(nativeCtx, chromedp.Location(&nativeLocation)); err != nil {
				t.Fatalf("read native Pick'em location: %v", err)
			}
			if !strings.Contains(nativeLocation, "#game-g-loss") {
				t.Fatalf("native Pick'em change landed at %q, want #game-g-loss", nativeLocation)
			}
			// The managed context is still JavaScript-enabled and gives the
			// fresh server-render proof after the native submission.
			assertPickemLockQAPersistedPick(t, ctx, child, "g-loss", "DEN")

			// Replace the cached source with its later, scored snapshot and
			// restart the same persisted league.  This models a real source
			// update after the market froze: the line changes to +8.5 in the
			// feed, but the stored +3.5 remains authoritative.
			child.Stop()
			writePickemLockQASnapshot(t, statsRoot, pickemLockQAFinalScheduleCSV, pickemLockQAFinalScheduleManifest)
			child = startSimChild(t, child.DataFile, "GOSX_APP_ROOT="+root, "OPEN_STATS_ROOT="+statsRoot, "NFL_SEASON=2026")
			league.repoint(t, child)
			setClockAbsolute(t, child.URL, pickemLockQAFinal)

			// Move the fixture beyond every kickoff.  The static scores in the
			// updated cached schedule are the real final-state inputs; the
			// clock only advances lock/obligation state and never fabricates an
			// outcome.
			refreshPickemLockQAPage(t, ctx, child)
			assertPickemLockQARowContains(t, ctx, "g-loss", "KC +3.5", "DEN -3.5", "FROZEN LINE")
			assertPickemLockQARowContains(t, ctx, "g-win", "20-27", "WIN · MIA COVERED")
			assertPickemLockQARowContains(t, ctx, "g-loss", "24-20", "LOSS · KC COVERED")
			assertPickemLockQARowContains(t, ctx, "g-push", "17-17", "PUSH")
			assertPickemLockQARowContains(t, ctx, "g-void", "17-20", "NO PICK · MARKET VOID")
			assertPickemLockQARowContains(t, ctx, "g-missed", "24-20", "MISSED LOSS")
			assertPickemLockQAOverflow(t, ctx, viewport, "final-outcomes")
		})
	}
}

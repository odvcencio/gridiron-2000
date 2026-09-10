package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// waiverActivityQAScheduleCSV is a real, local open-stats schedule mirror.
// The game is deliberately in the future relative to the controlled clock:
// the two manager drops therefore exercise the normal unlocked roster path,
// while the non-empty schedule also keeps the waiver processor from treating
// the source as unavailable.
const waiverActivityQAScheduleCSV = "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score,spread_line\n" +
	"waiver-qa-future,2026,REG,1,2026-09-20,13:00,BUF,,MIA,,\n"

var waiverActivityQAStart = time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

type waiverActivityQAViewport struct {
	name   string
	width  int64
	height int64
}

type waiverActivityQAPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type waiverActivityQAClaimRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

type waiverActivityQAMetrics struct {
	ScrollWidth int64    `json:"scrollWidth"`
	InnerWidth  int64    `json:"innerWidth"`
	Clipped     []string `json:"clipped"`
}

type waiverActivityQAFocus struct {
	RootPresent   bool   `json:"rootPresent"`
	RootID        string `json:"rootID"`
	RootTag       string `json:"rootTag"`
	RootConnected bool   `json:"rootConnected"`
	RootManaged   bool   `json:"rootManaged"`
	RootTabIndex  string `json:"rootTabIndex"`
	ActiveID      string `json:"activeID"`
	ActiveTag     string `json:"activeTag"`
	ActiveIsRoot  bool   `json:"activeIsRoot"`
	SameRoot      bool   `json:"sameRoot"`
}

func (f waiverActivityQAFocus) valid(requireSameRoot bool) bool {
	return f.RootPresent && f.RootID == "waivers" && f.RootTag == "DIV" && f.RootConnected && f.RootManaged && f.RootTabIndex == "-1" && f.ActiveIsRoot && (!requireSameRoot || f.SameRoot)
}

const waiverActivityQACaptureFocusScript = `(function(){
	var root=document.querySelector('#waivers');
	var active=document.activeElement;
	if (root) window.__waiverActivityQABRoot=root;
	return {
		rootPresent:!!root,
		rootID:root ? root.id : '',
		rootTag:root ? String(root.tagName || '') : '',
		rootConnected:!!(root && root.isConnected),
		rootManaged:!!(root && root.hasAttribute('data-gosx-focus-managed')),
		rootTabIndex:root ? (root.getAttribute('tabindex') || '') : '',
		activeID:active ? (active.id || '') : '',
		activeTag:active ? String(active.tagName || '') : '',
		activeIsRoot:!!root && active === root,
		sameRoot:!!root && root === window.__waiverActivityQABRoot
	};
})()`

const waiverActivityQAReadFocusScript = `(function(){
	var root=document.querySelector('#waivers');
	var active=document.activeElement;
	return {
		rootPresent:!!root,
		rootID:root ? root.id : '',
		rootTag:root ? String(root.tagName || '') : '',
		rootConnected:!!(root && root.isConnected),
		rootManaged:!!(root && root.hasAttribute('data-gosx-focus-managed')),
		rootTabIndex:root ? (root.getAttribute('tabindex') || '') : '',
		activeID:active ? (active.id || '') : '',
		activeTag:active ? String(active.tagName || '') : '',
		activeIsRoot:!!root && active === root,
		sameRoot:!!root && root === window.__waiverActivityQABRoot
	};
})()`

// writeWaiverActivityQASchedule creates the only source-state input this
// lane owns. It stays under t.TempDir, so the browser acceptance never reads
// or mutates a shared league or open-stats cache.
func writeWaiverActivityQASchedule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "games.csv"), []byte(waiverActivityQAScheduleCSV), 0o600); err != nil {
		t.Fatalf("write waiver QA schedule: %v", err)
	}
	return root
}

// startWaiverActivityQALeague uses the shipped gridiron-house roster and the
// same offline fantasy cache as the fixed-target Team acceptance. The draft
// is completed over the real bot HTTP endpoints before any browser mutation,
// so every subsequent drop, claim, and waiver run is handled by production
// service paths and persisted in the child league file.
func startWaiverActivityQALeague(t *testing.T, root string) (*simChild, *simLeague) {
	t.Helper()
	leagueFile := writeTeamTransferFlagshipLeague(t, root)
	fantasyRoot := writeTeamTransferFantasyCache(t)
	statsRoot := writeWaiverActivityQASchedule(t)
	child := startSimChild(t, "",
		"GOSX_APP_ROOT="+root,
		"LEAGUE_FILE="+leagueFile,
		"FANTASY_ROOT="+fantasyRoot,
		"GRIDIRON_TEST_POOL=",
		"OPEN_STATS_ROOT="+statsRoot,
		"NFL_SEASON=2026",
	)
	league := seatLeagueWith(t, child, true)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start waiver QA draft: %v", err)
	}
	completeSimDraft(t, league)
	setClockAbsolute(t, child.URL, waiverActivityQAStart)
	return child, league
}

func waiverActivityQANavigate(t *testing.T, ctx context.Context, child *simChild, path string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+path),
		chromedp.WaitVisible(`#main-content`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate to %s: %v", path, err)
	}
}

func waiverActivityQACaptureManagedWaiverFocus(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var focus waiverActivityQAFocus
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = chromedp.Run(ctx, chromedp.Evaluate(waiverActivityQACaptureFocusScript, &focus))
		if lastErr == nil && focus.valid(false) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s: managed #waivers root did not retain focus: focus=%+v err=%v", label, focus, lastErr)
}

func waiverActivityQAReadBody(t *testing.T, ctx context.Context) string {
	t.Helper()
	var body string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.innerText || ''`, &body)); err != nil {
		t.Fatalf("read browser body text: %v", err)
	}
	return strings.TrimSpace(body)
}

func waiverActivityQAReadBench(t *testing.T, ctx context.Context) []waiverActivityQAPlayer {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.roster-list .roster-row[id^="bench-"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for bench rows: %v", err)
	}
	var players []waiverActivityQAPlayer
	const script = `(function(){
		return Array.from(document.querySelectorAll('.roster-list .roster-row[id^="bench-"]'))
			.map(function(row){
				var id=row.getAttribute('data-gosx-transfer-source') || row.id.slice(6);
				var name=row.querySelector('.player-identity__text strong');
				return {id:id, name:name ? (name.textContent || '').trim() : ''};
			})
			.filter(function(player){return player.id !== '' && player.name !== ''});
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &players)); err != nil {
		t.Fatalf("read bench player identities: %v", err)
	}
	if len(players) < 2 {
		t.Fatalf("Team rendered %d bench players, want at least two unlocked drop candidates: %+v", len(players), players)
	}
	return players[:2]
}

// waiverActivityQAClickManagedConfirm performs a real click sequence on the
// app's two-step confirmation form. The query is exact to one player, so the
// selector remains the server-rendered form rather than a synthetic action.
func waiverActivityQAClickManagedConfirm(t *testing.T, ctx context.Context, formAction, confirmationValue string) {
	t.Helper()
	form := `form[action*="` + formAction + `"]`
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(form+` details > summary`),
		chromedp.Click(form+` details > summary`, chromedp.ByQuery),
		chromedp.Click(form+` input[name="confirmation"][value="`+confirmationValue+`"]`, chromedp.ByQuery),
		chromedp.Click(form+` button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.gosx-toast`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("managed %s confirmation: %v", formAction, err)
	}
}

func waiverActivityQAClickNativeConfirm(t *testing.T, ctx context.Context, formAction, confirmationValue string) {
	t.Helper()
	form := `form[action*="` + formAction + `"]`
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(form+` details > summary`),
		chromedp.Click(form+` details > summary`, chromedp.ByQuery),
		chromedp.Click(form+` input[name="confirmation"][value="`+confirmationValue+`"]`, chromedp.ByQuery),
		chromedp.Click(form+` button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.notice-stack .flash-message`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("native %s confirmation: %v", formAction, err)
	}
}

func waiverActivityQADrop(t *testing.T, ctx context.Context, child *simChild, player waiverActivityQAPlayer, managed bool, width, height int64) {
	t.Helper()
	path := "/players?avail=all&q=" + url.QueryEscape(player.Name)
	if managed {
		waiverActivityQANavigate(t, ctx, child, path)
		if err := chromedp.Run(ctx, chromedp.WaitVisible(`form[action*="player-drop"]`, chromedp.ByQuery)); err != nil {
			t.Fatalf("managed drop form for %s at %dx%d: %v", player.Name, width, height, err)
		}
		waiverActivityQAClickManagedConfirm(t, ctx, "player-drop", "drop-player")
		return
	}
	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable GoSX for native drop %s: %v", player.Name, err)
	}
	waiverActivityQANavigate(t, ctx, child, path)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`form[action*="player-drop"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("native drop form for %s at %dx%d: %v", player.Name, width, height, err)
	}
	waiverActivityQAClickNativeConfirm(t, ctx, "player-drop", "drop-player")
}

func waiverActivityQAAssertOnWaivers(t *testing.T, ctx context.Context, child *simChild, player waiverActivityQAPlayer) {
	t.Helper()
	waiverActivityQANavigate(t, ctx, child, "/players?avail=all&q="+url.QueryEscape(player.Name))
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.pool-row`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for dropped player %s row: %v", player.Name, err)
	}
	body := waiverActivityQAReadBody(t, ctx)
	if !strings.Contains(body, "ON WAIVERS") {
		t.Fatalf("fresh Players render for dropped player %s has no ON WAIVERS state: %q", player.Name, body)
	}
}

func waiverActivityQAMarkClaimForm(t *testing.T, ctx context.Context, player waiverActivityQAPlayer) {
	t.Helper()
	var found bool
	script := fmt.Sprintf(`(function(){
		var forms=Array.from(document.querySelectorAll('form[action*="claim-file"]'));
		for (var i=0;i<forms.length;i++) {
			var id=forms[i].querySelector('input[name="player_id"]');
			if (id && id.value === %q) {
				document.querySelectorAll('[data-waiver-activity-qa-claim]').forEach(function(e){e.removeAttribute('data-waiver-activity-qa-claim');});
				forms[i].setAttribute('data-waiver-activity-qa-claim','1');
				return true;
			}
		}
		return false;
	})()`, player.ID)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
		t.Fatalf("find claim form for %s: %v", player.Name, err)
	}
	if !found {
		t.Fatalf("fresh ON WAIVERS row for %s has no claim-file form", player.Name)
	}
}

func waiverActivityQAClaim(t *testing.T, ctx context.Context, child *simChild, add waiverActivityQAPlayer, drop waiverActivityQAPlayer, managed bool, width, height int64) {
	t.Helper()
	path := "/players?avail=all&q=" + url.QueryEscape(add.Name)
	if managed {
		waiverActivityQANavigate(t, ctx, child, path)
		if err := chromedp.Run(ctx, chromedp.WaitVisible(`form[action*="claim-file"]`, chromedp.ByQuery)); err != nil {
			t.Fatalf("managed claim form for %s at %dx%d: %v", add.Name, width, height, err)
		}
		waiverActivityQAMarkClaimForm(t, ctx, add)
		form := `[data-waiver-activity-qa-claim="1"]`
		if err := chromedp.Run(ctx,
			chromedp.ScrollIntoView(form+` select[name="drop_id"]`),
			chromedp.SetValue(form+` select[name="drop_id"]`, drop.ID, chromedp.ByQuery),
			chromedp.Click(form+` details > summary`, chromedp.ByQuery),
			chromedp.Click(form+` input[name="confirmation"][value="claim-drop-player"]`, chromedp.ByQuery),
			chromedp.Click(form+` button[type="submit"]`, chromedp.ByQuery),
			chromedp.WaitVisible(`.gosx-toast`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("managed claim %s dropping %s: %v", add.Name, drop.Name, err)
		}
		return
	}
	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable GoSX for native claim %s: %v", add.Name, err)
	}
	waiverActivityQANavigate(t, ctx, child, path)
	form := `form[action*="claim-file"]`
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(form+` select[name="drop_id"]`, chromedp.ByQuery),
		chromedp.SetValue(form+` select[name="drop_id"]`, drop.ID, chromedp.ByQuery),
		chromedp.ScrollIntoView(form+` details > summary`),
		chromedp.Click(form+` details > summary`, chromedp.ByQuery),
		chromedp.Click(form+` input[name="confirmation"][value="claim-drop-player"]`, chromedp.ByQuery),
		chromedp.Click(form+` button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.notice-stack .flash-message`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("native claim %s dropping %s: %v", add.Name, drop.Name, err)
	}
}

func waiverActivityQAReadClaims(t *testing.T, ctx context.Context) []waiverActivityQAClaimRow {
	t.Helper()
	var claims []waiverActivityQAClaimRow
	const script = `Array.from(document.querySelectorAll('.waiver-claim-row')).map(function(row){
		var id=row.querySelector('input[name="claim_id"]');
		var name=row.querySelector('.pool-player__text strong');
		return {id:id ? id.value : '', name:name ? (name.textContent || '').trim() : '', text:(row.innerText || '').trim()};
	})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &claims)); err != nil {
		t.Fatalf("read waiver claims: %v", err)
	}
	return claims
}

func waiverActivityQAMoveClaimUp(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	var found bool
	script := fmt.Sprintf(`(function(){
		for (var row of document.querySelectorAll('.waiver-claim-row')) {
			var strong=row.querySelector('.pool-player__text strong');
			if (!strong || (strong.textContent || '').trim() !== %q) continue;
			var form=row.querySelector('form[action*="claim-move"] input[name="direction"][value="up"]');
			if (!form) return false;
			form.closest('form').setAttribute('data-waiver-activity-qa-move','1');
			return true;
		}
		return false;
	})()`, name)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil || !found {
		if err != nil {
			t.Fatalf("find move-up claim for %s: %v", name, err)
		}
		t.Fatalf("claim %s has no move-up form", name)
	}
	// A claim row can sit below the phone tab bar after a preceding pool
	// refresh. Center the exact button before clicking so the browser's
	// trusted hit test cannot land on fixed chrome at the bottom of the page.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var e=document.querySelector('[data-waiver-activity-qa-move="1"] button[type="submit"]');
		if (!e) return false;
		var html=document.documentElement;
		var body=document.body;
		var htmlBehavior=html.style.scrollBehavior;
		var bodyBehavior=body ? body.style.scrollBehavior : '';
		html.style.scrollBehavior='auto';
		if (body) body.style.scrollBehavior='auto';
		e.scrollIntoView({block:'center', inline:'nearest'});
		html.style.scrollBehavior=htmlBehavior;
		if (body) body.style.scrollBehavior=bodyBehavior;
		return true;
	})()`, &found)); err != nil || !found {
		if err != nil {
			t.Fatalf("center move-up claim for %s: %v", name, err)
		}
		t.Fatalf("move-up claim button for %s disappeared before click", name)
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond)); err != nil {
		t.Fatalf("wait for centered move-up claim for %s: %v", name, err)
	}
	var hit string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var e=document.querySelector('[data-waiver-activity-qa-move="1"] button[type="submit"]');
		if (!e) return 'missing';
		var r=e.getBoundingClientRect();
		var hit=document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
		if (hit === e || (hit && e.contains(hit))) return 'target';
		return (hit ? hit.tagName.toLowerCase() + '.' + String(hit.className || '') : 'none') +
			' at ' + Math.round(r.left) + ',' + Math.round(r.top) + ' ' + Math.round(r.width) + 'x' + Math.round(r.height);
	})()`, &hit)); err != nil {
		t.Fatalf("hit-test move claim for %s: %v", name, err)
	}
	if hit != "target" {
		t.Fatalf("move-up claim button for %s is covered after centered scroll: %s", name, hit)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-waiver-activity-qa-move="1"] button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click move claim for %s up: %v", name, err)
	}
	deadline := time.Now().Add(8 * time.Second)
	var order []string
	for time.Now().Before(deadline) {
		var names []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.waiver-claim-row')).map(function(row){
			var strong=row.querySelector('.pool-player__text strong');
			return strong ? (strong.textContent || '').trim() : '';
		}).filter(Boolean)`, &names)); err != nil {
			t.Fatalf("read claim order after moving %s: %v", name, err)
		}
		order = names
		if len(order) == 2 && order[0] == name {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("claim order did not converge after moving %s up: %v", name, order)
}

func waiverActivityQACancelClaim(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	var found bool
	script := fmt.Sprintf(`(function(){
		for (var row of document.querySelectorAll('.waiver-claim-row')) {
			var strong=row.querySelector('.pool-player__text strong');
			if (!strong || (strong.textContent || '').trim() !== %q) continue;
			var form=row.querySelector('form[action*="claim-cancel"]');
			if (!form) return false;
			form.setAttribute('data-waiver-activity-qa-cancel','1');
			return true;
		}
		return false;
	})()`, name)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil || !found {
		if err != nil {
			t.Fatalf("find cancel claim for %s: %v", name, err)
		}
		t.Fatalf("claim %s has no cancel form", name)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var e=document.querySelector('[data-waiver-activity-qa-cancel="1"] button[type="submit"]');
		if (!e) return false;
		var html=document.documentElement;
		var body=document.body;
		var htmlBehavior=html.style.scrollBehavior;
		var bodyBehavior=body ? body.style.scrollBehavior : '';
		html.style.scrollBehavior='auto';
		if (body) body.style.scrollBehavior='auto';
		e.scrollIntoView({block:'center', inline:'nearest'});
		html.style.scrollBehavior=htmlBehavior;
		if (body) body.style.scrollBehavior=bodyBehavior;
		return true;
	})()`, &found)); err != nil || !found {
		if err != nil {
			t.Fatalf("center cancel claim for %s: %v", name, err)
		}
		t.Fatalf("cancel claim button for %s disappeared before click", name)
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond)); err != nil {
		t.Fatalf("wait for centered cancel claim for %s: %v", name, err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-waiver-activity-qa-cancel="1"] button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("cancel claim for %s: %v", name, err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		script := fmt.Sprintf(`Array.from(document.querySelectorAll('.waiver-claim-row .pool-player__text strong')).some(function(e){return (e.textContent || '').trim() === %q;})`, name)
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &present)); err != nil {
			t.Fatalf("read claim order after cancelling %s: %v", name, err)
		}
		if !present {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("claim %s did not disappear after cancellation", name)
}

func waiverActivityQAAssertClaimOrder(t *testing.T, ctx context.Context, child *simChild, first, second string) {
	t.Helper()
	waiverActivityQANavigate(t, ctx, child, "/players#waivers")
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#waivers-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for waiver desk: %v", err)
	}
	claims := waiverActivityQAReadClaims(t, ctx)
	if len(claims) != 2 {
		t.Fatalf("fresh waiver desk has %d claims after filing two: %+v", len(claims), claims)
	}
	if claims[0].Name != first || claims[1].Name != second {
		t.Fatalf("initial claim order = %q, %q, want %q, %q", claims[0].Name, claims[1].Name, first, second)
	}
}

func waiverActivityQAAssertMovedAndCancelled(t *testing.T, ctx context.Context, child *simChild, moved, cancelled string) {
	t.Helper()
	waiverActivityQANavigate(t, ctx, child, "/players#waivers")
	claims := waiverActivityQAReadClaims(t, ctx)
	if len(claims) != 2 || claims[0].Name != moved || !strings.Contains(claims[0].Text, "Claim order 1 of 2") {
		t.Fatalf("claim order after move = %+v, want %s first at order 1 of 2", claims, moved)
	}
	waiverActivityQACancelClaim(t, ctx, cancelled)
	waiverActivityQANavigate(t, ctx, child, "/players#waivers")
	claims = waiverActivityQAReadClaims(t, ctx)
	if len(claims) != 1 || claims[0].Name != moved || strings.Contains(claims[0].Text, cancelled) {
		t.Fatalf("claims after cancelling %s = %+v, want only %s", cancelled, claims, moved)
	}
}

func waiverActivityQARunWaivers(t *testing.T, ctx context.Context, child *simChild, width, height int64) {
	t.Helper()
	waiverActivityQANavigate(t, ctx, child, "/admin#week-close")
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#admin-run-waivers-confirm`, chromedp.ByQuery)); err != nil {
		t.Fatalf("commissioner admin waiver control at %dx%d: %v", width, height, err)
	}
	var token string
	var buttonText string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('form[action*="run-waivers"] input[name="waiver_run_token"]').value`, &token),
		chromedp.Evaluate(`document.querySelector('form[action*="run-waivers"] button[type="submit"]').textContent`, &buttonText),
	); err != nil {
		t.Fatalf("read fresh waiver run control: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatal("commissioner waiver run form rendered an empty waiver_run_token")
	}
	if !strings.Contains(strings.ToUpper(strings.TrimSpace(buttonText)), "WAIVERS") {
		t.Fatalf("commissioner waiver run button = %q, want the real waiver-run action", buttonText)
	}
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#admin-run-waivers-confirm`, "RUN WAIVERS NOW", chromedp.ByQuery),
		chromedp.Click(`form[action*="run-waivers"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.gosx-toast, .notice-stack .flash-message`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("commissioner force-run waivers: %v", err)
	}
	body := strings.ToLower(waiverActivityQAReadBody(t, ctx))
	if !strings.Contains(body, "waiver run") {
		t.Fatalf("force-run response contains no waiver-run notice: %q", body)
	}
}

func waiverActivityQAReadActivityItems(t *testing.T, ctx context.Context) []string {
	t.Helper()
	var items []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.activity-item')).map(function(e){return (e.innerText || '').trim();})`, &items)); err != nil {
		t.Fatalf("read Activity rows: %v", err)
	}
	return items
}

func waiverActivityQAWaitActivity(t *testing.T, ctx context.Context, targetName string, beforeHref, expectedStamp string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var lastItems []string
	for time.Now().Before(deadline) {
		var href string
		var stamp string
		if err := chromedp.Run(ctx,
			chromedp.Location(&href),
			chromedp.WaitVisible(`#activity-feed-region`, chromedp.ByQuery),
			chromedp.Evaluate(`String(window.__waiverActivityQAAStamp || '')`, &stamp),
		); err != nil {
			t.Fatalf("read Activity location while waiting for poll: %v", err)
		}
		if href != beforeHref {
			t.Fatalf("Activity browser navigated during polling: before %q, after %q", beforeHref, href)
		}
		if stamp != expectedStamp {
			t.Fatalf("Activity document was replaced during polling: before stamp %q, after %q", expectedStamp, stamp)
		}
		lastItems = waiverActivityQAReadActivityItems(t, ctx)
		hasClaim, hasForce := false, false
		for _, item := range lastItems {
			lower := strings.ToLower(item)
			if strings.Contains(item, targetName) && strings.Contains(lower, "claims") {
				hasClaim = true
			}
			if strings.Contains(lower, "force-ran the waiver cycle") {
				hasForce = true
			}
		}
		if hasClaim && hasForce {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("Activity feed did not converge within 8s to claim %q and force-run event; rows=%q", targetName, lastItems)
}

func waiverActivityQAWaitPrivateWon(t *testing.T, ctx context.Context, winning waiverActivityQAPlayer, expectedHref, expectedStamp string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var lastReceipts []string
	var lastFocus waiverActivityQAFocus
	for time.Now().Before(deadline) {
		var href, stamp string
		if err := chromedp.Run(ctx,
			chromedp.Location(&href),
			chromedp.WaitVisible(`#waivers-content`, chromedp.ByQuery),
			chromedp.Evaluate(`String(window.__waiverActivityQABStamp || '')`, &stamp),
			chromedp.Evaluate(waiverActivityQAReadFocusScript, &lastFocus),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('.waiver-receipt-row')).map(function(e){return (e.innerText || '').trim();})`, &lastReceipts),
		); err != nil {
			t.Fatalf("read B Players region while waiting for private receipt: %v", err)
		}
		if href != expectedHref {
			t.Fatalf("B Players browser navigated during waiver convergence: before %q, after %q", expectedHref, href)
		}
		if stamp != expectedStamp {
			t.Fatalf("B Players document was replaced during waiver convergence: before stamp %q, after %q", expectedStamp, stamp)
		}
		for _, receipt := range lastReceipts {
			if strings.Contains(receipt, winning.Name) && strings.Contains(strings.ToUpper(receipt), "WON") {
				if !lastFocus.valid(true) {
					t.Fatalf("B Players private WON receipt converged without retained managed #waivers focus: focus=%+v", lastFocus)
				}
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("B Players region did not converge within 8s to private WON receipt for %s: receipts=%q focus=%+v", winning.Name, lastReceipts, lastFocus)
}

func waiverActivityQAAssertReceiptsAndRoster(t *testing.T, bCtx, aCtx context.Context, child *simChild, winning, cancelled, bDrop waiverActivityQAPlayer, bBeforeHref, bStamp string, width, height int64) {
	t.Helper()
	// First observe the existing authenticated B Players document converge in
	// place. Its fresh server render below is a second persistence check, not a
	// substitute for proving that the existing GoSX region did not navigate.
	waiverActivityQAWaitPrivateWon(t, bCtx, winning, bBeforeHref, bStamp)
	// The B context is the authenticated manager who filed the surviving
	// claim. Its fresh server render must show a private WON receipt and the
	// winning player in the actual roster, with the named drop gone.
	waiverActivityQANavigate(t, bCtx, child, "/players#waivers")
	if err := chromedp.Run(bCtx, chromedp.WaitVisible(`#waivers-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for B waiver receipt region at %dx%d: %v", width, height, err)
	}
	var receipts []string
	if err := chromedp.Run(bCtx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.waiver-receipt-row')).map(function(e){return (e.innerText || '').trim();})`, &receipts)); err != nil {
		t.Fatalf("read B private receipts: %v", err)
	}
	foundWon := false
	for _, receipt := range receipts {
		if strings.Contains(receipt, winning.Name) && strings.Contains(strings.ToUpper(receipt), "WON") {
			foundWon = true
		}
	}
	if !foundWon {
		t.Fatalf("B private receipts do not contain WON %s: %q", winning.Name, receipts)
	}
	for _, receipt := range receipts {
		if strings.Contains(receipt, cancelled.Name) {
			t.Fatalf("cancelled claim %s produced a B receipt: %q", cancelled.Name, receipt)
		}
	}

	waiverActivityQANavigate(t, bCtx, child, "/team")
	if err := chromedp.Run(bCtx, chromedp.WaitVisible(`.team-lineup-region`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for B Team after waiver run: %v", err)
	}
	var rosterIDs []string
	if err := chromedp.Run(bCtx, chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-player-id],[data-gosx-transfer-source]')).map(function(e){return e.getAttribute('data-player-id') || e.getAttribute('data-gosx-transfer-source') || '';}).filter(Boolean)`, &rosterIDs)); err != nil {
		t.Fatalf("read B fresh roster identities: %v", err)
	}
	contains := func(want string) bool {
		for _, id := range rosterIDs {
			if id == want {
				return true
			}
		}
		return false
	}
	if !contains(winning.ID) {
		t.Fatalf("B fresh Team roster omitted winning player %s (%s): %v", winning.Name, winning.ID, rosterIDs)
	}
	if contains(bDrop.ID) {
		t.Fatalf("B fresh Team roster still contains named drop %s (%s): %v", bDrop.Name, bDrop.ID, rosterIDs)
	}

	// A is a different manager. Their Players page must not disclose B's
	// private receipt or the cancelled claim's outcome.
	waiverActivityQANavigate(t, aCtx, child, "/players#waivers")
	if err := chromedp.Run(aCtx, chromedp.WaitVisible(`#waivers-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait for A waiver desk: %v", err)
	}
	var aReceipts []string
	if err := chromedp.Run(aCtx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.waiver-receipt-row')).map(function(e){return (e.innerText || '').trim();})`, &aReceipts)); err != nil {
		t.Fatalf("read A private receipts: %v", err)
	}
	for _, receipt := range aReceipts {
		if strings.Contains(receipt, winning.Name) || strings.Contains(strings.ToUpper(receipt), "WON") {
			t.Fatalf("A private receipt view disclosed B's outcome %q: %q", winning.Name, receipt)
		}
	}
}

func waiverActivityQAAssertViewport(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	const script = `(function(){
		var clipped=[];
		document.querySelectorAll('a[href],button,input,select,summary').forEach(function(e){
			var style=getComputedStyle(e), r=e.getBoundingClientRect();
			if(style.display==='none'||style.visibility==='hidden'||r.width<=0||r.height<=0) return;
			if(r.left < -1 || r.right > window.innerWidth + 1) clipped.push((e.getAttribute('aria-label') || e.innerText || e.name || e.tagName).trim());
		});
		return {scrollWidth:document.documentElement.scrollWidth, innerWidth:window.innerWidth, clipped:clipped.slice(0,12)};
	})()`
	var metrics waiverActivityQAMetrics
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &metrics)); err != nil {
		t.Fatalf("read %s viewport geometry: %v", label, err)
	}
	if metrics.ScrollWidth > metrics.InnerWidth {
		t.Fatalf("%s has horizontal overflow: scrollWidth=%d innerWidth=%d", label, metrics.ScrollWidth, metrics.InnerWidth)
	}
	if len(metrics.Clipped) > 0 {
		t.Fatalf("%s has clipped visible controls: %v", label, metrics.Clipped)
	}
}

// TestBrowserWaiverActivityJourney exercises the real post-draft waiver
// workflow at a phone and desktop viewport. It intentionally uses two
// browser sessions for manager A and B plus an independent Activity session:
// claims, filing-order changes, cancellation, the commissioner run, receipts,
// and roster persistence are all checked through fresh server renders. The
// Activity tab is opened before the run and must converge through its native
// four-second GoSX region poll without a hard navigation.
func TestBrowserWaiverActivityJourney(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	root := teamTransferBrowserAppRoot(t)
	chrome := chromePath(t)
	for _, viewport := range []waiverActivityQAViewport{
		{name: "phone", width: 390, height: 844},
		{name: "desktop", width: 1440, height: 900},
	} {
		viewport := viewport
		t.Run(viewport.name, func(t *testing.T) {
			child, league := startWaiverActivityQALeague(t, root)
			a := league.bots[0]
			b := league.bots[1]

			aCtx := newBrowserContext(t, chrome)
			signInBrowserSeat(t, aCtx, child, a, "/team", viewport.width, viewport.height)
			aBench := waiverActivityQAReadBench(t, aCtx)
			// A's first drop uses GoSX managed transport.
			waiverActivityQADrop(t, aCtx, child, aBench[0], true, viewport.width, viewport.height)

			// A's second drop takes the ordinary native form path, with page
			// script execution disabled before sign-in and submission.
			aNativeCtx := newBrowserContext(t, chrome)
			if err := chromedp.Run(aNativeCtx, emulation.SetScriptExecutionDisabled(true)); err != nil {
				t.Fatalf("disable GoSX for A native drop: %v", err)
			}
			signInBrowserSeat(t, aNativeCtx, child, a, "/players?avail=all&q="+url.QueryEscape(aBench[1].Name), viewport.width, viewport.height)
			if err := chromedp.Run(aNativeCtx, chromedp.WaitVisible(`form[action*="player-drop"]`, chromedp.ByQuery)); err != nil {
				t.Fatalf("A native drop form for %s: %v", aBench[1].Name, err)
			}
			waiverActivityQAClickNativeConfirm(t, aNativeCtx, "player-drop", "drop-player")
			waiverActivityQAAssertOnWaivers(t, aCtx, child, aBench[0])
			waiverActivityQAAssertOnWaivers(t, aCtx, child, aBench[1])

			bCtx := newBrowserContext(t, chrome)
			signInBrowserSeat(t, bCtx, child, b, "/team", viewport.width, viewport.height)
			bBench := waiverActivityQAReadBench(t, bCtx)
			if len(bBench) < 2 {
				t.Fatalf("B rendered fewer than two bench drop candidates: %+v", bBench)
			}

			// B files two full-roster claims, one managed and one native. Each
			// select is the app's own drop_id option and each confirmation is
			// the exact claim-drop-player guard.
			waiverActivityQAClaim(t, bCtx, child, aBench[0], bBench[0], true, viewport.width, viewport.height)
			bNativeCtx := newBrowserContext(t, chrome)
			signInBrowserSeat(t, bNativeCtx, child, b, "/players?avail=all&q="+url.QueryEscape(aBench[1].Name), viewport.width, viewport.height)
			waiverActivityQAClaim(t, bNativeCtx, child, aBench[1], bBench[1], false, viewport.width, viewport.height)
			waiverActivityQANavigate(t, bCtx, child, "/players#waivers")
			waiverActivityQAAssertClaimOrder(t, bCtx, child, aBench[0].Name, aBench[1].Name)
			waiverActivityQAMoveClaimUp(t, bCtx, aBench[1].Name)
			waiverActivityQAAssertMovedAndCancelled(t, bCtx, child, aBench[1].Name, aBench[0].Name)

			// A separate Activity browser is already open before the run, so
			// its post-run rows can only arrive from the managed 4s region poll.
			activityCtx := newBrowserContext(t, chrome)
			signInBrowserSeat(t, activityCtx, child, a, "/activity", viewport.width, viewport.height)
			waiverActivityQAAssertViewport(t, activityCtx, viewport.name+" Activity before run")
			var activityBefore string
			const activityStamp = "waiver-activity-pre-run"
			if err := chromedp.Run(activityCtx,
				chromedp.Location(&activityBefore),
				chromedp.Evaluate(`window.__waiverActivityQAAStamp = "waiver-activity-pre-run";`, nil),
			); err != nil {
				t.Fatalf("read pre-run Activity URL: %v", err)
			}
			var bBefore string
			const bStamp = "waiver-b-pre-run"
			// Re-navigate immediately before the commissioner action and assert
			// the real GoSX hash target is focused. Do not blur it: the live
			// private receipt must converge in this same document while this
			// managed region root remains the active element.
			waiverActivityQANavigate(t, bCtx, child, "/players#waivers")
			waiverActivityQACaptureManagedWaiverFocus(t, bCtx, "B Players before commissioner run")
			if err := chromedp.Run(bCtx,
				chromedp.Location(&bBefore),
				chromedp.Evaluate(`window.__waiverActivityQABStamp = "waiver-b-pre-run";`, nil),
			); err != nil {
				t.Fatalf("read pre-run B Players URL: %v", err)
			}

			advanceClock(t, child.URL, 76*time.Hour)
			adminCtx := newBrowserContext(t, chrome)
			signInBrowserSeat(t, adminCtx, child, league.commish, "/admin#week-close", viewport.width, viewport.height)
			waiverActivityQARunWaivers(t, adminCtx, child, viewport.width, viewport.height)
			waiverActivityQAWaitActivity(t, activityCtx, aBench[1].Name, activityBefore, activityStamp)
			waiverActivityQAAssertViewport(t, activityCtx, viewport.name+" Activity after run")

			waiverActivityQAAssertReceiptsAndRoster(t, bCtx, aCtx, child, aBench[1], aBench[0], bBench[1], bBefore, bStamp, viewport.width, viewport.height)
		})
	}
}

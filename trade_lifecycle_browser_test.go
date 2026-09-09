package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// tradeLifecycleQAPlayer is the small stable part of a rendered roster
// option that this acceptance test needs.  The IDs come from the real
// compose form; no player or roster is fabricated in the browser.
type tradeLifecycleQAPlayer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position string `json:"position"`
}

type tradeLifecycleQAComposer struct {
	Give []tradeLifecycleQAPlayer `json:"give"`
	Get  []tradeLifecycleQAPlayer `json:"get"`
}

type tradeLifecycleQAViewport struct {
	name   string
	width  int64
	height int64
}

func tradeLifecycleQANavigate(t *testing.T, ctx context.Context, child *simChild, path string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+path),
		chromedp.WaitVisible(`#main-content`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate to %s: %v", path, err)
	}
}

func tradeLifecycleQAWaitText(t *testing.T, ctx context.Context, selector, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	var last string
	for time.Now().Before(deadline) {
		expression := fmt.Sprintf(`(function(){var e=document.querySelector(%q);return e ? (e.innerText || e.textContent || '') : '';})()`, selector)
		if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &last)); err != nil {
			t.Fatalf("read %s while waiting for %q: %v", selector, want, err)
		}
		if strings.Contains(last, want) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not contain %q within %s; last text=%q", selector, want, within, last)
}

func tradeLifecycleQAWaitTradeArticle(t *testing.T, ctx context.Context, section, firstAsset, secondAsset, status string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	selector := section + ` article`
	var found bool
	for time.Now().Before(deadline) {
		script := fmt.Sprintf(`(function(){
  return Array.from(document.querySelectorAll(%q)).some(function(article) {
    var text = article.innerText || article.textContent || '';
    return text.indexOf(%q) >= 0 && text.indexOf(%q) >= 0 && text.indexOf(%q) >= 0;
  });
})()`, selector, firstAsset, secondAsset, status)
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
			t.Fatalf("read %s trade article while waiting for %q/%q/%q: %v", section, firstAsset, secondAsset, status, err)
		}
		if found {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not contain a trade article for %q and %q with status %q within %s", section, firstAsset, secondAsset, status, within)
}

func tradeLifecycleQAWaitOpenOffer(t *testing.T, ctx context.Context, firstAsset, secondAsset string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	selector := `#outbox article`
	var offerID string
	for time.Now().Before(deadline) {
		script := fmt.Sprintf(`(function(){
  var article = Array.from(document.querySelectorAll(%q)).find(function(e) {
    var text = e.innerText || e.textContent || '';
    return text.indexOf(%q) >= 0 && text.indexOf(%q) >= 0 && text.indexOf('Open') >= 0;
  });
  if (!article) return '';
  var input = article.querySelector('form[action*="trade-withdraw"] input[name="offer_id"]');
  return input ? (input.value || '') : '';
})()`, selector, firstAsset, secondAsset)
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &offerID)); err != nil {
			t.Fatalf("read open outbox offer for %q/%q: %v", firstAsset, secondAsset, err)
		}
		if offerID != "" {
			return offerID
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("outbox did not contain an Open trade article for %q and %q within %s", firstAsset, secondAsset, within)
	return ""
}

func tradeLifecycleQAAssertNoOfferActions(t *testing.T, ctx context.Context, section, offerID string) {
	t.Helper()
	selector := section + ` form input[name="offer_id"]`
	script := fmt.Sprintf(`Array.from(document.querySelectorAll(%q)).some(function(input){return input.value === %q;})`, selector, offerID)
	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
		t.Fatalf("check expired offer actions in %s: %v", section, err)
	}
	if found {
		t.Fatalf("offer %s still has an actionable form in %s", offerID, section)
	}
}

func tradeLifecycleQASectionText(t *testing.T, ctx context.Context, selector string) string {
	t.Helper()
	var text string
	expression := fmt.Sprintf(`(function(){var e=document.querySelector(%q);return e ? (e.innerText || e.textContent || '') : '';})()`, selector)
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &text)); err != nil {
		t.Fatalf("read %s: %v", selector, err)
	}
	return text
}

func tradeLifecycleQAReadComposer(t *testing.T, ctx context.Context) tradeLifecycleQAComposer {
	t.Helper()
	const script = `(function(){
  function options(name) {
    return Array.from(document.querySelectorAll('form[action*="trade-propose"] input[type="checkbox"][name="' + name + '"]')).map(function(input) {
      var label = input.closest('label');
      var text = label ? (label.innerText || label.textContent || '').trim() : '';
      var match = /\(([A-Z/]+)\)/.exec(text);
      var nameText = match ? text.slice(0, match.index).trim() : text;
      return {id: input.value || '', name: nameText, position: match ? match[1] : ''};
    }).filter(function(option){return option.id !== '' && option.position !== '';});
  }
  return {give: options('give'), get: options('get')};
})()`
	var composer tradeLifecycleQAComposer
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &composer)); err != nil {
		t.Fatalf("read rendered trade composer options: %v", err)
	}
	if len(composer.Give) == 0 || len(composer.Get) == 0 {
		t.Fatalf("trade composer rendered no usable options: %+v", composer)
	}
	return composer
}

func tradeLifecycleQAChoosePair(t *testing.T, composer tradeLifecycleQAComposer) (tradeLifecycleQAPlayer, tradeLifecycleQAPlayer) {
	t.Helper()
	for _, give := range composer.Give {
		for _, get := range composer.Get {
			if give.Position == get.Position && give.ID != get.ID {
				return give, get
			}
		}
	}
	t.Fatalf("trade fixture has no same-position pair: %+v", composer)
	return tradeLifecycleQAPlayer{}, tradeLifecycleQAPlayer{}
}

func tradeLifecycleQASelectPair(t *testing.T, ctx context.Context, give, get tradeLifecycleQAPlayer, note string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(fmt.Sprintf(`form[action*="trade-propose"] input[name="give"][value=%q]`, give.ID)),
		chromedp.ScrollIntoView(fmt.Sprintf(`form[action*="trade-propose"] input[name="get"][value=%q]`, get.ID)),
		chromedp.SetValue(`form[action*="trade-propose"] textarea[name="note"]`, note, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("select trade assets %s for %s: %v", give.Name, get.Name, err)
	}
	tradeLifecycleQAEnsureChecked(t, ctx, fmt.Sprintf(`form[action*="trade-propose"] input[name="give"][value=%q]`, give.ID))
	tradeLifecycleQAEnsureChecked(t, ctx, fmt.Sprintf(`form[action*="trade-propose"] input[name="get"][value=%q]`, get.ID))
}

func tradeLifecycleQAMarkForm(t *testing.T, ctx context.Context, action, note, marker string) {
	t.Helper()
	script := fmt.Sprintf(`(function(){
  var forms = Array.from(document.querySelectorAll('form[action*=%q]'));
  for (var i=0; i<forms.length; i++) {
    var article = forms[i].closest('article');
    var text = article ? (article.innerText || article.textContent || '') : '';
    if (text.indexOf(%q) < 0) continue;
    forms[i].setAttribute('data-trade-lifecycle-form', %q);
    return true;
  }
  return false;
})()`, action, note, marker)
	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
		t.Fatalf("find %s form for note %q: %v", action, note, err)
	}
	if !found {
		t.Fatalf("no %s form rendered for note %q", action, note)
	}
}

func tradeLifecycleQAMarkFirstForm(t *testing.T, ctx context.Context, action, marker string) {
	t.Helper()
	selector := fmt.Sprintf(`form[action*=%q]`, action)
	script := fmt.Sprintf(`(function(){
  var form = document.querySelector(%q);
  if (!form) return false;
  form.setAttribute('data-trade-lifecycle-form', %q);
  return true;
})()`, selector, marker)
	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
		t.Fatalf("find first %s form: %v", action, err)
	}
	if !found {
		t.Fatalf("no %s form rendered", action)
	}
}

func tradeLifecycleQAMarkCounterDetails(t *testing.T, ctx context.Context, note, marker string) {
	t.Helper()
	script := fmt.Sprintf(`(function(){
  var article = Array.from(document.querySelectorAll('#inbox article')).find(function(e) {
    return e.querySelector('form[action*="trade-counter"]') && ((e.innerText || e.textContent || '').indexOf(%q) >= 0);
  });
  var details = article ? article.querySelector('details.trade-counter-details') : null;
  if (!details) return false;
  details.setAttribute('data-trade-lifecycle-details', %q);
  return true;
})()`, note, marker)
	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
		t.Fatalf("find counter details for note %q: %v", note, err)
	}
	if !found {
		t.Fatalf("no counter details rendered for note %q", note)
	}
}

func tradeLifecycleQAEnsureChecked(t *testing.T, ctx context.Context, selector string) {
	t.Helper()
	script := fmt.Sprintf(`(function(){
  var input = document.querySelector(%q);
  if (!input) return false;
  if (!input.checked) input.click();
  return input.checked === true;
})()`, selector)
	var checked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &checked)); err != nil {
		t.Fatalf("check %s: %v", selector, err)
	}
	if !checked {
		t.Fatalf("input %s was not checked", selector)
	}
}

func tradeLifecycleQAMarkFormByOffer(t *testing.T, ctx context.Context, action, offerID, marker string) {
	t.Helper()
	script := fmt.Sprintf(`(function(){
  var forms = Array.from(document.querySelectorAll('form[action*=%q]'));
  for (var i=0; i<forms.length; i++) {
    var input = forms[i].querySelector('input[name="offer_id"]');
    if (!input || input.value !== %q) continue;
    forms[i].setAttribute('data-trade-lifecycle-form', %q);
    return true;
  }
  return false;
})()`, action, offerID, marker)
	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
		t.Fatalf("find %s form for offer %s: %v", action, offerID, err)
	}
	if !found {
		t.Fatalf("no %s form rendered for offer %s", action, offerID)
	}
}

func tradeLifecycleQAFormOfferID(t *testing.T, ctx context.Context, action, note string) string {
	t.Helper()
	script := fmt.Sprintf(`(function(){
  var forms = Array.from(document.querySelectorAll('form[action*=%q]'));
  for (var i=0; i<forms.length; i++) {
    var article = forms[i].closest('article');
    var text = article ? (article.innerText || article.textContent || '') : '';
    if (text.indexOf(%q) < 0) continue;
    var input = forms[i].querySelector('input[name="offer_id"]');
    if (input && input.value) return input.value;
  }
  return '';
})()`, action, note)
	var offerID string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &offerID)); err != nil {
		t.Fatalf("read %s offer ID for note %q: %v", action, note, err)
	}
	if offerID == "" {
		t.Fatalf("%s form for note %q had no offer ID", action, note)
	}
	return offerID
}

func tradeLifecycleQAFirstFormOfferID(t *testing.T, ctx context.Context, action string) string {
	t.Helper()
	selector := fmt.Sprintf(`form[action*=%q]`, action)
	script := fmt.Sprintf(`(function(){
  var form = document.querySelector(%q);
  var input = form ? form.querySelector('input[name="offer_id"]') : null;
  return input ? (input.value || '') : '';
})()`, selector)
	var offerID string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &offerID)); err != nil {
		t.Fatalf("read first %s offer ID: %v", action, err)
	}
	if offerID == "" {
		t.Fatalf("first %s form had no offer ID", action)
	}
	return offerID
}

func tradeLifecycleQAClickConfirm(t *testing.T, ctx context.Context, marker, confirmation string) {
	t.Helper()
	form := `[data-trade-lifecycle-form="` + marker + `"]`
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(form+` details > summary`),
		chromedp.Click(form+` details > summary`, chromedp.ByQuery),
		chromedp.Click(form+` input[name="confirmation"][value="`+confirmation+`"]`, chromedp.ByQuery),
		chromedp.Click(form+` button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit %s confirmation: %v", marker, err)
	}
}

func tradeLifecycleQAProposeManaged(t *testing.T, ctx context.Context, child *simChild, toTeam string, give, get tradeLifecycleQAPlayer, note string, width, height int64) {
	t.Helper()
	tradeLifecycleQANavigate(t, ctx, child, "/trades?counterparty="+url.QueryEscape(toTeam))
	composer := tradeLifecycleQAReadComposer(t, ctx)
	// Re-read the pair on the page that will actually submit it. This guards
	// against accidentally posting an ID from a stale server render.
	validGive, validGet := tradeLifecycleQAChoosePair(t, composer)
	if validGive.Position != give.Position || validGet.Position != get.Position {
		t.Fatalf("trade composer pair changed at %dx%d: selected %+v/%+v, page has %+v/%+v", width, height, give, get, validGive, validGet)
	}
	tradeLifecycleQASelectPair(t, ctx, give, get, note)
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(`form[action*="trade-propose"] button[type="submit"]`),
		chromedp.Click(`form[action*="trade-propose"] button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("managed propose %s for %s: %v", give.Name, get.Name, err)
	}
	tradeLifecycleQAWaitOpenOffer(t, ctx, give.Name, get.Name, 12*time.Second)
}

func tradeLifecycleQACounterManaged(t *testing.T, ctx context.Context, child *simChild, originalNote string, give, get tradeLifecycleQAPlayer, counterNote string, width, height int64) {
	t.Helper()
	tradeLifecycleQANavigate(t, ctx, child, "/trades")
	tradeLifecycleQAWaitText(t, ctx, `#inbox`, originalNote, 12*time.Second)
	tradeLifecycleQAMarkCounterDetails(t, ctx, originalNote, "counter")
	form := `[data-trade-lifecycle-details="counter"]`
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(form+` > summary`),
		chromedp.Click(form+` > summary`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("managed counter at %dx%d: %v", width, height, err)
	}
	tradeLifecycleQAEnsureChecked(t, ctx, fmt.Sprintf(form+` input[name="give"][value=%q]`, give.ID))
	tradeLifecycleQAEnsureChecked(t, ctx, fmt.Sprintf(form+` input[name="get"][value=%q]`, get.ID))
	if err := chromedp.Run(ctx,
		chromedp.SetValue(form+` textarea[name="note"]`, counterNote, chromedp.ByQuery),
		chromedp.Click(form+` button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit managed counter at %dx%d: %v", width, height, err)
	}
	tradeLifecycleQAWaitTradeArticle(t, ctx, `#outbox`, give.Name, get.Name, "Open", 12*time.Second)
	tradeLifecycleQAWaitText(t, ctx, `#history`, originalNote, 12*time.Second)
	if !strings.Contains(tradeLifecycleQASectionText(t, ctx, `#history`), "Countered") {
		t.Fatalf("counter response did not mark original offer Countered: %q", originalNote)
	}
}

func tradeLifecycleQAAcceptManaged(t *testing.T, ctx context.Context, child *simChild, counterNote, firstAsset, secondAsset string) string {
	t.Helper()
	tradeLifecycleQANavigate(t, ctx, child, "/trades")
	tradeLifecycleQAWaitText(t, ctx, `#inbox`, counterNote, 12*time.Second)
	offerID := tradeLifecycleQAFormOfferID(t, ctx, `trade-accept`, counterNote)
	tradeLifecycleQAMarkForm(t, ctx, `trade-accept`, counterNote, "accept")
	tradeLifecycleQAClickConfirm(t, ctx, "accept", "accept-trade")
	tradeLifecycleQAWaitTradeArticle(t, ctx, `#pending-review`, firstAsset, secondAsset, "Accepted", 12*time.Second)
	if !strings.Contains(tradeLifecycleQASectionText(t, ctx, `#pending-review`), "Accepted") {
		t.Fatalf("accepted counter %s did not enter pending review", offerID)
	}
	return offerID
}

func tradeLifecycleQAApproveManaged(t *testing.T, ctx context.Context, child *simChild, offerID, counterNote string) {
	t.Helper()
	tradeLifecycleQANavigate(t, ctx, child, "/trades#review")
	tradeLifecycleQAWaitText(t, ctx, `#review`, "Approve", 12*time.Second)
	tradeLifecycleQAMarkFormByOffer(t, ctx, `trade-approve`, offerID, "approve")
	tradeLifecycleQAClickConfirm(t, ctx, "approve", "approve-trade")
	tradeLifecycleQAWaitText(t, ctx, `#history`, counterNote, 12*time.Second)
	tradeLifecycleQAWaitText(t, ctx, `#history`, "Executed", 12*time.Second)
}

func tradeLifecycleQAReadRosterIDs(t *testing.T, ctx context.Context) map[string]bool {
	t.Helper()
	var raw []string
	const script = `Array.from(document.querySelectorAll('[data-player-id],[data-gosx-transfer-source]')).map(function(e){return e.getAttribute('data-player-id') || e.getAttribute('data-gosx-transfer-source') || '';}).filter(Boolean)`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw)); err != nil {
		t.Fatalf("read rendered roster identities: %v", err)
	}
	ids := make(map[string]bool, len(raw))
	for _, id := range raw {
		ids[id] = true
	}
	if len(ids) < 17 {
		t.Fatalf("fresh Team render exposed %d roster identities, want the flagship 17-player roster: %v", len(ids), raw)
	}
	return ids
}

func tradeLifecycleQAAssertSwap(t *testing.T, label string, before, after map[string]bool, give, get tradeLifecycleQAPlayer) {
	t.Helper()
	if !before[give.ID] || before[get.ID] {
		t.Fatalf("%s baseline roster did not have only give %s (get %s was present=%t)", label, give.Name, get.Name, before[get.ID])
	}
	if len(before) != len(after) {
		t.Fatalf("%s roster identity count changed: before=%d after=%d", label, len(before), len(after))
	}
	if !after[get.ID] || after[give.ID] {
		t.Fatalf("%s roster after trade has give=%t get=%t for %s -> %s; identities=%v", label, after[give.ID], after[get.ID], give.Name, get.Name, after)
	}
}

func tradeLifecycleQAAssertSameRoster(t *testing.T, label string, want, got map[string]bool) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s roster identity count changed: before=%d after=%d (%v)", label, len(want), len(got), got)
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("%s roster lost %s: want=%v got=%v", label, id, want, got)
		}
	}
}

func tradeLifecycleQAWaitActivity(t *testing.T, ctx context.Context, beforeHref, stamp, teamA, teamB, give, get string) {
	t.Helper()
	deadline := time.Now().Add(16 * time.Second)
	var last []string
	for time.Now().Before(deadline) {
		var href, gotStamp string
		if err := chromedp.Run(ctx,
			chromedp.Location(&href),
			chromedp.Evaluate(`String(window.__tradeLifecycleQAStamp || '')`, &gotStamp),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('.activity-item')).map(function(e){return (e.innerText || '').trim();})`, &last),
		); err != nil {
			t.Fatalf("read Activity convergence state: %v", err)
		}
		if href != beforeHref {
			t.Fatalf("Activity hard-navigated during trade convergence: before=%q after=%q", beforeHref, href)
		}
		if gotStamp != stamp {
			t.Fatalf("Activity document was replaced during trade convergence: before stamp=%q after=%q", stamp, gotStamp)
		}
		tradeSeen, commissionerSeen := false, false
		for _, item := range last {
			lower := strings.ToLower(item)
			if strings.Contains(lower, "gives") && strings.Contains(item, give) && strings.Contains(item, get) && strings.Contains(item, teamA) && strings.Contains(item, teamB) {
				tradeSeen = true
			}
			if strings.Contains(lower, "commissioner") && strings.Contains(lower, "approved the trade between") && strings.Contains(item, teamA) && strings.Contains(item, teamB) {
				commissionerSeen = true
			}
		}
		if tradeSeen && commissionerSeen {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("Activity did not converge to trade and commissioner approval within 16s; rows=%q", last)
}

func tradeLifecycleQASetActivityStamp(t *testing.T, ctx context.Context) string {
	t.Helper()
	const stamp = "trade-lifecycle-activity-before-approval"
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__tradeLifecycleQAStamp = "`+stamp+`"`, nil)); err != nil {
		t.Fatalf("set Activity document stamp: %v", err)
	}
	var href string
	if err := chromedp.Run(ctx, chromedp.Location(&href)); err != nil {
		t.Fatalf("read Activity URL before approval: %v", err)
	}
	return href
}

func tradeLifecycleQAAssertViewport(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	var metrics struct {
		ScrollWidth int64    `json:"scrollWidth"`
		InnerWidth  int64    `json:"innerWidth"`
		Clipped     []string `json:"clipped"`
	}
	const script = `(function(){
  var clipped=[];
  document.querySelectorAll('a[href],button,input,select,summary').forEach(function(e){
    var style=getComputedStyle(e), r=e.getBoundingClientRect();
    if(style.display==='none'||style.visibility==='hidden'||r.width<=0||r.height<=0) return;
    if(r.left < -1 || r.right > window.innerWidth + 1) clipped.push((e.getAttribute('aria-label') || e.innerText || e.name || e.tagName).trim());
  });
  return {scrollWidth:document.documentElement.scrollWidth, innerWidth:window.innerWidth, clipped:clipped.slice(0,12)};
})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &metrics)); err != nil {
		t.Fatalf("read %s geometry: %v", label, err)
	}
	if metrics.ScrollWidth > metrics.InnerWidth {
		t.Fatalf("%s has horizontal overflow: scrollWidth=%d innerWidth=%d", label, metrics.ScrollWidth, metrics.InnerWidth)
	}
	if len(metrics.Clipped) > 0 {
		t.Fatalf("%s has clipped visible controls: %v", label, metrics.Clipped)
	}
}

func tradeLifecycleQANativeWithdraw(t *testing.T, child *simChild, managerEmail, managerName, recipientEmail, recipientName, toTeam string, note string, viewport tradeLifecycleQAViewport) {
	t.Helper()
	ctx := newBrowserContext(t, chromePath(t))
	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable GoSX for native trade fallback: %v", err)
	}
	// Build a normal simulator bot solely for sign-in identity; the browser
	// still submits the real native forms, with no action runtime installed.
	bot := newSimBotForTradeLifecycle(child, managerEmail, managerName)
	signInBrowserSeat(t, ctx, child, bot, "/team", viewport.width, viewport.height)
	beforeManager := tradeLifecycleQAReadRosterIDs(t, ctx)
	recipientCtx := newBrowserContext(t, chromePath(t))
	recipient := newSimBotForTradeLifecycle(child, recipientEmail, recipientName)
	signInBrowserSeat(t, recipientCtx, child, recipient, "/team", viewport.width, viewport.height)
	beforeRecipient := tradeLifecycleQAReadRosterIDs(t, recipientCtx)
	tradeLifecycleQANavigate(t, ctx, child, "/trades?counterparty="+url.QueryEscape(toTeam))
	composer := tradeLifecycleQAReadComposer(t, ctx)
	give, get := tradeLifecycleQAChoosePair(t, composer)
	tradeLifecycleQASelectPair(t, ctx, give, get, note)
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(`form[action*="trade-propose"] button[type="submit"]`),
		chromedp.Click(`form[action*="trade-propose"] button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("native trade proposal: %v", err)
	}
	tradeLifecycleQAWaitOpenOffer(t, ctx, give.Name, get.Name, 12*time.Second)
	tradeLifecycleQAMarkFirstForm(t, ctx, `trade-withdraw`, "withdraw")
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(`[data-trade-lifecycle-form="withdraw"] button[type="submit"]`),
		chromedp.Click(`[data-trade-lifecycle-form="withdraw"] button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("native trade withdrawal: %v", err)
	}
	tradeLifecycleQAWaitText(t, ctx, `#history`, note, 12*time.Second)
	if !strings.Contains(tradeLifecycleQASectionText(t, ctx, `#history`), "Withdrawn") {
		t.Fatalf("native withdrawal did not render Withdrawn history for %q", note)
	}
	tradeLifecycleQANavigate(t, ctx, child, "/team")
	tradeLifecycleQAAssertSameRoster(t, "native withdrawal manager roster", beforeManager, tradeLifecycleQAReadRosterIDs(t, ctx))

	tradeLifecycleQANavigate(t, recipientCtx, child, "/trades")
	tradeLifecycleQAWaitText(t, recipientCtx, `#history`, note, 12*time.Second)
	if !strings.Contains(tradeLifecycleQASectionText(t, recipientCtx, `#history`), "Withdrawn") {
		t.Fatalf("recipient history did not render Withdrawn for %q", note)
	}
	if inbox := tradeLifecycleQASectionText(t, recipientCtx, `#inbox`); strings.Contains(inbox, note) {
		t.Fatalf("withdrawn offer remained actionable in recipient inbox: %q", inbox)
	}
	tradeLifecycleQANavigate(t, recipientCtx, child, "/team")
	tradeLifecycleQAAssertSameRoster(t, "native withdrawal recipient roster", beforeRecipient, tradeLifecycleQAReadRosterIDs(t, recipientCtx))
}

// newSimBotForTradeLifecycle gives the native-only browser fallback the same
// test identity as an existing seated manager without exposing simulator
// internals or posting the mutation over HTTP.
func newSimBotForTradeLifecycle(child *simChild, email, name string) *draft.Bot {
	return draft.New(child.URL, email, name)
}

func TestBrowserTradeDeskLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	for _, viewport := range []tradeLifecycleQAViewport{
		{name: "phone", width: 390, height: 844},
		{name: "desktop", width: 1440, height: 900},
	} {
		viewport := viewport
		t.Run(viewport.name, func(t *testing.T) {
			child, league := startTeamTransferFlagshipDraftedChild(t)
			setClockAbsolute(t, child.URL, time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC))
			a := league.bots[0]
			b := league.bots[1]
			viewer := league.bots[2]

			aCtx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, aCtx, child, a, "/team", viewport.width, viewport.height)
			beforeA := tradeLifecycleQAReadRosterIDs(t, aCtx)
			tradeLifecycleQANavigate(t, aCtx, child, "/trades?counterparty="+url.QueryEscape(b.TeamID))
			give, get := tradeLifecycleQAChoosePair(t, tradeLifecycleQAReadComposer(t, aCtx))

			bCtx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, bCtx, child, b, "/team", viewport.width, viewport.height)
			beforeB := tradeLifecycleQAReadRosterIDs(t, bCtx)
			if !beforeA[give.ID] || !beforeB[get.ID] {
				t.Fatalf("selected pair is not on the expected rosters: A has give=%t, B has get=%t", beforeA[give.ID], beforeB[get.ID])
			}

			primaryNote := "trade-lifecycle-primary-" + viewport.name
			tradeLifecycleQAProposeManaged(t, aCtx, child, b.TeamID, give, get, primaryNote, viewport.width, viewport.height)

			counterCtx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, counterCtx, child, b, "/trades", viewport.width, viewport.height)
			tradeLifecycleQACounterManaged(t, counterCtx, child, primaryNote, get, give, "trade-lifecycle-counter-"+viewport.name, viewport.width, viewport.height)
			counterNote := "trade-lifecycle-counter-" + viewport.name

			counterOfferID := tradeLifecycleQAAcceptManaged(t, aCtx, child, counterNote, give.Name, get.Name)
			tradeLifecycleQANavigate(t, bCtx, child, "/trades")
			tradeLifecycleQAWaitTradeArticle(t, bCtx, `#outbox`, give.Name, get.Name, "Accepted", 12*time.Second)
			tradeLifecycleQANavigate(t, aCtx, child, "/team")
			tradeLifecycleQAAssertSameRoster(t, "A before commissioner approval", beforeA, tradeLifecycleQAReadRosterIDs(t, aCtx))
			tradeLifecycleQANavigate(t, bCtx, child, "/team")
			tradeLifecycleQAAssertSameRoster(t, "B before commissioner approval", beforeB, tradeLifecycleQAReadRosterIDs(t, bCtx))

			activityCtx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, activityCtx, child, viewer, "/activity?q="+url.QueryEscape(simTeamNames[0]), viewport.width, viewport.height)
			activityBefore := tradeLifecycleQASetActivityStamp(t, activityCtx)

			commissionerCtx := newBrowserContext(t, chromePath(t))
			signInBrowserSeat(t, commissionerCtx, child, league.commish, "/trades#review", viewport.width, viewport.height)
			tradeLifecycleQAApproveManaged(t, commissionerCtx, child, counterOfferID, counterNote)
			tradeLifecycleQAWaitActivity(t, activityCtx, activityBefore, "trade-lifecycle-activity-before-approval", simTeamNames[0], simTeamNames[1], give.Name, get.Name)

			tradeLifecycleQANavigate(t, aCtx, child, "/team")
			afterA := tradeLifecycleQAReadRosterIDs(t, aCtx)
			tradeLifecycleQANavigate(t, bCtx, child, "/team")
			afterB := tradeLifecycleQAReadRosterIDs(t, bCtx)
			tradeLifecycleQAAssertSwap(t, "A after executed trade", beforeA, afterA, give, get)
			tradeLifecycleQAAssertSwap(t, "B after executed trade", beforeB, afterB, get, give)

			tradeLifecycleQANavigate(t, aCtx, child, "/trades")
			tradeLifecycleQAWaitText(t, aCtx, `#history`, primaryNote, 12*time.Second)
			tradeLifecycleQAWaitText(t, aCtx, `#history`, counterNote, 12*time.Second)
			if !strings.Contains(tradeLifecycleQASectionText(t, aCtx, `#history`), "Countered") || !strings.Contains(tradeLifecycleQASectionText(t, aCtx, `#history`), "Executed") {
				t.Fatalf("A history omitted the primary counter/execution states")
			}
			tradeLifecycleQANavigate(t, bCtx, child, "/trades")
			tradeLifecycleQAWaitText(t, bCtx, `#history`, primaryNote, 12*time.Second)
			tradeLifecycleQAWaitText(t, bCtx, `#history`, counterNote, 12*time.Second)

			tradeLifecycleQAAssertViewport(t, aCtx, viewport.name+" Team/Trades")
			tradeLifecycleQAAssertViewport(t, bCtx, viewport.name+" B Trades")
			tradeLifecycleQAAssertViewport(t, activityCtx, viewport.name+" Activity")

			// Native forms remain a meaningful fallback when GoSX is absent. It
			// exercises a second real offer and withdrawal without adding a
			// synthetic server or bypassing the trade service.
			withdrawNote := "trade-lifecycle-withdraw-" + viewport.name
			tradeLifecycleQANativeWithdraw(t, child, a.Email, a.Name, b.Email, b.Name, b.TeamID, withdrawNote, viewport)

			// Expiry is driven by the actual 60-second roster-ops ticker after
			// the harness clock advances eight days. This scenario intentionally
			// waits for that process path rather than calling a store method.
			if viewport.name == "phone" {
				expiryCtx := newBrowserContext(t, chromePath(t))
				signInBrowserSeat(t, expiryCtx, child, a, "/trades?counterparty="+url.QueryEscape(b.TeamID), viewport.width, viewport.height)
				expiryComposer := tradeLifecycleQAReadComposer(t, expiryCtx)
				expireGive, expireGet := tradeLifecycleQAChoosePair(t, expiryComposer)
				expireNote := "trade-lifecycle-expired-phone"
				tradeLifecycleQASelectPair(t, expiryCtx, expireGive, expireGet, expireNote)
				if err := chromedp.Run(expiryCtx,
					chromedp.ScrollIntoView(`form[action*="trade-propose"] button[type="submit"]`),
					chromedp.Click(`form[action*="trade-propose"] button[type="submit"]`, chromedp.ByQuery),
				); err != nil {
					t.Fatalf("managed expiry proposal: %v", err)
				}
				expireOfferID := tradeLifecycleQAWaitOpenOffer(t, expiryCtx, expireGive.Name, expireGet.Name, 12*time.Second)
				advanceClock(t, child.URL, 8*24*time.Hour)
				tradeLifecycleQAWaitText(t, expiryCtx, `#history`, expireNote, 75*time.Second)
				if !strings.Contains(tradeLifecycleQASectionText(t, expiryCtx, `#history`), "Expired") {
					t.Fatalf("roster-ops expiry did not render Expired for %q", expireNote)
				}
				tradeLifecycleQANavigate(t, expiryCtx, child, "/trades")
				tradeLifecycleQAAssertNoOfferActions(t, expiryCtx, `#outbox`, expireOfferID)
				if inbox := tradeLifecycleQASectionText(t, expiryCtx, `#inbox`); strings.Contains(inbox, expireNote) {
					t.Fatalf("expired offer remained actionable in the inbox: %q", inbox)
				}

				expiryACtx := newBrowserContext(t, chromePath(t))
				signInBrowserSeat(t, expiryACtx, child, a, "/team", viewport.width, viewport.height)
				tradeLifecycleQAAssertSameRoster(t, "A after expiry", afterA, tradeLifecycleQAReadRosterIDs(t, expiryACtx))
				expiryBCtx := newBrowserContext(t, chromePath(t))
				signInBrowserSeat(t, expiryBCtx, child, b, "/trades", viewport.width, viewport.height)
				tradeLifecycleQAWaitText(t, expiryBCtx, `#history`, expireNote, 12*time.Second)
				tradeLifecycleQAAssertNoOfferActions(t, expiryBCtx, `#inbox`, expireOfferID)
				if inbox := tradeLifecycleQASectionText(t, expiryBCtx, `#inbox`); strings.Contains(inbox, expireNote) {
					t.Fatalf("expired offer remained actionable in recipient inbox: %q", inbox)
				}
				tradeLifecycleQANavigate(t, expiryBCtx, child, "/team")
				tradeLifecycleQAAssertSameRoster(t, "B after expiry", afterB, tradeLifecycleQAReadRosterIDs(t, expiryBCtx))
			}
		})
	}
}

package main

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const contextualHelpTopicPath = "/help/lineups-locks-matchups-and-scoring"

func teamContextRegionRequestCount(t *testing.T, ctx context.Context) int {
	t.Helper()
	var requests int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`performance.getEntriesByType('resource').filter(function(entry) {
			return new URL(entry.name, window.location.href).pathname === '/team/fragment';
		}).length`, &requests)); err != nil {
		t.Fatalf("count Team lineup fragment requests: %v", err)
	}
	return requests
}

func waitForTeamContextRegionRequest(t *testing.T, ctx context.Context, before int) {
	t.Helper()
	deadline := time.Now().Add(browserRegionSwapWait)
	for time.Now().Before(deadline) {
		if teamContextRegionRequestCount(t, ctx) > before {
			return
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("Team lineup region did not issue a new /team/fragment request within %s (before=%d)", browserRegionSwapWait, before)
}

func markTeamContextRegionBeforeRefresh(t *testing.T, ctx context.Context) {
	t.Helper()
	var marked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function() {
		var region = document.querySelector('.team-lineup-region');
		var help = region && region.querySelector('a[href^="/help/lineups-locks-matchups-and-scoring"]');
		if (!region || !help) {
			return false;
		}
		window.__contextualTeamRegionBefore = region;
		window.__contextualTeamHelpBefore = help;
		var host = region.closest('[data-gosx-region]');
		if (!host) {
			return false;
		}
		window.__contextualTeamRegionHost = host;
		window.__contextualTeamRegionStates = [];
		window.__contextualTeamRegionStateObserver = new MutationObserver(function(records) {
			records.forEach(function(record) {
				if (record.attributeName === 'data-gosx-region-state') {
					window.__contextualTeamRegionStates.push(host.getAttribute('data-gosx-region-state') || '');
				}
			});
		});
		window.__contextualTeamRegionStateObserver.observe(host, {attributes: true, attributeFilter: ['data-gosx-region-state']});
		region.setAttribute('data-contextual-team-before', 'true');
		help.setAttribute('data-contextual-team-before', 'true');
		return true;
	})()`, &marked)); err != nil {
		t.Fatalf("mark Team lineup region before refresh: %v", err)
	}
	if !marked {
		t.Fatal("Team lineup region was missing before refresh")
	}
}

func triggerTeamContextRegionRefresh(t *testing.T, ctx context.Context) {
	t.Helper()
	const selector = `button[data-gosx-set="$team.lineup.refresh"]`
	if err := chromedp.Run(ctx,
		chromedp.ScrollIntoView(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery, chromedp.NodeVisible),
	); err != nil {
		t.Fatalf("trigger existing Team lineup refresh control: %v", err)
	}
}

func waitForTeamContextRegionReady(t *testing.T, ctx context.Context) {
	// GoSX keeps a declarative region's root node when swapping its content,
	// and a matching ETag can legitimately leave its descendants untouched.
	// The held nodes catch a real subtree replacement when one occurs; in all
	// cases require a fresh request's pending -> ready transition before
	// reading the contextual link.
	t.Helper()
	deadline := time.Now().Add(browserRegionSwapWait)
	for time.Now().Before(deadline) {
		var refreshed bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(function() {
			var beforeRegion = window.__contextualTeamRegionBefore;
			var beforeHelp = window.__contextualTeamHelpBefore;
			var currentRegion = document.querySelector('.team-lineup-region');
			var currentHelp = currentRegion && currentRegion.querySelector('a[href^="/help/lineups-locks-matchups-and-scoring"]');
			var host = window.__contextualTeamRegionHost;
			var states = window.__contextualTeamRegionStates || [];
			var sawPending = states.indexOf('pending') >= 0;
			return !!beforeRegion && !!beforeHelp && !!currentRegion && !!currentHelp && !!host &&
				host.getAttribute('data-gosx-region-state') === 'ready' &&
				(sawPending && states.indexOf('ready') >= 0 || !beforeRegion.isConnected || currentRegion !== beforeRegion ||
					!beforeHelp.isConnected || currentHelp !== beforeHelp);
		})()`, &refreshed)); err != nil {
			t.Fatalf("check Team lineup region refresh: %v", err)
		}
		if refreshed {
			return
		}
		time.Sleep(browserPollInterval)
	}
	var diagnostics struct {
		OldRegionConnected bool     `json:"oldRegionConnected"`
		OldHelpConnected   bool     `json:"oldHelpConnected"`
		CurrentRegion      bool     `json:"currentRegion"`
		CurrentHelp        bool     `json:"currentHelp"`
		RegionState        string   `json:"regionState"`
		RegionStates       []string `json:"regionStates"`
		RegionMarked       bool     `json:"regionMarked"`
		HelpMarked         bool     `json:"helpMarked"`
	}
	_ = chromedp.Run(ctx, chromedp.Evaluate(`(function() {
		var beforeRegion = window.__contextualTeamRegionBefore;
		var beforeHelp = window.__contextualTeamHelpBefore;
		var currentRegion = document.querySelector('.team-lineup-region');
		var currentHelp = currentRegion && currentRegion.querySelector('a[href^="/help/lineups-locks-matchups-and-scoring"]');
		var host = window.__contextualTeamRegionHost;
		return {
			oldRegionConnected: !!beforeRegion && beforeRegion.isConnected,
			oldHelpConnected: !!beforeHelp && beforeHelp.isConnected,
			currentRegion: !!currentRegion,
			currentHelp: !!currentHelp,
			regionState: host ? host.getAttribute('data-gosx-region-state') : '',
			regionStates: window.__contextualTeamRegionStates || [],
			regionMarked: !!currentRegion && currentRegion.getAttribute('data-contextual-team-before') === 'true',
			helpMarked: !!currentHelp && currentHelp.getAttribute('data-contextual-team-before') === 'true'
		};
	})()`, &diagnostics))
	t.Fatalf("Team lineup region was requested but did not reach ready after its marked refresh within %s (oldRegionConnected=%t oldHelpConnected=%t currentRegion=%t currentHelp=%t regionState=%q regionStates=%q regionMarkerStillPresent=%t helpMarkerStillPresent=%t)", browserRegionSwapWait, diagnostics.OldRegionConnected, diagnostics.OldHelpConnected, diagnostics.CurrentRegion, diagnostics.CurrentHelp, diagnostics.RegionState, diagnostics.RegionStates, diagnostics.RegionMarked, diagnostics.HelpMarked)
}

func readContextualHelpHref(t *testing.T, ctx context.Context, selector string) string {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := chromedp.Run(waitCtx, chromedp.WaitVisible(selector, chromedp.ByQuery)); err != nil {
		var anchors []string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('a')).map(function(a) {
			return (a.getAttribute('href') || '') + '|' + (a.textContent || '').trim();
		})`, &anchors))
		t.Fatalf("wait for contextual-help link %s: %v (anchors=%q)", selector, err, anchors)
	}
	var href string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(selector, "href", &href, nil, chromedp.ByQuery)); err != nil {
		t.Fatalf("read contextual-help href %s: %v", selector, err)
	}
	return href
}

func contextualHrefPath(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse contextual href %q: %v", raw, err)
	}
	if parsed.IsAbs() {
		if parsed.Path == "" || parsed.Host == "" {
			t.Fatalf("contextual href %q has no same-origin path", raw)
		}
		return parsed.Path + queryAndFragment(parsed)
	}
	return raw
}

func assertContextualLocation(t *testing.T, ctx context.Context, childURL, expectedPath string) *url.URL {
	t.Helper()
	var location string
	if err := chromedp.Run(ctx, chromedp.Location(&location)); err != nil {
		t.Fatalf("read browser location: %v", err)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse browser location %q: %v", location, err)
	}
	if parsed.Scheme != "http" || parsed.Host == "" {
		t.Fatalf("browser location %q is not an absolute local URL", location)
	}
	if childURL != "" && !strings.HasPrefix(location, childURL) {
		t.Fatalf("browser location %q left the local child origin %q", location, childURL)
	}
	if parsed.Path+queryAndFragment(parsed) != expectedPath {
		t.Fatalf("browser location path = %q, want %q", parsed.Path+queryAndFragment(parsed), expectedPath)
	}
	return parsed
}

func queryAndFragment(parsed *url.URL) string {
	value := parsed.RawQuery
	if value != "" {
		value = "?" + value
	}
	if parsed.Fragment != "" {
		value += "#" + parsed.Fragment
	}
	return value
}

func runContextualHelpBrowserJourney(t *testing.T, width, height int64) {
	t.Helper()
	child, league, ctx := startSeatedBrowserChild(t)
	bot := league.bots[0]

	signInBrowserSeat(t, ctx, child, bot, "/team?week=1", width, height)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".team-lineup-region", chromedp.ByQuery)); err != nil {
		t.Fatalf("Team lineup region did not render at %dx%d: %v", width, height, err)
	}
	assertContextualLocation(t, ctx, child.URL, "/team?week=1")
	if width <= 390 {
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Fatalf("Team overflows at %dx%d: scrollWidth=%d innerWidth=%d", width, height, scrollWidth, innerWidth)
		}
	}

	// The initial page is server-rendered. Mark its region/link nodes, then
	// require a fresh region request to reach ready before reading the link.
	requestsBefore := teamContextRegionRequestCount(t, ctx)
	markTeamContextRegionBeforeRefresh(t, ctx)
	triggerTeamContextRegionRefresh(t, ctx)
	waitForTeamContextRegionRequest(t, ctx, requestsBefore)
	waitForTeamContextRegionReady(t, ctx)
	teamHelpHref := readContextualHelpHref(t, ctx, ".team-lineup-region a[href^=\""+contextualHelpTopicPath+"\"]")
	wantTeamHelpHref := contextualHelpTopicPath + "?return_to=%2Fteam%3Fweek%3D1%23lineup"
	if teamHelpHref != wantTeamHelpHref {
		t.Fatalf("Team contextual-help href after region refresh = %q, want %q", teamHelpHref, wantTeamHelpHref)
	}
	if err := chromedp.Run(ctx, chromedp.Click(".team-lineup-region a[href^=\""+contextualHelpTopicPath+"\"]", chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Team contextual-help link at %dx%d: %v", width, height, err)
	}
	waitForLocation(t, ctx, child.URL+teamHelpHref)
	assertContextualLocation(t, ctx, child.URL, contextualHelpTopicPath+"?return_to=%2Fteam%3Fweek%3D1%23lineup")

	teamReturnSelector := `a[href$="/team?week=1#lineup"]`
	teamReturnHref := contextualHrefPath(t, readContextualHelpHref(t, ctx, teamReturnSelector))
	if teamReturnHref != "/team?week=1#lineup" {
		t.Fatalf("Team help return CTA href = %q, want /team?week=1#lineup", teamReturnHref)
	}
	if err := chromedp.Run(ctx, chromedp.Click(teamReturnSelector, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Team help return CTA at %dx%d: %v", width, height, err)
	}
	waitForLocation(t, ctx, child.URL+teamReturnHref)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".team-lineup-region", chromedp.ByQuery)); err != nil {
		t.Fatalf("Team lineup region did not return at %dx%d: %v", width, height, err)
	}
	assertContextualLocation(t, ctx, child.URL, "/team?week=1#lineup")

	signInBrowserSeat(t, ctx, child, bot, "/matchups?week=1", width, height)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(".matchup-status-line__projection-help", chromedp.ByQuery)); err != nil {
		t.Fatalf("Matchups projection-help link did not render at %dx%d: %v", width, height, err)
	}
	assertContextualLocation(t, ctx, child.URL, "/matchups?week=1")
	matchupsHelpHref := readContextualHelpHref(t, ctx, ".matchup-status-line__projection-help")
	wantMatchupsHelpHref := contextualHelpTopicPath + "?return_to=%2Fmatchups%3Fweek%3D1%23main-content"
	if matchupsHelpHref != wantMatchupsHelpHref {
		t.Fatalf("Matchups contextual-help href = %q, want %q", matchupsHelpHref, wantMatchupsHelpHref)
	}
	if err := chromedp.Run(ctx, chromedp.Click(".matchup-status-line__projection-help", chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Matchups contextual-help link at %dx%d: %v", width, height, err)
	}
	waitForLocation(t, ctx, child.URL+matchupsHelpHref)
	assertContextualLocation(t, ctx, child.URL, contextualHelpTopicPath+"?return_to=%2Fmatchups%3Fweek%3D1%23main-content")

	matchupsReturnSelector := `a[href$="/matchups?week=1#main-content"]`
	matchupsReturnHref := contextualHrefPath(t, readContextualHelpHref(t, ctx, matchupsReturnSelector))
	if matchupsReturnHref != "/matchups?week=1#main-content" {
		t.Fatalf("Matchups help return CTA href = %q, want /matchups?week=1#main-content", matchupsReturnHref)
	}
	if err := chromedp.Run(ctx, chromedp.Click(matchupsReturnSelector, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Matchups help return CTA at %dx%d: %v", width, height, err)
	}
	waitForLocation(t, ctx, child.URL+matchupsReturnHref)
	assertContextualLocation(t, ctx, child.URL, "/matchups?week=1#main-content")
}

func TestBrowserContextualHelpReturnsToTeamAndMatchupsTask(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{name: "phone", width: 390, height: 844},
		{name: "desktop", width: 1440, height: 900},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			runContextualHelpBrowserJourney(t, viewport.width, viewport.height)
		})
	}
}

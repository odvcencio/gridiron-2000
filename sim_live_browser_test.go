package main

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// browserReplayScoreLoop bounds TestBrowserReplayScoreReachesMatchupsWithinTenSeconds's
// score-watching loop (round-2 note 32: matches the Task 12 budget).
const browserReplayScoreLoop = 45 * time.Second

// TestBrowserReplayScoreReachesMatchupsWithinTenSeconds proves Goal 5 on
// the replay path end to end: poll (5 s) + hub + bind fetch. Each frame k
// is due at start + k*step; every observed score change on /matchups must
// land within 10 s of the due time of the latest served frame.
func TestBrowserReplayScoreReachesMatchupsWithinTenSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	chrome := chromePath(t)
	root := browserAppRoot(t)
	child, fantasyLeague := startReplayLeague(t, "3s", "GOSX_APP_ROOT="+root)
	ctx := newBrowserContext(t, chrome)
	// Identify the team startReplayLeague reserved a full BAL/BUF lineup
	// for (replayLineup, sim_live_test.go): any of its nine starters'
	// NFL team is BAL or BUF, so the first one found in starterNFLTeam
	// names that team.
	//
	// The scan retries for up to 15 s rather than reading once: the live
	// feed caches its snapshot by the poller's own version
	// (internal/league/feed.go's liveFeed.Snapshot), and a page render
	// during the draft can populate that cache with a "preseason"
	// snapshot (taken before GenerateSchedule published a schedule)
	// that survives until the poller's version next moves — up to one
	// LIVE_POLL_INTERVAL tick, not instantaneous (round-1 empirical
	// finding).
	var teamID string
	starterScanDeadline := time.Now().Add(15 * time.Second)
	for teamID == "" && time.Now().Before(starterScanDeadline) {
		view, _ := liveWeek(t, child, fantasyLeague.bots[0])
		teams, _ := view["starterNFLTeam"].(map[string]any)
		for key, team := range teams {
			if team == "BUF" || team == "BAL" {
				teamID = strings.SplitN(key, "_", 2)[0]
				break
			}
		}
		if teamID == "" {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if teamID == "" {
		t.Fatal("no BAL/BUF starter within 15 s; startReplayLeague should have reserved a full BAL/BUF lineup")
	}
	bot := fantasyLeague.byTeam(teamID)
	target := child.URL + "/test/signin?user=" + url.QueryEscape(bot.Email+"|"+bot.Name) + "&to=/matchups"
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 900), chromedp.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	// Watch every one of the reserved team's nine starterPoints cells
	// (starterPoints.<liveKey>, app/matchups/page.gsx's matchup-ledger
	// row) together, concatenated, rather than the team's aggregate
	// score cell (data-score-team): the aggregate only ever shows a
	// number once every one of the nine starting slots has a matched
	// stat line (TeamWeekLedger.Known, internal/league/
	// matchup_ledger.go's "complete" gate) — and parseBoxScore drops a
	// player from a frame's Players map entirely until their first
	// nonzero stat (internal/fantasy/preseason.go's projectionStats
	// idiom), so a slower-touching starter (a kicker's first field goal,
	// a third receiver's first target) can leave the aggregate at "—"
	// for most or all of a 45 s window (round-1 empirical finding: one
	// run saw 3/9 slots ever match in that window, so the aggregate
	// never moved off "—" at all). Each individual starterPoints cell,
	// by contrast, is set for every slot from the first render — "0.0"
	// until that player's own line changes — so reading all nine
	// together loses none of the "many independent signal sources"
	// benefit replayLineup exists to provide, without waiting on the
	// slowest starter to ever touch the ball.
	selector := `[data-gosx-live-bind^="starterPoints.` + teamID + `_"]`
	read := func() string {
		var text string
		script := `Array.from(document.querySelectorAll('` + selector + `')).map(e => e.textContent).join('|')`
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &text)); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(text)
	}
	loaded := time.Now()
	last := read()
	changes := 0
	var latencies []time.Duration
	deadline := time.Now().Add(browserReplayScoreLoop)
	for time.Now().Before(deadline) && changes < 3 {
		time.Sleep(150 * time.Millisecond)
		current := read()
		if current == last {
			continue
		}
		observed := time.Now()
		status := readTestLive(t, child)
		// Round-2 note 40: the frame index is int(now.Sub(start) / step).
		due := status.Replay.Start.Add(time.Duration(status.Replay.ServedIndex) * time.Duration(status.Replay.StepMS) * time.Millisecond)
		if due.Before(loaded) {
			last = current // a frame served before the page loaded is not a sample
			continue
		}
		latency := observed.Sub(due)
		if latency > 10*time.Second {
			t.Fatalf("score %q -> %q arrived %s after frame %d was due (limit 10 s)", last, current, latency, status.Replay.ServedIndex)
		}
		latencies = append(latencies, latency)
		changes++
		last = current
	}
	if changes < 3 {
		t.Fatalf("only %d score changes observed in %s", changes, browserReplayScoreLoop)
	}
	t.Logf("browser replay latencies: %v", latencies)
	var fetches int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`performance.getEntriesByType('resource').filter(e => e.name.includes('/matchups') && e.initiatorType === 'fetch').length`, &fetches)); err != nil {
		t.Fatal(err)
	}
	if fetches != 0 {
		t.Fatalf("the page refetched HTML %d times on score changes", fetches)
	}
}

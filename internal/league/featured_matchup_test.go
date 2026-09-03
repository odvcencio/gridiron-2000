package league

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// featuredMatchupFixture drafts Josh Allen (p-09, BUF) to team-1 and
// Saquon Barkley (p-11, PHI) to team-2, pins each into a starting slot,
// publishes a one-week schedule, and wires a live status/week-stats pair
// that gives the two sides different point totals — team-1 outscoring
// team-2 — so a same-value bug (for example every side reading the same
// win probability) cannot pass by coincidence.
func featuredMatchupFixture(t *testing.T) (*Service, time.Time) {
	t.Helper()
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-09", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.MakePick("team-2", "p-11", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-1", 1, "QB", "p-09", now); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-2", 1, "RB1", "p-11", now); err != nil {
		t.Fatal(err)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "g1", Week: 1, Kickoff: now.Add(-time.Hour), Away: "BAL", Home: "BUF"},
			{ID: "g2", Week: 1, Kickoff: now.Add(-time.Hour), Away: "PHI", Home: "SEA"},
		}
	})
	svc.SetWeekStatsSource(func(int) []WeekStatLine {
		return []WeekStatLine{
			{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 3}, Source: StatSourceLive},
			{Key: normalizePlayerKey("Saquon Barkley", "RB"), Stats: map[string]float64{"rushYards": 20}, Source: StatSourceLive},
		}
	})
	svc.SetLiveStatusSource(func() LiveStatus {
		return LiveStatus{Enabled: true, Games: map[string]LiveGameState{
			"BUF": {GameID: "g1", Away: "BAL", Home: "BUF", Period: "Q2", InProgress: true},
			"BAL": {GameID: "g1", Away: "BAL", Home: "BUF", Period: "Q2", InProgress: true},
			"PHI": {GameID: "g2", Away: "PHI", Home: "SEA", Period: "Q2", InProgress: true},
			"SEA": {GameID: "g2", Away: "PHI", Home: "SEA", Period: "Q2", InProgress: true},
		}}
	})
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0
	return svc, now
}

// TestLiveScoresViewWinProbIsKeyedPerTeamNotPerMatchup covers round-2
// review finding 1 (commit 133d1d7): winProb must publish each side's OWN
// win probability under its own team ID — never a single matchup-keyed
// value fixed to one side — so the featured card's live-bind (which
// always reads its own team's key) cannot flip to the wrong number
// merely because the viewer's team is Away instead of Home.
func TestLiveScoresViewWinProbIsKeyedPerTeamNotPerMatchup(t *testing.T) {
	svc, now := featuredMatchupFixture(t)
	live, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Matchups) == 0 {
		t.Fatal("fixture schedule produced no matchups")
	}
	view := svc.LiveScoresView(context.Background())
	winProb, ok := view["winProb"].(map[string]string)
	if !ok {
		t.Fatalf("winProb = %#v, want a typed team-keyed map", view["winProb"])
	}
	parse := func(text string) float64 {
		t.Helper()
		value, err := strconv.ParseFloat(strings.TrimSuffix(text, "%"), 64)
		if err != nil {
			t.Fatalf("winProb %q did not parse as a percentage: %v", text, err)
		}
		return value
	}
	// Every real matchup's two team IDs carry complementary win
	// probabilities keyed by their own team ID — never a single value
	// fixed to one side or keyed by the matchup ID itself. A matchup where
	// NEITHER side has ever set a lineup (this fixture only drafts and
	// starts players for team-1/team-2 — every other team's roster is
	// still empty) has nothing to project, so both sides honestly dash
	// together (wave-8 audit item 2) instead of a meaningless 50/50.
	for _, matchup := range live.Matchups {
		away, ok := winProb[matchup.Away.ID]
		if !ok {
			t.Fatalf("winProb missing away team key %q for matchup %q", matchup.Away.ID, matchup.ID)
		}
		home, ok := winProb[matchup.Home.ID]
		if !ok {
			t.Fatalf("winProb missing home team key %q for matchup %q", matchup.Home.ID, matchup.ID)
		}
		if away == winProbabilityDashText || home == winProbabilityDashText {
			if away != home {
				t.Fatalf("matchup %q: away %q vs home %q, want both sides to dash together when neither has a lineup", matchup.ID, away, home)
			}
		} else if sum := parse(away) + parse(home); sum < 99 || sum > 101 {
			t.Fatalf("matchup %q: away %q + home %q = %v, want the two complementary probabilities to sum to ~100%%", matchup.ID, away, home, sum)
		}
		if _, ok := winProb[matchup.ID]; ok {
			t.Fatalf("winProb still carries a matchup-ID key %q; the matchup-keyed shape must be dropped entirely", matchup.ID)
		}
		// Only the matchup pairing BOTH fixture teams has a lineup on both
		// sides (this fixture only drafts and starts a player for team-1
		// and team-2); any matchup with just one of them still dashes,
		// since the OTHER side has nothing to project.
		bothFixtureTeams := (matchup.Away.ID == "team-1" && matchup.Home.ID == "team-2") || (matchup.Away.ID == "team-2" && matchup.Home.ID == "team-1")
		if bothFixtureTeams && away == home {
			// This pairing wires stats for Josh Allen and Saquon Barkley,
			// so the two sides never legitimately tie.
			t.Fatalf("matchup %q: away %q == home %q, want two distinct, complementary probabilities", matchup.ID, away, home)
		}
	}
	if _, ok := winProb["team-1"]; !ok {
		t.Fatalf("winProb = %#v, want a team-1 key", winProb)
	}
	if _, ok := winProb["team-2"]; !ok {
		t.Fatalf("winProb = %#v, want a team-2 key", winProb)
	}
}

// TestStillToPlayPublishesBareCountAndTotalAsInts covers round-2 review
// finding 2 (commit 133d1d7): still_to_play must be the same two-int
// shape everywhere it appears — my_matchup, other_matchups, and the
// live-bind map — never a pre-composed "N of M" sentence, so the page
// (not the server) owns composing the sentence from the two numbers.
func TestStillToPlayPublishesBareCountAndTotalAsInts(t *testing.T) {
	svc, _ := featuredMatchupFixture(t)
	data := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))

	myMatchup, ok := data["my_matchup"].(map[string]any)
	if !ok || !myMatchup["has_matchup"].(bool) {
		t.Fatalf("my_matchup = %#v, want a resolved featured matchup", data["my_matchup"])
	}
	if _, ok := myMatchup["still_to_play"].(int); !ok {
		t.Fatalf("my_matchup.still_to_play = %#v (%T), want a plain int", myMatchup["still_to_play"], myMatchup["still_to_play"])
	}
	if _, ok := myMatchup["still_to_play_total"].(int); !ok {
		t.Fatalf("my_matchup.still_to_play_total = %#v (%T), want a plain int", myMatchup["still_to_play_total"], myMatchup["still_to_play_total"])
	}

	view := svc.LiveScoresView(context.Background())
	stillToPlay, ok := view["stillToPlay"].(map[string]string)
	if !ok {
		t.Fatalf("stillToPlay = %#v, want a typed map", view["stillToPlay"])
	}
	stillToPlayTotal, ok := view["stillToPlayTotal"].(map[string]string)
	if !ok {
		t.Fatalf("stillToPlayTotal = %#v, want a typed map", view["stillToPlayTotal"])
	}
	for matchupID, count := range stillToPlay {
		if _, err := strconv.Atoi(count); err != nil {
			t.Fatalf("stillToPlay[%q] = %q, want a bare int (the page composes the sentence): %v", matchupID, count, err)
		}
		total, ok := stillToPlayTotal[matchupID]
		if !ok {
			t.Fatalf("stillToPlayTotal missing the matching key %q", matchupID)
		}
		if _, err := strconv.Atoi(total); err != nil {
			t.Fatalf("stillToPlayTotal[%q] = %q, want a bare int: %v", matchupID, total, err)
		}
	}
}

// TestFeaturedMatchupMapShowsProjectionBeforeKickoff covers wave-8 audit
// item 2: /matchups used to show "proj —" for a lineup /team's own
// PROJECTED figure had a real number for, because projectedText and
// winProbabilityText were gated on ScoreKnown — which stays false for the
// entire pre-kickoff window (no stat lines exist yet) — instead of on
// whether the side actually has a lineup to project. Both starters here
// are drafted and started, kickoff is an hour out, and no week-stats
// source is wired (so ScoreKnown is false, matching the real pre-draft
// production case), yet the projection and win probability must still
// read as real numbers, and the win-probability bar must not collapse to
// 0% width.
func TestFeaturedMatchupMapShowsProjectionBeforeKickoff(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-09", "manager", now, time.Time{}); err != nil { // Josh Allen, BUF
		t.Fatal(err)
	}
	if _, err := svc.store.MakePick("team-2", "p-11", "manager", now, time.Time{}); err != nil { // Saquon Barkley, PHI
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-1", 1, "QB", "p-09", now); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-2", 1, "RB1", "p-11", now); err != nil {
		t.Fatal(err)
	}
	// Seed 4 (see the schedule package's seed sweep) pairs team-1 against
	// team-2 in week 1 — the two teams this fixture actually drafted and
	// started a player for, so the featured matchup has real projections
	// on both sides.
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	// Kickoff is still ahead, and no WeekStatsSource is wired at all — the
	// real pre-draft-day shape: nflverse has nothing yet, so ScoreKnown
	// (TeamWeekLedger.Known) reads false for every side.
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "g1", Week: 1, Kickoff: now.Add(time.Hour), Away: "BAL", Home: "BUF"},
			{ID: "g2", Week: 1, Kickoff: now.Add(time.Hour), Away: "PHI", Home: "SEA"},
		}
	})
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0

	data := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	myMatchup, ok := data["my_matchup"].(map[string]any)
	if !ok || !myMatchup["has_matchup"].(bool) {
		t.Fatalf("my_matchup = %#v, want a resolved featured matchup", data["my_matchup"])
	}
	mine, ok := myMatchup["mine"].(map[string]any)
	if !ok {
		t.Fatalf("my_matchup.mine = %#v, want a team map", myMatchup["mine"])
	}
	if mine["score"] != "—" {
		t.Fatalf("pre-kickoff score = %#v, want the honest dash (ScoreKnown is false)", mine["score"])
	}
	if mine["projected"] == "—" || mine["projected"] == "" {
		t.Fatalf("pre-kickoff projected = %#v, want a real projected total even though the score itself is unknown", mine["projected"])
	}
	if myMatchup["win_prob"] == winProbabilityDashText {
		t.Fatalf("pre-kickoff win_prob = %#v, want a computed percentage", myMatchup["win_prob"])
	}
	if myMatchup["win_prob_width"] == "0%" {
		t.Fatalf("pre-kickoff win_prob_width = %#v, want a non-zero bar width with a projection present", myMatchup["win_prob_width"])
	}
}

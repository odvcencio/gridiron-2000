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
	// fixed to one side or keyed by the matchup ID itself.
	for _, matchup := range live.Matchups {
		away, ok := winProb[matchup.Away.ID]
		if !ok {
			t.Fatalf("winProb missing away team key %q for matchup %q", matchup.Away.ID, matchup.ID)
		}
		home, ok := winProb[matchup.Home.ID]
		if !ok {
			t.Fatalf("winProb missing home team key %q for matchup %q", matchup.Home.ID, matchup.ID)
		}
		if sum := parse(away) + parse(home); sum < 99 || sum > 101 {
			t.Fatalf("matchup %q: away %q + home %q = %v, want the two complementary probabilities to sum to ~100%%", matchup.ID, away, home, sum)
		}
		if _, ok := winProb[matchup.ID]; ok {
			t.Fatalf("winProb still carries a matchup-ID key %q; the matchup-keyed shape must be dropped entirely", matchup.ID)
		}
		involvesFixtureTeam := matchup.Away.ID == "team-1" || matchup.Away.ID == "team-2" || matchup.Home.ID == "team-1" || matchup.Home.ID == "team-2"
		if involvesFixtureTeam && away == home {
			// Every other matchup can legitimately tie at 50/50 (both
			// sides scoreless under this fixture, which wires stats for
			// only Josh Allen and Saquon Barkley); the matchup carrying
			// one of them must not.
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

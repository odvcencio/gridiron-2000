package league

import (
	"context"
	"testing"
	"time"
)

// teamMatchupProjectionFixture drafts a QB and an RB onto team-1 (no
// explicit lineup set — effectiveLineup's own auto-fill places them),
// wires a one-game week-1 NFL schedule, and publishes a one-week fantasy
// season schedule so team-1 has a real opponent to compare against.
func teamMatchupProjectionFixture(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "g-pit", Week: 1, Kickoff: now.Add(8 * time.Hour), Away: "PIT", Home: "NYJ"},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	players := []Player{
		{ID: "proj-qb", Name: "Test Passer", Position: "QB", NFLTeam: "PIT", Projection: 21.4},
		{ID: "proj-rb", Name: "Test Rusher", Position: "RB", NFLTeam: "PIT", Projection: 14.8},
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 1, "test" })
	draftFixtureOntoTeam1(t, svc, now, []string{"proj-qb", "proj-rb"})
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 11})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0
	return svc
}

// TestTeamStatStripProjectionMatchesMatchupsFeaturedCard pins section-B
// item 2 / section-C item 8: the /team stat strip's PROJECTED figure and
// /matchups' featured-card projection for the viewer's own side must be
// the exact same starters-only number for the same team and week — never
// the whole-roster sum the strip used to print (188.0 while /matchups
// read 134.6 for the identical team, the audit's own J3 finding).
func TestTeamStatStripProjectionMatchesMatchupsFeaturedCard(t *testing.T) {
	svc := teamMatchupProjectionFixture(t)

	teamRequest := matchupDataRequest(t, "/team?week=1")
	teamData := svc.TeamData(teamRequest)
	stripProjected, ok := teamData["projected"].(string)
	if !ok || stripProjected == "" {
		t.Fatalf("team stat strip projected = %#v, want a non-empty string", teamData["projected"])
	}

	matchupsRequest := matchupDataRequest(t, "/matchups?week=1")
	matchupsData := svc.MatchupsData(context.Background(), matchupsRequest)
	myMatchup, ok := matchupsData["my_matchup"].(map[string]any)
	if !ok || myMatchup["has_matchup"] != true {
		t.Fatalf("my_matchup = %#v, want a resolved featured matchup", matchupsData["my_matchup"])
	}
	mine, ok := myMatchup["mine"].(map[string]any)
	if !ok {
		t.Fatalf("my_matchup.mine = %#v, want a map", myMatchup["mine"])
	}
	matchupsProjected, _ := mine["projected"].(string)

	if stripProjected == "—" || matchupsProjected == "—" {
		t.Fatalf("projections must be known for this fixture: strip=%q matchups=%q", stripProjected, matchupsProjected)
	}
	if stripProjected != matchupsProjected {
		t.Fatalf("stat strip projected %q != matchups featured card projected %q for the same team and week", stripProjected, matchupsProjected)
	}

	// current_matchup (section-B item 1) must publish the identical
	// starters-only figure on team-1's own side of its own card.
	currentMatchup, ok := teamData["current_matchup"].(map[string]any)
	if !ok || currentMatchup["has_matchup"] != true {
		t.Fatalf("current_matchup = %#v, want a resolved matchup card", teamData["current_matchup"])
	}
	if currentMatchup["proj_mine"] != stripProjected {
		t.Fatalf("current_matchup proj_mine %q != stat strip projected %q", currentMatchup["proj_mine"], stripProjected)
	}
}

// TestTeamStatStripProjectionExcludesBenchAndReserve is the direct
// regression test for the bug itself: a bench-only player (never
// eligible for any starting slot this roster carries — two rostered QBs
// with only one QB slot) must never inflate the starters-only figure.
func TestTeamStatStripProjectionExcludesBenchAndReserve(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games := []GameInfo{{ID: "g-pit", Week: 1, Kickoff: now.Add(8 * time.Hour), Away: "PIT", Home: "NYJ"}}
	svc.SetScheduleSource(func() []GameInfo { return games })
	players := []Player{
		{ID: "starter-qb", Name: "Starting Passer", Position: "QB", NFLTeam: "PIT", Projection: 20},
		{ID: "bench-qb", Name: "Bench Passer", Position: "QB", NFLTeam: "PIT", Projection: 99},
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return players, 1, "test" })
	draftFixtureOntoTeam1(t, svc, now, []string{"starter-qb", "bench-qb"})
	// Pin the weaker player into the sole QB slot explicitly — auto-fill
	// alone would pick the higher-projection bench-qb, which is exactly
	// the case this test needs to NOT happen for the assertion below to
	// mean anything.
	if err := svc.store.SetLineupSlot("team-1", 1, "QB", "starter-qb", now); err != nil {
		t.Fatal(err)
	}

	request := matchupDataRequest(t, "/team?week=1")
	data := svc.TeamData(request)
	projected, _ := data["projected"].(string)
	if projected != "20.0" {
		t.Fatalf("projected = %q, want 20.0 (starter only, the bench QB's 99 excluded)", projected)
	}
}

// TestTeamDataInSeasonFieldFollowsSeasonPhase covers section-B item 6:
// the draft-class callout must retire once week 1 has kicked off.
// in_season (data["in_season"]) is the gate page.gsx reads; this pins it
// to the same SeasonPhase truth every other season-lifecycle surface
// already agrees on, rather than a second, locally-invented rule.
func TestTeamDataInSeasonFieldFollowsSeasonPhase(t *testing.T) {
	svc := newTestService(t, true)
	request := matchupDataRequest(t, "/team")

	preseason := svc.TeamData(request)
	if preseason["season_phase"] != "preseason" || preseason["in_season"] != false {
		t.Fatalf("preseason team data = phase:%v in_season:%v, want preseason/false", preseason["season_phase"], preseason["in_season"])
	}

	svc.store.mu.Lock()
	svc.store.state.Phase = PhaseRegularSeason
	svc.store.mu.Unlock()

	inSeason := svc.TeamData(request)
	if inSeason["season_phase"] != PhaseRegularSeason || inSeason["in_season"] != true {
		t.Fatalf("in-season team data = phase:%v in_season:%v, want %s/true", inSeason["season_phase"], inSeason["in_season"], PhaseRegularSeason)
	}
}

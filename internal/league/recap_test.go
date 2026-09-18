package league

import (
	"strings"
	"testing"
	"time"
)

// TestLockerWeekRecapBody pins the shape of the Locker Room recap that
// week close posts for every member to read in the app, the shared
// counterpart of the per-member recap email: a headline, one line per
// matchup with the margin and a plain tie, then the week's awards.
func TestLockerWeekRecapBody(t *testing.T) {
	svc := schedulerTestService(t)
	state := svc.store.Snapshot()
	teams := state.Schedule.Weeks[0].Matchups
	if len(teams) < 2 {
		t.Fatalf("fixture week has %d matchups, want at least 2", len(teams))
	}
	week := ScheduleWeek{Week: 3, Matchups: []LeagueMatchup{
		{HomeTeamID: teams[0].HomeTeamID, AwayTeamID: teams[0].AwayTeamID, HomeScore: 101.2, AwayScore: 98.4, Final: true},
		{HomeTeamID: teams[1].HomeTeamID, AwayTeamID: teams[1].AwayTeamID, HomeScore: 88.0, AwayScore: 88.0, Final: true},
	}}
	body := svc.lockerWeekRecapBody(state, week)
	home := svc.teamView(state, teams[0].HomeTeamID).Name
	away := svc.teamView(state, teams[0].AwayTeamID).Name
	for _, want := range []string{
		"WEEK 3 IS FINAL.",
		away + " 98.4 @ " + home + " 101.2 — " + home + " by 2.8",
		"— tie",
		"★ High Score: " + home + " — 101.2 points",
		"⚡ Nailbiter: " + home + " — won by 2.8",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("recap body missing %q:\n%s", want, body)
		}
	}
	if len([]rune(body)) > lockerBodyMaxRunes {
		t.Fatalf("recap body is %d runes, over the locker limit of %d", len([]rune(body)), lockerBodyMaxRunes)
	}
}

// TestCloseWeekPostsOneLockerRecap: closing a week posts exactly one recap
// to the Locker Room under the system author, and closing it again (an
// already-final week re-commits without rescoring) never posts a second.
func TestCloseWeekPostsOneLockerRecap(t *testing.T) {
	svc := schedulerTestService(t)
	week := svc.store.Snapshot().Schedule.Weeks[0].Week
	kickoff := time.Date(2026, 9, 10, 20, 20, 0, 0, time.UTC)
	now := kickoff.Add(2 * time.Hour)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "g1", Week: week, Kickoff: kickoff, Final: true, ScoresPresent: true}}
	})
	svc.SetWeekStatsSource(func(int) []WeekStatLine { return nil })

	if _, _, err := svc.closeWeek(week, now); err != nil {
		t.Fatal(err)
	}
	recaps := func() []LockerPost {
		var out []LockerPost
		for _, post := range svc.store.Snapshot().LockerPosts {
			if post.AuthorEmail == lockerRecapAuthorEmail {
				out = append(out, post)
			}
		}
		return out
	}
	posts := recaps()
	if len(posts) != 1 {
		t.Fatalf("recap posts after close = %d, want 1", len(posts))
	}
	if !strings.HasPrefix(posts[0].Body, "WEEK "+itoa(week)+" IS FINAL.") || posts[0].AuthorName != lockerRecapAuthorName || posts[0].CommissionerNote {
		t.Fatalf("recap post = %+v", posts[0])
	}
	if _, _, err := svc.closeWeek(week, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if again := recaps(); len(again) != 1 {
		t.Fatalf("recap posts after a second close = %d, want still 1", len(again))
	}
}

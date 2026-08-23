package league

import (
	"net/http"
	"testing"
	"time"
)

func deadlineFixtureLineup(locked bool) EffectiveLineup {
	return EffectiveLineup{
		Week: 1,
		Slots: []SlotAssignment{
			{Slot: SlotInstance{ID: "QB"}, Locked: locked},
			{Slot: SlotInstance{ID: "RB1"}, Locked: false},
		},
	}
}

func TestLineupDeadlineStatesAreExplicit(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	location, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	roster := []Player{{ID: "p1", Name: "Future", NFLTeam: "PIT"}}

	tests := []struct {
		name  string
		games []GameInfo
		want  LineupDeadlineState
	}{
		{name: "no schedule", want: LineupDeadlineNoSchedule},
		{
			name:  "next lock",
			games: []GameInfo{{Week: 1, Away: "PIT", Home: "NYJ", Kickoff: now.Add(2 * time.Hour)}},
			want:  LineupDeadlineUpcoming,
		},
		{
			name:  "all locked",
			games: []GameInfo{{Week: 1, Away: "PIT", Home: "NYJ", Kickoff: now.Add(-time.Minute)}},
			want:  LineupDeadlineAllLocked,
		},
		{
			name:  "no upcoming roster game",
			games: []GameInfo{{Week: 2, Away: "PIT", Home: "NYJ", Kickoff: now.Add(time.Hour)}},
			want:  LineupDeadlineNoUpcoming,
		},
		{
			name:  "incomplete kickoff",
			games: []GameInfo{{Week: 1, Away: "PIT", Home: "NYJ"}},
			want:  LineupDeadlineDegraded,
		},
		{
			name:  "unrelated incomplete game does not poison roster deadline",
			games: []GameInfo{{Week: 1, Away: "DAL", Home: "NYG"}},
			want:  LineupDeadlineNoUpcoming,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := lineupDeadlineFor(deadlineFixtureLineup(false), roster, tt.games, 1, now, location)
			if view.State != tt.want {
				t.Fatalf("state = %q, want %q; view = %+v", view.State, tt.want, view)
			}
			if tt.want == LineupDeadlineUpcoming {
				if !view.HasDeadline || view.Relative != "in 2 hours" || view.Timezone != "America/Denver" {
					t.Fatalf("upcoming deadline = %+v", view)
				}
				if view.Exact != "Sunday, September 13, 2026 · 8:00 AM MDT" {
					t.Fatalf("exact deadline = %q", view.Exact)
				}
			}
			if tt.want != LineupDeadlineUpcoming && view.HasDeadline {
				t.Fatalf("state %q must not claim an exact deadline: %+v", tt.want, view)
			}
		})
	}
}

func TestNormalizeLineupWeekUsesPublishedScheduleAndCurrentFallback(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	games := []GameInfo{
		{Week: 1, Kickoff: now.Add(time.Hour), Away: "PIT", Home: "NYJ"},
		{Week: 3, Kickoff: now.Add(72 * time.Hour), Away: "TB", Home: "ATL"},
	}
	for _, tt := range []struct {
		name   string
		raw    string
		want   int
		notice bool
	}{
		{name: "empty", want: 1},
		{name: "published future", raw: "3", want: 3},
		{name: "past", raw: "0", want: 1, notice: true},
		{name: "closed", raw: "-1", want: 1, notice: true},
		{name: "unknown", raw: "2", want: 1, notice: true},
		{name: "malformed", raw: "week-two", want: 1, notice: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			selection := normalizeLineupWeek(tt.raw, games, now)
			if selection.Week != tt.want {
				t.Fatalf("week = %d, want %d; selection = %+v", selection.Week, tt.want, selection)
			}
			if (selection.Notice != "") != tt.notice {
				t.Fatalf("notice = %q, want present=%t", selection.Notice, tt.notice)
			}
		})
	}
}

func TestTeamDataCarriesNormalizedWeekAndDeadlineView(t *testing.T) {
	service, games, now := newLineupTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/team?week=2", nil)
	data := service.TeamData(request)
	if data["week"] != "1" {
		t.Fatalf("invalid week selected = %#v, want current week 1", data["week"])
	}
	if data["has_week_notice"] != true {
		t.Fatalf("invalid week should produce an actionable notice: %#v", data["week_notice"])
	}
	deadline, ok := data["lineup_deadline"].(map[string]any)
	if !ok || deadline["state"] != string(LineupDeadlineUpcoming) || deadline["has_deadline"] != true {
		t.Fatalf("deadline view = %#v", data["lineup_deadline"])
	}
	if deadline["exact"] != "Sunday, September 13, 2026 · 9:00 AM EDT" {
		t.Fatalf("deadline exact = %#v", deadline["exact"])
	}
	// Add a published future week and prove TeamData exposes it instead of
	// manufacturing current+5 options.
	games = append(games, GameInfo{ID: "g-pit-2", Week: 2, Kickoff: now.Add(48 * time.Hour), Away: "PIT", Home: "NYJ"})
	service.SetScheduleSource(func() []GameInfo { return games })
	request = httptestNewGET("/team?week=2")
	data = service.TeamData(request)
	if data["week"] != "2" {
		t.Fatalf("published future week selected = %#v, want 2", data["week"])
	}
}

func httptestNewGET(target string) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, target, nil)
	return request
}

func TestDegradedEarlierWeekRemainsCurrentSelectableAndEditable(t *testing.T) {
	service, games, now := newLineupTestService(t)
	// PIT is rostered by team-1. Its Week 1 row is published but has no
	// kickoff truth; a valid Week 2 game must not close that degraded week.
	games[0].Kickoff = time.Time{}
	games = append(games, GameInfo{ID: "g-pit-2", Week: 2, Kickoff: now.Add(48 * time.Hour), Away: "PIT", Home: "NYJ"})
	service.SetScheduleSource(func() []GameInfo { return games })

	selection := normalizeLineupWeek("", games, now)
	if selection.Week != 1 || selection.CurrentWeek != 1 {
		t.Fatalf("degraded selection = %+v, want Week 1 as current", selection)
	}
	if got := service.NormalizeLineupWeek("2"); got != 2 {
		t.Fatalf("published future Week 2 should remain selectable, got %d", got)
	}

	request := httptestNewGET("/team")
	data := service.TeamData(request)
	if data["week"] != "1" {
		t.Fatalf("TeamData week = %#v, want Week 1", data["week"])
	}
	deadline, ok := data["lineup_deadline"].(map[string]any)
	if !ok {
		t.Fatalf("lineup deadline view = %#v, want map", data["lineup_deadline"])
	}
	if deadline["state"] != string(LineupDeadlineDegraded) || deadline["headline"] != "SCHEDULE DATA INCOMPLETE" {
		t.Fatalf("degraded deadline = %#v", deadline)
	}
	if deadline["has_editable"] != true || deadline["has_deadline"] != false {
		t.Fatalf("degraded deadline editability = %#v", deadline)
	}

	post, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := service.SetLineup(post, "team-1", 1, "RB1", "rb-open"); err != nil {
		t.Fatalf("Week 1 lineup action must remain allowed while kickoff is degraded: %v", err)
	}
}

func TestAuthoritativeEarlierWeekStillCloses(t *testing.T) {
	service, games, now := newLineupTestService(t)
	// All Week 1 kickoffs are authoritative and beyond pick'em's four-hour
	// grace window; the valid future Week 2 game therefore closes Week 1.
	games[0].Kickoff = now.Add(-5 * time.Hour)
	games[1].Kickoff = now.Add(-6 * time.Hour)
	games = append(games, GameInfo{ID: "g-pit-2", Week: 2, Kickoff: now.Add(48 * time.Hour), Away: "PIT", Home: "NYJ"})
	service.SetScheduleSource(func() []GameInfo { return games })

	selection := normalizeLineupWeek("1", games, now)
	if selection.Week != 2 || selection.CurrentWeek != 2 || selection.Notice == "" {
		t.Fatalf("authoritative closed selection = %+v, want Week 2 with notice", selection)
	}
	post, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := service.SetLineup(post, "team-1", 1, "RB1", "rb-open"); err == nil || err.Error() != "week 1 is closed; lineups can no longer change" {
		t.Fatalf("closed Week 1 action error = %v", err)
	}
}

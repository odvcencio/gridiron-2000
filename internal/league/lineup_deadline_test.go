package league

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

// fourteenWeekSeasonSchedule builds a minimal SeasonSchedule spanning
// weeks 1 through 14 — the same shape a real fantasy league's own
// published (commonly 14-week) season carries, distinct from the raw NFL
// regular-season mirror (18 weeks) newLineupTestService's own games
// fixture stands in for below.
func fourteenWeekSeasonSchedule() *SeasonSchedule {
	weeks := make([]ScheduleWeek, 0, 14)
	for week := 1; week <= 14; week++ {
		weeks = append(weeks, ScheduleWeek{Week: week})
	}
	return &SeasonSchedule{Weeks: weeks}
}

// TestTeamDataLimitsWeekSelectorToPublishedSeasonSchedule is item 6's own
// regression test (2026-08-31 post-wave audit): /team's week selector
// must offer only the weeks THIS league's own published season schedule
// carries (14, in this fixture), not every week the raw NFL schedule
// mirror carries (up to 18) — and every invalid/out-of-range request
// (99, 0, "abc") must read the same "Week N is not on the published
// schedule. Showing Week 1." notice /matchups uses, not a silent
// fallback or one of three differently-worded messages.
func TestTeamDataLimitsWeekSelectorToPublishedSeasonSchedule(t *testing.T) {
	service, games, now := newLineupTestService(t)
	// Widen the raw NFL mirror to 18 weeks (the real regular-season
	// length) while the league's own published schedule stays at 14 —
	// the exact real-world gap the bug report named.
	for week := 2; week <= 18; week++ {
		games = append(games, GameInfo{ID: fmt.Sprintf("g-wk%d", week), Week: week, Kickoff: now.Add(time.Duration(week) * 7 * 24 * time.Hour), Away: "PIT", Home: "NYJ"})
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	service.store.state.Schedule = fourteenWeekSeasonSchedule()

	base := service.TeamData(httptestNewGET("/team"))
	weekOptions, ok := base["week_options"].([]map[string]any)
	if !ok {
		t.Fatalf("week_options = %#v, want []map[string]any", base["week_options"])
	}
	for _, option := range weekOptions {
		value, _ := option["value"].(string)
		n, err := strconv.Atoi(value)
		if err != nil || n > 14 {
			t.Errorf("week_options carries %q, want only published weeks 1-14", value)
		}
	}
	if len(weekOptions) == 0 {
		t.Fatal("week_options is empty, want at least the current week")
	}

	for _, tt := range []struct {
		name string
		raw  string
	}{
		{"beyond NFL and league schedule", "99"},
		{"zero", "0"},
		{"malformed", "abc"},
		{"inside NFL calendar but past the league's own 14-week season", "15"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := service.TeamData(httptestNewGET("/team?week=" + tt.raw))
			if data["has_week_notice"] != true {
				t.Fatalf("week=%s produced no notice, want the out-of-range notice", tt.raw)
			}
			notice, _ := data["week_notice"].(string)
			want := fmt.Sprintf("Showing Week %s.", data["week"])
			if !strings.Contains(notice, "is not on the published schedule.") || !strings.Contains(notice, want) {
				t.Fatalf("week=%s notice = %q, want the /matchups-style \"...is not on the published schedule. %s\" wording", tt.raw, notice, want)
			}
		})
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

	// WR1/wr-open, not RB1/rb-open: with the P0 auto-fill fix (see
	// effectiveLineup's auto-fill loop comment), RB1 in this fixture already
	// auto-resolves to rb-locked (TB, the higher-projection RB, genuinely
	// locked — unrelated to this test's PIT degradation) before this call
	// ever runs, so assigning rb-open there would correctly fail L7. WR1 has
	// no such collision: this fixture's two WR-eligible players exactly fill
	// its two WR slots, so nothing pre-empts the explicit set below.
	post, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := service.SetLineup(post, "team-1", 1, "WR1", "wr-open"); err != nil {
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

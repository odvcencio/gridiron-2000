package league

import (
	"math"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPickemLiveScoresStaySeparateFromGradesAndFrozenMarkets(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 27, 22, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	game := GameInfo{ID: "live-game", Week: 3, Away: "BUF", Home: "MIA", Kickoff: now.Add(-time.Hour),
		SpreadLinePresent: true, SpreadLineTenths: 30, SourceObservedAt: now.Add(-7 * 24 * time.Hour)}
	svc.SetScheduleSource(func() []GameInfo { return []GameInfo{game} })
	if err := svc.pickemMarketTick(now); err != nil {
		t.Fatal(err)
	}
	for owner, pick := range map[string]string{"demo-guest": "MIA", "away-picker": "BUF"} {
		if err := svc.store.SetPickem(owner, game.ID, pick, game.Kickoff.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	live := LiveGameState{GameID: game.ID, Week: game.Week, Away: game.Away, Home: game.Home,
		Period: "Q2", Clock: "8:12", InProgress: true, ScoresPresent: true, AwayPoints: 10, HomePoints: 14}
	status := LiveStatus{Enabled: true, GamesByWeek: map[int]map[string]LiveGameState{3: {"BUF": live}}}
	svc.SetLiveStatusSource(func() LiveStatus { return status })
	before := svc.store.Snapshot()
	request := httptest.NewRequest("GET", "/pickem?week=3", nil)
	for _, tc := range []struct {
		home float64
		want string
	}{
		{14, "winning"}, {12, "losing"}, {13, "tied"},
	} {
		live.HomePoints = tc.home
		status.GamesByWeek[3]["BUF"] = live
		data := svc.PickemDataReadOnly(request)
		row := data["games"].([]PickemGameRow)[0]
		if row.LivePickState != tc.want || row.GameState != "Q2 8:12" || !row.HasScores || row.Final || row.Correct || row.Wrong || row.Outcome != "pending" {
			t.Fatalf("live projection = %+v, want pending grade and %s standing", row, tc.want)
		}
		if len(row.LeaguePicks) != 2 {
			t.Fatalf("live ledger = %+v", row.LeaguePicks)
		}
		for _, entry := range row.LeaguePicks {
			want := tc.want
			if entry.PickLabel == "BUF" && want != "tied" {
				if want == "winning" {
					want = "losing"
				} else {
					want = "winning"
				}
			}
			if entry.LivePickState != want || entry.Outcome != "pending" || entry.Correct || entry.Wrong {
				t.Fatalf("live ledger entry = %+v, want %s and pending grade", entry, want)
			}
		}
		record := data["record"].(map[string]any)
		if record["week_wins"] != 0 || record["week_losses"] != 0 {
			t.Fatalf("provisional scores changed the record: %#v", record)
		}
		if !reflect.DeepEqual(before, svc.store.Snapshot()) {
			t.Fatal("live render changed durable picks or markets")
		}
	}
	status.Degraded = true
	if note := svc.PickemDataReadOnly(request)["live_score_note"].(string); !strings.Contains(note, "paused") {
		t.Fatalf("degraded note = %q", note)
	}
	status.Enabled = false
	row := svc.PickemDataReadOnly(request)["games"].([]PickemGameRow)[0]
	if row.HasScores || row.LivePickState != "" {
		t.Fatalf("disabled poller claimed a live standing: %+v", row)
	}
	if _, err := svc.PickemSet(request, game.ID, "BUF"); err == nil {
		t.Fatal("live presentation reopened a kickoff lock")
	}
}

func TestPickemLiveScoreRequiresMatchingGameAndCompleteScores(t *testing.T) {
	now := time.Date(2026, 9, 27, 22, 0, 0, 0, time.UTC)
	game := GameInfo{ID: "live-game", Week: 3, Away: "LA", Home: "WAS", Kickoff: now.Add(-time.Hour)}
	live := LiveGameState{GameID: game.ID, Week: 3, Away: "LAR", Home: "WSH", InProgress: true, ScoresPresent: true, Period: "OT", Clock: "4:01"}
	for _, tc := range []struct {
		name      string
		change    func(*LiveGameState)
		wantScore bool
	}{
		{"real zero tie", func(*LiveGameState) {}, true},
		{"missing", func(g *LiveGameState) { g.ScoresPresent = false }, false},
		{"wrong game", func(g *LiveGameState) { g.GameID = "other" }, false},
		{"wrong week", func(g *LiveGameState) { g.Week++ }, false},
		{"wrong opponent", func(g *LiveGameState) { g.Home = "SF" }, false},
		{"pregame", func(g *LiveGameState) { g.InProgress = false }, false},
		{"expired window", func(g *LiveGameState) { g.Final = true }, false},
		{"negative", func(g *LiveGameState) { g.HomePoints = -1 }, false},
		{"fractional", func(g *LiveGameState) { g.HomePoints = 1.5 }, false},
		{"nonfinite", func(g *LiveGameState) { g.HomePoints = math.NaN() }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := live
			tc.change(&candidate)
			view := pickemGameLiveView(game, LiveStatus{Enabled: true, Games: map[string]LiveGameState{"LA": candidate}}, now)
			if view.HasScores != tc.wantScore {
				t.Fatalf("live view = %+v", view)
			}
			if tc.wantScore && (view.State != "OT 4:01" || view.ScoreDisplay != "0-0") {
				t.Fatalf("zero tie = %+v", view)
			}
		})
	}
}

func TestPickemLiveStandingWaitsForFrozenLine(t *testing.T) {
	game := GameInfo{Away: "BUF", Home: "MIA"}
	view := pickemLiveView{InProgress: true, HasScores: true, AwayScore: 10, HomeScore: 14}
	state, label := pickemLiveStanding(game, PickemMarket{LinePresent: true, LineTenths: 30}, "MIA", view)
	if state != "pending" || label != "WAITING FOR LINE" {
		t.Fatalf("unfrozen standing = %q %q", state, label)
	}
	for _, market := range []PickemMarket{{Frozen: true}, {Frozen: true, LinePresent: true, Void: true}} {
		if state, _ := pickemLiveStanding(game, market, "MIA", view); state != "" {
			t.Fatalf("void standing = %q", state)
		}
	}
}

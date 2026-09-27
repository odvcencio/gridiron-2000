package league

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestPickemLiveFinalSettlesEveryProjectionWithoutChangingFrozenMarket(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 27, 22, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	kickoff := now.Add(-time.Hour)
	base := []GameInfo{{
		ID: "2026_03_BUF_MIA", Week: 3, Kickoff: kickoff, Away: "BUF", Home: "MIA",
		SpreadLineTenths: 30, SpreadLinePresent: true, SourceObservedAt: kickoff.Add(-7 * 24 * time.Hour),
	}}
	service.SetScheduleSource(func() []GameInfo { return base })
	if err := service.pickemMarketTick(now); err != nil {
		t.Fatal(err)
	}
	marketBefore := service.store.Snapshot().PickemMarkets[base[0].ID]
	if !marketBefore.Frozen || !marketBefore.LinePresent || marketBefore.LineTenths != 30 {
		t.Fatalf("test market was not frozen at the source line: %+v", marketBefore)
	}
	if err := service.store.SetPickem("demo-guest", base[0].ID, "MIA", kickoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	live := base[0]
	live.Final, live.ScoresPresent = true, true
	live.AwayScore, live.HomeScore = 17, 24
	// The live source contains no market fields; any grading overlay must
	// preserve the raw nflverse line and never reconcile the durable market.
	live.SpreadLinePresent, live.SpreadLineTenths = false, 0
	service.SetPickemFinalSource(func() []GameInfo { return []GameInfo{live} })

	request, err := http.NewRequest(http.MethodGet, "/pickem?week=3", nil)
	if err != nil {
		t.Fatal(err)
	}
	data := service.PickemDataReadOnly(request)
	rows, ok := data["games"].([]PickemGameRow)
	if !ok || len(rows) != 1 {
		t.Fatalf("Pick'em rows = %#v", data["games"])
	}
	if row := rows[0]; !row.Final || row.ScoreDisplay != "17-24" || !row.Correct || row.Winner != "MIA" || row.Outcome != string(pickemWin) {
		t.Fatalf("live final card = %+v", row)
	}
	record, ok := data["record"].(map[string]any)
	if !ok || record["week_wins"] != 1 || record["season_wins"] != 1 || record["streak"] != 1 {
		t.Fatalf("Pick'em records did not grade from the live final: %#v", data["record"])
	}
	for _, key := range []string{"leaderboard", "week_leaderboard"} {
		board, ok := data[key].([]PickemLeaderboardEntry)
		if !ok || len(board) != 1 || board[0].Wins != 1 || board[0].Total != 1 {
			t.Fatalf("%s = %#v, want one 1-0 live-final entrant", key, data[key])
		}
	}
	home := service.pickemHomeSummary(request, now)
	if home["season_correct"] != 1 || home["streak"] != 1 {
		t.Fatalf("home Pick'em summary = %#v, want the same live-final record", home)
	}
	if got := service.store.Snapshot().PickemMarkets[base[0].ID]; !reflect.DeepEqual(got, marketBefore) {
		t.Fatalf("live result changed the frozen market: before=%+v after=%+v", marketBefore, got)
	}

	// Once the canonical mirror publishes a complete final, its later score
	// correction supersedes the temporary live score.
	corrected := base[0]
	corrected.Final, corrected.ScoresPresent = true, true
	corrected.AwayScore, corrected.HomeScore = 10, 31
	service.SetScheduleSource(func() []GameInfo { return []GameInfo{corrected} })
	if got := service.pickemSchedule()[0]; got.AwayScore != 10 || got.HomeScore != 31 {
		t.Fatalf("canonical final correction was masked by Tank01: %+v", got)
	}
}

func TestPickemLiveFinalOverlayRequiresFreshCompleteMatchingGame(t *testing.T) {
	kickoff := time.Date(2026, 9, 20, 17, 0, 0, 0, time.UTC)
	base := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "LA", Home: "WAS", SpreadLineTenths: 25, SpreadLinePresent: true}}
	invalid := []GameInfo{
		{ID: "g1", Week: 1, Away: "LA", Home: "WAS", Final: false, ScoresPresent: true, AwayScore: 10, HomeScore: 20},
		{ID: "g1", Week: 1, Away: "LA", Home: "WAS", Final: true, ScoresPresent: false, AwayScore: 10, HomeScore: 20},
		{ID: "other", Week: 1, Away: "LA", Home: "WAS", Final: true, ScoresPresent: true, AwayScore: 10, HomeScore: 20},
		{ID: "g1", Week: 2, Away: "LA", Home: "WAS", Final: true, ScoresPresent: true, AwayScore: 10, HomeScore: 20},
		{ID: "g1", Week: 1, Away: "WAS", Home: "LA", Final: true, ScoresPresent: true, AwayScore: 10, HomeScore: 20},
	}
	for _, candidate := range invalid {
		if got := overlayPickemLiveFinals(base, []GameInfo{candidate}); !reflect.DeepEqual(got, base) {
			t.Errorf("invalid live row %+v changed the canonical schedule: %+v", candidate, got)
		}
	}

	// A real 0-0 final remains distinguishable from missing score fields;
	// ties are preserved through OT and grade using the already frozen line.
	tied := GameInfo{ID: "g1", Week: 1, Away: "LA", Home: "WAS", Final: true, ScoresPresent: true}
	merged := overlayPickemLiveFinals(base, []GameInfo{tied})
	if len(merged) != 1 || !merged[0].Final || !merged[0].ScoresPresent || merged[0].AwayScore != 0 || merged[0].HomeScore != 0 {
		t.Fatalf("complete tied 0-0 final was not preserved: %+v", merged)
	}
	market := PickemMarket{Frozen: true, LinePresent: true, LineTenths: 0}
	grade := gradePickemAt(merged[0], market, "LA", time.Time{}, kickoff.Add(time.Hour))
	if grade.Outcome != pickemLoss {
		t.Fatalf("a tied OT score against a frozen pick'em line graded as %+v, want loss/push policy", grade)
	}
	if got := overlayPickemLiveFinals(base, nil); !reflect.DeepEqual(got, base) {
		t.Fatalf("missing/stale live snapshot changed the canonical schedule: %+v", got)
	}
}

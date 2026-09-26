package league

import (
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestPickemWaitsForMarketFreezeWithoutVoidingSavedPick(t *testing.T) {
	svc := newTestService(t, true)
	kickoff := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	now := kickoff.Add(-time.Hour)
	svc.now = func() time.Time { return now }
	games := []GameInfo{{ID: "freeze-pending", Week: 1, Kickoff: kickoff, Away: "BUF", Home: "MIA", SpreadLinePresent: true, SpreadLineTenths: 35, SourceObservedAt: now}}
	svc.SetScheduleSource(func() []GameInfo { return games })
	request := httptest.NewRequest("GET", "/pickem?week=1", nil)
	if _, err := svc.PickemSet(request, games[0].ID, "MIA"); err != nil {
		t.Fatal(err)
	}
	now = kickoff
	before := svc.store.Snapshot()
	rows := svc.PickemData(request)["games"].([]PickemGameRow)
	if len(rows) != 1 || rows[0].Void || !rows[0].Locked || rows[0].ResultLabel != "LOCKED · WAITING FOR LINE" {
		t.Fatalf("saved pick before the freeze tick must be locked and pending, not void: %+v", rows)
	}
	if !reflect.DeepEqual(before, svc.store.Snapshot()) {
		t.Fatal("reading the pending freeze mutated durable state")
	}
	if _, err := svc.PickemSet(request, games[0].ID, "BUF"); err == nil {
		t.Fatal("waiting for the market freeze reopened the pick lock")
	}
	if err := svc.pickemMarketTick(now); err != nil {
		t.Fatal(err)
	}
	rows = svc.PickemData(request)["games"].([]PickemGameRow)
	if rows[0].Void || rows[0].ResultLabel != "LOCKED · IN PROGRESS" {
		t.Fatalf("frozen pick did not leave the pending state: %+v", rows[0])
	}
}

func TestPickemUnresolvedMarketDoesNotGradeFinalScores(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	game := GameInfo{Kickoff: now.Add(-time.Hour), Away: "BUF", Home: "MIA", HomeScore: 27, AwayScore: 20, Final: true, ScoresPresent: true}
	for _, market := range []PickemMarket{{}, {LinePresent: true, LineTenths: 35}} {
		got := gradePickemAt(game, market, "MIA", game.Kickoff.Add(-time.Hour), now)
		if got.Outcome != pickemPending || pickemResultLabel(got, true, false, true) != "LOCKED · WAITING FOR LINE" {
			t.Fatalf("unresolved market graded as %+v", got)
		}
	}
	if got := gradePickemAt(game, PickemMarket{Void: true}, "MIA", game.Kickoff.Add(-time.Hour), now); got.Outcome != pickemVoid {
		t.Fatalf("durable void decision was lost: %+v", got)
	}
}

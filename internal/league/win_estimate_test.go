package league

import (
	"strconv"
	"strings"
	"testing"
)

func TestRemainingWinEstimate(t *testing.T) {
	pool := map[string]Player{"a": {ID: "a", Projection: 20}, "b": {ID: "b", Projection: 20}}
	a := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "a", NFLTeam: "MIA", Points: 10, JoinState: "matched"}}}
	b := ScoreTeam{StarterLedger: []StarterLedgerRow{{PlayerID: "b", NFLTeam: "BUF", JoinState: "matched"}}}
	parse := func(value string) int {
		t.Helper()
		n, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
		if err != nil {
			t.Fatal(value)
		}
		return n
	}
	pre := parse(matchupWinEstimate(a, b, MatchupStateScheduled, pool, LiveStatus{}, false))
	lateStatus := LiveStatus{Games: map[string]LiveGameState{"MIA": {InProgress: true, Period: "Q4"}, "BUF": {InProgress: true, Period: "Q4"}}}
	late := parse(matchupWinEstimate(a, b, MatchupStateInProgress, pool, lateStatus, true))
	if late <= pre || late >= 100 {
		t.Fatalf("pre=%d late=%d", pre, late)
	}
	if late+parse(matchupWinEstimate(b, a, MatchupStateInProgress, pool, lateStatus, true)) != 100 {
		t.Fatal("not complementary")
	}
	if got := matchupWinEstimate(a, b, MatchupStateDegraded, pool, lateStatus, true); got != "" {
		t.Fatal(got)
	}
	delete(pool, "b")
	if got := matchupWinEstimate(a, b, MatchupStateScheduled, pool, LiveStatus{}, false); got != "" {
		t.Fatal(got)
	}
	// A completed player's missing projection is harmless; missing actuals aren't.
	b.StarterLedger[0].GameFinal = true
	if got := matchupWinEstimate(a, b, MatchupStateInProgress, pool, LiveStatus{}, false); got == "" {
		t.Fatal("finished player required projection")
	}
	b.StarterLedger[0].JoinState = "missing-join"
	if got := matchupWinEstimate(a, b, MatchupStateInProgress, pool, LiveStatus{}, false); got != "" {
		t.Fatal("missing actual treated as zero")
	}
}

func TestWinEstimateVerifiedFinal(t *testing.T) {
	a, b := ScoreTeam{Score: 100, ScoreKnown: true}, ScoreTeam{Score: 90, ScoreKnown: true}
	for _, tc := range []struct {
		a, b ScoreTeam
		want string
	}{{a, b, "WON"}, {b, a, "LOST"}, {a, a, "TIED"}, {ScoreTeam{}, b, ""}} {
		if got := matchupWinEstimate(tc.a, tc.b, MatchupStateFinal, nil, LiveStatus{}, false); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}

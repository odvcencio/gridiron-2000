package livescore

import (
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
)

func TestLivePunterBoxScoreSurvivesSnapshotAndLeagueOverlay(t *testing.T) {
	box := fantasy.ParseBoxScore([]byte(`{"gameID":"20260913_TB@CIN","away":"TB","home":"CIN","currentPeriod":"2nd","gameStatusCode":"1","playerStats":{"4608820":{"longName":"Ryan Rehkow","teamAbv":"CIN","Punting":{"punts":"1","puntYds":"51","puntLong":"51","puntsin20":"0","puntTouchBacks":"0"}}}}`))
	snapshot := SnapshotFromBoxScores(1, time.Now(), box)
	resolve := func(id, name string) (league.Player, bool) {
		return league.Player{ID: id, Name: name, Position: "P", NFLTeam: "CIN"}, true
	}
	lines := MergeLines(nil, 1, snapshot, resolve)
	if len(lines) != 1 || lines[0].Key != "ryanrehkow|P" || lines[0].Source != league.StatSourceLive {
		t.Fatalf("punter missing from live overlay: %+v", lines)
	}
	stats := lines[0].Stats
	if stats["puntYards"] != 51 || stats["puntLong50"] != 1 {
		t.Fatalf("normalized punting rules = %v", stats)
	}
	if got := stats["puntYards"]*.02 + stats["puntLong50"]; got != 2.02 {
		t.Fatalf("51-yard punt scoring = %v, want 2.02", got)
	}
}

// The week 2 2026 Monday box placed Kyren Williams's lost fumble under
// Defense. The live score used to omit its two-point deduction while the
// later weekly ledger included it, reversing a matchup at week close.
func TestLiveOffensiveFumbleInDefenseGroupScoresBeforeFinal(t *testing.T) {
	box := fantasy.ParseBoxScore([]byte(`{"gameID":"20260921_NYG@LAR","away":"NYG","home":"LAR","gameStatusCode":"1","currentPeriod":"4th","playerStats":{"4430737":{"longName":"Kyren Williams","teamAbv":"LAR","Rushing":{"rushYds":"85"},"Receiving":{"receptions":"2","recYds":"12","recTD":"1"},"Defense":{"fumblesLost":"1"}}}}`))
	resolve := func(id, name string) (league.Player, bool) {
		return league.Player{ID: id, Name: name, Position: "RB", NFLTeam: "LAR"}, true
	}
	for _, final := range []bool{false, true} {
		box.Final = final
		lines := MergeLines(nil, 2, SnapshotFromBoxScores(2, time.Now(), box), resolve)
		if len(lines) != 1 || lines[0].Stats["fumbleLost"] != 1 {
			t.Fatalf("final=%v: lost fumble missing from live line: %+v", final, lines)
		}
		if points := league.ScoreRuleStats(lines[0].Stats, nil); points != 14.7 {
			t.Fatalf("final=%v: Kyren score = %.2f, want 14.70", final, points)
		}
	}
}

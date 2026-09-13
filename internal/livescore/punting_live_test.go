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

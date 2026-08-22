package openstats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSchedulesPreservesOptionalSpreadAndResultPresence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.csv")
	csv := "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score,result,spread_line\n" +
		"blank,2026,REG,1,2026-09-10,20:15,AAA,,BBB,,,\n" +
		"pickem,2026,REG,1,2026-09-13,13:00,CCC,0,DDD,0,0,0\n" +
		"favorite,2026,REG,1,2026-09-14,20:15,EEE,17,FFF,24,7,3.5\n" +
		"underdog,2026,REG,1,2026-09-15,20:15,GGG,21,HHH,20,-1,-2.5\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	games, err := parseSchedules(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 4 {
		t.Fatalf("games = %d, want 4", len(games))
	}
	if games[0].SpreadLine != nil || games[0].HasResult() {
		t.Fatalf("blank values lost optionality: %+v", games[0])
	}
	if games[1].SpreadLine == nil || *games[1].SpreadLine != 0 || !games[1].HasResult() {
		t.Fatalf("true zero values were treated as blank: %+v", games[1])
	}
	if got := *games[2].SpreadLine; got != 3.5 {
		t.Fatalf("positive half line = %v", got)
	}
	if got := *games[3].SpreadLine; got != -2.5 {
		t.Fatalf("negative half line = %v", got)
	}
}

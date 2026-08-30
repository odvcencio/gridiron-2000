package replay

import (
	"path/filepath"
	"testing"

	"gridiron-2000/internal/fantasy"
)

// loadGame loads the BAL-BUF play-by-play fixture. The testdata file uses
// a hyphen, not "@" (box-20250907_BAL-BUF-pbp.json): a literal "@" here
// would read as an email-shaped string to privacy_contract_test.go (see
// the plan's "Testdata naming" note).
func loadGame(t *testing.T) *Game {
	t.Helper()
	game, err := Load(filepath.Join("testdata", "box-20250907_BAL-BUF-pbp.json"))
	if err != nil {
		t.Fatal(err)
	}
	return game
}

func TestFramesAreCumulativeAndEndOnTheRealFinal(t *testing.T) {
	game := loadGame(t)
	frames := game.Frames()
	if len(frames) != game.PlayCount()+2 {
		t.Fatalf("frames = %d want plays+2 (pre-game and final)", len(frames))
	}
	pre := fantasy.ParseBoxScore(frames[0].Body)
	if pre.Period != "" || pre.Final || pre.InProgress || len(pre.Players) != 0 || pre.AwayPoints != 0 || pre.StatusCode != "0" {
		t.Fatalf("pre-game frame = %+v", pre)
	}
	mid := fantasy.ParseBoxScore(frames[len(frames)/2].Body)
	if mid.Final || !mid.InProgress || mid.Period == "" || mid.Clock == "" || mid.StatusCode != "1" {
		t.Fatalf("mid frame status = %+v", mid)
	}
	last := fantasy.ParseBoxScore(frames[len(frames)-1].Body)
	if !last.Final || last.AwayPoints != 40 || last.HomePoints != 41 {
		t.Fatalf("final frame = %+v", last)
	}
	// Only passYds reconciles play by play with the final box; int and
	// rushTD step at the final frame. Frame N (the last in-progress frame)
	// must therefore carry the final passing yards already.
	lastInProgress := fantasy.ParseBoxScore(frames[len(frames)-2].Body)
	if got, want := lastInProgress.Players["3918298"].Stats["passYds"], last.Players["3918298"].Stats["passYds"]; got == 0 || got != want {
		t.Fatalf("frame N passYds = %v, final = %v", got, want)
	}
	if lastInProgress.Final || lastInProgress.StatusCode != "1" {
		t.Fatalf("frame N must still be in progress: %+v", lastInProgress)
	}
}

func TestFramesCarryScoreAndDSTFromScoringPlays(t *testing.T) {
	game := loadGame(t)
	var sawLead bool
	for _, frame := range game.Frames() {
		box := fantasy.ParseBoxScore(frame.Body)
		if box.DST["BAL"]["ptsAllowed"] != box.HomePoints || box.DST["BUF"]["ptsAllowed"] != box.AwayPoints {
			t.Fatalf("ptsAllowed drifted from the score: %+v", box)
		}
		if box.AwayPoints > box.HomePoints {
			sawLead = true
		}
	}
	if !sawLead {
		t.Fatal("BAL led at some point in this game; the score track never showed it")
	}
}

package league

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestMakePickAppliesScarcityGuard is rules-audit item 3: autopickChoice
// already refused a scarcity-blocked candidate
// (positionScarcityBlocksCandidate, draftclock.go), but MakePick — the
// manual-pick path — never consulted the guard, so a manager whose own
// punter requirement was already covered could keep drafting more
// punters, one seat at a time, while a peer had not yet drafted a first
// one and the league's punter supply ran out from under them. This test
// puts team-1 in exactly that position (its own P slot already filled;
// every other seat still needs one) and checks MakePick refuses the
// second punter with a plain-language reason, then still allows a
// non-scarce pick through unblocked.
//
// Revert proof: remove MakePick's scarcity check (rules-audit item 3's
// own fix) and this test fails — the second punter pick would succeed.
func TestMakePickAppliesScarcityGuard(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"]) // P: 1 slot, 8 teams
	t.Cleanup(clearRosterShape)
	draftAt := time.Date(2026, 9, 6, 20, 5, 0, 0, time.UTC)
	service, _ := newClockTestService(t, true, draftAt, draftAt) // demo mode: teamID follows teamOnClock

	punterID := func(index int) string { return fmt.Sprintf("scarcity-p-%03d", index) }
	pool := make([]Player, 0, 24)
	for index := 0; index < 8; index++ { // 8 punters total
		pool = append(pool, Player{
			ID: punterID(index), Name: fmt.Sprintf("Scarcity Punter %03d", index),
			Position: "P", NFLTeam: "TST", ADP: float64(900 + index), ADPRank: 900 + index,
			Projection: 9.0 - float64(index)*0.05,
		})
	}
	wrID := func(index int) string { return fmt.Sprintf("scarcity-wr-%03d", index) }
	for index := 0; index < 16; index++ { // filler for the 15 dummy picks plus one spare
		pool = append(pool, Player{
			ID: wrID(index), Name: fmt.Sprintf("Scarcity Wideout %03d", index),
			Position: "WR", NFLTeam: "TST", ADP: float64(index + 1), ADPRank: index + 1,
			Projection: 12.0 - float64(index)*0.05,
		})
	}
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	// Picks 1..16: team-1 drafts its one punter at pick 1; picks 2..16
	// (teams 2..8, cycling) draft filler WRs — no other seat drafts a
	// punter. Pick 17 (round 3, snake order) is team-1 on the clock again.
	teams := defaultTeamIDs()
	picks := make([]DraftPick, 0, 16)
	picks = append(picks, DraftPick{Number: 1, Round: 1, TeamID: teams[0], PlayerID: punterID(0), MadeAt: draftAt})
	for number := 2; number <= 16; number++ {
		teamID := teamOnClock(nil, number)
		picks = append(picks, DraftPick{
			Number: number, Round: pickRound(len(teams), number), TeamID: teamID,
			PlayerID: wrID(number - 2), MadeAt: draftAt,
		})
	}
	service.store.state.Picks = picks

	request, _ := http.NewRequest(http.MethodPost, "/draft", nil)

	// Team-1's own P requirement is already covered (punterID(0)); the
	// other 7 seats have drafted zero punters; 7 of the 8 punters remain
	// undrafted. supply (7) <= stillMissing (7): the guard must block.
	_, _, _, err := service.MakePick(request, teams[0], punterID(1))
	if err == nil {
		t.Fatal("MakePick must refuse a scarcity-blocked second punter for team-1")
	}
	want := "Only 7 punters are left for 7 teams that still need one; pick another position first."
	if err.Error() != want {
		t.Fatalf("MakePick scarcity error = %q, want %q", err.Error(), want)
	}
	for _, id := range currentRosters(service.store.Snapshot())[teams[0]] {
		if id == punterID(1) {
			t.Fatal("a refused pick must not land on any roster")
		}
	}

	// The same seat, the same pick number, a non-scarce WR: unblocked.
	pick, player, _, err := service.MakePick(request, teams[0], wrID(15))
	if err != nil {
		t.Fatalf("MakePick for a non-scarce position must succeed: %v", err)
	}
	if pick.PlayerID != wrID(15) || player.ID != wrID(15) {
		t.Fatalf("MakePick returned %+v / %+v, want player %s", pick, player, wrID(15))
	}
}

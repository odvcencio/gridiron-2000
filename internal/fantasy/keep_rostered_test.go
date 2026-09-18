package fantasy

import "testing"

// TestMergePoolKeepsEveryRosteredPlayer pins the invariant the free-agent
// fix broke on 2026-09-18: a player a league team rosters must never fall
// out of the pool, whatever his rank. Johnny Hekker, released and so
// unprojected, lost his punter slot to projected punters, and because the
// league resolves rosters through the pool, he vanished from the roster
// that owned him and could not even be dropped.
func TestMergePoolKeepsEveryRosteredPlayer(t *testing.T) {
	base := map[string]Player{
		"p1":    {ID: "p1", Name: "Ranked Punter", Position: "P", NFLTeam: "BUF", Projection: 8},
		"p2":    {ID: "p2", Name: "Second Punter", Position: "P", NFLTeam: "NE", Projection: 7},
		"15153": {ID: "15153", Name: "Johnny Hekker", Position: "P", NFLTeam: FreeAgentTeam},
		"w1":    {ID: "w1", Name: "Deep Receiver", Position: "WR", NFLTeam: "KC"},
	}
	// Everyone but the released punter carries a provider projection, so
	// he sorts last and falls past a limit of 2 without the keep rule.
	projections := map[string]projEntry{"p1": {Points: 8}, "p2": {Points: 7}, "w1": {Points: 9}}
	keep := map[string]bool{"15153": true}
	pool := mergePoolKeeping(base, nil, projections, nil, nil, 2, nil, nil, keep)
	found := false
	for _, player := range pool {
		if player.ID == "15153" {
			found = true
			if player.NFLTeam != FreeAgentTeam || player.Projection != 0 {
				t.Fatalf("kept rostered free agent = %+v, want FA with no projection", player)
			}
		}
	}
	if !found {
		t.Fatalf("rostered player 15153 fell out of a limit-2 pool: %+v", pool)
	}
	if len(pool) != 3 {
		t.Fatalf("pool size = %d, want the limit of 2 plus exactly the one kept rostered player: %+v", len(pool), pool)
	}
	without := mergePool(base, nil, projections, nil, nil, 2, nil, nil)
	for _, player := range without {
		if player.ID == "15153" {
			t.Fatal("fixture is wrong: the free agent survives the limit even without a keep set")
		}
	}
}

// TestKeptPlayersMissingTriggersAnImmediateSync pins the startup half: a
// cached pool that lacks a rostered player is refreshed at once rather
// than an interval later, so a deploy restores that player immediately.
func TestKeptPlayersMissingTriggersAnImmediateSync(t *testing.T) {
	cached := []Player{{ID: "p1"}, {ID: "p2"}}
	if !keptPlayersMissing(cached, map[string]bool{"p1": true, "15153": true}) {
		t.Fatal("a rostered player absent from the cache must trigger a sync")
	}
	if keptPlayersMissing(cached, map[string]bool{"p1": true}) {
		t.Fatal("a cache holding every rostered player must not trigger a sync")
	}
	if keptPlayersMissing(cached, nil) {
		t.Fatal("no keep set must never trigger a sync")
	}
}

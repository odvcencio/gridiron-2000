package league

import "testing"

// This file reproduces the two draft-night defects the owner reported
// after the 2026-09-06 draft (Wave D debrief): one team ended the real
// draft holding six quarterbacks, and elsewhere a manager's Big Board #1
// (house rank about 140) was autopicked at his second selection, pick 16
// overall. Both are fixed in draftclock.go: autopickPositionCap (a
// roster-shape-derived per-position ceiling AUTOPICK enforces) and
// autopickBoardEntryFailsValueGuard (a board-first value check). Every
// test below runs against the shipped gridiron-house/8 preset, the real
// league's own roster shape.

// TestAutopickPositionCapTable is autopickPositionCap's own table-driven
// pin: cap and applies for every position gridiron-house/8 fields, at a
// representative early round and inside the final two rounds.
func TestAutopickPositionCapTable(t *testing.T) {
	preset := rosterPresets["gridiron-house"]
	totalRounds := preset.Total() // 17 for gridiron-house (11 starters + 6 bench)
	cases := []struct {
		name         string
		position     string
		currentRound int
		wantLimit    int
		wantApplies  bool
	}{
		// QB/RB/WR/TE: the sum of every eligible starter slot (own slot
		// plus FLEX/SUPERFLEX where eligible) + 1, unaffected by round.
		// gridiron-house: QB fits QB+SUPERFLEX (1+1=2, +1=3); RB fits
		// RB+FLEX+SUPERFLEX (2+1+1=4, +1=5); WR the same shape as RB
		// (2+1+1=4, +1=5); TE fits TE+FLEX+SUPERFLEX (1+1+1=3, +1=4).
		{"QB early round", "QB", 1, 3, true},
		{"QB final round", "QB", totalRounds, 3, true},
		{"RB early round", "RB", 1, 5, true},
		{"WR early round", "WR", 1, 5, true},
		{"TE early round", "TE", 1, 4, true},
		// K/P/DST: flat cap of 1 until the final two rounds, then no cap.
		{"K early round", "K", 1, autopickPositionCapDefault, true},
		{"P mid round", "P", totalRounds - 3, autopickPositionCapDefault, true},
		{"DST just before final two", "DST", totalRounds - 2, autopickPositionCapDefault, true},
		{"K in the final two rounds lifts", "K", totalRounds - 1, 0, false},
		{"P in the last round lifts", "P", totalRounds, 0, false},
		{"DST in the last round lifts", "DST", totalRounds, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit, applies := autopickPositionCap(preset, tc.position, tc.currentRound, totalRounds)
			if applies != tc.wantApplies {
				t.Fatalf("autopickPositionCap(%s, round %d) applies = %v, want %v", tc.position, tc.currentRound, applies, tc.wantApplies)
			}
			if applies && limit != tc.wantLimit {
				t.Fatalf("autopickPositionCap(%s, round %d) limit = %d, want %d", tc.position, tc.currentRound, limit, tc.wantLimit)
			}
		})
	}
}

// TestAutopickPositionCapsSumCoverEveryPresetsRosterSize is
// autopickPositionCap's own load-bearing invariant, checked for every
// shipped preset: the sum of every position's own cap (pre-final-rounds,
// the strictest point) must be at least the preset's own Total() roster
// size. Underneath this: a team can always find SOME legal, in-cap
// position for every one of its picks, so autopickChoiceWith's own
// last-resort "drop the cap" pass (draftclock.go) stays a genuine rarity
// (a real supply shortage) rather than an arithmetic certainty the caps
// themselves create — the earlier "starters only, no FLEX/SUPERFLEX"
// cap shape summed to 13 against gridiron-house's own 17-slot roster and
// reliably forced that fallback by round 13 of a single-position-
// dominant pool; summing every ELIGIBLE slot (own position plus any
// FLEX/SUPERFLEX it also fills) instead closes that gap.
func TestAutopickPositionCapsSumCoverEveryPresetsRosterSize(t *testing.T) {
	for name, preset := range rosterPresets {
		t.Run(name, func(t *testing.T) {
			sum := 0
			for _, position := range []string{"QB", "RB", "WR", "TE", "K", "P", "DST"} {
				limit, applies := autopickPositionCap(preset, position, 1, preset.Total())
				if applies {
					sum += limit
				}
			}
			if sum < preset.Total() {
				t.Errorf("%s: position cap sum = %d, want at least Total() = %d", name, sum, preset.Total())
			}
		})
	}
}

// TestAutopickPositionCapPreventsQBHoarding reproduces the six-quarterback
// defect: a board-less team-1, autopicking against a QB-dominant pool
// (houseRankQBDominantPool — VORP favors QB at every step under
// gridiron-house's superflex slot, by construction), used to keep taking
// the next-best QB indefinitely; nothing capped it. This drives team-1's
// own autopick through a full gridiron-house/8 draft (CurrentDraftRounds,
// safe now that TestAutopickPositionCapsSumCoverEveryPresetsRosterSize
// holds — the cap sum comfortably covers all 17 rounds, so this never
// needs the last-resort "drop the cap" pass) and asserts team-1 never
// rosters more QBs than autopickPositionCap's own limit, while also
// proving the cap is the actual reason (not mere supply exhaustion): the
// pool carries 20 QBs, far more than any cap, so team-1 must be blocked
// by the guard, not by running out of quarterbacks to take.
func TestAutopickPositionCapPreventsQBHoarding(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, false)
	pool := houseRankQBDominantPool()
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	preset := rosterPresets["gridiron-house"]
	totalRounds := CurrentDraftRounds()
	wantCap, applies := autopickPositionCap(preset, "QB", 1, totalRounds)
	if !applies {
		t.Fatal("QB should always carry a cap (not a K/P/DST position)")
	}

	state := PersistedState{}
	qbCount := 0
	for round := 1; round <= totalRounds; round++ {
		playerID, ok := service.autopickChoice(state, "team-1")
		if !ok {
			t.Fatalf("autopickChoice stalled at round %d for team-1", round)
		}
		poolSnapshot := service.pool()
		player, exists := poolSnapshot.byID[playerID]
		if !exists {
			t.Fatalf("autopick chose %q, not found in the pool", playerID)
		}
		if player.Position == "QB" {
			qbCount++
		}
		if qbCount > wantCap {
			t.Fatalf("team-1 rostered a %dth QB (%s) at round %d — the position cap (%d) should have blocked it", qbCount, player.Name, round, wantCap)
		}
		state.Picks = append(state.Picks, DraftPick{Number: len(state.Picks) + 1, Round: round, TeamID: "team-1", PlayerID: playerID})
	}
	if qbCount < wantCap {
		t.Errorf("team-1 rostered only %d QBs across a full %d-round draft against a 20-QB-deep, QB-dominant pool, want exactly %d (the cap should be reached, not merely respected by coincidence)", qbCount, totalRounds, wantCap)
	}
}

// autopickBoardValueGuardFixtureState builds a 15-pick draft history
// ending just before overall pick 16: team-1's own first selection (a QB,
// so team-1's QB/SUPERFLEX requirement is already covered by the time its
// second selection comes up, matching the real draft-night sequence — "at
// his second selection"), plus 14 filler picks scattered across seven
// other seats (never team-1), so pickRound/nextPickNumber math lands
// exactly on pick 16 for team-1's next autopick call with no effect on
// team-1's own roster beyond that first QB.
func autopickBoardValueGuardFixtureState(firstPickPlayerID string) PersistedState {
	state := PersistedState{Picks: make([]DraftPick, 0, 15)}
	state.Picks = append(state.Picks, DraftPick{Number: 1, Round: 1, TeamID: "team-1", PlayerID: firstPickPlayerID})
	otherTeams := []string{"team-2", "team-3", "team-4", "team-5", "team-6", "team-7", "team-8"}
	for i := 0; i < 14; i++ {
		state.Picks = append(state.Picks, DraftPick{
			Number:   len(state.Picks) + 1,
			Round:    pickRound(8, len(state.Picks)+1),
			TeamID:   otherTeams[i%len(otherTeams)],
			PlayerID: "filler-pick-" + otherTeams[i%len(otherTeams)] + "-" + string(rune('a'+i)),
		})
	}
	return state
}

// TestAutopickBoardValueGuardAtPick16 reproduces the second draft-night
// defect directly: a manager's Big Board #1, house rank about 140, was
// autopicked at his second selection, pick 16 overall. rb-20's own house
// rank in houseRankFixturePool is 48 (well past the guard's 24-rank
// margin at pick 16: 48-16=32); the exact number differs from the real
// draft's ~140, but the shape — a far-off-value board head, a real open
// need nearby in house order, autopick spending an early pick on the
// sleeper anyway — is the same defect, and the guard's own margin is
// absolute, not scaled to the fixture's pool size.
func TestAutopickBoardValueGuardAtPick16(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	service := newTestService(t, false)
	pool := houseRankFixturePool()
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	member, _, err := service.store.AssignMember("board16@example.com", "Board16")
	if err != nil {
		t.Fatal(err)
	}
	// rb-20: house rank 48 (confirmed by this package's own autopick
	// log line), the far-off-value board head — team-1's SECOND
	// selection, at pick 16, mirrors the real draft-night sequence.
	if err := service.store.BoardAdd(member.Email, "rb-20"); err != nil {
		t.Fatal(err)
	}
	state := autopickBoardValueGuardFixtureState("qb-01")
	// Re-key the fixture's dummy first pick and every filler pick onto
	// member.TeamID / real presence — only the TeamID on state.Picks[0]
	// matters for ownPlayers below; swap it onto member.TeamID so the
	// "second selection" framing holds for this seat specifically.
	state.Picks[0].TeamID = member.TeamID
	got, ok := service.autopickChoice(state, member.TeamID)
	if !ok {
		t.Fatal("autopickChoice reported no legal candidate at pick 16")
	}
	if got == "rb-20" {
		t.Fatalf("autopickChoice took the far-off-value board head (rb-20, house rank 48) at pick 16 — the value guard should have redirected to a needed alternative")
	}
	poolSnapshot := service.pool()
	won, exists := poolSnapshot.byID[got]
	if !exists {
		t.Fatalf("autopickChoice returned %q, not a player in the pool", got)
	}
	if won.HouseRank == 0 || won.HouseRank-16 > autopickBoardValueGuardMargin {
		t.Errorf("the redirected pick %q carries house rank %d, more than %d ranks below pick 16 — the guard should redirect to a BETTER value, not another far-off one", got, won.HouseRank, autopickBoardValueGuardMargin)
	}
}

// TestAutopickBoardValueGuardHonorsBoardWhenNoNeedAlternativeExists is the
// value guard's own narrow-scope proof: a far-off-value board head still
// wins outright once every other seat requirement the guard could
// redirect toward is already covered — the guard exists to redirect
// autopick toward a real, still-open need, never to run its own private
// best-player-available pass over a manager's ranking.
func TestAutopickBoardValueGuardHonorsBoardWhenNoNeedAlternativeExists(t *testing.T) {
	farOff := Player{ID: "sleeper", Name: "Sleeper", Position: "WR", NFLTeam: "TST", Projection: 3, HouseRank: 500}
	fillsNoHole := func(string) bool { return false } // every requirement already covered
	fitsEverything := func(string) bool { return true }
	pool := playerPool{
		players: []Player{farOff},
		byID:    map[string]Player{farOff.ID: farOff},
		byHouse: []Player{farOff},
	}
	if autopickBoardEntryFailsValueGuard(farOff, 1, pool, map[string]bool{}, fitsEverything, fillsNoHole) {
		t.Error("the value guard fired even though house order has no needed alternative to redirect toward — it should only ever redirect toward a real, still-open need")
	}
}

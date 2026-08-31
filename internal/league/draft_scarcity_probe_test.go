package league

import (
	"fmt"
	"testing"

	"gridiron-2000/internal/fantasy"
)

// This file is the adversarial-review probe for finding 1 (2026-08-30):
// house-ordered autopick (houserank.go's pool.byHouse) clusters
// same-position players together by VORP, so a seat's spare bench pick
// can legally take a DUPLICATE scarce specialist while a later seat has
// not yet drafted its own required one. The reviewer proved this stalls
// forced-autopick full drafts in 4 of 6 supply shapes under house order
// while market-ADP order — which naturally interleaves positions — never
// stalls on any of them. positionScarcityBlocksCandidate (draftclock.go)
// is the fix; this file reproduces all six shapes as one table, run
// under both orders, so a future regression here is caught the same way
// the reviewer caught the original bug.

// probeTeamCount fixes every scarcity-probe shape at 8 teams, matching
// the reviewer's own "/8" shape names and defaultTeams()'s 8-team
// default.
const probeTeamCount = 8

// probeConvertFantasyPlayers adapts fantasy.Player rows (fantasy.OfflinePool's
// own type) onto league.Player, the same field set app/draft's own
// livePool() test helper uses.
func probeConvertFantasyPlayers(rows []fantasy.Player) []Player {
	out := make([]Player, 0, len(rows))
	for _, p := range rows {
		out = append(out, Player{ID: p.ID, Name: p.Name, Position: p.Position, NFLTeam: p.NFLTeam, ADP: p.ADP, ADPRank: p.ADPRank, Projection: p.Projection, Status: "Available"})
	}
	return out
}

// probeFilterPositions returns only players whose Position is one of
// positions, preserving order.
func probeFilterPositions(players []Player, positions ...string) []Player {
	allow := make(map[string]bool, len(positions))
	for _, position := range positions {
		allow[position] = true
	}
	out := make([]Player, 0, len(players))
	for _, player := range players {
		if allow[player.Position] {
			out = append(out, player)
		}
	}
	return out
}

// probeFillerPlayers synthesizes count low-ADP, low-name-collision-risk
// players at position, projections descending from floor by step per
// player — the same "flat, closely-bunched values" shape the review's own
// evidence blames for house order's clustering (houseRankSupplyFiller,
// app/draft/shell_render_test.go, uses the identical pattern).
func probeFillerPlayers(prefix, position string, count int, floor, step float64) []Player {
	out := make([]Player, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, Player{
			ID:         fmt.Sprintf("probe-%s-%03d", prefix, i+1),
			Name:       fmt.Sprintf("Probe %s Player %03d", prefix, i+1),
			Position:   position,
			NFLTeam:    "TST",
			ADP:        float64(2000 + i),
			ADPRank:    2000 + i,
			Projection: floor - float64(i)*step,
			Status:     "Available",
		})
	}
	return out
}

// scarcityProbeShape is one of the reviewer's six supply shapes.
type scarcityProbeShape struct {
	name   string
	preset RosterPreset
	pool   func() []Player
}

// scarcityProbeShapes reproduces the reviewer's six probe shapes:
//
//  1. OfflinePool raw x standard/8 — the embedded offline demo pool
//     (real skill-position depth, 12 K, 12 DST, no P — standard carries
//     no P slot, so that is not a gap here) under the conventional
//     standard preset.
//  2. OfflinePool raw x gridiron-house/8 — the same offline pool, topped
//     up with a P supply as thin as its own K/DST supply, under
//     gridiron-house (the reference league, which starts a punter and a
//     SUPERFLEX QB).
//  3. live-like 59P/32K/32DST — OfflinePool's skill rows only (QB/RB/WR/TE),
//     with K/DST/P supply set to the review's own live Tank01 shape
//     (tank01.go's own doc comment: "carries 59 punters ... verified
//     live"), under gridiron-house.
//  4. K==8 — kicker supply set to the exact standard/8 demand (8), every
//     other position generously supplied, under standard.
//  5. K==8+DST==8 — both K and DST pinned to exact demand, under standard.
//  6. P==8 — punter supply set to the exact gridiron-house/8 demand (8),
//     under gridiron-house.
func scarcityProbeShapes() []scarcityProbeShape {
	offline := func() []Player { return probeConvertFantasyPlayers(fantasy.OfflinePool()) }
	skillOnly := func() []Player {
		return probeFilterPositions(probeConvertFantasyPlayers(fantasy.OfflinePool()), "QB", "RB", "WR", "TE")
	}
	skillAndDST := func() []Player {
		return probeFilterPositions(probeConvertFantasyPlayers(fantasy.OfflinePool()), "QB", "RB", "WR", "TE", "DST")
	}
	return []scarcityProbeShape{
		{
			name:   "OfflinePool raw x standard/8",
			preset: rosterPresets["standard"],
			pool:   offline,
		},
		{
			name:   "OfflinePool raw x gridiron-house/8",
			preset: rosterPresets["gridiron-house"],
			pool: func() []Player {
				out := offline()
				return append(out, probeFillerPlayers("p", "P", 12, 7.0, 0.05)...)
			},
		},
		{
			name:   "live-like 59P/32K/32DST",
			preset: rosterPresets["gridiron-house"],
			pool: func() []Player {
				out := skillOnly()
				out = append(out, probeFillerPlayers("k", "K", 32, 9.0, 0.03)...)
				out = append(out, probeFillerPlayers("d", "DST", 32, 8.0, 0.03)...)
				out = append(out, probeFillerPlayers("p", "P", 59, 8.0, 0.02)...)
				return out
			},
		},
		{
			name:   "K==8",
			preset: rosterPresets["standard"],
			pool: func() []Player {
				out := skillAndDST()
				return append(out, probeFillerPlayers("k", "K", 8, 9.0, 0.05)...)
			},
		},
		{
			name:   "K==8+DST==8",
			preset: rosterPresets["standard"],
			pool: func() []Player {
				out := skillOnly()
				out = append(out, probeFillerPlayers("k", "K", 8, 9.0, 0.05)...)
				out = append(out, probeFillerPlayers("d", "DST", 8, 8.0, 0.05)...)
				return out
			},
		},
		{
			name:   "P==8",
			preset: rosterPresets["gridiron-house"],
			pool: func() []Player {
				out := offline()
				return append(out, probeFillerPlayers("p", "P", 8, 7.0, 0.05)...)
			},
		},
	}
}

// probeAutopickByADP mirrors the pre-house-rank autopick algorithm (plain
// market-ADP order, no scarcity guard) — this probe's control. It proves
// the six shapes above are a house-order clustering artifact, not a
// supply shortfall market ADP's own naturally interleaved order would
// also hit: the reviewer's own evidence found zero stalls under ADP order
// across all six.
func probeAutopickByADP(state PersistedState, pool playerPool, teamID string) (string, bool) {
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	fits := func(playerID string) bool {
		_, _, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, nil)
		return !breach && draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID)
	}
	for _, player := range pool.byADP {
		if !picked[player.ID] && fits(player.ID) {
			return player.ID, true
		}
	}
	for _, player := range pool.byADP {
		if !picked[player.ID] && draftCandidateKeepsRosterViable(state, pool.byID, teamID, player.ID) {
			return player.ID, true
		}
	}
	return "", false
}

// runProbeDraft simulates a full forced-autopick snake draft purely in
// memory under order ("house" reads service.autopickChoice;
// anything else reads probeAutopickByADP): no store writes, no clock, no
// boards, so every pick is that order's own best-available choice.
// stallAt is the 1-based pick number where no legal candidate remained
// (0 on a clean completion); rosters is every team's final drafted
// players, keyed by team ID, valid only on a clean completion.
func runProbeDraft(service *Service, order string) (stallAt int, rosters map[string][]Player) {
	state := PersistedState{}
	total := probeTeamCount * CurrentDraftRounds()
	pool := service.pool()
	for number := 1; number <= total; number++ {
		teamID := teamOnClock(nil, number)
		var playerID string
		var ok bool
		if order == "house" {
			playerID, ok = service.autopickChoice(state, teamID)
		} else {
			playerID, ok = probeAutopickByADP(state, pool, teamID)
		}
		if !ok {
			return number, nil
		}
		state.Picks = append(state.Picks, DraftPick{Number: number, Round: pickRound(probeTeamCount, number), TeamID: teamID, PlayerID: playerID})
	}
	rosters = make(map[string][]Player, probeTeamCount)
	for _, teamID := range defaultTeamIDs() {
		players, _ := teamDraftedPlayers(state, pool.byID, teamID)
		rosters[teamID] = players
	}
	return 0, rosters
}

// TestAutopickScarcityGuardAcrossSixSupplyShapes is the finding 1 probe
// table: every shape, under both house order (the scarcity-guarded
// production path) and ADP order (the control), must complete a full
// forced-autopick draft with zero stalls and every seat's final roster
// legal (every starter slot filled).
func TestAutopickScarcityGuardAcrossSixSupplyShapes(t *testing.T) {
	for _, shape := range scarcityProbeShapes() {
		for _, order := range []string{"house", "adp"} {
			t.Run(shape.name+"/"+order, func(t *testing.T) {
				setRosterShape(shape.preset)
				t.Cleanup(clearRosterShape)
				service := newTestService(t, false)
				pool := shape.pool()
				service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

				stallAt, rosters := runProbeDraft(service, order)
				if stallAt != 0 {
					t.Fatalf("%s under %s order stalled at pick %d of %d (supply %d players)", shape.name, order, stallAt, probeTeamCount*CurrentDraftRounds(), len(pool))
				}
				preset := CurrentRoster()
				for _, teamID := range defaultTeamIDs() {
					roster := rosters[teamID]
					if len(roster) != CurrentDraftRounds() {
						t.Errorf("%s/%s: %s roster = %d players, want %d", shape.name, order, teamID, len(roster), CurrentDraftRounds())
					}
					if filled := maximumDraftStarterFill(roster, preset); filled != preset.Starters() {
						t.Errorf("%s/%s: %s fills %d/%d starter slots, want every slot filled", shape.name, order, teamID, filled, preset.Starters())
					}
				}
			})
		}
	}
}

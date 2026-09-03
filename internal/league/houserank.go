package league

import (
	"math"
	"sort"
	"sync/atomic"
)

// houserank.go computes HOUSE RANK: a format-aware replacement-value
// (VORP) ranking under the league's ACTIVE roster preset and team count,
// shown beside market ADP (never instead of it — see model.go's
// Player.HouseRank and service.go's playerMap). The pool board itself, and
// every "best available" DISPLAY consumer, stay market-ADP-ordered;
// autopickChoice (draftclock.go) is the one consumer that reads house
// order instead. See docs/season-operations.md's "House rank" section for
// the model in plain language.
//
// The model, in three steps:
//
//  1. Demand per position: each real position's own preset slot count
//     times the team count (housePositionDemand's fixed-slot pass), plus
//     its share of the league-wide FLEX/SUPERFLEX slots, greedily
//     allocated by per-game Projection (allocateFlexSuperflex).
//  2. Replacement level per position: the Projection of the best player
//     left at that position once demand is filled — the (demand+1)-th
//     best (houseReplacementLevels). A position whose pool is no larger
//     than its own demand has no such player, so its replacement level is
//     0.
//  3. HouseVORP = Projection minus that position's replacement level.
//     HouseRank is 1..N over every player with a positive Projection,
//     ordered by VORP descending, ties broken by market ADP ascending (an
//     ADP of 0 sorts last), then Name, then ID (applyHouseRanks). A
//     zero-Projection player carries no house rank (HouseRank 0).
//
// This model uses starter demand only — it carries no bench-depth term.

// housePositionOrder lists the real, non-flex positions the house-rank
// model demands and ranks: slotTable's keys minus FLEX and SUPERFLEX,
// which are not real player positions and never appear on a Player.
var housePositionOrder = []string{"QB", "RB", "WR", "TE", "DST", "K", "P"}

// houseFlexPriority is allocateFlexSuperflex's fixed position-evaluation
// order: QB, then RB, WR, TE. It is the deterministic tie-break when two
// positions' next-unclaimed players carry the exact same per-game
// Projection at the same greedy step — never input order or map order.
var houseFlexPriority = []string{"QB", "RB", "WR", "TE"}

// houseRankBuildCalls is a test seam (mirrors performanceBaseOrderCalls,
// waivers.go): nil in production, so applyHouseRanks pays no cost beyond
// one atomic load per call. A test that must prove applyHouseRanks runs
// once per pool version — not once per render — installs a counting func
// here and must clear it afterward (setHouseRankBuildCalls(nil)).
var houseRankBuildCalls atomic.Pointer[func()]

// setHouseRankBuildCalls installs fn as the test seam, or clears it when
// fn is nil.
func setHouseRankBuildCalls(fn func()) {
	if fn == nil {
		houseRankBuildCalls.Store(nil)
		return
	}
	houseRankBuildCalls.Store(&fn)
}

// applyHouseRanks returns a copy of players with HouseRank computed per
// the house-rank model above; every other field is unchanged. Called once
// per pool version from buildPool (service.go), alongside the existing
// byADP build — not once per render.
func applyHouseRanks(players []Player, preset RosterPreset, teamCount int) []Player {
	if fn := houseRankBuildCalls.Load(); fn != nil {
		(*fn)()
	}
	out := make([]Player, len(players))
	copy(out, players)
	for i := range out {
		out[i].HouseRank = 0
	}

	demand := housePositionDemand(out, preset, teamCount)
	replacement := houseReplacementLevels(out, demand)

	ranked := make([]int, 0, len(out))
	for i, player := range out {
		if player.Projection > 0 {
			ranked = append(ranked, i)
		}
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		left, right := out[ranked[a]], out[ranked[b]]
		vorpLeft := left.Projection - replacement[left.Position]
		vorpRight := right.Projection - replacement[right.Position]
		if vorpLeft != vorpRight {
			return vorpLeft > vorpRight
		}
		adpLeft, adpRight := left.ADP, right.ADP
		if adpLeft <= 0 {
			adpLeft = math.MaxFloat64
		}
		if adpRight <= 0 {
			adpRight = math.MaxFloat64
		}
		if adpLeft != adpRight {
			return adpLeft < adpRight
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
	assignHouseRanksWithSanityClamp(out, ranked)
	return out
}

// houseRankSanityClampMargin (rules-audit item 5, data-quality/optional):
// under superflex demand, QB demand is high enough (every seat's own QB
// slot plus most SUPERFLEX slots) that the QB replacement level sits high
// too, compressing QB VORP — a real, intended format effect, not a bug.
// But that compression can let a thin-evidence player (a rookie with a
// handful of starts behind the projection, not a full season) VORP-
// outrank far more proven names by a wide margin: Jaxson Dart, ADP rank
// 96, reached HouseRank 15 under gridiron-house/8 superflex before this
// clamp (see TestApplyHouseRanksClampsExtremeADPDivergence). 60 places is
// wide on purpose — ordinary format-driven VORP compression routinely
// moves a player 10-30 places from ADP and must never be touched; only an
// extreme divergence between the model and the market gets pulled back
// toward the market, and only far enough to stop being extreme, not all
// the way to ADP itself.
const houseRankSanityClampMargin = 60

// assignHouseRanksWithSanityClamp assigns each ranked player (VORP order,
// index into out) a final 1..N HouseRank, honoring houseRankSanityClampMargin:
// a player's natural VORP position (1-based index into ranked) is
// promoted no more than houseRankSanityClampMargin places ahead of its
// own ADPRank. A player with no ADP (ADPRank <= 0) is never clamped —
// there is no market signal to clamp against, matching applyHouseRanks'
// own "skip players with no ADP" rule for this fix.
//
// Implementation: each player's sortKey is the larger of its natural
// VORP position and its clamp floor (ADPRank - houseRankSanityClampMargin,
// when that exceeds the natural position); a stable sort by sortKey turns
// those floors into a single valid permutation. When no player's floor
// ever exceeds its natural position — the overwhelmingly common case —
// every sortKey equals its natural position and this reproduces the
// pre-clamp order exactly, so the clamp changes nothing until it is
// actually needed.
//
// The tiebreak when two candidates share a sortKey matters more than it
// looks: a clamped candidate's sortKey is its FLOOR, a promise of "no
// better than this," not its true position, so on a tie it must sort
// AFTER the unclamped candidate whose natural position genuinely earned
// that slot — sorting by natural position alone would let the clamped
// candidate win the tie (its natural is always smaller, that is what
// clamping means) and land one place BETTER than its own floor,
// silently breaking the promise by exactly one rank. Ordering
// (clamped after unclamped, then natural ascending within either group)
// closes that gap; TestApplyHouseRanksClampTiebreakDoesNotBeatItsFloor
// pins it.
func assignHouseRanksWithSanityClamp(out []Player, ranked []int) {
	type clampCandidate struct {
		playerIndex int
		natural     int
		sortKey     int
		clamped     bool
	}
	candidates := make([]clampCandidate, len(ranked))
	for i, playerIndex := range ranked {
		natural := i + 1
		sortKey := natural
		clamped := false
		if adpRank := out[playerIndex].ADPRank; adpRank > 0 {
			if floor := adpRank - houseRankSanityClampMargin; floor > sortKey {
				sortKey = floor
				clamped = true
			}
		}
		candidates[i] = clampCandidate{playerIndex: playerIndex, natural: natural, sortKey: sortKey, clamped: clamped}
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		if candidates[a].sortKey != candidates[b].sortKey {
			return candidates[a].sortKey < candidates[b].sortKey
		}
		if candidates[a].clamped != candidates[b].clamped {
			return !candidates[a].clamped // unclamped wins the tie; see doc comment above
		}
		return candidates[a].natural < candidates[b].natural
	})
	for rank, candidate := range candidates {
		out[candidate.playerIndex].HouseRank = rank + 1
	}
}

// housePositionDemand computes the active preset's starter demand per real
// position (housePositionOrder) at teamCount teams: each position's own
// preset.Slots count times teamCount, plus its share of the league-wide
// greedy FLEX/SUPERFLEX allocation (allocateFlexSuperflex). players
// supplies each position's per-game Projection order for that greedy
// fill.
func housePositionDemand(players []Player, preset RosterPreset, teamCount int) map[string]int {
	demand := make(map[string]int, len(housePositionOrder))
	for _, position := range housePositionOrder {
		demand[position] = preset.Slots[position] * teamCount
	}
	sorted := housePlayersByPosition(players)
	allocateFlexSuperflex(sorted, preset, teamCount, demand)
	return demand
}

// housePlayersByPosition groups players by Position, each group's slice
// sorted by per-game Projection descending (ties keep the group's
// original relative order, sort.SliceStable) — the "next-unclaimed
// player" order both allocateFlexSuperflex and houseReplacementLevels
// read.
func housePlayersByPosition(players []Player) map[string][]Player {
	grouped := make(map[string][]Player, len(housePositionOrder))
	for _, player := range players {
		grouped[player.Position] = append(grouped[player.Position], player)
	}
	for _, list := range grouped {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Projection > list[j].Projection })
	}
	return grouped
}

// allocateFlexSuperflex greedily distributes the preset's FLEX (RB/WR/TE
// eligible) and SUPERFLEX (QB/RB/WR/TE eligible) starter slots across
// their eligible positions, adding each assignment onto demand in place.
// demand[position] must already carry that position's fixed-slot count
// (housePositionDemand); this treats it as each position's starting
// "next-unclaimed" pointer into sorted[position].
//
// It walks the flex-type slots one at a time: at each step, among every
// position with an unclaimed player left AND at least one open eligible
// slot type, it assigns the next flex-type slot to whichever position's
// next-unclaimed player carries the highest per-game Projection (a tie
// keeps houseFlexPriority's fixed QB/RB/WR/TE order). When the winning
// position is RB, WR, or TE — eligible for both slot types — the
// assignment spends a FLEX slot first, reserving SUPERFLEX capacity for a
// QB that might still need it; a QB, or an RB/WR/TE once FLEX is
// exhausted, spends a SUPERFLEX slot instead. Spending the less flexible
// slot type first is what lets QBs claim most of a superflex league's
// SUPERFLEX slots without a separate two-pass model: RB/WR/TE's own FLEX
// slots are used up first, in the same greedy order, and only then does
// the greedy comparison start pitting a weaker RB/WR/TE against a
// still-strong QB for the SUPERFLEX slots that remain.
func allocateFlexSuperflex(sorted map[string][]Player, preset RosterPreset, teamCount int, demand map[string]int) {
	flexSlots := preset.Slots["FLEX"] * teamCount
	superflexSlots := preset.Slots["SUPERFLEX"] * teamCount
	if flexSlots <= 0 && superflexSlots <= 0 {
		return
	}
	pointer := make(map[string]int, len(houseFlexPriority))
	for _, position := range houseFlexPriority {
		pointer[position] = demand[position]
	}
	for flexSlots > 0 || superflexSlots > 0 {
		best := ""
		bestProjection := 0.0
		for _, position := range houseFlexPriority {
			if position == "QB" && superflexSlots <= 0 {
				continue
			}
			list := sorted[position]
			index := pointer[position]
			if index >= len(list) {
				continue
			}
			projection := list[index].Projection
			if best == "" || projection > bestProjection {
				best = position
				bestProjection = projection
			}
		}
		if best == "" {
			break
		}
		pointer[best]++
		demand[best]++
		switch {
		case best == "QB":
			superflexSlots--
		case flexSlots > 0:
			flexSlots--
		default:
			superflexSlots--
		}
	}
}

// houseReplacementLevels resolves the replacement-level Projection per
// real position: the (demand+1)-th best player's per-game Projection at
// that position — the top player NOT absorbed by demand
// (housePositionDemand). A position whose pool is no larger than its own
// demand has no such player, so its replacement level is 0 (a position
// absent from the pool entirely resolves the same way, an empty list).
func houseReplacementLevels(players []Player, demand map[string]int) map[string]float64 {
	sorted := housePlayersByPosition(players)
	replacement := make(map[string]float64, len(housePositionOrder))
	for _, position := range housePositionOrder {
		list := sorted[position]
		index := demand[position]
		if index < len(list) {
			replacement[position] = list[index].Projection
		}
	}
	return replacement
}

// houseOrderedIndex returns players reordered for autopick: every player
// carrying a HouseRank, in HouseRank order (best VORP first), followed by
// every HouseRank-0 player in their existing relative order — players'
// own order (market ADP order), left completely unchanged. This keeps a
// forced pick that only a rankless position can legally fill (for example
// a punter pool too small to reach house-rank demand) finding its best
// pool-order candidate in the rankless tail, exactly as autopickChoice's
// pool.players walk always has.
func houseOrderedIndex(players []Player) []Player {
	ranked := make([]Player, 0, len(players))
	rest := make([]Player, 0, len(players))
	for _, player := range players {
		if player.HouseRank > 0 {
			ranked = append(ranked, player)
		} else {
			rest = append(rest, player)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].HouseRank < ranked[j].HouseRank })
	return append(ranked, rest...)
}

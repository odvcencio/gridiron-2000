package livescore

import (
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
)

// Resolver maps a Tank01 row onto a pool player (league.Service.ResolveLivePlayer).
type Resolver func(tank01ID, longName string) (league.Player, bool)

// MergeLines applies the A2 precedence rule per player key. A live row
// wins while its game is in progress (a stale all-zero ledger row must
// not hide live points). A ledger row wins once the game is final, or
// when live has no data for it — unless the ledger row is itself a
// partial mirror of the final live row (ledgerBehind), in which case the
// final live row wins until the ledger catches up. When the ledger wins
// outright (final and not behind), it still absorbs any category the
// final live row carries that the ledger's own stat mapping never
// reports at all (mergeLedgerOnlyCategories, GC-1 fix 4) — otherwise a
// scored return touchdown would vanish the moment the ledger posts. A
// live row with no ledger row is added. The D/ST key comes from DSTName
// and the pool's "{Nickname} D/ST" naming.
func MergeLines(base []league.WeekStatLine, week int, snapshot Snapshot, resolve Resolver) []league.WeekStatLine {
	live, ok := snapshot.Weeks[week]
	if !ok {
		return append([]league.WeekStatLine(nil), base...) // a copy: never return the caller's own slice
	}
	inProgress := map[string]bool{}
	inProgressGame := map[string]bool{}
	for _, game := range snapshot.Games {
		if game.Week == week && game.InProgress && !game.Final {
			inProgress[game.Away], inProgress[game.Home] = true, true
			inProgressGame[game.ID] = true
		}
	}
	index := make(map[string]int, len(base))
	out := make([]league.WeekStatLine, len(base))
	copy(out, base)
	for i, line := range out {
		index[line.Key] = i // ledger sources deduplicate by key already; a duplicate key here would just leave the earlier row stale and unreachable
	}
	apply := func(key, team, gameID string, stats map[string]float64, final bool) {
		// Tank01 sometimes omits teamAbv, leaving team empty (round-2
		// note 3); fall back to the game's own in-progress state so an
		// empty team never reads as "not in progress" by default.
		inProgressNow := inProgress[team]
		if team == "" {
			inProgressNow = inProgressGame[gameID]
		}
		line := league.WeekStatLine{Key: key, Stats: league.RuleStatsFromTank01(stats, final), Source: sourceFor(final)}
		if at, seen := index[key]; seen {
			switch {
			case inProgressNow:
				// live wins while the game runs
			case final && ledgerBehind(out[at].Stats, line.Stats):
				// the live side is final but the ledger mirror still holds a
				// partial (or all-zero) row; a final live row is never below
				// the truth, so it wins until the ledger catches up
			case final:
				// The ledger is complete and wins for every category it
				// reports (see ledgerBehind above). But the ledger's own
				// stat mapping (main.go's offenseStatLine) never carries
				// some live-only categories at all — returnTD is the one in
				// production today (breakdown.go's returnTD row doc
				// comment). Without this, a scored return touchdown a live
				// game reported would silently vanish the moment the ledger
				// posts (GC-1 fix 4). mergeLedgerOnlyCategories copies in
				// only the categories genuinely absent from the ledger row;
				// every category the ledger DOES report keeps its own
				// value untouched, so the ledger stays close-week truth for
				// everything it actually measures.
				if merged, ok := mergeLedgerOnlyCategories(out[at], line.Stats); ok {
					out[at] = merged
				}
				return
			default:
				return // ledger wins: live has no clock for it
			}
			out[at] = line
			return
		}
		index[key] = len(out)
		out = append(out, line)
	}
	for _, row := range live.Lines {
		player, ok := resolve(row.PlayerID, row.Name)
		if !ok {
			continue
		}
		apply(openstats.NormalizePlayerKey(player.Name, player.Position), row.Team, row.GameID, row.Stats, row.Final)
	}
	for team, unit := range live.DST {
		name, ok := DSTName(team)
		if !ok {
			continue
		}
		// A D/ST unit's team comes from the box score's own map key, so
		// it is never empty; the gameID fallback above never triggers,
		// hence the empty literal here.
		apply(openstats.NormalizePlayerKey(name, "DST"), team, "", unit.Stats, unit.Final)
	}
	return out
}

func sourceFor(final bool) string {
	if final {
		return league.StatSourceLiveFinal
	}
	return league.StatSourceLive
}

// ledgerBehind reports whether a ledger stat line is a stale partial
// mirror of a live rule-keyed stat line: true when the ledger row has no
// non-zero value at all (no data yet), or when every ledger value is <=
// the live value for the same key and at least one is strictly lower (the
// ledger has started catching up but has not finished). A ledger value
// that exceeds the live value at any key means the ledger already has
// data the live row lacks — not behind — so ledgerBehind is false and the
// ledger keeps winning (round-2 note 44).
func ledgerBehind(ledger, live map[string]float64) bool {
	anyNonZero := false
	anyLower := false
	for key, value := range ledger {
		if value != 0 {
			anyNonZero = true
		}
		if liveValue := live[key]; value > liveValue {
			return false
		} else if value < liveValue {
			anyLower = true
		}
	}
	if !anyNonZero {
		return true
	}
	return anyLower
}

// mergeLedgerOnlyCategories returns a copy of ledger with every stat key
// live carries but ledger.Stats itself does not — merged in — plus ok
// reporting whether it merged anything (GC-1 fix 4). A key the ledger
// already reports, even at a different value, keeps its own ledger value:
// only a category genuinely absent from the ledger row is ever copied
// from live. ledger.Stats is never mutated in place: out is a shallow
// copy of base (MergeLines above), so out[i].Stats and base[i].Stats
// start as the same map, and writing into it here would silently mutate
// the caller's own base slice.
func mergeLedgerOnlyCategories(ledger league.WeekStatLine, live map[string]float64) (league.WeekStatLine, bool) {
	var merged map[string]float64
	for key, value := range live {
		if _, exists := ledger.Stats[key]; exists {
			continue
		}
		if merged == nil {
			merged = make(map[string]float64, len(ledger.Stats)+1)
			for k, v := range ledger.Stats {
				merged[k] = v
			}
		}
		merged[key] = value
	}
	if merged == nil {
		return ledger, false
	}
	ledger.Stats = merged
	ledger.Source = league.StatSourceLedgerLive
	return ledger, true
}

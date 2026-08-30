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
// final live row wins until the ledger catches up. A live row with no
// ledger row is added. The D/ST key comes from DSTName and the pool's
// "{Nickname} D/ST" naming.
func MergeLines(base []league.WeekStatLine, week int, snapshot Snapshot, resolve Resolver) []league.WeekStatLine {
	live, ok := snapshot.Weeks[week]
	if !ok {
		return base
	}
	inProgress := map[string]bool{}
	for _, game := range snapshot.Games {
		if game.Week == week && game.InProgress && !game.Final {
			inProgress[game.Away], inProgress[game.Home] = true, true
		}
	}
	index := make(map[string]int, len(base))
	out := make([]league.WeekStatLine, len(base))
	copy(out, base)
	for i, line := range out {
		index[line.Key] = i
	}
	apply := func(key, team string, stats map[string]float64, final bool) {
		line := league.WeekStatLine{Key: key, Stats: league.RuleStatsFromTank01(stats, final), Source: sourceFor(final)}
		if at, seen := index[key]; seen {
			switch {
			case inProgress[team]:
				// live wins while the game runs
			case final && ledgerBehind(out[at].Stats, line.Stats):
				// the live side is final but the ledger mirror still holds a
				// partial (or all-zero) row; a final live row is never below
				// the truth, so it wins until the ledger catches up
			default:
				return // ledger wins: the game is final and the ledger is complete, or live has no clock for it
			}
			out[at] = line
			return
		}
		index[key] = len(out)
		out = append(out, line)
	}
	for _, line := range live.Lines {
		player, ok := resolve(line.PlayerID, line.Name)
		if !ok {
			continue
		}
		apply(openstats.NormalizePlayerKey(player.Name, player.Position), line.Team, line.Stats, line.Final)
	}
	for team, unit := range live.DST {
		name, ok := DSTName(team)
		if !ok {
			continue
		}
		apply(openstats.NormalizePlayerKey(name, "DST"), team, unit.Stats, unit.Final)
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

package league

import (
	"sort"
	"time"
)

// TrendingMove is one player's add or drop tally over the trending window,
// the "most added / most dropped" strip every hosted provider shows on its
// player pool.
type TrendingMove struct {
	PlayerID string
	Name     string
	Position string
	NFLTeam  string
	Count    int
}

// trendingWindow is how far back the pool's trending strip looks. A week
// matches the rhythm the ledger already runs on: one waiver run, one slate.
const trendingWindow = 7 * 24 * time.Hour

// trendingLimit keeps the strip a glance, not a ledger.
const trendingLimit = 3

// trendingMoves tallies the league's adds and drops over the last seven
// days from the transaction ledger. Manager adds, winning claims, and
// drops count, each player once per transaction. Trades move a player
// between rosters without a verdict on the player, so they do not count;
// commissioner corrections and lineup entries are bookkeeping, not
// sentiment. Most-moved first, name as the tie-break, three per list.
func trendingMoves(transactions []Transaction, now time.Time) (adds, drops []TrendingMove) {
	since := now.Add(-trendingWindow)
	addTally := make(map[string]TrendingMove)
	dropTally := make(map[string]TrendingMove)
	for _, txn := range transactions {
		if txn.At.IsZero() || txn.At.Before(since) || txn.At.After(now) {
			continue
		}
		switch txn.Type {
		case "add", "claim", "waiver", "drop", "auto-drop":
		default:
			continue
		}
		for _, player := range txn.Adds {
			tallyTrending(addTally, player)
		}
		for _, player := range txn.Drops {
			tallyTrending(dropTally, player)
		}
	}
	return topTrending(addTally), topTrending(dropTally)
}

func tallyTrending(tally map[string]TrendingMove, player TransactionPlayer) {
	key := player.PlayerID
	if key == "" {
		key = player.Name
	}
	if key == "" {
		return
	}
	move, ok := tally[key]
	if !ok {
		move = TrendingMove{PlayerID: player.PlayerID, Name: player.Name, Position: player.Position, NFLTeam: player.NFLTeam}
	}
	move.Count++
	tally[key] = move
}

func topTrending(tally map[string]TrendingMove) []TrendingMove {
	out := make([]TrendingMove, 0, len(tally))
	for _, move := range tally {
		out = append(out, move)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > trendingLimit {
		out = out[:trendingLimit]
	}
	return out
}

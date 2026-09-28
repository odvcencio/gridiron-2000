package league

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Pick'em trophy computation. Every trophy comes from settled weeks only: a
// week counts once each of its games is final or void (pickemSettledWeeks),
// so a half-played week never awards anything. Nothing is stored; each win is
// re-derived from picks, frozen markets, and results, so two renders of the
// same settled data agree. The kinds, their rules, and their tie handling are
// listed in trophyDefs (trophies.go).

// pickemGradedPick is one graded call: a win, a loss, or a missed loss.
// Voids and pending games never appear.
type pickemGradedPick struct {
	Week     int
	Win      bool
	Underdog bool // the pick took the market underdog
	Minority bool // the pick sided with the league minority
}

// pickemEntrantPicks is one entrant's graded picks. Owner is the persisted
// pick'em key (the entrant's email), never displayed.
type pickemEntrantPicks struct {
	Owner string
	Picks []pickemGradedPick
}

// pickemTrophyInput carries the graded picks plus each settled week's count
// of contested (non-void) games, which Perfect Week needs to tell a full week
// from a late entrant's partial one.
type pickemTrophyInput struct {
	Entrants  []pickemEntrantPicks
	Contested map[int]int
}

// trophyWin is one entrant's win of one trophy. Week is 0 for a season
// trophy.
type trophyWin struct {
	Kind  string
	Week  int
	Owner string
	Value string
}

func pickemRecordCompare(a, b PickemATSRecord) int {
	at, bt := a.Wins+a.Losses, b.Wins+b.Losses
	left, right := a.Wins*bt, b.Wins*at
	switch {
	case left > right:
		return 1
	case left < right:
		return -1
	case a.Wins > b.Wins:
		return 1
	case a.Wins < b.Wins:
		return -1
	}
	return 0
}

func pickemRecordText(r PickemATSRecord) string { return fmt.Sprintf("%d-%d", r.Wins, r.Losses) }

// pickemGameVoided is true for a game whose durable market record exists and
// offers no contest. A missing record is a pre-sync state, not a void.
func pickemGameVoided(markets map[string]PickemMarket, id string) bool {
	market, ok := markets[id]
	return ok && pickemMarketUnavailable(market)
}

// pickemSettledWeeks lists, ascending, the weeks whose every game is final or
// void. A week with no final game holds no graded result and is skipped.
func pickemSettledWeeks(games []GameInfo, markets map[string]PickemMarket) []int {
	type weekState struct{ open, final int }
	states := make(map[int]*weekState)
	for _, game := range games {
		if game.Week < 1 {
			continue
		}
		st := states[game.Week]
		if st == nil {
			st = &weekState{}
			states[game.Week] = st
		}
		switch {
		case game.Final:
			st.final++
		case pickemGameVoided(markets, game.ID):
		default:
			st.open++
		}
	}
	weeks := make([]int, 0, len(states))
	for week, st := range states {
		if st.open == 0 && st.final > 0 {
			weeks = append(weeks, week)
		}
	}
	sort.Ints(weeks)
	return weeks
}

func (e pickemEntrantPicks) record(filter func(pickemGradedPick) bool) PickemATSRecord {
	var r PickemATSRecord
	for _, p := range e.Picks {
		if filter != nil && !filter(p) {
			continue
		}
		if p.Win {
			r.Wins++
		} else {
			r.Losses++
		}
	}
	return r
}

// pickemSeasonWins awards Best Picker and Best Weekly Record.
//
// Best Picker: best record over all settled weeks; ties go to win
// percentage, then wins, and anyone still level shares it.
// Best Weekly Record: best single-week record; ties go to win percentage,
// then wins, then the earlier week, and anyone still level shares it.
func pickemSeasonWins(in pickemTrophyInput) []trophyWin {
	var out []trophyWin

	var best PickemATSRecord
	var owners []string
	for _, e := range in.Entrants {
		r := e.record(nil)
		if r.Wins+r.Losses == 0 {
			continue
		}
		cmp := 1
		if len(owners) > 0 {
			cmp = pickemRecordCompare(r, best)
		}
		switch {
		case cmp > 0:
			best, owners = r, []string{e.Owner}
		case cmp == 0:
			owners = append(owners, e.Owner)
		}
	}
	for _, owner := range owners {
		out = append(out, trophyWin{Kind: trophyBestPicker, Owner: owner, Value: pickemRecordText(best)})
	}

	var bestWeekRecord PickemATSRecord
	bestWeek := 0
	owners = nil
	for _, e := range in.Entrants {
		byWeek := make(map[int]PickemATSRecord)
		for _, p := range e.Picks {
			r := byWeek[p.Week]
			if p.Win {
				r.Wins++
			} else {
				r.Losses++
			}
			byWeek[p.Week] = r
		}
		for week, r := range byWeek {
			cmp := 1
			if len(owners) > 0 {
				cmp = pickemRecordCompare(r, bestWeekRecord)
				if cmp == 0 {
					switch {
					case week < bestWeek:
						cmp = 1
					case week > bestWeek:
						cmp = -1
					}
				}
			}
			switch {
			case cmp > 0:
				bestWeekRecord, bestWeek, owners = r, week, []string{e.Owner}
			case cmp == 0:
				owners = append(owners, e.Owner)
			}
		}
	}
	for _, owner := range owners {
		out = append(out, trophyWin{Kind: trophyBestWeek, Owner: owner, Value: fmt.Sprintf("%s · Week %d", pickemRecordText(bestWeekRecord), bestWeek)})
	}
	return out
}

// pickemWeekWins awards Perfect Week, Upset Hunter, and Contrarian for one
// settled week.
//
// Perfect Week: every contested game of the week called correctly (a late
// entrant's partial week never qualifies). Everyone perfect shares it.
// Upset Hunter: most correct picks on the market underdog; a game with no
// favorite does not count. Ties share it.
// Contrarian: most correct picks against the league majority; an even split
// has no minority. Ties share it.
func pickemWeekWins(in pickemTrophyInput, week int) []trophyWin {
	var out []trophyWin
	contested := in.Contested[week]
	inWeek := func(p pickemGradedPick) bool { return p.Week == week }
	for _, e := range in.Entrants {
		r := e.record(inWeek)
		if contested > 0 && r.Losses == 0 && r.Wins == contested {
			out = append(out, trophyWin{Kind: trophyPerfectWeek, Week: week, Owner: e.Owner, Value: fmt.Sprintf("%d-0", contested)})
		}
	}
	most := func(kind string, want func(pickemGradedPick) bool) {
		best := 0
		var owners []string
		for _, e := range in.Entrants {
			n := 0
			for _, p := range e.Picks {
				if p.Week == week && p.Win && want(p) {
					n++
				}
			}
			switch {
			case n == 0:
			case n > best:
				best, owners = n, []string{e.Owner}
			case n == best:
				owners = append(owners, e.Owner)
			}
		}
		for _, owner := range owners {
			out = append(out, trophyWin{Kind: kind, Week: week, Owner: owner, Value: fmt.Sprintf("%d correct", best)})
		}
	}
	most(trophyUpsetHunter, func(p pickemGradedPick) bool { return p.Underdog })
	most(trophyContrarian, func(p pickemGradedPick) bool { return p.Minority })
	return out
}

// pickemTrophyInputFor grades every established entrant over the settled
// weeks with the same entry instant and frozen markets the leaderboards use.
func pickemTrophyInputFor(state PersistedState, allGames []GameInfo, weeks []int, now time.Time) pickemTrophyInput {
	in := pickemTrophyInput{Contested: make(map[int]int, len(weeks))}
	type side struct{ away, home int }
	splits := make(map[string]side)
	for _, game := range allGames {
		var sp side
		for _, picks := range state.Pickems {
			switch picks[game.ID] {
			case game.Away:
				sp.away++
			case game.Home:
				sp.home++
			}
		}
		splits[game.ID] = sp
	}
	settled := make(map[int][]GameInfo, len(weeks))
	for _, week := range weeks {
		games := gamesInWeek(allGames, week)
		settled[week] = games
		for _, game := range games {
			if game.Final && !pickemGameVoided(state.PickemMarkets, game.ID) {
				in.Contested[week]++
			}
		}
	}
	owners := make([]string, 0, len(state.Pickems))
	for owner := range state.Pickems {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		enteredAt := effectivePickemEnteredAt(state, owner, allGames)
		entrant := pickemEntrantPicks{Owner: owner}
		picks := state.Pickems[owner]
		for _, week := range weeks {
			for _, game := range settled[week] {
				market := state.PickemMarkets[game.ID]
				outcome := gradePickemAt(game, market, picks[game.ID], enteredAt, now).Outcome
				if outcome != pickemWin && outcome != pickemLoss && outcome != pickemMissedLoss {
					continue
				}
				graded := pickemGradedPick{Week: week, Win: outcome == pickemWin}
				if pick := picks[game.ID]; pick != "" {
					if market.LineTenths != 0 {
						// nflverse: a positive line means the home team is favored.
						underdog := game.Away
						if market.LineTenths < 0 {
							underdog = game.Home
						}
						graded.Underdog = pick == underdog
					}
					sp := splits[game.ID]
					mine, other := sp.away, sp.home
					if pick == game.Home {
						mine, other = sp.home, sp.away
					}
					graded.Minority = mine < other
				}
				entrant.Picks = append(entrant.Picks, graded)
			}
		}
		in.Entrants = append(in.Entrants, entrant)
	}
	return in
}

// pickemTrophyWins is every pick'em trophy win across all settled weeks.
func pickemTrophyWins(state PersistedState, allGames []GameInfo, now time.Time) []trophyWin {
	weeks := pickemSettledWeeks(allGames, state.PickemMarkets)
	if len(weeks) == 0 {
		return nil
	}
	in := pickemTrophyInputFor(state, allGames, weeks, now)
	out := pickemSeasonWins(in)
	for _, week := range weeks {
		out = append(out, pickemWeekWins(in, week)...)
	}
	return out
}

// PickemTrophy is one season highlight row on the Pick'em season leaderboard.
// Awarded is false until a week settles; the row still renders, with
// placeholders, so the leaderboard never changes height when a week settles.
type PickemTrophy struct {
	Label   string
	Names   string
	Value   string
	Rule    string
	Awarded bool
}

// pickemSeasonHighlights returns Best Picker and Best Weekly Record for the
// Pick'em season leaderboard, in that order. Co-leaders are joined in name
// order.
func (s *Service) pickemSeasonHighlights(state PersistedState, allGames []GameInfo, now time.Time) []PickemTrophy {
	weeks := pickemSettledWeeks(allGames, state.PickemMarkets)
	var wins []trophyWin
	if len(weeks) > 0 {
		wins = pickemSeasonWins(pickemTrophyInputFor(state, allGames, weeks, now))
	}
	out := make([]PickemTrophy, 0, 2)
	for _, kind := range []string{trophyBestPicker, trophyBestWeek} {
		def := trophyDefFor(kind)
		row := PickemTrophy{Label: def.Title, Names: "Not awarded yet", Value: "—", Rule: def.Rule}
		var names []string
		for _, w := range wins {
			if w.Kind == kind {
				names = append(names, s.pickemDisplayName(state, w.Owner))
				row.Value = w.Value
			}
		}
		if len(names) > 0 {
			sort.Strings(names)
			row.Names, row.Awarded = strings.Join(names, " · "), true
		}
		out = append(out, row)
	}
	return out
}

// pickemDisplayName is the leaderboard's own naming rule: the member's name,
// else the local part of the email.
func (s *Service) pickemDisplayName(state PersistedState, email string) string {
	name := strings.TrimSpace(state.Members[email].Name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	return name
}

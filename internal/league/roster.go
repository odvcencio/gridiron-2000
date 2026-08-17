package league

import "sort"

// currentRosters replays Picks then Transactions, in order, to derive
// every team's current roster (roster-ops spec section 3's architecture
// decision: append-only log, derived state). Returns teamID -> ordered
// player IDs — draft order first, then free-agent adds in transaction
// order. The replay is written generically over each Transaction's Adds
// and Drops (section 7.1's replay semantics: "TeamID gains Adds and loses
// Drops"), plus the trade-specific OtherTeamID reversal, so WP-R3's
// add/drop entries, WP-R4's claim entries, and WP-R5's trade entries all
// replay through the same code with no change here.
func currentRosters(state PersistedState) map[string][]string {
	rosters := map[string][]string{}
	present := map[string]map[string]bool{}

	add := func(teamID, playerID string) {
		if teamID == "" || playerID == "" {
			return
		}
		if present[teamID] == nil {
			present[teamID] = map[string]bool{}
		}
		if present[teamID][playerID] {
			return
		}
		present[teamID][playerID] = true
		rosters[teamID] = append(rosters[teamID], playerID)
	}
	remove := func(teamID, playerID string) {
		if teamID == "" || playerID == "" || !present[teamID][playerID] {
			return
		}
		delete(present[teamID], playerID)
		list := rosters[teamID]
		for index, id := range list {
			if id == playerID {
				rosters[teamID] = append(list[:index], list[index+1:]...)
				break
			}
		}
	}

	picks := append([]DraftPick(nil), state.Picks...)
	sort.Slice(picks, func(i, j int) bool { return picks[i].Number < picks[j].Number })
	for _, pick := range picks {
		add(pick.TeamID, pick.PlayerID)
	}

	for _, txn := range state.Transactions {
		for _, drop := range txn.Drops {
			remove(txn.TeamID, drop.PlayerID)
		}
		for _, gain := range txn.Adds {
			add(txn.TeamID, gain.PlayerID)
		}
		if txn.Type == "trade" && txn.OtherTeamID != "" {
			// The trade counterparty replays the opposite side (section
			// 7.1): OtherTeamID gains Drops and loses Adds.
			for _, gain := range txn.Adds {
				remove(txn.OtherTeamID, gain.PlayerID)
			}
			for _, drop := range txn.Drops {
				add(txn.OtherTeamID, drop.PlayerID)
			}
		}
	}
	return rosters
}

// rosterOwner inverts currentRosters into playerID -> teamID, for O(1)
// ownership lookups (availability resolution, add/drop validation).
func rosterOwner(rosters map[string][]string) map[string]string {
	owner := make(map[string]string, len(rosters)*20)
	for teamID, ids := range rosters {
		for _, id := range ids {
			owner[id] = teamID
		}
	}
	return owner
}

// draftComplete reports whether every roster spot has been filled by a
// draft pick (the same cap MakePick and AutoPick enforce). Free agency
// opens the moment this turns true (roster-ops spec section 5.1's
// post-draft initial-state decision: "Undrafted players are free agents
// the moment the draft completes"); before it, AddPlayer fails closed and
// the /players page renders adds disabled rather than pretend a pool of
// undrafted players are already free agents mid-draft.
func draftComplete(state PersistedState) bool {
	return len(state.Picks) >= len(defaultTeams())*CurrentDraftRounds()
}

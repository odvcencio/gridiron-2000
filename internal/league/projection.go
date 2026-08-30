package league

import "math"

// remainingFractionByPeriod holds the fixed remaining-time-of-game
// fraction A6's projection uses for each quarter (decision, Task 11a): the
// browser never learns the game clock's exact seconds remaining, so each
// quarter's fraction is that quarter's own midpoint estimate — "half of
// this quarter, plus every quarter still to come" — rather than a false
// precision the render path cannot actually back up. OT reads the same as
// Q4: once a game reaches overtime its own remaining fraction is already
// exhausted to the Q4 estimate, and no further schedule signal exists to
// refine it.
var remainingFractionByPeriod = map[string]float64{
	"Q1": 0.875,
	"Q2": 0.625,
	"Q3": 0.375,
	"Q4": 0.125,
	"OT": 0.125,
}

// remainingFraction is projectedTotal's per-game multiplier on a starter's
// remaining projection. known is false when the live poller has no
// affirmative signal for this exact game (no live status source wired at
// all, or the poller's status simply has no entry for this team yet); in
// that case the whole projection still applies (fraction 1): the
// manager-facing total should read as "the player's full week is still
// ahead of them", not silently drop the projection because the poller has
// not caught up. A game the schedule or the poller marks Final always
// contributes zero remaining projection, regardless of known. Once known
// and InProgress, an unrecognized period label (for example Tank01's
// "HALF") is a real live game the table just has no entry for, not the
// same "nothing has happened yet" claim a pre-kickoff or unwired read is
// — it reads as 0.5 (round-2 review of commit 133d1d7, finding 6), the
// same neutral halfway estimate remainingFractionByPeriod already picks
// for the middle of a normal quarter. A known, not-in-progress,
// unrecognized period (the pre-kickoff case, Period == "") keeps the
// full-projection fraction 1.
func remainingFraction(state LiveGameState, known bool) float64 {
	if state.Final {
		return 0
	}
	if !known {
		return 1
	}
	if fraction, ok := remainingFractionByPeriod[state.Period]; ok {
		return fraction
	}
	if state.InProgress {
		return 0.5
	}
	return 1
}

// winProbability is the A6 heuristic (decision, settled in the Task 10
// review): a logistic on the two sides' projected-total gap, scale 10 —
// chosen so a 10-point projected edge alone reads as roughly a 73% win
// probability, a comfortable-but-not-certain lead, without needing any
// larger statistical model this render path has no data to support.
func winProbability(mine, theirs float64) float64 {
	return 1 / (1 + math.Exp(-(mine-theirs)/10))
}

// projectedTotal is one side's rest-of-game projected total: every
// starter's points already on the board, plus their remaining Tank01
// weekly projection scaled by their own game's remainingFraction. hasLive
// gates whether status.Games is trusted at all — with no live status
// source wired, every row falls back to remainingFraction's known=false
// case (fraction 1) as if no game had been checked yet, exactly like a
// per-row miss.
func projectedTotal(rows []StarterLedgerRow, projections map[string]float64, status LiveStatus, hasLive bool) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.Points
		game, ok := status.Games[row.NFLTeam]
		total += projections[row.PlayerID] * remainingFraction(game, hasLive && ok)
	}
	return total
}

// stillToPlay counts configured starters (a row with a player assigned)
// whose NFL game the poller has not yet marked in progress or final —
// not started, or a team the poller has no entry for at all. An empty
// slot contributes nothing to either side of the "X of Y" count this
// backs (MatchupsData's still_to_play label).
func stillToPlay(rows []StarterLedgerRow, status LiveStatus) int {
	count := 0
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		game, ok := status.Games[row.NFLTeam]
		if !ok || (!game.Final && !game.InProgress) {
			count++
		}
	}
	return count
}

// starterProjections reads each row's player's weekly Tank01 projection
// from byID, keyed by PlayerID exactly as projectedTotal expects. byID is
// the caller's own single s.pool().byID read for the whole render — a
// render can build a projections map for every side of every matchup
// (round-2 review of commit 133d1d7, finding 3: about 28 calls for a
// seven-matchup week), so this takes the pool by reference instead of
// calling s.pool() (which takes poolMu) once per side. Rows with no
// matching pool entry (an empty slot, or a player the pool no longer
// carries) are simply absent from the map, which projectedTotal already
// treats as a zero projection.
func starterProjections(rows []StarterLedgerRow, byID map[string]Player) map[string]float64 {
	out := make(map[string]float64, len(rows))
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		if player, ok := byID[row.PlayerID]; ok {
			out[row.PlayerID] = player.Projection
		}
	}
	return out
}

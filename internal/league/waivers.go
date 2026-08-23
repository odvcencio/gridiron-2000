// Waiver claims, the performance-weighted priority order, availability
// derivation, and the daily processing run (roster-ops spec section 5).
// Everything here is a pure function over (state, cfg, games, now) —
// section 3's architecture decision: nothing about waiver order or player
// availability is stored; both derive at read time from the append-only
// Transactions log plus the open WaiverClaims list.
package league

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"
)

// WaiverClaim is one open, unprocessed waiver claim (roster-ops spec
// section 5.3). Store.ProcessWaivers removes a claim the instant it
// resolves; there is no "resolved claim" record beyond the claim
// Transaction a win appends (a loss or a failure is never logged — the
// claimant's N14 email is that attempt's only record, section 7.2).
type WaiverClaim struct {
	ID     string `json:"id"` // "clm-" + 8 random hex
	TeamID string `json:"teamId"`
	AddID  string `json:"addId"`
	DropID string `json:"dropId,omitempty"` // required when the roster is full
	Bid    int    `json:"bid,omitempty"`    // faab mode only; 0 allowed
	// Priority is the manager's own claim order across their own open
	// claims, 1 first (section 5.3). Store.FileClaim sets it to one past
	// the filing team's current open-claim count; it never changes after
	// that except by cancellation renumbering (CancelClaim does not
	// renumber — a gap is harmless, only relative order inside one team's
	// claims matters).
	Priority int       `json:"priority"`
	FiledAt  time.Time `json:"filedAt"`
}

// randomClaimID draws "clm-" + 8 random hex characters from crypto/rand,
// the same shape randomTransactionID uses (transactions.go).
func randomClaimID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "clm-" + hex.EncodeToString(buf[:]), nil
}

// ---------------------------------------------------------------------
// Availability (roster-ops spec section 5.1)
// ---------------------------------------------------------------------

// Availability is a pool player's derived acquisition state.
type Availability string

const (
	AvailabilityRostered  Availability = "rostered"
	AvailabilityOnWaivers Availability = "on_waivers"
	AvailabilityFreeAgent Availability = "free_agent"
)

// waiverStatus is availability's full result: the state, plus (only when
// State is AvailabilityOnWaivers) the instant claims on this player
// resolve and which rule put them there.
type waiverStatus struct {
	State      Availability
	ResolvesAt time.Time
	// Reason is "dropped" or "kickoff" — which of section 5.1's two
	// ON WAIVERS conditions applies. Empty outside AvailabilityOnWaivers.
	Reason string
}

// lastDropInstant returns the At instant and Type ("drop" or "auto-drop")
// of the most recent drop or auto-drop transaction naming playerID among
// its Drops, and whether one exists at all. origin backs clearsAt's IR
// auto-cut carve-out (SK spec): an "auto-drop" clears on a different,
// deferred schedule than an ordinary manager drop — see
// playerWaiverStatus and deferredClearsAt (rosterops.go).
func lastDropInstant(state PersistedState, playerID string) (at time.Time, origin string, found bool) {
	for _, txn := range state.Transactions {
		if txn.Type != "drop" && txn.Type != "auto-drop" {
			continue
		}
		for _, drop := range txn.Drops {
			if drop.PlayerID != playerID {
				continue
			}
			if !found || txn.At.After(at) {
				at = txn.At
				origin = txn.Type
				found = true
			}
		}
	}
	return at, origin, found
}

// kickoffLockedGame finds a game for nflTeam that has kicked off (now is
// at or past Kickoff) and is not yet Final — section 5.1's second ON
// WAIVERS condition, applied per player (the pick'em lock rule,
// pickem.go:240-242, extended to free agency).
func kickoffLockedGame(games []GameInfo, nflTeam string, now time.Time) (GameInfo, bool) {
	for _, g := range games {
		if g.Away != nflTeam && g.Home != nflTeam {
			continue
		}
		if !g.Kickoff.IsZero() && !g.Final && !now.Before(g.Kickoff) {
			return g, true
		}
	}
	return GameInfo{}, false
}

// gameFinalAt approximates the instant a game turns Final: kickoff plus
// five hours, the schedule adapter's own rule (roster-ops spec fact 4,
// main.go:366-370). GameInfo carries only the Final bool, not the instant
// it flipped, so this is the best available estimate for a "resolves at"
// display; Store.ProcessWaivers never uses it for the due decision itself
// (that reads the live g.Final flag, never this estimate).
func gameFinalAt(g GameInfo) time.Time {
	return g.Kickoff.Add(5 * time.Hour)
}

// playerWaiverStatus classifies one player's availability (roster-ops
// spec section 5.1's three-state table): ROSTERED when currentRosters
// names an owner; ON WAIVERS while a drop's clear window is still open or
// the player's current game has kicked off and is not yet final; FREE
// AGENT otherwise.
func playerWaiverStatus(state PersistedState, cfg Config, games []GameInfo, playerID, nflTeam string, now time.Time) waiverStatus {
	owner := rosterOwner(currentRosters(state))
	if owner[playerID] != "" {
		return waiverStatus{State: AvailabilityRostered}
	}
	if droppedAt, origin, ok := lastDropInstant(state, playerID); ok {
		clears := clearsAt(cfg, droppedAt)
		if origin == "auto-drop" {
			// SK IR rule: a healed-IR auto-cut never clears on the
			// ordinary clear_days schedule — it defers to the following
			// NFL week's processing run, never an instant free agent.
			clears = deferredClearsAt(cfg, games, droppedAt)
		}
		if now.Before(clears) {
			return waiverStatus{State: AvailabilityOnWaivers, ResolvesAt: clears, Reason: "dropped"}
		}
	}
	if game, ok := kickoffLockedGame(games, nflTeam, now); ok {
		return waiverStatus{
			State:      AvailabilityOnWaivers,
			ResolvesAt: firstRunAtOrAfter(cfg, gameFinalAt(game)),
			Reason:     "kickoff",
		}
	}
	return waiverStatus{State: AvailabilityFreeAgent}
}

// ---------------------------------------------------------------------
// Daily run-instant arithmetic (roster-ops spec section 5.1, 5.4)
// ---------------------------------------------------------------------

// waiverProcessClock resolves cfg's daily run hour/minute and league
// timezone, falling back to the spec's default (09:00, America/New_York
// via DefaultDraftTZ) when cfg carries an unparseable value — the same
// resilient fallback every other s.draftTZ reader in this package uses.
func waiverProcessClock(cfg Config) (hour, minute int, loc *time.Location) {
	hour, minute = 9, 0
	if parts := strings.SplitN(cfg.Waivers.ProcessTime, ":", 2); len(parts) == 2 {
		if h, err := strconv.Atoi(parts[0]); err == nil {
			hour = h
		}
		if m, err := strconv.Atoi(parts[1]); err == nil {
			minute = m
		}
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil || loc == nil {
		loc, _ = time.LoadLocation(DefaultDraftTZ)
	}
	if loc == nil {
		loc = time.UTC
	}
	return hour, minute, loc
}

// firstRunAtOrAfter returns the first daily waiver-processing instant, in
// cfg's league timezone, at or after from (inclusive) — clearsAt's
// definition (section 5.1: "the first daily process instant at or after
// T + clear_days x 24h").
func firstRunAtOrAfter(cfg Config, from time.Time) time.Time {
	hour, minute, loc := waiverProcessClock(cfg)
	local := from.In(loc)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if candidate.Before(from) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

// firstRunStrictlyAfter returns the first daily run instant strictly
// after from — section 5.4's run predicate: "the first daily instant at
// waivers.process_time ... strictly after WaiversProcessedThrough".
func firstRunStrictlyAfter(cfg Config, from time.Time) time.Time {
	candidate := firstRunAtOrAfter(cfg, from)
	if !candidate.After(from) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

// clearsAt resolves a dropped player's clear instant (section 5.1):
// the first daily process instant at or after droppedAt plus
// waivers.clear_days days.
func clearsAt(cfg Config, droppedAt time.Time) time.Time {
	threshold := droppedAt.Add(time.Duration(cfg.Waivers.ClearDays) * 24 * time.Hour)
	return firstRunAtOrAfter(cfg, threshold)
}

// formatResolvesAt renders a resolve instant in the league's timezone,
// matching activityMaps' feed-line time format (service.go) for one
// consistent time idiom across every roster-ops surface.
func formatResolvesAt(cfg Config, t time.Time) string {
	_, _, loc := waiverProcessClock(cfg)
	return t.In(loc).Format("Jan 2, 3:04 PM MST")
}

// ---------------------------------------------------------------------
// waiverOrder — the section 5.2.1 formula
// ---------------------------------------------------------------------

// lastClosedWeek returns the highest ScheduleWeek.Week whose matchups are
// all Final (matchupsAllFinal, season.go), or 0 when sch is nil or no
// week is fully closed yet (section 5.2.1's W).
func lastClosedWeek(sch *SeasonSchedule) int {
	if sch == nil {
		return 0
	}
	week := 0
	for _, wk := range sch.Weeks {
		if wk.Week > week && matchupsAllFinal(wk.Matchups) {
			week = wk.Week
		}
	}
	return week
}

// roundOneOrder resolves the draft's round-1 pick order for activeTeamCount(order)
// teams via teamOnClock — round 1 never snakes, so pick N's team is exactly
// this ordering's Nth entry.
func roundOneOrder(order []string) []string {
	n := activeTeamCount(order)
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = teamOnClock(order, i+1)
	}
	return out
}

func reverseStrings(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

// weeklyPointsRank ranks each team by its week-W matchup points,
// descending (rank 1 = best); equal points break by the seeded draw,
// lower hash first, so every team gets a distinct integer rank. A team
// with no week-W matchup (a bye) substitutes its season rank (section
// 5.2.1 step 2).
func weeklyPointsRank(sch SeasonSchedule, teamIDs []string, week int, seasonRank map[string]int) map[string]int {
	points := map[string]float64{}
	has := map[string]bool{}
	for _, wk := range sch.Weeks {
		if wk.Week != week {
			continue
		}
		for _, m := range wk.Matchups {
			points[m.HomeTeamID] = m.HomeScore
			has[m.HomeTeamID] = true
			points[m.AwayTeamID] = m.AwayScore
			has[m.AwayTeamID] = true
		}
	}
	ranked := make([]string, 0, len(teamIDs))
	for _, id := range teamIDs {
		if has[id] {
			ranked = append(ranked, id)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		pi, pj := points[ranked[i]], points[ranked[j]]
		if pi != pj {
			return pi > pj
		}
		hi := seededDrawHash(sch.Seed, ranked[i])
		hj := seededDrawHash(sch.Seed, ranked[j])
		return bytes.Compare(hi[:], hj[:]) < 0
	})
	out := make(map[string]int, len(teamIDs))
	for i, id := range ranked {
		out[id] = i + 1
	}
	for _, id := range teamIDs {
		if !has[id] {
			out[id] = seasonRank[id]
		}
	}
	return out
}

// performanceBaseOrder derives the post-close base order (section 5.2.1,
// W >= 1): a season/weekly-rank blend, worst combined performance first.
func performanceBaseOrder(sch SeasonSchedule, teamIDs []string, cfg Config, week int) []string {
	standings := ComputeStandings(sch, teamIDs, TiebreakInputs{SeasonSeed: sch.Seed})
	seasonRank := make(map[string]int, len(standings))
	for _, st := range standings {
		seasonRank[st.TeamID] = st.Rank
	}
	weeklyRank := weeklyPointsRank(sch, teamIDs, week, seasonRank)

	sw := cfg.Waivers.SeasonWeightPct
	combined := make(map[string]int, len(teamIDs))
	for _, id := range teamIDs {
		combined[id] = sw*seasonRank[id] + (100-sw)*weeklyRank[id]
	}

	ordered := append([]string(nil), teamIDs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if combined[a] != combined[b] {
			return combined[a] > combined[b] // worst combined performance (highest number) claims first
		}
		ha := seededDrawHash(sch.Seed, a)
		hb := seededDrawHash(sch.Seed, b)
		return bytes.Compare(ha[:], hb[:]) < 0
	})
	return ordered
}

// moveToBack relocates teamID to the end of order, leaving every other
// team's relative order unchanged. A teamID absent from order is a no-op.
func moveToBack(order []string, teamID string) []string {
	index := -1
	for i, id := range order {
		if id == teamID {
			index = i
			break
		}
	}
	if index < 0 {
		return order
	}
	out := make([]string, 0, len(order))
	out = append(out, order[:index]...)
	out = append(out, order[index+1:]...)
	out = append(out, teamID)
	return out
}

// applyInPeriodPenalties replays every claim Transaction with Week > week,
// in At order, moving each winner to the back (section 5.2.1's in-period
// penalty). The penalty set empties automatically once week advances past
// a claim's Week, which is how the penalty "expires at the next
// recompute" with no separate expiry bookkeeping.
func applyInPeriodPenalties(base []string, transactions []Transaction, week int) []string {
	claims := make([]Transaction, 0)
	for _, txn := range transactions {
		if txn.Type == "claim" && txn.Week > week {
			claims = append(claims, txn)
		}
	}
	sort.SliceStable(claims, func(i, j int) bool { return claims[i].At.Before(claims[j].At) })
	order := append([]string(nil), base...)
	for _, txn := range claims {
		order = moveToBack(order, txn.TeamID)
	}
	return order
}

// waiverOrder derives the full current claim order (roster-ops spec
// section 5.2.1): the performance base at the last week close (or inverse
// draft order before week 1 closes), then in-period claim penalties
// replayed from the transaction log. Nothing here is stored.
func waiverOrder(state PersistedState, cfg Config) []string {
	teamIDs := defaultTeamIDs()
	week := lastClosedWeek(state.Schedule)

	var base []string
	if week == 0 || state.Schedule == nil {
		base = reverseStrings(roundOneOrder(state.DraftOrder))
	} else {
		base = performanceBaseOrder(*state.Schedule, teamIDs, cfg, week)
	}
	return applyInPeriodPenalties(base, state.Transactions, week)
}

// faabRemaining derives each team's remaining FAAB budget (faab mode
// only): budget minus the sum of claim-transaction bids (section 3).
func faabRemaining(state PersistedState, budget int) map[string]int {
	teamIDs := defaultTeamIDs()
	remaining := make(map[string]int, len(teamIDs))
	for _, id := range teamIDs {
		remaining[id] = budget
	}
	for _, txn := range state.Transactions {
		if txn.Type != "claim" {
			continue
		}
		if _, known := remaining[txn.TeamID]; known {
			remaining[txn.TeamID] -= txn.Bid
		}
	}
	return remaining
}

// faabUnits renders a FAAB amount as explicit non-currency units.
// FAAB is a season allotment of bidding units, never money, so it must
// never carry a currency symbol.
func faabUnits(amount int) string {
	return strconv.Itoa(amount) + " FAAB"
}

// waiverOrderPosition returns teamID's 1-based position in order, or 0
// when absent (should not happen for a known team; defensive only).
func waiverOrderPosition(order []string, teamID string) int {
	for i, id := range order {
		if id == teamID {
			return i + 1
		}
	}
	return 0
}

// waiverClaimByTeamAndPlayer reports whether teamID already holds an open
// claim on addID (section 5.3, W5).
func waiverClaimByTeamAndPlayer(claims []WaiverClaim, teamID, addID string) (WaiverClaim, bool) {
	for _, c := range claims {
		if c.TeamID == teamID && c.AddID == addID {
			return c, true
		}
	}
	return WaiverClaim{}, false
}

// teamOpenClaimCount counts teamID's open claims (Store.FileClaim uses
// this to number a new claim's Priority, one past the count).
func teamOpenClaimCount(claims []WaiverClaim, teamID string) int {
	count := 0
	for _, c := range claims {
		if c.TeamID == teamID {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------
// Processing-run resolution (roster-ops spec section 5.4)
// ---------------------------------------------------------------------

// WaiverResult is one resolved claim's outcome, returned by
// Store.ProcessWaivers so the caller can fire N14 notifications after the
// run's single persist has already committed (section 5.4 step 6: sends
// happen only once the state is durably written).
type WaiverResult struct {
	Claim WaiverClaim
	// Outcome is "won", "beaten" (another claim took the same add player,
	// this run or earlier), or "failed" (any other re-validation reason).
	Outcome string
	// Reason carries the exact failure message for a "failed" outcome.
	Reason string
	// Position is the 1-based waiverOrder position a won perf-priority
	// claim used (Transaction.Position, section 7.1).
	Position int
	// WinningTeamID names the team that ended up owning AddID, for a
	// "beaten" claim's notification ("went to {teamName}").
	WinningTeamID string
	// WinningBid is the winning claim's bid, for a "beaten" faab claim's
	// notification.
	WinningBid int
	// Week is the league week a "won" claim's Transaction recorded.
	Week int
}

// claimLess orders two due claims for one selection round (section 5.4
// step 2): perf-priority by the claiming team's waiverOrder position
// ascending, then the team's own Priority ascending; faab by bid
// descending, ties by waiverOrder position ascending. Two claims from the
// same team at the same bid (faab) fall through to the original slice
// order — deterministic, since that order is itself the stable filing
// order (see pickNextClaim).
func claimLess(a, b WaiverClaim, order []string) bool {
	posA, posB := waiverOrderPosition(order, a.TeamID), waiverOrderPosition(order, b.TeamID)
	if posA != posB {
		return posA < posB
	}
	return a.Priority < b.Priority
}

func claimLessFAAB(a, b WaiverClaim, order []string) bool {
	if a.Bid != b.Bid {
		return a.Bid > b.Bid
	}
	return waiverOrderPosition(order, a.TeamID) < waiverOrderPosition(order, b.TeamID)
}

// pickNextClaim selects the single highest-priority claim from remaining
// (section 5.4 step 2/3, one selection round) and returns it plus
// remaining with that claim removed, order otherwise preserved. mode
// selects the comparator: "faab" or anything else (perf-priority).
func pickNextClaim(remaining []WaiverClaim, order []string, mode string) (WaiverClaim, []WaiverClaim) {
	less := claimLess
	if mode == "faab" {
		less = claimLessFAAB
	}
	best := 0
	for i := 1; i < len(remaining); i++ {
		if less(remaining[i], remaining[best], order) {
			best = i
		}
	}
	chosen := remaining[best]
	rest := make([]WaiverClaim, 0, len(remaining)-1)
	rest = append(rest, remaining[:best]...)
	rest = append(rest, remaining[best+1:]...)
	return chosen, rest
}

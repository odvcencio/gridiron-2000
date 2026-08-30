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
	"sync/atomic"
	"time"
)

// WaiverClaim is one open, unprocessed waiver claim (roster-ops spec
// section 5.3). Store.ProcessWaivers removes a claim the instant it resolves
// and appends a team-private WaiverReceipt in the same transaction.
type WaiverClaim struct {
	ID     string `json:"id"` // "clm-" + 8 random hex
	TeamID string `json:"teamId"`
	AddID  string `json:"addId"`
	DropID string `json:"dropId,omitempty"` // required when the roster is full
	Bid    int    `json:"bid,omitempty"`    // faab mode only; 0 allowed
	// Priority is the manager's own claim order across their own open
	// claims, 1 first (section 5.3). File, cancel, and move operations
	// normalize it atomically to a gap-free 1..N sequence per team.
	Priority int       `json:"priority"`
	FiledAt  time.Time `json:"filedAt"`
	// DeferredStreak counts how many consecutive Store.ProcessWaivers runs
	// have deferred this claim because its AddID sat outside the bounded
	// player pool (2026-08-30 review, finding 6: a deferred claim used to
	// hold its team's cap slot forever with no signal). It resets to 0 the
	// moment the claim resolves through any other path (still-open on
	// waivers, or due).
	DeferredStreak int `json:"deferredStreak,omitempty"`
	// FirstDeferredAt is the instant this claim's current deferral streak
	// began — the run that first set DeferredStreak to 1 (2026-08-30
	// review round 2, finding 3). Store.ProcessWaivers expires a claim
	// only once BOTH DeferredStreak reaches waiverClaimDeferralLimit AND
	// at least waiverClaimDeferralWindow of wall-clock time has actually
	// elapsed since FirstDeferredAt: DeferredStreak alone counts runs, not
	// time, so a short outage replayed in a burst of catch-up runs (or a
	// commissioner's force-run button pressed three times in a row) could
	// otherwise destroy a claim in seconds. Resets to the zero time
	// alongside DeferredStreak.
	FirstDeferredAt time.Time `json:"firstDeferredAt,omitempty"`
}

// waiverClaimDeferralLimit is how many consecutive deferred runs a claim
// tolerates (finding 6) before Store.ProcessWaivers becomes willing to
// expire it outright, with a final notification naming the reason —
// together with waiverClaimDeferralWindow, not alone (finding 3, 2026-08-30
// review round 2).
const waiverClaimDeferralLimit = 3

// waiverClaimDeferralWindow is the minimum wall-clock time that must have
// actually elapsed since a claim's FirstDeferredAt before
// waiverClaimDeferralLimit consecutive deferred runs are allowed to expire
// it (finding 3, 2026-08-30 review round 2): a real recovery window, not
// merely a run count a replayed outage or a rapid force-run can rack up in
// minutes or seconds.
const waiverClaimDeferralWindow = 48 * time.Hour

// WaiverReceipt is the season-scoped, team-private resolution ledger for one
// claim. Player identity is snapshotted so receipts survive pool churn. Team
// IDs are safe league identities; manager emails never enter this record.
type WaiverReceipt struct {
	ClaimID           string              `json:"claimId"`
	Season            int                 `json:"season"`
	Week              int                 `json:"week"`
	TeamID            string              `json:"teamId"`
	Add               TransactionPlayer   `json:"add"`
	Drops             []TransactionPlayer `json:"drops,omitempty"`
	Bid               int                 `json:"bid,omitempty"`
	SubmittedPriority int                 `json:"submittedPriority"`
	WaiverPosition    int                 `json:"waiverPosition"`
	WaiverTeamCount   int                 `json:"waiverTeamCount"`
	Mode              string              `json:"mode"`
	Outcome           string              `json:"outcome"`
	WinningTeamID     string              `json:"winningTeamId,omitempty"`
	WinningBid        int                 `json:"winningBid,omitempty"`
	WinningBidKnown   bool                `json:"winningBidKnown,omitempty"`
	Reason            string              `json:"reason"`
	FiledAt           time.Time           `json:"filedAt"`
	ResolvedAt        time.Time           `json:"resolvedAt"`
}

func receiptPlayer(poolByID map[string]Player, playerID string) TransactionPlayer {
	if player, ok := poolByID[playerID]; ok {
		return transactionPlayerFromPlayer(player)
	}
	return TransactionPlayer{PlayerID: playerID, Name: playerID}
}

func waiverWinningBid(transactions []Transaction, playerID, teamID string) (int, bool) {
	for index := len(transactions) - 1; index >= 0; index-- {
		txn := transactions[index]
		// A winning bid describes the transaction that gave the current
		// owner this current copy of the player. Walk newest-first until
		// that ownership edge; an older claim is historical once a later
		// add, trade, or drop has crossed the edge.
		acquired, released := false, false
		if txn.TeamID == teamID {
			for _, add := range txn.Adds {
				if add.PlayerID == playerID {
					acquired = true
					break
				}
			}
			for _, drop := range txn.Drops {
				if drop.PlayerID == playerID {
					released = true
					break
				}
			}
		} else if txn.Type == "trade" && txn.OtherTeamID == teamID {
			// In a trade the counterparty receives Drops and gives up Adds.
			for _, drop := range txn.Drops {
				if drop.PlayerID == playerID {
					acquired = true
					break
				}
			}
			for _, add := range txn.Adds {
				if add.PlayerID == playerID {
					released = true
					break
				}
			}
		}
		if !acquired && !released {
			continue
		}
		if acquired && !released && txn.Type == "claim" {
			return txn.Bid, true
		}
		// A direct add, trade, drop, or malformed/ambiguous event is the
		// current boundary but carries no trustworthy winning bid.
		return 0, false
	}
	return 0, false
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

// waiverKickoffPendingLabel is the one shared answer for a kickoff-locked
// claim's resolve time (F7): the live processing run decides "due" by
// reading the game's current Final flag, never gameFinalAt's
// kickoff-plus-five-hours estimate, so no surface may promise a specific
// instant for it. Both the pool row (PlayersData) and MY CLAIMS
// (waiverClaimResolutionView) render this exact string so the same
// kickoff-locked player can never show two different answers.
const waiverKickoffPendingLabel = "Pending — resolves once this player's game is marked final."

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

// nextWaiverProcessingRun is the processor's authoritative next cycle.
// A zero baseline means the next tick only records now, so the first cycle
// that can resolve a claim is the daily run strictly after now.
func nextWaiverProcessingRun(cfg Config, processedThrough, now time.Time) time.Time {
	if processedThrough.IsZero() {
		return firstRunStrictlyAfter(cfg, now)
	}
	return firstRunStrictlyAfter(cfg, processedThrough)
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

// performanceBaseOrderCalls is a test seam for F4's per-run hoist
// invariant: one Store.ProcessWaivers run must call performanceBaseOrder
// (the standings/weekly-rank computation) exactly once, no matter how
// many claims it resolves. nil in production — an active production-path
// counter had no production purpose and cost every call an atomic
// increment (2026-08-30 review, finding 7); only a test that needs to
// count invocations sets it, and must clear it afterward.
//
// It is an atomic.Pointer, not a plain package-level func var (2026-08-30
// review round 2, finding 11): a bare `var f func()` written directly by a
// test and read directly by performanceBaseOrder is a data race the
// instant two tests exercising this seam ever run in parallel — setPointer
// and getFunc make every read and write here a single atomic operation.
var performanceBaseOrderCalls atomic.Pointer[func()]

// setPerformanceBaseOrderCalls installs fn as the test seam, or clears it
// when fn is nil — the same "nil in production" contract the plain func
// var used to carry, now race-proof.
func setPerformanceBaseOrderCalls(fn func()) {
	if fn == nil {
		performanceBaseOrderCalls.Store(nil)
		return
	}
	performanceBaseOrderCalls.Store(&fn)
}

// performanceBaseOrder derives the post-close base order (section 5.2.1,
// W >= 1): a season/weekly-rank blend, worst combined performance first.
func performanceBaseOrder(sch SeasonSchedule, teamIDs []string, cfg Config, week int) []string {
	if fn := performanceBaseOrderCalls.Load(); fn != nil {
		(*fn)()
	}
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

// weekClosedAt looks up week's persisted ScheduleWeek.ClosedAt (2026-08-30
// review round 2, finding 2), reporting whether that week both exists in
// sch and carries a non-zero stamp. A zero ClosedAt means either the week
// has not closed, or it closed before this field existed (a legacy row);
// either way the caller must not trust it as a settlement instant.
func weekClosedAt(sch *SeasonSchedule, week int) (time.Time, bool) {
	if sch == nil {
		return time.Time{}, false
	}
	for _, wk := range sch.Weeks {
		if wk.Week == week {
			return wk.ClosedAt, !wk.ClosedAt.IsZero()
		}
	}
	return time.Time{}, false
}

// weekSettledBoundary resolves the best available already-settled instant
// for week: its own persisted ClosedAt when present (2026-08-30 review
// round 2, finding 2 — the true close instant, not an estimate), or, for a
// legacy row written before ClosedAt existed, its own latest known
// kickoff — the latest instant by which every one of that week's games
// had kicked off, and so the earliest instant that week could legitimately
// have closed. Never a following week's kickoff: the prior design anchored
// to week+1's kickoff, a future instant relative to every scheduled run
// right after week `week` closes, which made txn.At.After(boundary) false
// even for a transaction this exact run just created (At == now) — the
// in-period penalty was suppressed on every run, letting one team sweep
// every contested claim (the audited bug).
func weekSettledBoundary(sch *SeasonSchedule, games []GameInfo, week int) (time.Time, bool) {
	if closedAt, ok := weekClosedAt(sch, week); ok {
		return closedAt, true
	}
	if boundary, found, kickoffOK := weekCloseLastKickoff(games, week); found && kickoffOK {
		return boundary, true
	}
	return time.Time{}, false
}

// waiverPenaltyBoundary resolves the wall-clock instant (F1, corrected by
// the 2026-08-30 review's finding 1 and finding 2) after which a claim
// transaction counts as "in period" for the section 5.2.1 in-period
// penalty: a claim WIN that happened after the most recent week-close
// sends that team to the back for subsequent claims in the same period;
// the weekly close recomputes the base order from standings.
//
// now must be the run's own processing instant, not a stored value: a
// candidate boundary that is not strictly before now cannot yet be
// trusted to separate "this period" from "next period" (a commissioner
// force-close made ahead of the mirror catching up, or a degenerate
// mirror entry), so the caller falls back to waiverPenaltyFallbackFloor
// instead of risking the same suppression bug through a different route.
func waiverPenaltyBoundary(state PersistedState, games []GameInfo, now time.Time) time.Time {
	week := lastClosedWeek(state.Schedule)
	if week > 0 {
		if boundary, ok := weekSettledBoundary(state.Schedule, games, week); ok && now.After(boundary) {
			return boundary
		}
	}
	return waiverPenaltyFallbackFloor(state)
}

// waiverPenaltyFallbackFloor is F1's safe-direction floor (corrected by the
// 2026-08-30 review round 2's finding 1) for when waiverPenaltyBoundary
// cannot derive a firm, already-past week-close instant: no week has
// closed yet, the schedule mirror carries no kickoff for a legacy row
// missing ClosedAt (a source outage), or a commissioner force-close was
// made ahead of the mirror catching up.
//
// The floor must move only at a recompute — a week actually closing —
// never on every run. Before any week has closed, it anchors to the
// schedule's own GeneratedAt (season start): fixed for the whole
// pre-week-1 period, no matter how many daily runs pass with nothing to
// recompute against. Once a week has closed, it anchors to that week's
// own settled boundary (weekSettledBoundary — its ClosedAt, or a legacy
// row's last kickoff): fixed until the NEXT week closes. The prior design
// derived this floor from WaiversProcessedThrough, the last run's own
// commit watermark, which advances on every single run whether or not
// anything actually recomputed; that let a run-1 winner's penalty survive
// only into run 2 and then silently lapse by run 3, with the whole
// post-draft, pre-week-1 period (lastClosedWeek == 0, this floor's only
// path) exposed to it every season. A truly fresh store with neither a
// closed week nor a schedule returns the zero time, under which every
// real claim counts as in period — the safe direction this floor exists
// to bound, not remove.
func waiverPenaltyFallbackFloor(state PersistedState) time.Time {
	week := lastClosedWeek(state.Schedule)
	if week > 0 {
		if boundary, ok := weekSettledBoundary(state.Schedule, nil, week); ok {
			return boundary
		}
	}
	if state.Schedule != nil {
		return state.Schedule.GeneratedAt
	}
	return time.Time{}
}

// applyInPeriodPenalties replays every claim Transaction resolved after
// boundary, in At order, moving each winner to the back (section 5.2.1's
// in-period penalty). The penalty set empties automatically once a fresh
// recompute's boundary passes a claim's At instant, which is how the
// penalty "expires at the next recompute" with no separate expiry
// bookkeeping.
func applyInPeriodPenalties(base []string, transactions []Transaction, boundary time.Time) []string {
	claims := make([]Transaction, 0)
	for _, txn := range transactions {
		if txn.Type != "claim" {
			continue
		}
		if txn.At.After(boundary) {
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

// waiverBaseOrder computes section 5.2.1's pre-penalty base order: the
// inverse round-1 draft order before week 1 closes, or the
// performance-weighted blend anchored at lastClosedWeek once one has.
// This is the expensive, per-run-constant half of waiverOrder — it does
// not depend on anything a claim resolution can change (Transactions),
// only on the persisted schedule and draft order — so Store.ProcessWaivers
// computes it exactly once per run (F4) and replays only the cheap
// applyInPeriodPenalties half after each win, instead of recomputing
// standings once per resolved claim.
func waiverBaseOrder(state PersistedState, cfg Config) []string {
	teamIDs := defaultTeamIDs()
	week := lastClosedWeek(state.Schedule)
	if week == 0 || state.Schedule == nil {
		return reverseStrings(roundOneOrder(state.DraftOrder))
	}
	return performanceBaseOrder(*state.Schedule, teamIDs, cfg, week)
}

// waiverOrder derives the full current claim order (roster-ops spec
// section 5.2.1): the performance base at the last week close (or inverse
// draft order before week 1 closes), then in-period claim penalties
// replayed from the transaction log. Nothing here is stored. games backs
// the in-period penalty's time boundary (F1) — pass the same schedule
// mirror every other roster-ops read uses (Service.schedule()). now must
// be the caller's own processing/read instant (see waiverPenaltyBoundary).
func waiverOrder(state PersistedState, cfg Config, games []GameInfo, now time.Time) []string {
	base := waiverBaseOrder(state, cfg)
	boundary := waiverPenaltyBoundary(state, games, now)
	return applyInPeriodPenalties(base, state.Transactions, boundary)
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

// teamClaimIndices returns one team's claim indexes in their authoritative
// within-team order. Legacy gaps/duplicates sort by the old priority first,
// then filing time and ID for deterministic normalization.
func teamClaimIndices(claims []WaiverClaim, teamID string) []int {
	indices := make([]int, 0, teamOpenClaimCount(claims, teamID))
	for index, claim := range claims {
		if claim.TeamID == teamID {
			indices = append(indices, index)
		}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := claims[indices[i]], claims[indices[j]]
		priorityA, priorityB := a.Priority, b.Priority
		if priorityA <= 0 {
			priorityA = int(^uint(0) >> 1)
		}
		if priorityB <= 0 {
			priorityB = int(^uint(0) >> 1)
		}
		if priorityA != priorityB {
			return priorityA < priorityB
		}
		if !a.FiledAt.Equal(b.FiledAt) {
			return a.FiledAt.Before(b.FiledAt)
		}
		return a.ID < b.ID
	})
	return indices
}

func normalizeTeamClaimPriorities(claims []WaiverClaim, teamID string) []int {
	indices := teamClaimIndices(claims, teamID)
	for position, index := range indices {
		claims[index].Priority = position + 1
	}
	return indices
}

func normalizeAllClaimPriorities(claims []WaiverClaim) {
	seen := make(map[string]struct{})
	for _, claim := range claims {
		if _, ok := seen[claim.TeamID]; ok {
			continue
		}
		seen[claim.TeamID] = struct{}{}
		normalizeTeamClaimPriorities(claims, claim.TeamID)
	}
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
	// this run or earlier), "failed" (any other re-validation reason),
	// "deferred" (finding 6: a one-time notice the first time this claim's
	// AddID sits outside the bounded pool — the claim itself stays open,
	// unlike every other outcome here), or "expired" (finding 6, timing
	// corrected by the 2026-08-30 review round 2's finding 3: the claim
	// deferred for waiverClaimDeferralLimit consecutive runs, spanning at
	// least waiverClaimDeferralWindow of real time, and was removed
	// automatically).
	Outcome string
	// Reason carries the exact failure/expiry message for a "failed" or
	// "expired" outcome.
	Reason string
	// Position is the claiming team's public 1-based waiverOrder position at
	// this claim's selection step. A won perf-priority transaction records the
	// same value.
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
// descending, ties by waiverOrder position ascending and then that team's
// private Priority. Thus a manager's visible move is authoritative even
// when two of their own FAAB claims carry the same bid.
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
	posA, posB := waiverOrderPosition(order, a.TeamID), waiverOrderPosition(order, b.TeamID)
	if posA != posB {
		return posA < posB
	}
	return a.Priority < b.Priority
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

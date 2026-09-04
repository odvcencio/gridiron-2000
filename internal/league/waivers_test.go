package league

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------
// lastClosedWeek
// ---------------------------------------------------------------------

func TestLastClosedWeekNilSchedule(t *testing.T) {
	if got := lastClosedWeek(nil); got != 0 {
		t.Fatalf("lastClosedWeek(nil) = %d, want 0", got)
	}
}

func TestLastClosedWeekPartiallyClosed(t *testing.T) {
	sch := &SeasonSchedule{Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-2", Final: false}}},
	}}
	if got := lastClosedWeek(sch); got != 1 {
		t.Fatalf("lastClosedWeek = %d, want 1", got)
	}
}

func TestLastClosedWeekEmptyWeekNeverCounts(t *testing.T) {
	sch := &SeasonSchedule{Weeks: []ScheduleWeek{
		{Week: 1, Matchups: nil}, // an empty matchup list is never "closed"
	}}
	if got := lastClosedWeek(sch); got != 0 {
		t.Fatalf("lastClosedWeek = %d, want 0 (an empty week is never final)", got)
	}
}

// ---------------------------------------------------------------------
// waiverOrder — pre-week-1 inverse draft order (section 5.2.1, W == 0)
// ---------------------------------------------------------------------

func TestWaiverOrderPreWeek1InverseDraftOrder(t *testing.T) {
	teamIDs := defaultTeamIDs() // team-1..team-8, config order
	state := PersistedState{DraftOrder: append([]string(nil), teamIDs...)}
	order := waiverOrder(state, DefaultConfig(), nil, time.Time{})
	want := reverseStrings(teamIDs)
	if len(order) != len(want) {
		t.Fatalf("len(order) = %d, want %d", len(order), len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want the exact reverse of round-1 draft order %v", order, want)
		}
	}
}

func TestWaiverOrderPreWeek1FallsBackToDefaultOrderWhenUndrawn(t *testing.T) {
	state := PersistedState{} // no DraftOrder drawn yet
	order := waiverOrder(state, DefaultConfig(), nil, time.Time{})
	want := reverseStrings(defaultTeamIDs())
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v (teamOnClock's default-order fallback)", order, want)
		}
	}
}

// ---------------------------------------------------------------------
// waiverOrder — the section 5.2.1 performance-weighted formula
// ---------------------------------------------------------------------

// weightingFixtureSchedule builds a 2-week, 8-team schedule (fixed pairs
// team-1/2, team-3/4, team-5/6, team-7/8) where every team finishes 1-1,
// so ComputeStandings' record tier ties all 8 teams and the whole order
// falls to points-for (section 5.2.1 step 1). Season PF is set to
// strictly favor team-1 (highest) through team-8 (lowest), while week-2
// (the last closed week)'s points-only total is set to the exact mirror
// image — team-8 highest, team-1 lowest — so seasonRank and weeklyRank
// are perfectly anti-correlated across all 8 teams. This isolates
// season_weight_pct's effect cleanly: any sw in (0, 100] produces the
// same worst-season-first order (team-8..team-1); only the pure-weekly
// edge case, sw == 0, flips it (team-1..team-8).
func weightingFixtureSchedule() SeasonSchedule {
	// Per-team week1/week2 points, chosen so home wins week1 and away
	// wins week2 for every pair (every team finishes exactly 1-1).
	week1 := map[string]float64{"team-1": 290, "team-2": 270, "team-3": 250, "team-4": 230, "team-5": 210, "team-6": 190, "team-7": 170, "team-8": 150}
	week2 := map[string]float64{"team-1": 10, "team-2": 20, "team-3": 30, "team-4": 40, "team-5": 50, "team-6": 60, "team-7": 70, "team-8": 80}
	pairs := [][2]string{{"team-1", "team-2"}, {"team-3", "team-4"}, {"team-5", "team-6"}, {"team-7", "team-8"}}

	week1Matchups := make([]LeagueMatchup, 0, 4)
	week2Matchups := make([]LeagueMatchup, 0, 4)
	for _, pair := range pairs {
		home, away := pair[0], pair[1]
		week1Matchups = append(week1Matchups, LeagueMatchup{
			HomeTeamID: home, AwayTeamID: away, HomeScore: week1[home], AwayScore: week1[away], Final: true,
		})
		week2Matchups = append(week2Matchups, LeagueMatchup{
			HomeTeamID: home, AwayTeamID: away, HomeScore: week2[home], AwayScore: week2[away], Final: true,
		})
	}
	return SeasonSchedule{
		Season: 2026, Seed: 1,
		Weeks: []ScheduleWeek{
			{Week: 1, Matchups: week1Matchups},
			{Week: 2, Matchups: week2Matchups},
		},
	}
}

// TestWaiverOrderSeasonWeightDefault60 pins the owner-decided default
// weighting (spec section 5.2.1, config.go's season_weight_pct: 60):
// against the anti-correlated fixture, the 60/40 blend produces the same
// worst-season-first order the pure-season edge case (100) does — proof
// the season term dominates at 60, exactly as the fixture is designed to
// show (any sw in [1,100] gives this order here; sw == 0 is the only
// flip, pinned separately below).
func TestWaiverOrderSeasonWeightDefault60(t *testing.T) {
	sch := weightingFixtureSchedule()
	cfg := DefaultConfig()
	cfg.Waivers.SeasonWeightPct = 60
	state := PersistedState{Schedule: &sch}
	order := waiverOrder(state, cfg, nil, time.Time{})
	want := []string{"team-8", "team-7", "team-6", "team-5", "team-4", "team-3", "team-2", "team-1"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("order = %v, want %v (worst combined performance first)", order, want)
		}
	}
}

// TestWaiverOrderPureSeasonAt100 and TestWaiverOrderPureWeeklyAt0 pin
// test-plan item 2: season_weight_pct 0 and 100 reduce to pure weekly and
// pure season order, respectively.
func TestWaiverOrderPureSeasonAt100(t *testing.T) {
	sch := weightingFixtureSchedule()
	cfg := DefaultConfig()
	cfg.Waivers.SeasonWeightPct = 100
	order := waiverOrder(PersistedState{Schedule: &sch}, cfg, nil, time.Time{})
	want := []string{"team-8", "team-7", "team-6", "team-5", "team-4", "team-3", "team-2", "team-1"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("sw=100 order = %v, want pure-season order %v", order, want)
		}
	}
}

func TestWaiverOrderPureWeeklyAt0(t *testing.T) {
	sch := weightingFixtureSchedule()
	cfg := DefaultConfig()
	cfg.Waivers.SeasonWeightPct = 0
	order := waiverOrder(PersistedState{Schedule: &sch}, cfg, nil, time.Time{})
	want := []string{"team-1", "team-2", "team-3", "team-4", "team-5", "team-6", "team-7", "team-8"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("sw=0 order = %v, want pure-weekly order %v (the flip against sw=100)", order, want)
		}
	}
}

// TestWaiverOrderByeSubstitutesSeasonRank pins section 5.2.1 step 2: a
// team with no week-W matchup substitutes its season rank for the weekly
// rank.
func TestWaiverOrderByeSubstitutesSeasonRank(t *testing.T) {
	teamIDs := defaultTeamIDs()
	// A simple 1-week schedule: team-1 beats everyone it plays (paired
	// against team-2..team-7), and team-8 sits the week-1 bye — no week-1
	// matchup at all, so weeklyRank[team-8] must fall back to
	// seasonRank[team-8] rather than crash or read zero.
	sch := SeasonSchedule{Season: 2026, Seed: 1, Weeks: []ScheduleWeek{
		{Week: 1, ByeTeamID: "team-8", Matchups: []LeagueMatchup{
			{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 100, AwayScore: 50, Final: true},
			{HomeTeamID: "team-3", AwayTeamID: "team-4", HomeScore: 90, AwayScore: 60, Final: true},
			{HomeTeamID: "team-5", AwayTeamID: "team-6", HomeScore: 80, AwayScore: 70, Final: true},
			{HomeTeamID: "team-7", AwayTeamID: "team-1", HomeScore: 0, AwayScore: 0, Final: false}, // not part of week 1's close; ignored
		}},
	}}
	_ = teamIDs
	standings := ComputeStandings(sch, defaultTeamIDs(), TiebreakInputs{SeasonSeed: sch.Seed})
	seasonRank := map[string]int{}
	for _, st := range standings {
		seasonRank[st.TeamID] = st.Rank
	}
	weeklyRank := weeklyPointsRank(sch, defaultTeamIDs(), 1, seasonRank)
	if weeklyRank["team-8"] != seasonRank["team-8"] {
		t.Fatalf("bye team-8's weeklyRank = %d, want its seasonRank %d", weeklyRank["team-8"], seasonRank["team-8"])
	}
}

// TestWaiverOrderEqualCombinedBreaksBySeededDraw pins section 5.2.1 step
// 4's tie-break: equal combined scores order by the seeded draw, lower
// hash first. Two teams tied on every input (identical record, PF, and
// week-1 points) push both their seasonRank and weeklyRank ties down to
// ComputeStandings' and weeklyPointsRank's own seeded-draw fallback —
// the same seed, so the same team wins both, landing it at seasonRank 1
// and weeklyRank 1 (combined 100, the better performance) while the
// other lands at 2/2 (combined 200, the worse performance, ordered
// first). The outcome is deterministic and repeatable, and it must agree
// with a direct seededDrawHash comparison — this is the only knob
// performanceBaseOrder's own final tie-break line reads.
func TestWaiverOrderEqualCombinedBreaksBySeededDraw(t *testing.T) {
	sch := SeasonSchedule{Season: 2026, Seed: 42, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{
			{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 50, AwayScore: 50, Final: true}, // a tie: identical PF, identical record
		}},
	}}
	order1 := performanceBaseOrder(sch, []string{"team-1", "team-2"}, DefaultConfig(), 1)
	order2 := performanceBaseOrder(sch, []string{"team-1", "team-2"}, DefaultConfig(), 1)
	if order1[0] != order2[0] || order1[1] != order2[1] {
		t.Fatalf("seeded-draw tie-break is not repeatable: %v vs %v", order1, order2)
	}
	h1 := seededDrawHash(sch.Seed, "team-1")
	h2 := seededDrawHash(sch.Seed, "team-2")
	// The team with the HIGHER hash loses both underlying tie-breaks
	// (season and weekly), so it ends up with the worse combined score
	// and claims first (index 0) — the opposite team of the one
	// groupBySeededDraw would rank first internally.
	higherHashFirst := "team-1"
	if bytes.Compare(h1[:], h2[:]) < 0 {
		higherHashFirst = "team-2"
	}
	if order1[0] != higherHashFirst {
		t.Fatalf("order = %v, want %s first (it loses both the season and weekly seeded-draw ties, giving it the worse combined score)", order1, higherHashFirst)
	}
}

// ---------------------------------------------------------------------
// waiverOrder — in-period penalty (section 5.2.1's "move to back")
// ---------------------------------------------------------------------

func TestApplyInPeriodPenaltyMovesWinnerToBack(t *testing.T) {
	base := []string{"team-1", "team-2", "team-3", "team-4"}
	claimAt := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	transactions := []Transaction{
		{Type: "claim", TeamID: "team-1", At: claimAt},
	}
	boundary := claimAt.Add(-time.Hour) // the claim resolved after the boundary: in period
	order := applyInPeriodPenalties(base, transactions, boundary)
	want := []string{"team-2", "team-3", "team-4", "team-1"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("order = %v, want %v (team-1 moved to the back after its win)", order, want)
		}
	}
}

func TestApplyInPeriodPenaltyExpiresAtNextRecompute(t *testing.T) {
	base := []string{"team-1", "team-2", "team-3", "team-4"}
	claimAt := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	// The claim resolved before the new boundary once the period advances
	// (the following week's kickoff moved past it) — the penalty set
	// empties automatically.
	transactions := []Transaction{
		{Type: "claim", TeamID: "team-1", At: claimAt},
	}
	boundary := claimAt.Add(time.Hour)
	order := applyInPeriodPenalties(base, transactions, boundary)
	for i, id := range base {
		if order[i] != id {
			t.Fatalf("order = %v, want the untouched base %v (penalty expired)", order, base)
		}
	}
}

func TestApplyInPeriodPenaltyMultipleWinsReplayInAtOrder(t *testing.T) {
	base := []string{"team-1", "team-2", "team-3", "team-4"}
	transactions := []Transaction{
		{Type: "claim", TeamID: "team-3", At: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)},
		{Type: "claim", TeamID: "team-1", At: time.Date(2026, 9, 20, 9, 1, 0, 0, time.UTC)},
	}
	boundary := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	order := applyInPeriodPenalties(base, transactions, boundary)
	want := []string{"team-2", "team-4", "team-3", "team-1"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("order = %v, want %v (both wins replayed in At order)", order, want)
		}
	}
}

// TestApplyInPeriodPenaltyZeroBoundaryPenalizesEverything pins F1's safe
// direction at applyInPeriodPenalties' own level: a zero-value boundary
// (waiverPenaltyFallbackFloor's own last resort, a truly fresh store with
// neither a committed run nor a schedule) must treat every claim as in
// period, never as exempt — closing the equality loophole the old
// Week-number comparison had. waiverPenaltyBoundary's bounded fallback
// (finding 2 of the 2026-08-30 review) is pinned separately in
// TestWaiverPenaltyBoundary and TestProcessWaiversFallbackBoundedToRecentClaims.
func TestApplyInPeriodPenaltyZeroBoundaryPenalizesEverything(t *testing.T) {
	base := []string{"team-1", "team-2", "team-3", "team-4"}
	transactions := []Transaction{
		{Type: "claim", TeamID: "team-1", At: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)},
	}
	order := applyInPeriodPenalties(base, transactions, time.Time{})
	want := []string{"team-2", "team-3", "team-4", "team-1"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("order = %v, want %v (no boundary: every claim counts as in period)", order, want)
		}
	}
}

// ---------------------------------------------------------------------
// F3: an IR-occupant drop must not double-credit the roster cap
// ---------------------------------------------------------------------

// TestProcessWaiversRejectsIRDropAsFreeingARosterSpot ports the
// roster-ops audit's probe 2 for the claim-resolution path: a claim that
// names an IR occupant as its drop must not bypass rosterCap at
// resolution either — effectiveRosterSize already excludes the IR
// occupant, so the old `next.DropID == ""` gate (which skipped the cap
// check entirely whenever any drop was named) let this combination push
// the effective roster past cap.
func TestProcessWaiversRejectsIRDropAsFreeingARosterSpot(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	draftPlayerToTeam(t, store, "team-8", "d-a", now)
	draftPlayerToTeam(t, store, "team-8", "d-b", now)
	pool := processWaiversFixturePool()
	if err := store.PlaceInZone("team-8", "d-a", zoneIR, "RB", now); err != nil {
		t.Fatalf("PlaceInZone: %v", err)
	}
	state := store.Snapshot()
	rosterCap := effectiveRosterSize(state, "team-8") // team-8 is exactly at its effective cap right now

	if err := store.FileClaim(WaiverClaim{ID: "clm-ir", TeamID: "team-8", AddID: "wv-1", DropID: "d-a", FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, pool, rosterCap)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "failed" {
		t.Fatalf("results = %+v, want the claim to fail (an IR drop must not free a spot)", results)
	}
	after := store.Snapshot()
	if newEff := effectiveRosterSize(after, "team-8"); newEff > rosterCap {
		t.Fatalf("effective roster size %d exceeds cap %d after an IR-drop claim", newEff, rosterCap)
	}
}

// ---------------------------------------------------------------------
// F1: time-authoritative in-period penalty (roster-ops audit)
// ---------------------------------------------------------------------

// closedWeek1Schedule is the shared 4-matchup, all-final week-1 fixture
// the three F1 regression tests below all close over — the exact shape
// the roster-ops audit's probes used.
func closedWeek1Schedule() SeasonSchedule {
	return SeasonSchedule{Season: 2026, Seed: 1, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{
			{HomeTeamID: "team-1", AwayTeamID: "team-2", HomeScore: 10, AwayScore: 20, Final: true},
			{HomeTeamID: "team-3", AwayTeamID: "team-4", HomeScore: 30, AwayScore: 40, Final: true},
			{HomeTeamID: "team-5", AwayTeamID: "team-6", HomeScore: 50, AwayScore: 60, Final: true},
			{HomeTeamID: "team-7", AwayTeamID: "team-8", HomeScore: 70, AwayScore: 80, Final: true},
		}},
	}}
}

// TestProcessWaiversPenaltyAppliesWhenScheduleMirrorIsEmpty ports the
// roster-ops audit's probe 1: an empty schedule mirror (a source outage)
// used to make lineupCurrentWeekAt fall back to week 1, exactly equal to
// lastClosedWeek(1) — the old strict Week-number comparison
// (txn.Week > lastClosedWeek) silently suppressed the in-period penalty
// on that equality, letting the winner keep waiver position 1 and sweep
// every remaining claim in the same run.
func TestProcessWaiversPenaltyAppliesWhenScheduleMirrorIsEmpty(t *testing.T) {
	store := processWaiversFixtureStore(t)
	if err := store.SetSchedule(closedWeek1Schedule()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	cfg := processWaiversCfg()
	order := waiverOrder(store.Snapshot(), cfg, nil, now)
	first, rival := order[0], order[1]
	for _, c := range []WaiverClaim{
		{ID: "clm-a", TeamID: first, AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-b", TeamID: first, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-c", TeamID: rival, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
	} {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	// games == nil reproduces a schedule-source outage.
	results, err := store.ProcessWaivers(now, cfg, nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	wins := 0
	for _, r := range results {
		if r.Outcome == "won" && r.Claim.TeamID == first {
			wins++
		}
	}
	if wins > 1 {
		t.Fatalf("%s swept %d claims in one run after an empty schedule mirror; the in-period penalty must apply after its first win", first, wins)
	}
	if after := waiverOrder(store.Snapshot(), cfg, nil, now); after[0] == first {
		t.Fatalf("winner %s stayed at waiver position 1 after winning; no in-period penalty applied (order=%v)", first, after)
	}
}

// TestProcessWaiversPenaltyAppliesAfterForceClose ports the roster-ops
// audit's probe 1b: a commissioner force-closes week 1 (every matchup
// marked Final) while week 1's Monday-night game has not actually kicked
// off yet in the schedule mirror. lineupCurrentWeekAt still reads the
// live mirror's own current week (1), exactly equal to lastClosedWeek(1)
// — the same equality-suppression bug as the empty-mirror probe, this
// time from a real, healthy mirror that simply has not caught up to the
// force-close yet.
func TestProcessWaiversPenaltyAppliesAfterForceClose(t *testing.T) {
	store := processWaiversFixtureStore(t)
	if err := store.SetSchedule(closedWeek1Schedule()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC) // Monday morning run
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(-72 * time.Hour), Away: "PIT", Home: "NYJ", Final: true},
		{ID: "gmnf", Week: 1, Kickoff: now.Add(11 * time.Hour), Away: "KC", Home: "LV"}, // MNF, not kicked
	}
	cfg := processWaiversCfg()
	order := waiverOrder(store.Snapshot(), cfg, games, now)
	first, rival := order[0], order[1]
	for _, c := range []WaiverClaim{
		{ID: "clm-a", TeamID: first, AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-b", TeamID: first, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-c", TeamID: rival, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
	} {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.ProcessWaivers(now, cfg, games, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	wins := 0
	for _, r := range results {
		if r.Outcome == "won" && r.Claim.TeamID == first {
			wins++
		}
	}
	if wins > 1 {
		t.Fatalf("%s swept %d claims in one run after a force close; the in-period penalty must still apply", first, wins)
	}
}

// TestProcessWaiversCatchUpRunAfterForceCloseAppliesPenalty ports the
// 2026-08-30 review round 3's finding-1 probe: a deferred 03:00 run
// executes with the run's own now (rosterops.go passes nextRun, not
// wall-clock now) still short of the commissioner's own 12:30 force-close
// stamped into ClosedAt the same day (a source outage delaying the
// scheduled run behind an earlier force-close). weekSettledBoundary
// correctly resolves to that future ClosedAt, so
// waiverPenaltyBoundary's own now.After(boundary) guard fails and falls
// back — but the unguarded fallback used to re-derive the exact same
// future ClosedAt and return it anyway, so applyInPeriodPenalties'
// txn.At.After(boundary) was false for every claim this run resolved and
// one team swept all three contested players (the audited bug: "wins by
// team: map[team-2:3]"). The fixed fallback must drop to a floor
// strictly before now instead.
func TestProcessWaiversCatchUpRunAfterForceCloseAppliesPenalty(t *testing.T) {
	store := processWaiversFixtureStore(t)
	sch := closedWeek1Schedule()
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	closedAt := day.Add(12*time.Hour + 30*time.Minute) // commissioner's 12:30 force-close
	sch.Weeks[0].ClosedAt = closedAt
	if err := store.SetSchedule(sch); err != nil {
		t.Fatal(err)
	}
	now := day.Add(3 * time.Hour) // the deferred 03:00 catch-up run, still short of 12:30

	if boundary := waiverPenaltyBoundary(store.Snapshot(), nil, now); !boundary.Before(now) {
		t.Fatalf("waiverPenaltyBoundary = %v, want a floor strictly before the run instant %v", boundary, now)
	}

	cfg := processWaiversCfg()
	order := waiverOrder(store.Snapshot(), cfg, nil, now)
	first, r1, r2, r3 := order[0], order[1], order[2], order[3]

	pool := processWaiversFixturePool()
	pool["wv-3"] = Player{ID: "wv-3", Name: "Wire Three", Position: "WR", NFLTeam: "PIT"}

	var claims []WaiverClaim
	for _, addID := range []string{"wv-1", "wv-2", "wv-3"} {
		claims = append(claims,
			WaiverClaim{ID: "clm-" + addID + "-first", TeamID: first, AddID: addID, FiledAt: now.Add(-2 * time.Hour)},
			WaiverClaim{ID: "clm-" + addID + "-r1", TeamID: r1, AddID: addID, FiledAt: now.Add(-2 * time.Hour)},
			WaiverClaim{ID: "clm-" + addID + "-r2", TeamID: r2, AddID: addID, FiledAt: now.Add(-2 * time.Hour)},
			WaiverClaim{ID: "clm-" + addID + "-r3", TeamID: r3, AddID: addID, FiledAt: now.Add(-2 * time.Hour)},
		)
	}
	for _, c := range claims {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}

	results, err := store.ProcessWaivers(now, cfg, nil, pool, 99)
	if err != nil {
		t.Fatal(err)
	}
	winsByTeam := map[string]int{}
	for _, r := range results {
		if r.Outcome == "won" {
			winsByTeam[r.Claim.TeamID]++
		}
	}
	for team, wins := range winsByTeam {
		if wins > 1 {
			t.Fatalf("wins by team: %v; %s swept %d of 3 contested players in one catch-up run — the in-period penalty must still apply", winsByTeam, team, wins)
		}
	}
	if winsByTeam[first] == 3 {
		t.Fatalf("wins by team: %v; %s swept all 3 contested players", winsByTeam, first)
	}
}

// TestProcessWaiversPenaltyAppliesAfterFinalScheduledWeek covers the
// season's last scheduled week: once it closes there is no following week
// in the mirror at all (not merely an outage). Under the corrected
// finding-1 design, waiverPenaltyBoundary needs no following week — it
// anchors to lastClosedWeek's OWN last known kickoff, still present in
// the mirror here, so this exercises the legitimate anchored boundary,
// not a fallback. The old design anchored to a following week's kickoff,
// which never exists once the season ends, making its fallback
// permanent; that bounded-fallback behavior is covered separately by
// TestProcessWaiversFallbackBoundedToRecentClaims. Either way the
// invariant holds: the very last week's waiver leader must not earn a
// permanent, un-demotable position 1 for the rest of the franchise's
// history.
func TestProcessWaiversPenaltyAppliesAfterFinalScheduledWeek(t *testing.T) {
	store := processWaiversFixtureStore(t)
	sch := closedWeek1Schedule() // the whole season: exactly one week, already closed
	if err := store.SetSchedule(sch); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(-192 * time.Hour), Away: "PIT", Home: "NYJ", Final: true},
	} // no week-2 game exists anywhere: the season is over
	cfg := processWaiversCfg()
	order := waiverOrder(store.Snapshot(), cfg, games, now)
	first, rival := order[0], order[1]
	for _, c := range []WaiverClaim{
		{ID: "clm-a", TeamID: first, AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-b", TeamID: first, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-c", TeamID: rival, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
	} {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.ProcessWaivers(now, cfg, games, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	wins := 0
	for _, r := range results {
		if r.Outcome == "won" && r.Claim.TeamID == first {
			wins++
		}
	}
	if wins > 1 {
		t.Fatalf("%s swept %d claims after the season's last scheduled week; a post-season claim must still be penalized", first, wins)
	}
	if after := waiverOrder(store.Snapshot(), cfg, games, now); after[0] == first {
		t.Fatal("winner stayed at waiver position 1 after the season's last scheduled week; the post-season fallback must still demote a winner")
	}
}

// TestWaiverPenaltyBoundary is a direct table test of the boundary
// derivation itself (2026-08-30 review, finding 4; round 2 findings 1 and
// 2): every branch of waiverPenaltyBoundary/waiverPenaltyFallbackFloor,
// including the healthy path finding 1 was missing entirely — every prior
// F1 test's fixture had no next-week game, so every one of them took the
// fallback and none exercised a real, already-past anchored boundary.
func TestWaiverPenaltyBoundary(t *testing.T) {
	sundayKickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	wednesdayRun := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	closedWeek1 := closedWeek1Schedule()

	closedAtTuesdayNoon := func() SeasonSchedule {
		sch := closedWeek1Schedule()
		sch.Weeks[0].ClosedAt = sundayKickoff.Add(43 * time.Hour) // Tuesday ~noon
		return sch
	}()

	tests := []struct {
		name  string
		state PersistedState
		games []GameInfo
		now   time.Time
		want  time.Time
	}{
		{
			// A legacy row — closed before ClosedAt existed — still falls
			// back to its own last kickoff (finding 2's back-compat
			// requirement).
			name:  "healthy path: week 1 closed, its own kickoff already past (legacy row, ClosedAt zero)",
			state: PersistedState{Schedule: &closedWeek1},
			games: []GameInfo{{ID: "g1", Week: 1, Kickoff: sundayKickoff, Final: true}},
			now:   wednesdayRun,
			want:  sundayKickoff,
		},
		{
			name:  "healthy path ignores a later week's kickoff in the mirror",
			state: PersistedState{Schedule: &closedWeek1},
			games: []GameInfo{
				{ID: "g1", Week: 1, Kickoff: sundayKickoff, Final: true},
				{ID: "g2", Week: 2, Kickoff: sundayKickoff.AddDate(0, 0, 4), Final: false},
			},
			now:  wednesdayRun,
			want: sundayKickoff, // anchored to week 1's own kickoff, not week 2's
		},
		{
			// finding 2: once ClosedAt is persisted, it is the boundary —
			// not the last kickoff, even when the two disagree.
			name:  "healthy path prefers the persisted ClosedAt over the legacy kickoff estimate",
			state: PersistedState{Schedule: &closedAtTuesdayNoon},
			games: []GameInfo{{ID: "g1", Week: 1, Kickoff: sundayKickoff, Final: true}},
			now:   wednesdayRun,
			want:  sundayKickoff.Add(43 * time.Hour),
		},
		{
			name:  "candidate boundary not yet in the past falls back (force-close ahead of the mirror, ClosedAt zero)",
			state: PersistedState{Schedule: &closedWeek1},
			games: []GameInfo{{ID: "g1", Week: 1, Kickoff: sundayKickoff, Final: true}, {ID: "gmnf", Week: 1, Kickoff: wednesdayRun.Add(time.Hour)}},
			now:   wednesdayRun,
			want:  time.Time{}, // no GeneratedAt in this fixture either
		},
		{
			name:  "no games for the closed week at all falls back (source outage, ClosedAt zero)",
			state: PersistedState{Schedule: &closedWeek1},
			games: nil,
			now:   wednesdayRun,
			want:  time.Time{},
		},
		{
			name:  "week 0 (nothing closed yet) falls back to the zero time with no schedule",
			state: PersistedState{},
			games: []GameInfo{{ID: "g1", Week: 1, Kickoff: sundayKickoff, Final: true}},
			now:   wednesdayRun,
			want:  time.Time{},
		},
		{
			// Round 2, finding 1: before any week has closed, the floor is
			// the schedule's own GeneratedAt, fixed for the whole
			// pre-week-1 period — not derived from WaiversProcessedThrough,
			// which is deleted from this computation entirely.
			name:  "week 0 (nothing closed yet) falls back to the schedule's GeneratedAt",
			state: PersistedState{Schedule: &SeasonSchedule{Season: 2026, GeneratedAt: sundayKickoff.Add(-96 * time.Hour)}},
			games: nil,
			now:   wednesdayRun,
			want:  sundayKickoff.Add(-96 * time.Hour),
		},
		{
			// Round 2, finding 1: once a week has closed but this week's
			// own boundary is not (yet) derivable, the floor still anchors
			// to the schedule's GeneratedAt, no epsilon — never to a
			// per-run watermark.
			name:  "week closed but its own boundary is not derivable falls back to GeneratedAt",
			state: PersistedState{Schedule: &SeasonSchedule{Season: 2026, GeneratedAt: sundayKickoff.Add(-72 * time.Hour), Weeks: closedWeek1.Weeks}},
			games: nil,
			now:   wednesdayRun,
			want:  sundayKickoff.Add(-72 * time.Hour),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := waiverPenaltyBoundary(tc.state, tc.games, tc.now)
			if !got.Equal(tc.want) {
				t.Fatalf("waiverPenaltyBoundary = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWaiverPenaltyFallbackFloorFixedAcrossRuns pins finding 1 of the
// 2026-08-30 review round 2 directly against waiverPenaltyFallbackFloor:
// the floor must move only when a week actually closes (or, before any
// close, never at all — it stays pinned to GeneratedAt), regardless of
// how many times WaiversProcessedThrough itself advances. The deleted
// watermark-derived design would have returned a different answer on
// every one of these three calls even though nothing here recomputed.
func TestWaiverPenaltyFallbackFloorFixedAcrossRuns(t *testing.T) {
	generatedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	now := generatedAt.Add(200 * time.Hour) // strictly after every candidate below
	sch := SeasonSchedule{Season: 2026, GeneratedAt: generatedAt}
	state := PersistedState{Schedule: &sch}

	for i, processedThrough := range []time.Time{
		generatedAt.Add(24 * time.Hour),
		generatedAt.Add(48 * time.Hour),
		generatedAt.Add(72 * time.Hour),
	} {
		state.WaiversProcessedThrough = processedThrough
		got := waiverPenaltyFallbackFloor(state, now)
		if !got.Equal(generatedAt) {
			t.Fatalf("run %d: waiverPenaltyFallbackFloor = %v, want the fixed GeneratedAt %v regardless of WaiversProcessedThrough (%v)", i+1, got, generatedAt, processedThrough)
		}
	}

	// Once a week closes, the floor moves to that close's own boundary —
	// and only then.
	closedAt := generatedAt.Add(96 * time.Hour)
	sch.Weeks = []ScheduleWeek{{Week: 1, ClosedAt: closedAt, Matchups: []LeagueMatchup{
		{HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true},
	}}}
	state.Schedule = &sch
	if got := waiverPenaltyFallbackFloor(state, now); !got.Equal(closedAt) {
		t.Fatalf("after week 1 closes, waiverPenaltyFallbackFloor = %v, want the week's own ClosedAt %v", got, closedAt)
	}
}

// TestWaiverPenaltyFallbackFloorGuardsBothBranches pins the 2026-08-30
// review round 3's finding 1: when now itself has not yet reached a
// candidate — the closed week's own settled boundary, or the schedule's
// GeneratedAt — the fallback must not return that future candidate. A
// deferred run's own now can fall before a week-close instant its
// watermark has not caught up to (a source outage delaying a run behind
// a commissioner's earlier force-close); returning the future candidate
// there reproduces the exact suppression bug finding 1 fixed at the
// waiverPenaltyBoundary level, through this unguarded fallback instead.
func TestWaiverPenaltyFallbackFloorGuardsBothBranches(t *testing.T) {
	t.Run("closed-week branch: candidate not yet in the past is not returned", func(t *testing.T) {
		closedAt := time.Date(2026, 9, 20, 12, 30, 0, 0, time.UTC)
		sch := SeasonSchedule{
			Season:      2026,
			GeneratedAt: closedAt.Add(-96 * time.Hour),
			Weeks: []ScheduleWeek{{Week: 1, ClosedAt: closedAt, Matchups: []LeagueMatchup{
				{HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true},
			}}},
		}
		state := PersistedState{Schedule: &sch}
		now := closedAt.Add(-1 * time.Hour) // before the close the watermark hasn't caught up to
		got := waiverPenaltyFallbackFloor(state, now)
		if got.Equal(closedAt) {
			t.Fatalf("waiverPenaltyFallbackFloor = %v, must not be the future ClosedAt %v when now (%v) precedes it", got, closedAt, now)
		}
		if !got.Equal(sch.GeneratedAt) {
			t.Fatalf("waiverPenaltyFallbackFloor = %v, want the fallback to drop to GeneratedAt %v (still before now)", got, sch.GeneratedAt)
		}
	})

	t.Run("GeneratedAt branch: candidate not yet in the past falls back to the zero time", func(t *testing.T) {
		generatedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		sch := SeasonSchedule{Season: 2026, GeneratedAt: generatedAt}
		state := PersistedState{Schedule: &sch}
		now := generatedAt.Add(-1 * time.Hour) // before the schedule was even generated
		got := waiverPenaltyFallbackFloor(state, now)
		if !got.Equal(time.Time{}) {
			t.Fatalf("waiverPenaltyFallbackFloor = %v, want the zero time when now (%v) precedes GeneratedAt (%v)", got, now, generatedAt)
		}
	})
}

// TestProcessWaiversHealthyBoundaryDemotesWithinRun ports the 2026-08-30
// review's reviewer-verified probe for finding 1: week 1 closed with its
// own kickoffs already in the mirror (the healthy path, not a fallback),
// the week-2 Thursday game also present, a Wednesday 09:00 run, three
// teams, two contested claims. The first winner must drop back and must
// not also win the second contested claim in the same run.
func TestProcessWaiversHealthyBoundaryDemotesWithinRun(t *testing.T) {
	store := processWaiversFixtureStore(t)
	if err := store.SetSchedule(closedWeek1Schedule()); err != nil {
		t.Fatal(err)
	}
	sundayKickoff := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	thursdayKickoff := sundayKickoff.AddDate(0, 0, 4)
	now := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC) // Wednesday 09:00
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: sundayKickoff, Away: "PIT", Home: "NYJ", Final: true},
		{ID: "g2", Week: 1, Kickoff: sundayKickoff, Away: "SF", Home: "SEA", Final: true},
		{ID: "g3", Week: 1, Kickoff: sundayKickoff, Away: "DAL", Home: "PHI", Final: true},
		{ID: "g4", Week: 1, Kickoff: sundayKickoff, Away: "GB", Home: "CHI", Final: true},
		{ID: "gwk2", Week: 2, Kickoff: thursdayKickoff}, // week 2's own game, not yet kicked
	}
	cfg := processWaiversCfg()
	order := waiverOrder(store.Snapshot(), cfg, games, now)
	first, rival, third := order[0], order[1], order[2]
	for _, c := range []WaiverClaim{
		{ID: "clm-a", TeamID: first, AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-b", TeamID: rival, AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-c", TeamID: first, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-d", TeamID: third, AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
	} {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.ProcessWaivers(now, cfg, games, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	wins := 0
	for _, r := range results {
		if r.Outcome == "won" && r.Claim.TeamID == first {
			wins++
		}
	}
	if wins > 1 {
		t.Fatalf("%s swept %d contested claims in one run on the healthy anchored-boundary path (want at most 1)", first, wins)
	}
	if after := waiverOrder(store.Snapshot(), cfg, games, now); after[0] == first {
		t.Fatalf("winner %s stayed at waiver position 1 after winning on the healthy anchored-boundary path (order=%v)", first, after)
	}
}

// TestProcessWaiversPenaltyStaysFixedAcrossRunsUntilRecompute ports the
// 2026-08-30 review round 2's reviewer-verified probe for finding 1: the
// whole post-draft, pre-week-1 period runs the fallback floor exclusively
// (lastClosedWeek == 0 the entire time), and the old floor derived from
// WaiversProcessedThrough, which advances on every single
// Store.ProcessWaivers run whether or not anything actually recomputed.
// The probe showed a run-1 winner back at waiver position 1 by run 3 with
// no week ever closing in between. This asserts the winner instead stays
// demoted through runs 2, 3, and 4.
func TestProcessWaiversPenaltyStaysFixedAcrossRunsUntilRecompute(t *testing.T) {
	store := processWaiversFixtureStore(t)
	generatedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := store.SetSchedule(SeasonSchedule{Season: 2026, GeneratedAt: generatedAt}); err != nil {
		t.Fatal(err)
	}
	cfg := processWaiversCfg()

	run1 := generatedAt.Add(24 * time.Hour)
	order := waiverOrder(store.Snapshot(), cfg, nil, run1)
	winner, rival := order[0], order[1]
	for _, c := range []WaiverClaim{
		{ID: "clm-a", TeamID: winner, AddID: "wv-1", FiledAt: run1.Add(-time.Hour)},
		{ID: "clm-b", TeamID: rival, AddID: "wv-1", FiledAt: run1.Add(-time.Hour)},
	} {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ProcessWaivers(run1, cfg, nil, processWaiversFixturePool(), 99); err != nil {
		t.Fatal(err)
	}
	if after := waiverOrder(store.Snapshot(), cfg, nil, run1); after[0] == winner {
		t.Fatalf("winner %s stayed at waiver position 1 immediately after run 1 (order=%v)", winner, after)
	}

	// Runs 2-4: no new claims, no week closed. The old bug forgave the
	// penalty by run 3 because its floor tracked WaiversProcessedThrough;
	// the fixed floor must not move with no recompute in between.
	for i, at := range []time.Time{run1.Add(24 * time.Hour), run1.Add(48 * time.Hour), run1.Add(72 * time.Hour)} {
		if _, err := store.ProcessWaivers(at, cfg, nil, processWaiversFixturePool(), 99); err != nil {
			t.Fatal(err)
		}
		if after := waiverOrder(store.Snapshot(), cfg, nil, at); after[0] == winner {
			t.Fatalf("run %d: winner %s returned to waiver position 1 with no week-close recompute (order=%v)", i+2, winner, after)
		}
	}
}

// TestProcessWaiversPenaltyClearsAcrossAKickoffToCloseGap ports the
// 2026-08-30 review round 2's reviewer-verified probe for finding 2: a
// team wins a claim Tuesday morning, while week 1 has not actually closed
// yet (WeekCloseReady requires stats 24 hours past the last kickoff, so
// the close itself lands well after the kickoff that starts that clock).
// Week 1 closes Tuesday noon, stamping its own ClosedAt. By Wednesday's
// run, the winner must be back at the fresh post-close base order — not
// still demoted for a second period because the boundary anchored to
// Monday night's kickoff instead of Tuesday noon's actual close.
func TestProcessWaiversPenaltyClearsAcrossAKickoffToCloseGap(t *testing.T) {
	store := processWaiversFixtureStore(t)
	generatedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	openWeek1 := SeasonSchedule{Season: 2026, GeneratedAt: generatedAt, Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{
			{HomeTeamID: "team-1", AwayTeamID: "team-2"},
			{HomeTeamID: "team-3", AwayTeamID: "team-4"},
			{HomeTeamID: "team-5", AwayTeamID: "team-6"},
			{HomeTeamID: "team-7", AwayTeamID: "team-8"},
		}},
	}}
	if err := store.SetSchedule(openWeek1); err != nil {
		t.Fatal(err)
	}
	cfg := processWaiversCfg()

	mondayNightKickoff := time.Date(2026, 9, 14, 20, 15, 0, 0, time.UTC) // MNF
	tuesdayMorning := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g-mnf", Week: 1, Kickoff: mondayNightKickoff, Final: true}}

	// Tuesday morning: week 1 has not closed yet (lastClosedWeek == 0),
	// so this run resolves on the pre-week-1 base order and the winner is
	// demoted for the period.
	order := waiverOrder(store.Snapshot(), cfg, games, tuesdayMorning)
	winner, rival := order[0], order[1]
	for _, c := range []WaiverClaim{
		{ID: "clm-a", TeamID: winner, AddID: "wv-1", FiledAt: tuesdayMorning.Add(-time.Hour)},
		{ID: "clm-b", TeamID: rival, AddID: "wv-1", FiledAt: tuesdayMorning.Add(-time.Hour)},
	} {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ProcessWaivers(tuesdayMorning, cfg, games, processWaiversFixturePool(), 99); err != nil {
		t.Fatal(err)
	}
	if after := waiverOrder(store.Snapshot(), cfg, games, tuesdayMorning); after[0] == winner {
		t.Fatalf("winner %s stayed at waiver position 1 immediately after the Tuesday-morning win (order=%v)", winner, after)
	}

	// Week 1 closes Tuesday noon: stats caught up 24+ hours past the
	// Monday night kickoff, and closeWeek stamps its own ClosedAt
	// (season.go). This mirrors closeWeek's own commit without pulling in
	// the full Service/scorer stack this package-level test does not need.
	tuesdayNoon := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	closedWeek1 := openWeek1
	closedWeek1.Weeks = []ScheduleWeek{{Week: 1, ClosedAt: tuesdayNoon,
		Matchups: append([]LeagueMatchup(nil), openWeek1.Weeks[0].Matchups...)}}
	for i := range closedWeek1.Weeks[0].Matchups {
		closedWeek1.Weeks[0].Matchups[i].Final = true
	}
	if err := store.SetSchedule(closedWeek1); err != nil {
		t.Fatal(err)
	}

	// Wednesday's run: the Tuesday-morning win predates the Tuesday-noon
	// close, so it must not still be "in period" for this fresh,
	// post-close recompute — the winner returns to the base order this
	// close produced, exactly.
	wednesday := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	after := waiverOrder(store.Snapshot(), cfg, games, wednesday)
	base := performanceBaseOrder(closedWeek1, defaultTeamIDs(), cfg, 1)
	if after[0] != base[0] {
		t.Fatalf("winner %s stayed demoted after week 1's close recomputed; order=%v, want the fresh base order %v", winner, after, base)
	}
}

// TestProcessWaiversFallbackBoundedToRecentClaims pins finding 1 of the
// 2026-08-30 review round 2: before any week has closed, the fallback
// floor bounds to the schedule's own GeneratedAt — a fixed instant for
// the whole pre-week-1 period — not to WaiversProcessedThrough, which the
// old design used and which advanced on every single run. A win from
// before the schedule was even generated must stay exempt; a win after
// GeneratedAt must still be penalized.
func TestProcessWaiversFallbackBoundedToRecentClaims(t *testing.T) {
	base := []string{"team-1", "team-2", "team-3", "team-4"}
	generatedAt := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	state := PersistedState{
		Schedule: &SeasonSchedule{Season: 2026, GeneratedAt: generatedAt},
		Transactions: []Transaction{
			// Before the schedule's own GeneratedAt. Must not be
			// penalized — the deleted WaiversProcessedThrough-derived
			// floor would have moved on every run instead of staying
			// pinned to this schedule's own fixed start.
			{Type: "claim", TeamID: "team-1", At: generatedAt.Add(-30 * 24 * time.Hour)},
			// After GeneratedAt. Must still be penalized.
			{Type: "claim", TeamID: "team-2", At: generatedAt.Add(time.Hour)},
		},
	}
	// games == nil and no week closed: this exercises the bounded
	// fallback specifically, not the healthy anchored path.
	boundary := waiverPenaltyBoundary(state, nil, generatedAt.Add(2*time.Hour))
	order := applyInPeriodPenalties(base, state.Transactions, boundary)
	if order[0] != "team-1" {
		t.Fatalf("order = %v, want team-1 (the win before GeneratedAt) still at position 1 — the fallback must not reach back before the schedule's own start", order)
	}
	if order[len(order)-1] != "team-2" {
		t.Fatalf("order = %v, want team-2 (the win after GeneratedAt) demoted to the back", order)
	}
}

// ---------------------------------------------------------------------
// faabRemaining
// ---------------------------------------------------------------------

func TestFaabRemainingDerivesFromClaimBids(t *testing.T) {
	state := PersistedState{Transactions: []Transaction{
		{Type: "claim", TeamID: "team-1", Bid: 25},
		{Type: "claim", TeamID: "team-1", Bid: 10},
		{Type: "add", TeamID: "team-1", Bid: 999}, // not a claim; must not count
	}}
	remaining := faabRemaining(state, 100)
	if remaining["team-1"] != 65 {
		t.Fatalf("team-1 remaining = %d, want 65 (100 - 25 - 10)", remaining["team-1"])
	}
	if remaining["team-2"] != 100 {
		t.Fatalf("team-2 remaining = %d, want the untouched budget 100", remaining["team-2"])
	}
}

func TestFaabUnitsRendersNonCurrencyUnits(t *testing.T) {
	cases := []struct {
		amount int
		want   string
	}{
		{amount: 50, want: "50 FAAB"},
		{amount: 1, want: "1 FAAB"},
		{amount: 0, want: "0 FAAB"},
	}
	for _, c := range cases {
		got := faabUnits(c.amount)
		if got != c.want {
			t.Fatalf("faabUnits(%d) = %q, want %q", c.amount, got, c.want)
		}
		if bytes.ContainsRune([]byte(got), '$') {
			t.Fatalf("faabUnits(%d) = %q, must never contain a currency symbol", c.amount, got)
		}
	}
}

// ---------------------------------------------------------------------
// Daily run-instant arithmetic
// ---------------------------------------------------------------------

func TestFirstRunAtOrAfter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "UTC"
	cfg.Waivers.ProcessTime = "09:00"

	before := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	if got := firstRunAtOrAfter(cfg, before); !got.Equal(time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("firstRunAtOrAfter(08:00) = %v, want 09:00 the same day", got)
	}
	after := time.Date(2026, 9, 17, 9, 30, 0, 0, time.UTC)
	if got := firstRunAtOrAfter(cfg, after); !got.Equal(time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("firstRunAtOrAfter(09:30) = %v, want 09:00 the next day", got)
	}
	exact := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	if got := firstRunAtOrAfter(cfg, exact); !got.Equal(exact) {
		t.Fatalf("firstRunAtOrAfter(exact) = %v, want the same instant (inclusive)", got)
	}
}

func TestFirstRunStrictlyAfter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "UTC"
	cfg.Waivers.ProcessTime = "09:00"
	exact := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	if got := firstRunStrictlyAfter(cfg, exact); !got.Equal(time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("firstRunStrictlyAfter(exact) = %v, want the NEXT day's run (strict)", got)
	}
}

func TestClearsAtHonorsClearDays(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "UTC"
	cfg.Waivers.ProcessTime = "09:00"
	cfg.Waivers.ClearDays = 2
	droppedAt := time.Date(2026, 9, 15, 21, 40, 0, 0, time.UTC) // Tuesday 21:40
	got := clearsAt(cfg, droppedAt)
	want := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC) // Friday 09:00 (worked example 11.2)
	if !got.Equal(want) {
		t.Fatalf("clearsAt = %v, want %v (roster-ops spec worked example 11.2)", got, want)
	}
}

// TestWaiverProcessingClockAcrossDSTSpringForward covers the roster-ops
// audit's missing DST test: a run spanning a DST transition in the
// league timezone must keep landing on the configured local process_time
// (09:00), never drifting by the transition's lost or gained hour.
// firstRunAtOrAfter/firstRunStrictlyAfter build every candidate with
// time.Date in the league's *time.Location, which is already DST-aware —
// this walks six consecutive daily runs across March 8, 2026, the US
// spring-forward Sunday (America/New_York: 02:00 EST -> 03:00 EDT), to
// confirm that holds in practice, not just in the implementation's own
// reasoning.
func TestWaiverProcessingClockAcrossDSTSpringForward(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "America/New_York"
	cfg.Waivers.ProcessTime = "09:00"
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}

	run := firstRunAtOrAfter(cfg, time.Date(2026, 3, 5, 9, 0, 0, 0, loc)) // Thursday, before the transition week
	sawEST, sawEDT := false, false
	for i := 0; i < 6; i++ {
		local := run.In(loc)
		if local.Hour() != 9 || local.Minute() != 0 {
			t.Fatalf("run %d local time = %02d:%02d on %s, want 09:00 unshifted", i, local.Hour(), local.Minute(), local.Format("2006-01-02"))
		}
		switch _, offset := run.Zone(); offset {
		case -5 * 3600:
			sawEST = true
		case -4 * 3600:
			sawEDT = true
		default:
			t.Fatalf("run %d UTC offset = %ds, want EST (-18000) or EDT (-14400)", i, offset)
		}
		run = firstRunStrictlyAfter(cfg, run)
	}
	if !sawEST || !sawEDT {
		t.Fatalf("the walk did not span the DST transition: sawEST=%v sawEDT=%v", sawEST, sawEDT)
	}
}

// ---------------------------------------------------------------------
// Availability (roster-ops spec section 5.1)
// ---------------------------------------------------------------------

func TestPlayerWaiverStatusDropThenClear(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "UTC"
	cfg.Waivers.ProcessTime = "09:00"
	cfg.Waivers.ClearDays = 2
	droppedAt := time.Date(2026, 9, 15, 21, 40, 0, 0, time.UTC)
	state := PersistedState{Transactions: []Transaction{
		{Type: "drop", TeamID: "team-5", Drops: []TransactionPlayer{{PlayerID: "p-1"}}, At: droppedAt},
	}}
	games := []GameInfo{}

	stillOnWaivers := playerWaiverStatus(state, cfg, games, "p-1", "PIT", droppedAt.Add(time.Hour))
	if stillOnWaivers.State != AvailabilityOnWaivers || stillOnWaivers.Reason != "dropped" {
		t.Fatalf("status = %+v, want ON WAIVERS (dropped) shortly after the drop", stillOnWaivers)
	}
	wantClears := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if !stillOnWaivers.ResolvesAt.Equal(wantClears) {
		t.Fatalf("ResolvesAt = %v, want %v", stillOnWaivers.ResolvesAt, wantClears)
	}

	cleared := playerWaiverStatus(state, cfg, games, "p-1", "PIT", wantClears)
	if cleared.State != AvailabilityFreeAgent {
		t.Fatalf("status at clearsAt = %+v, want FREE AGENT (inclusive boundary)", cleared)
	}
}

// TestPlayerWaiverStatusAddWithDropComboOnWaivers covers AddPlayer's
// roster-full add-with-drop combo (players.go: a Type "add" transaction
// that also carries a Drops entry). In a freshly post-draft league every
// roster starts at capacity, so this combo is the majority drop path
// there; before isFreeAgencyDrop widened lastDropInstant's filter, this
// player skipped the waiver window and showed FREE AGENT immediately.
func TestPlayerWaiverStatusAddWithDropComboOnWaivers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "UTC"
	cfg.Waivers.ProcessTime = "09:00"
	cfg.Waivers.ClearDays = 2
	droppedAt := time.Date(2026, 9, 15, 21, 40, 0, 0, time.UTC)
	state := PersistedState{Transactions: []Transaction{
		{
			Type:   "add",
			TeamID: "team-5",
			Adds:   []TransactionPlayer{{PlayerID: "p-2"}},
			Drops:  []TransactionPlayer{{PlayerID: "p-1"}},
			At:     droppedAt,
		},
	}}
	games := []GameInfo{}

	stillOnWaivers := playerWaiverStatus(state, cfg, games, "p-1", "PIT", droppedAt.Add(time.Hour))
	if stillOnWaivers.State != AvailabilityOnWaivers || stillOnWaivers.Reason != "dropped" {
		t.Fatalf("status = %+v, want ON WAIVERS (dropped) shortly after an add-with-drop combo", stillOnWaivers)
	}
	wantClears := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if !stillOnWaivers.ResolvesAt.Equal(wantClears) {
		t.Fatalf("ResolvesAt = %v, want %v", stillOnWaivers.ResolvesAt, wantClears)
	}

	cleared := playerWaiverStatus(state, cfg, games, "p-1", "PIT", wantClears)
	if cleared.State != AvailabilityFreeAgent {
		t.Fatalf("status at clearsAt = %+v, want FREE AGENT (inclusive boundary)", cleared)
	}
}

// TestPlayerWaiverStatusClaimWithDropComboOnWaivers covers
// Store.ProcessWaivers' own roster-full claim-drop (store.go: a Type
// "claim" transaction that also carries a Drops entry) — the same
// isFreeAgencyDrop gap, on the claim-resolution path instead of AddPlayer.
func TestPlayerWaiverStatusClaimWithDropComboOnWaivers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "UTC"
	cfg.Waivers.ProcessTime = "09:00"
	cfg.Waivers.ClearDays = 2
	droppedAt := time.Date(2026, 9, 15, 21, 40, 0, 0, time.UTC)
	state := PersistedState{Transactions: []Transaction{
		{
			Type:   "claim",
			TeamID: "team-5",
			Adds:   []TransactionPlayer{{PlayerID: "p-2"}},
			Drops:  []TransactionPlayer{{PlayerID: "p-1"}},
			At:     droppedAt,
		},
	}}
	games := []GameInfo{}

	stillOnWaivers := playerWaiverStatus(state, cfg, games, "p-1", "PIT", droppedAt.Add(time.Hour))
	if stillOnWaivers.State != AvailabilityOnWaivers || stillOnWaivers.Reason != "dropped" {
		t.Fatalf("status = %+v, want ON WAIVERS (dropped) shortly after a claim-with-drop combo", stillOnWaivers)
	}
	wantClears := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if !stillOnWaivers.ResolvesAt.Equal(wantClears) {
		t.Fatalf("ResolvesAt = %v, want %v", stillOnWaivers.ResolvesAt, wantClears)
	}

	cleared := playerWaiverStatus(state, cfg, games, "p-1", "PIT", wantClears)
	if cleared.State != AvailabilityFreeAgent {
		t.Fatalf("status at clearsAt = %+v, want FREE AGENT (inclusive boundary)", cleared)
	}
}

// TestLastDropInstantIgnoresTrade guards the one exclusion isFreeAgencyDrop
// still enforces: a "trade" always sets OtherTeamID, and its Drops
// transfer straight to that team (currentRosters' trade-reversal branch,
// roster.go), never through free agency, so a trade must never start a
// waiver clock even though it carries a Drops entry shaped exactly like a
// real release.
func TestLastDropInstantIgnoresTrade(t *testing.T) {
	state := PersistedState{Transactions: []Transaction{
		{
			Type:        "trade",
			TeamID:      "team-5",
			OtherTeamID: "team-6",
			Drops:       []TransactionPlayer{{PlayerID: "p-1"}},
			At:          time.Date(2026, 9, 15, 21, 40, 0, 0, time.UTC),
		},
	}}
	if _, _, found := lastDropInstant(state, "p-1"); found {
		t.Fatalf("lastDropInstant recognized a trade's Drops entry; a trade must never start a waiver clock")
	}
}

func TestPlayerWaiverStatusKickoffLockedFreeAgent(t *testing.T) {
	cfg := DefaultConfig()
	kickoff := time.Date(2026, 9, 13, 13, 0, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "PIT", Home: "NYJ", Final: false}}
	state := PersistedState{}

	before := playerWaiverStatus(state, cfg, games, "p-1", "PIT", kickoff.Add(-time.Minute))
	if before.State != AvailabilityFreeAgent {
		t.Fatalf("pre-kickoff status = %+v, want FREE AGENT", before)
	}
	locked := playerWaiverStatus(state, cfg, games, "p-1", "PIT", kickoff.Add(time.Minute))
	if locked.State != AvailabilityOnWaivers || locked.Reason != "kickoff" {
		t.Fatalf("in-progress-game status = %+v, want ON WAIVERS (kickoff)", locked)
	}
	final := GameInfo{ID: "g1", Week: 1, Kickoff: kickoff, Away: "PIT", Home: "NYJ", Final: true}
	afterFinal := playerWaiverStatus(state, cfg, []GameInfo{final}, "p-1", "PIT", kickoff.Add(6*time.Hour))
	if afterFinal.State != AvailabilityFreeAgent {
		t.Fatalf("post-final status = %+v, want FREE AGENT", afterFinal)
	}
}

// TestPlayerWaiverStatusKickoffLockNormalizesTank01Abbreviations is item
// 2's own regression test (2026-08-31 post-wave audit): kickoffLockedGame
// must resolve a Tank01-sourced "LAR" player against an nflverse-
// normalized "LA" schedule entry, the same normalization teamHasGame/
// playerLockAt already apply. Before this fix a LAR player's in-progress
// game never matched, so they stayed FREE AGENT (addable/droppable)
// straight through their own kickoff instead of locking ON WAIVERS.
func TestPlayerWaiverStatusKickoffLockNormalizesTank01Abbreviations(t *testing.T) {
	cfg := DefaultConfig()
	kickoff := time.Date(2026, 9, 13, 13, 0, 0, 0, time.UTC)
	games := []GameInfo{{ID: "g1", Week: 1, Kickoff: kickoff, Away: "LA", Home: "SF", Final: false}} // nflverse-normalized
	state := PersistedState{}

	locked := playerWaiverStatus(state, cfg, games, "p-1", "LAR", kickoff.Add(time.Minute)) // Tank01-style
	if locked.State != AvailabilityOnWaivers || locked.Reason != "kickoff" {
		t.Fatalf("in-progress LAR status = %+v, want ON WAIVERS (kickoff)", locked)
	}
}

// TestWaiverResolutionPhrase pins J3 F17: /team's Signal Watch, /players'
// pool row, and /players' MY CLAIMS card all call this one helper, so the
// three surfaces can never print a different answer to "when does this
// waiver resolve" again.
func TestWaiverResolutionPhrase(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timezone = "America/New_York"
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	t.Run("not on waivers is empty", func(t *testing.T) {
		if got := waiverResolutionPhrase(cfg, "PIT", waiverStatus{State: AvailabilityFreeAgent}, now); got != "" {
			t.Fatalf("free-agent phrase = %q, want empty", got)
		}
	})

	t.Run("kickoff names the event before the run time", func(t *testing.T) {
		resolvesAt := now.Add(9 * time.Hour)
		status := waiverStatus{State: AvailabilityOnWaivers, Reason: "kickoff", ResolvesAt: resolvesAt}
		got := waiverResolutionPhrase(cfg, "PIT", status, now)
		want := "Resolves after the PIT game ends, at the next waiver run — " + formatResolvesAt(cfg, resolvesAt) + " · " + deadlineRelativeTime(now, resolvesAt) + "."
		if got != want {
			t.Fatalf("kickoff phrase = %q, want %q", got, want)
		}
	})

	t.Run("dropped names the exact run time and a relative phrase", func(t *testing.T) {
		resolvesAt := now.Add(2 * 24 * time.Hour)
		status := waiverStatus{State: AvailabilityOnWaivers, Reason: "dropped", ResolvesAt: resolvesAt}
		got := waiverResolutionPhrase(cfg, "PIT", status, now)
		want := "Resolves " + formatResolvesAt(cfg, resolvesAt) + " · " + deadlineRelativeTime(now, resolvesAt) + "."
		if got != want {
			t.Fatalf("dropped phrase = %q, want %q", got, want)
		}
	})

	t.Run("no team name still names the event", func(t *testing.T) {
		resolvesAt := now.Add(time.Hour)
		status := waiverStatus{State: AvailabilityOnWaivers, Reason: "kickoff", ResolvesAt: resolvesAt}
		got := waiverResolutionPhrase(cfg, "", status, now)
		if !strings.Contains(got, "Resolves after the player's game ends, at the next waiver run") {
			t.Fatalf("phrase with no team = %q, want the generic event phrasing", got)
		}
	})
}

func TestPlayerWaiverStatusRosteredPlayerNeverOnWaivers(t *testing.T) {
	cfg := DefaultConfig()
	state := PersistedState{Picks: []DraftPick{{Number: 1, TeamID: "team-1", PlayerID: "p-1"}}}
	status := playerWaiverStatus(state, cfg, nil, "p-1", "PIT", time.Now())
	if status.State != AvailabilityRostered {
		t.Fatalf("status = %+v, want ROSTERED", status)
	}
}

// ---------------------------------------------------------------------
// FileClaim / CancelClaim validation matrix (Service layer, exact W-messages)
// ---------------------------------------------------------------------

// waiversFixturePool mirrors playersFixturePool's shape with one
// additional waivers-relevant player: "wv-open", a dropped player still
// inside its clear window at the fixture clock.
func waiversFixturePool() []Player {
	pool := playersFixturePool()
	return append(pool, Player{ID: "wv-open", Name: "Waived Wideout", Position: "WR", NFLTeam: "PIT", Projection: 10})
}

func newWaiversTestService(t *testing.T) (svc *Service, now time.Time) {
	t.Helper()
	// wv-open is drafted onto team-3, then immediately dropped, so it
	// carries real provenance for a drop-based ON WAIVERS fixture (a drop
	// transaction always names a player the dropping team actually owned).
	svc, now = newPlayersTestServiceWithPicks(t, waiversFixturePool(), map[string][]string{"team-3": {"wv-open"}})
	dropTxn := Transaction{
		ID: "txn-seed", Type: "drop", TeamID: "team-3",
		Drops: []TransactionPlayer{{PlayerID: "wv-open", Name: "Waived Wideout", Position: "WR", NFLTeam: "PIT"}},
		At:    now.Add(-time.Hour),
	}
	if err := svc.store.RecordTransaction(dropTxn, 99); err != nil {
		t.Fatal(err)
	}
	return svc, now
}

func TestFileClaimRequiresSignIn(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	svc.demoMode = false
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-2", "wv-open", "", 0)
	want := "Google sign-in is required for league actions"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimUnknownPoolMember(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-2", "ghost", "", 0)
	want := "choose an available player"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimAlreadyRostered(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-2", "other-team-player", "", 0)
	want := "Other Team Player is already on a roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// team-2 is drafted to its 3-player cap in this fixture (like every team
// — CurrentDraftRounds() == 3 under the "tiny" roster shape), so a claim
// with no drop always fails W6 first; tests that need to reach a check
// past the roster-full gate name team-2's one hand-picked player,
// other-team-player, as the drop.
const waiversTeam2DropID = "other-team-player"

func TestFileClaimDuplicateClaim(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.FileClaim(request, "team-2", "wv-open", waiversTeam2DropID, 0); err != nil {
		t.Fatal(err)
	}
	_, err := svc.FileClaim(request, "team-2", "wv-open", waiversTeam2DropID, 0)
	want := "you already hold a claim for Waived Wideout"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimRosterFullRequiresDrop(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-1", "wv-open", "", 0) // team-1 is at cap (3 of 3)
	want := "your roster is full; choose a player to drop for Waived Wideout"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimDropNotOnRoster(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-1", "wv-open", "fa-two", 0)
	want := "that player is not on your roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimDropLocked(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-1", "wv-open", "rb-locked", 0)
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimNoBidOutsideFaabMode(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-2", "wv-open", waiversTeam2DropID, 5)
	want := "bids apply only in faab mode"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimFaabNegativeBid(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	svc.cfg.Waivers.Mode = "faab"
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-2", "wv-open", waiversTeam2DropID, -5)
	want := "bids must be between 0 and 100"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimFaabOverBudget(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	svc.cfg.Waivers.Mode = "faab"
	svc.cfg.Waivers.FAABBudget = 100
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-2", "wv-open", waiversTeam2DropID, 150)
	want := "your bid exceeds your remaining budget (100 FAAB left)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestFileClaimSucceeds(t *testing.T) {
	svc, now := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	message, err := svc.FileClaim(request, "team-2", "wv-open", waiversTeam2DropID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Claim filed for Waived Wideout; Other Team Player will drop if it wins."; message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
	state := svc.store.Snapshot()
	if len(state.WaiverClaims) != 1 {
		t.Fatalf("WaiverClaims = %+v, want exactly one open claim", state.WaiverClaims)
	}
	claim := state.WaiverClaims[0]
	if claim.TeamID != "team-2" || claim.AddID != "wv-open" || claim.DropID != waiversTeam2DropID || claim.Priority != 1 {
		t.Fatalf("claim = %+v, want team-2/wv-open/%s/priority 1", claim, waiversTeam2DropID)
	}
	if claim.FiledAt.IsZero() || claim.FiledAt.After(now.Add(time.Second)) {
		t.Fatalf("FiledAt = %v, want ~%v", claim.FiledAt, now)
	}
}

func TestCancelClaimRemovesOnlyTheOwnTeamsClaim(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.FileClaim(request, "team-2", "wv-open", waiversTeam2DropID, 0); err != nil {
		t.Fatal(err)
	}
	claimID := svc.store.Snapshot().WaiverClaims[0].ID

	// A different team's cancel attempt on the same claim ID is a no-op.
	if _, err := svc.CancelClaim(request, "team-1", claimID); err != nil {
		t.Fatal(err)
	}
	if len(svc.store.Snapshot().WaiverClaims) != 1 {
		t.Fatal("another team's cancel must not remove this claim")
	}

	if _, err := svc.CancelClaim(request, "team-2", claimID); err != nil {
		t.Fatal(err)
	}
	if len(svc.store.Snapshot().WaiverClaims) != 0 {
		t.Fatal("the owning team's cancel must remove the claim")
	}
}

func TestCancelClaimUnknownIDIsNoOp(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.CancelClaim(request, "team-2", "clm-ghost"); err != nil {
		t.Fatalf("canceling an unknown claim must be a harmless no-op, got: %v", err)
	}
}

// ---------------------------------------------------------------------
// AddPlayer W12/W13 — kickoff-locked / on-waivers routes to a claim
// ---------------------------------------------------------------------

func TestAddPlayerOnWaiversRoutesToClaimMessage(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-2", "wv-open", "", "")
	if err == nil {
		t.Fatal("expected an error routing to a claim")
	}
	if got := err.Error(); got[:len("Waived Wideout is on waivers; claims resolve ")] != "Waived Wideout is on waivers; claims resolve " {
		t.Fatalf("err = %q, want the W12 prefix", got)
	}
}

func TestAddPlayerKickoffLockedRoutesToClaimMessage(t *testing.T) {
	svc, now := newPlayersTestServiceWithPool(t, waiversFixturePool())
	// rb-locked's game (TB) kicked off an hour before `now` per the
	// players fixture, but rb-locked itself is rostered (team-1) — use a
	// FREE, kickoff-locked player instead: register a game for wv-open's
	// team that has kicked off and is not final.
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "g-wv", Week: 1, Kickoff: now.Add(-time.Minute), Away: "PIT", Home: "NYJ"}}
	})
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-2", "wv-open", "", "")
	if err == nil {
		t.Fatal("expected an error routing to a claim")
	}
	want := "Waived Wideout locked at kickoff; file a claim — it resolves "
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("err = %q, want the W13 prefix %q", got, want)
	}
}

// ---------------------------------------------------------------------
// Store.ProcessWaivers — the processing run (roster-ops spec section 5.4)
// ---------------------------------------------------------------------

// processWaiversFixturePool supplies the add/drop player identities the
// ProcessWaivers tests contest.
func processWaiversFixturePool() map[string]Player {
	return map[string]Player{
		"wv-1": {ID: "wv-1", Name: "Wire One", Position: "RB", NFLTeam: "PIT"},
		"wv-2": {ID: "wv-2", Name: "Wire Two", Position: "WR", NFLTeam: "PIT"},
		"d-a":  {ID: "d-a", Name: "Drop A", Position: "RB", NFLTeam: "PIT"},
		"d-b":  {ID: "d-b", Name: "Drop B", Position: "WR", NFLTeam: "PIT"},
	}
}

// processWaiversFixtureStore builds a Store with an 8-team draft order
// (pre-week-1: base waiverOrder is the exact reverse, team-8 claims
// first) and no schedule, so waiverOrder resolution stays on the section
// 5.2.1 W==0 path throughout — the simplest deterministic base to test
// the processing walk against.
func processWaiversFixtureStore(t *testing.T) *Store {
	t.Helper()
	store := newTestStore(t)
	if err := store.SetDraftOrder(defaultTeamIDs()); err != nil {
		t.Fatal(err)
	}
	return store
}

func processWaiversCfg() Config {
	cfg := DefaultConfig()
	cfg.Season = 2026
	return cfg
}

// draftPlayerToTeam drafts playerID onto teamID by walking sequential
// picks from the store's current pick count up to (and including)
// teamID's next on-the-clock slot under the store's DraftOrder, filling
// every intervening pick with a throwaway filler ID. MakePick enforces
// strict snake-draft order, so tests that need one specific team to own
// one specific player (for a drop-provenance or roster-cap fixture) must
// walk the order rather than call MakePick directly for an off-clock team.
func draftPlayerToTeam(t *testing.T, store *Store, teamID, playerID string, now time.Time) {
	t.Helper()
	order := defaultTeamIDs()
	for {
		number := len(store.Snapshot().Picks) + 1
		onClock := teamOnClock(order, number)
		id := playerID
		if onClock != teamID {
			id = fmt.Sprintf("filler-%d", number)
		}
		if _, err := store.MakePick(onClock, id, "manager", now, time.Time{}); err != nil {
			t.Fatalf("pick %d (%s, %s): %v", number, onClock, id, err)
		}
		if onClock == teamID {
			return
		}
	}
}

// ---------------------------------------------------------------------
// F4: open-claim rate limiting and the per-run standings hoist
// ---------------------------------------------------------------------

// TestFileClaimCapsOpenClaimsPerTeam pins F4 (roster-ops audit probe 4):
// one team filing an unbounded burst of open claims — the probe filed
// 500 in 1.36s with no rejection. maxOpenClaimsPerTeam bounds this at the
// roster size, enforced by Store.fileClaimWithAuthority even through the
// plain FileClaim path the probe used directly (no pool, no service
// layer, no rosterCap parameter available to lean on).
func TestFileClaimCapsOpenClaimsPerTeam(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	limit := maxOpenClaimsPerTeam()
	filed := 0
	var lastErr error
	for i := 0; i < limit+5; i++ {
		id := fmt.Sprintf("clm-%d", i)
		add := fmt.Sprintf("ghost-%d", i)
		if err := store.FileClaim(WaiverClaim{ID: id, TeamID: "team-1", AddID: add, FiledAt: now}); err != nil {
			lastErr = err
			break
		}
		filed++
	}
	if filed != limit {
		t.Fatalf("filed = %d claims before rejection, want exactly %d (maxOpenClaimsPerTeam)", filed, limit)
	}
	if lastErr == nil {
		t.Fatal("filing past the cap must be rejected")
	}
	if got := teamOpenClaimCount(store.Snapshot().WaiverClaims, "team-1"); got != limit {
		t.Fatalf("open claims for team-1 = %d, want %d", got, limit)
	}
}

// TestFileClaimWithAuthorityRateLimitAllowsSixtyRefusesSixtyFirst pins
// finding 4 of the 2026-08-30 review round 2: no test covered the filing
// rate limit at all, and the old ceiling of 20 per rolling hour was
// reachable by legitimate use alone — a full open-claim queue is
// roster-size-many claims (17 under the default gridiron-house preset),
// and one cancel/refile pass on top of that already approaches 20 with
// no spam involved. The raised 60/hour ceiling is exercised directly:
// allowWaiverFilingLocked admits a filing while its trailing-hour count
// sits strictly below waiverFilingRateLimit, so exactly 60 filings are
// admitted this rolling hour and the 61st is refused with the documented
// message; the window rolling then re-admits a slot once the earliest
// entry ages out, and the filing log stays pruned to the trailing window
// instead of growing without bound.
func TestFileClaimWithAuthorityRateLimitAllowsSixtyRefusesSixtyFirst(t *testing.T) {
	store := processWaiversFixtureStore(t)
	pool := processWaiversFixturePool()
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	openCap := maxOpenClaimsPerTeam()

	fileOne := func(n int, at time.Time) error {
		id := fmt.Sprintf("clm-rate-%d", n)
		add := fmt.Sprintf("ghost-rate-%d", n)
		pool[add] = Player{ID: add, Name: fmt.Sprintf("Ghost %d", n), Position: "RB", NFLTeam: "PIT"}
		claim := WaiverClaim{ID: id, TeamID: "team-1", AddID: add, FiledAt: at}
		return store.FileClaimWithAuthority(claim, nil, pool, at)
	}
	cancelOldest := func(n int) {
		id := fmt.Sprintf("clm-rate-%d", n)
		if err := store.CancelClaim("team-1", id); err != nil {
			t.Fatalf("cancel %s: %v", id, err)
		}
	}

	// Fill the open-claim cap first — a level, not a rate, and irrelevant
	// to this test beyond needing headroom to cancel/refile through.
	filed := 0
	for filed < openCap {
		if err := fileOne(filed, now); err != nil {
			t.Fatalf("fill claim %d: %v", filed, err)
		}
		filed++
	}
	// Cancel the oldest open claim and refile a fresh one, over and over,
	// up through the 60th successful filing this rolling hour — ordinary
	// cancel/refile use, never itself a level breach.
	for filed < waiverFilingRateLimit {
		cancelOldest(filed - openCap)
		if err := fileOne(filed, now); err != nil {
			t.Fatalf("refile %d (successful filings so far %d): %v", filed, filed, err)
		}
		filed++
	}

	// The 61st filing attempt inside the same rolling hour must be
	// refused with the documented message, and it must not touch the
	// open-claim set.
	before := store.Snapshot().WaiverClaims
	cancelOldest(filed - openCap)
	if err := fileOne(filed, now.Add(time.Minute)); err == nil {
		t.Fatal("the 61st filing within the rolling hour must be refused")
	} else if want := "you filed too many claims recently"; !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q, want it to contain %q", err.Error(), want)
	}
	if got := len(store.Snapshot().WaiverClaims); got != len(before)-1 {
		t.Fatalf("open claims after the refused 61st = %d, want %d (the cancel stands, the refile is refused)", got, len(before)-1)
	}

	// The window rolls: once the earliest of the 60 admitted filings falls
	// outside waiverFilingRateWindow, its slot re-admits a new filing.
	later := now.Add(waiverFilingRateWindow + time.Minute)
	if err := fileOne(filed, later); err != nil {
		t.Fatalf("filing after the window rolled past the earliest entry: %v", err)
	}

	// Pruning keeps the filing log bounded to the trailing window, never
	// growing without bound across every filing this store has ever
	// admitted.
	if got := len(store.filingLog["team-1"]); got > waiverFilingRateLimit {
		t.Fatalf("filingLog[team-1] length = %d, want pruned to at most %d", got, waiverFilingRateLimit)
	}
}

// TestFileClaimRejectsPlayerNotInPool pins F4's other closure: a claim
// naming a player this run's pool does not recognize at all is neither
// ON WAIVERS nor FREE AGENT (playerWaiverStatus's exhaustive three-state
// table needs pool membership just to classify a player), so
// FileClaimWithAuthority must reject it once a pool is available —
// closing the "ghost player" half of probe 4's unbounded-filing case.
func TestFileClaimRejectsPlayerNotInPool(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	pool := processWaiversFixturePool()
	claim := WaiverClaim{ID: "clm-ghost", TeamID: "team-1", AddID: "ghost-player", FiledAt: now}
	if err := store.FileClaimWithAuthority(claim, nil, pool, now); err == nil {
		t.Fatal("a claim naming a player outside the pool must be rejected")
	}
	if len(store.Snapshot().WaiverClaims) != 0 {
		t.Fatal("no claim should have been filed")
	}
}

// TestProcessWaiversComputesStandingsOnce pins F4's hoist: one run must
// call performanceBaseOrder (the standings/weekly-rank computation)
// exactly once, no matter how many claims it resolves in the same run —
// not once per resolved claim, the pre-fix cost the audit measured (500
// claims cloning state and recomputing waiverOrder under Store.mu).
func TestProcessWaiversComputesStandingsOnce(t *testing.T) {
	store := processWaiversFixtureStore(t)
	if err := store.SetSchedule(closedWeek1Schedule()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	claims := []WaiverClaim{
		{ID: "clm-a", TeamID: "team-1", AddID: "wv-1", FiledAt: now.Add(-4 * time.Hour)},
		{ID: "clm-b", TeamID: "team-2", AddID: "wv-1", FiledAt: now.Add(-3 * time.Hour)},
		{ID: "clm-c", TeamID: "team-3", AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-d", TeamID: "team-4", AddID: "wv-2", FiledAt: now.Add(-time.Hour)},
	}
	for _, c := range claims {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	// F7 (2026-08-30 review): performanceBaseOrderCalls is nil in
	// production; wire the test seam only for this call, and clear it
	// afterward so no other test observes it set.
	var calls int64
	setPerformanceBaseOrderCalls(func() { atomic.AddInt64(&calls, 1) })
	defer setPerformanceBaseOrderCalls(nil)
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %+v, want four outcomes", results)
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("performanceBaseOrder calls in one run = %d, want exactly 1 (hoisted out of the per-claim loop)", got)
	}
}

func TestProcessWaiversSimpleWin(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if err := store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "won" {
		t.Fatalf("results = %+v, want one win", results)
	}
	if results[0].Position != 2 { // base order [8,7,6,5,4,3,2,1]: team-7 is position 2
		t.Fatalf("Position = %d, want 2", results[0].Position)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 0 {
		t.Fatalf("WaiverClaims after processing = %+v, want empty", state.WaiverClaims)
	}
	if len(state.Transactions) != 1 || state.Transactions[0].Type != "claim" {
		t.Fatalf("Transactions = %+v, want one claim record", state.Transactions)
	}
	txn := state.Transactions[0]
	if txn.TeamID != "team-7" || len(txn.Adds) != 1 || txn.Adds[0].PlayerID != "wv-1" || txn.Position != 2 {
		t.Fatalf("txn = %+v, want team-7/wv-1/Position 2", txn)
	}
	if !state.WaiversProcessedThrough.Equal(now.UTC()) {
		t.Fatalf("WaiversProcessedThrough = %v, want %v", state.WaiversProcessedThrough, now)
	}
	// End to end: currentRosters must reflect the win by replay.
	if owner := rosterOwner(currentRosters(state)); owner["wv-1"] != "team-7" {
		t.Fatalf("currentRosters must show team-7 owning wv-1 after the claim transaction replays, got owner %q", owner["wv-1"])
	}
}

func TestProcessWaiversContestedClaimOrdersByPriority(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	// team-2 (position 7, worse priority) files first; team-7 (position 2,
	// better priority) files second — filing order must not matter.
	if err := store.FileClaim(WaiverClaim{ID: "clm-2", TeamID: "team-2", AddID: "wv-1", FiledAt: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.FileClaim(WaiverClaim{ID: "clm-7", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	byTeam := map[string]WaiverResult{}
	for _, r := range results {
		byTeam[r.Claim.TeamID] = r
	}
	if byTeam["team-7"].Outcome != "won" {
		t.Fatalf("team-7 (better priority) must win, got %+v", byTeam["team-7"])
	}
	if byTeam["team-2"].Outcome != "beaten" || byTeam["team-2"].WinningTeamID != "team-7" {
		t.Fatalf("team-2 must be beaten by team-7, got %+v", byTeam["team-2"])
	}
}

// TestProcessWaiversReDerivesOrderAfterEachWin pins section 5.4 step 3's
// named property: a win's back-of-order penalty applies to every LATER
// contested claim in the same run, even one the winning team itself
// filed. team-7 (position 2) wins wv-1 first and is demoted to the back;
// its second claim, on wv-2, then loses to team-6 (position 3 originally,
// now ahead of team-7's demoted position) — the exact reverse of what a
// naive one-time sort would produce.
func TestProcessWaiversReDerivesOrderAfterEachWin(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	claims := []WaiverClaim{
		{ID: "clm-7a", TeamID: "team-7", AddID: "wv-1", Priority: 1, FiledAt: now.Add(-4 * time.Hour)},
		{ID: "clm-7b", TeamID: "team-7", AddID: "wv-2", Priority: 2, FiledAt: now.Add(-3 * time.Hour)},
		{ID: "clm-6", TeamID: "team-6", AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
	}
	for _, c := range claims {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3", results)
	}
	byClaimID := map[string]WaiverResult{}
	for _, r := range results {
		byClaimID[r.Claim.ID] = r
	}
	if byClaimID["clm-7a"].Outcome != "won" {
		t.Fatalf("team-7's wv-1 claim must win (best original priority), got %+v", byClaimID["clm-7a"])
	}
	if byClaimID["clm-6"].Outcome != "won" {
		t.Fatalf("team-6's wv-2 claim must win (team-7 already demoted by its own win), got %+v", byClaimID["clm-6"])
	}
	if byClaimID["clm-7b"].Outcome != "beaten" || byClaimID["clm-7b"].WinningTeamID != "team-6" {
		t.Fatalf("team-7's wv-2 claim must lose to team-6 after team-7's own demotion, got %+v", byClaimID["clm-7b"])
	}
}

// TestProcessWaiversDeterminismAcrossRepeatedRuns pins test-plan item 15:
// a fixed state and instant produce identical winners and transactions no
// matter how many times the run is repeated from that same starting
// point.
func TestProcessWaiversDeterminismAcrossRepeatedRuns(t *testing.T) {
	build := func(t *testing.T) *Store {
		store := processWaiversFixtureStore(t)
		now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
		claims := []WaiverClaim{
			{ID: "clm-7a", TeamID: "team-7", AddID: "wv-1", Priority: 1, FiledAt: now.Add(-4 * time.Hour)},
			{ID: "clm-7b", TeamID: "team-7", AddID: "wv-2", Priority: 2, FiledAt: now.Add(-3 * time.Hour)},
			{ID: "clm-6", TeamID: "team-6", AddID: "wv-2", FiledAt: now.Add(-2 * time.Hour)},
			{ID: "clm-2", TeamID: "team-2", AddID: "wv-1", FiledAt: now.Add(-time.Hour)},
		}
		for _, c := range claims {
			if err := store.FileClaim(c); err != nil {
				t.Fatal(err)
			}
		}
		return store
	}
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	cfg := processWaiversCfg()
	pool := processWaiversFixturePool()

	var runs [][]WaiverResult
	for i := 0; i < 3; i++ {
		store := build(t)
		results, err := store.ProcessWaivers(now, cfg, nil, pool, 99)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, results)
	}
	for i := 1; i < len(runs); i++ {
		if len(runs[i]) != len(runs[0]) {
			t.Fatalf("run %d produced %d results, want %d", i, len(runs[i]), len(runs[0]))
		}
		for _, r := range runs[0] {
			match := false
			for _, other := range runs[i] {
				if other.Claim.ID == r.Claim.ID && other.Outcome == r.Outcome && other.WinningTeamID == r.WinningTeamID {
					match = true
					break
				}
			}
			if !match {
				t.Fatalf("run %d disagrees with run 0 on claim %s: %+v vs run0 %+v", i, r.Claim.ID, runs[i], runs[0])
			}
		}
	}
}

// TestProcessWaiversDropNotOwnedFails re-validates a claim whose named
// drop player was legitimately owned at filing time but no longer is by
// processing time — the exact race Store.ProcessWaivers's re-validation
// guards against (a claim naming an already-owned drop player can never
// be filed at all; Store.FileClaim blocks that at the source).
func TestProcessWaiversDropNotOwnedFails(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	draftPlayerToTeam(t, store, "team-7", "d-a", now.Add(-3*time.Hour))
	if err := store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", DropID: "d-a", FiledAt: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// team-7 trades d-a away (simulated as a drop) after filing but
	// before the run — the claim's drop is no longer theirs to give.
	traded := Transaction{ID: "txn-gone", Type: "drop", TeamID: "team-7",
		Drops: []TransactionPlayer{{PlayerID: "d-a"}}, At: now.Add(-time.Hour)}
	if err := store.RecordTransaction(traded, 99); err != nil {
		t.Fatal(err)
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "failed" || results[0].Reason != lineupNotOnRosterMessage {
		t.Fatalf("results = %+v, want one failure with %q", results, lineupNotOnRosterMessage)
	}
}

// TestProcessWaiversLimitsBlocks pins the optional Limits knob's
// waiver-resolution enforcement point: a won claim that would push a
// position over its configured cap resolves "failed" with the shared
// limitMessage pattern, even with roster-cap room to spare.
func TestProcessWaiversLimitsBlocks(t *testing.T) {
	setRosterShape(RosterPreset{Name: "limits-fixture", Slots: map[string]int{"RB": 1}, Bench: 5, Limits: map[string]int{"RB": 1}})
	t.Cleanup(clearRosterShape)
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	draftPlayerToTeam(t, store, "team-7", "d-a", now.Add(-time.Hour)) // d-a is an RB, already at the RB:1 limit
	if err := store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: now}); err != nil {
		t.Fatal(err)
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	want := limitMessage("RB", 1)
	if len(results) != 1 || results[0].Outcome != "failed" || results[0].Reason != want {
		t.Fatalf("results = %+v, want one failure with %q", results, want)
	}
}

func TestProcessWaiversRosterFullWithNoDropFails(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	draftPlayerToTeam(t, store, "team-7", "d-a", now.Add(-time.Hour))
	if err := store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: now}); err != nil {
		t.Fatal(err)
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 1) // cap 1: team-7 already at cap
	if err != nil {
		t.Fatal(err)
	}
	want := "your roster is full; choose a player to drop for Wire One"
	if len(results) != 1 || results[0].Outcome != "failed" || results[0].Reason != want {
		t.Fatalf("results = %+v, want one failure with %q", results, want)
	}
}

func TestProcessWaiversSameTeamConflictingClaimsSameDrop(t *testing.T) {
	// Test-plan item 16: a team winning claim A invalidates its own claim
	// B that named the same drop player.
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	draftPlayerToTeam(t, store, "team-7", "d-a", now.Add(-time.Hour))
	claims := []WaiverClaim{
		{ID: "clm-a", TeamID: "team-7", AddID: "wv-1", DropID: "d-a", Priority: 1, FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-b", TeamID: "team-7", AddID: "wv-2", DropID: "d-a", Priority: 2, FiledAt: now.Add(-time.Hour)},
	}
	for _, c := range claims {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 2)
	if err != nil {
		t.Fatal(err)
	}
	byClaimID := map[string]WaiverResult{}
	for _, r := range results {
		byClaimID[r.Claim.ID] = r
	}
	if byClaimID["clm-a"].Outcome != "won" {
		t.Fatalf("clm-a (earlier priority) must win, got %+v", byClaimID["clm-a"])
	}
	if byClaimID["clm-b"].Outcome != "failed" {
		t.Fatalf("clm-b must fail once d-a is already spent by clm-a, got %+v", byClaimID["clm-b"])
	}
}

func TestProcessWaiversNotYetDueClaimStaysOpen(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	// wv-1 is ON WAIVERS via a drop two hours ago; clear_days=2 means it
	// will not clear for two more days — this claim must not resolve yet.
	// A drop transaction always names a player the dropping team actually
	// owned, so wv-1 is drafted onto team-1 first.
	if _, err := store.MakePick("team-1", "wv-1", "manager", now.Add(-3*time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	dropTxn := Transaction{ID: "txn-seed", Type: "drop", TeamID: "team-1",
		Drops: []TransactionPlayer{{PlayerID: "wv-1"}}, At: now.Add(-2 * time.Hour)}
	if err := store.RecordTransaction(dropTxn, 99); err != nil {
		t.Fatal(err)
	}
	if err := store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: now}); err != nil {
		t.Fatal(err)
	}
	cfg := processWaiversCfg()
	cfg.Waivers.ClearDays = 2
	results, err := store.ProcessWaivers(now, cfg, nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none (claim not yet due)", results)
	}
	if len(store.Snapshot().WaiverClaims) != 1 {
		t.Fatal("the not-yet-due claim must remain open")
	}
}

func TestProcessWaiversFutureFiledClaimStaysOpenDuringHistoricalCatchUp(t *testing.T) {
	store := processWaiversFixtureStore(t)
	runAt := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	filedAt := runAt.Add(time.Hour)
	if err := store.FileClaim(WaiverClaim{ID: "clm-future", TeamID: "team-7", AddID: "wv-1", FiledAt: filedAt}); err != nil {
		t.Fatal(err)
	}

	results, err := store.ProcessWaivers(runAt, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none for a claim filed after the historical run", results)
	}
	state := store.Snapshot()
	if len(state.WaiverClaims) != 1 || state.WaiverClaims[0].ID != "clm-future" {
		t.Fatalf("WaiverClaims after historical catch-up = %+v, want clm-future still open", state.WaiverClaims)
	}
	if len(state.Transactions) != 0 || len(state.WaiverReceipts) != 0 {
		t.Fatalf("historical catch-up created transactions/receipts = %d/%d, want 0/0", len(state.Transactions), len(state.WaiverReceipts))
	}
	if !state.WaiversProcessedThrough.Equal(runAt) {
		t.Fatalf("WaiversProcessedThrough = %v, want %v", state.WaiversProcessedThrough, runAt)
	}

	firstEligible := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	results, err = store.ProcessWaivers(firstEligible, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "won" {
		t.Fatalf("results at first eligible cycle = %+v, want one win", results)
	}
	state = store.Snapshot()
	if len(state.WaiverClaims) != 0 || len(state.Transactions) != 1 || len(state.WaiverReceipts) != 1 {
		t.Fatalf("state after first eligible cycle = claims %d, transactions %d, receipts %d; want 0/1/1", len(state.WaiverClaims), len(state.Transactions), len(state.WaiverReceipts))
	}
	if got := state.WaiverReceipts[0]; got.FiledAt.After(got.ResolvedAt) {
		t.Fatalf("receipt has ResolvedAt before FiledAt: filed=%v resolved=%v", got.FiledAt, got.ResolvedAt)
	}
}

func TestProcessWaiversFAABModeBidDescendingWithTieBreak(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	// team-2 (worse waiverOrder position) outbids team-7 (better
	// position) — the higher bid must win regardless of order position.
	if err := store.FileClaim(WaiverClaim{ID: "clm-7", TeamID: "team-7", AddID: "wv-1", Bid: 10, FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.FileClaim(WaiverClaim{ID: "clm-2", TeamID: "team-2", AddID: "wv-1", Bid: 50, FiledAt: now}); err != nil {
		t.Fatal(err)
	}
	cfg := processWaiversCfg()
	cfg.Waivers.Mode = "faab"
	results, err := store.ProcessWaivers(now, cfg, nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	byClaimID := map[string]WaiverResult{}
	for _, r := range results {
		byClaimID[r.Claim.ID] = r
	}
	if byClaimID["clm-2"].Outcome != "won" || byClaimID["clm-2"].WinningBid != 50 {
		t.Fatalf("clm-2 (higher bid) must win, got %+v", byClaimID["clm-2"])
	}
	if byClaimID["clm-7"].Outcome != "beaten" {
		t.Fatalf("clm-7 (lower bid) must be beaten, got %+v", byClaimID["clm-7"])
	}
	txn := store.Snapshot().Transactions[0]
	if txn.Bid != 50 {
		t.Fatalf("winning txn.Bid = %d, want 50", txn.Bid)
	}
}

func TestProcessWaiversFAABBudgetExceededAtProcessingFails(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	// Two claims by team-7 together exceed its budget; the second (by
	// waiverOrder-position tie-break, since bids are equal) must fail at
	// processing even though each was individually valid at filing time.
	if err := store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", Bid: 80, FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.FileClaim(WaiverClaim{ID: "clm-2", TeamID: "team-7", AddID: "wv-2", Bid: 80, FiledAt: now}); err != nil {
		t.Fatal(err)
	}
	cfg := processWaiversCfg()
	cfg.Waivers.Mode = "faab"
	cfg.Waivers.FAABBudget = 100
	results, err := store.ProcessWaivers(now, cfg, nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	won, failed := 0, 0
	for _, r := range results {
		switch r.Outcome {
		case "won":
			won++
		case "failed":
			failed++
			want := "your bid exceeds your remaining budget (20 FAAB left)"
			if r.Reason != want {
				t.Fatalf("failure reason = %q, want %q", r.Reason, want)
			}
		}
	}
	if won != 1 || failed != 1 {
		t.Fatalf("results = %+v, want exactly one win and one budget failure", results)
	}
}

// TestProcessWaiversFAABFallsToNextValidBidInSameRun covers the roster-ops
// audit's missing FAAB test: when the top bidder for a contested player
// is over its own remaining budget, the claim falls to the next valid
// bid in the SAME run — the loop's re-derivation after each resolution
// (pickNextClaim over the whole remaining set, not scoped to one player)
// must reach the second-highest bidder without waiting for a later run.
func TestProcessWaiversFAABFallsToNextValidBidInSameRun(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for _, c := range []WaiverClaim{
		{ID: "clm-over", TeamID: "team-7", AddID: "wv-1", Bid: 90, FiledAt: now.Add(-3 * time.Hour)},
		{ID: "clm-valid", TeamID: "team-2", AddID: "wv-1", Bid: 40, FiledAt: now.Add(-2 * time.Hour)},
		{ID: "clm-low", TeamID: "team-6", AddID: "wv-1", Bid: 10, FiledAt: now.Add(-time.Hour)},
	} {
		if err := store.FileClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	cfg := processWaiversCfg()
	cfg.Waivers.Mode = "faab"
	cfg.Waivers.FAABBudget = 50 // team-7's bid (90) exceeds its own budget
	results, err := store.ProcessWaivers(now, cfg, nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v, want three outcomes", results)
	}
	byClaimID := map[string]WaiverResult{}
	for _, r := range results {
		byClaimID[r.Claim.ID] = r
	}
	if got := byClaimID["clm-over"]; got.Outcome != "failed" {
		t.Fatalf("over-budget top bid = %+v, want failed", got)
	}
	if got := byClaimID["clm-valid"]; got.Outcome != "won" || got.WinningBid != 40 {
		t.Fatalf("next valid bid = %+v, want won at 40 in the same run", got)
	}
	if got := byClaimID["clm-low"]; got.Outcome != "beaten" || got.WinningTeamID != "team-2" {
		t.Fatalf("lowest bid = %+v, want beaten by team-2", got)
	}
	state := store.Snapshot()
	if len(state.Transactions) != 1 || state.Transactions[0].TeamID != "team-2" || state.Transactions[0].Bid != 40 {
		t.Fatalf("Transactions = %+v, want one claim txn for team-2 at bid 40", state.Transactions)
	}
}

func TestProcessWaiversZeroClaimsStillBaselinesProcessedThrough(t *testing.T) {
	store := processWaiversFixtureStore(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	results, err := store.ProcessWaivers(now, processWaiversCfg(), nil, processWaiversFixturePool(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none", results)
	}
	if !store.Snapshot().WaiversProcessedThrough.Equal(now.UTC()) {
		t.Fatal("WaiversProcessedThrough must advance even with zero claims")
	}
}

package league

import (
	"bytes"
	"fmt"
	"net/http"
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
	order := waiverOrder(state, DefaultConfig())
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
	order := waiverOrder(state, DefaultConfig())
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
	order := waiverOrder(state, cfg)
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
	order := waiverOrder(PersistedState{Schedule: &sch}, cfg)
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
	order := waiverOrder(PersistedState{Schedule: &sch}, cfg)
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
	transactions := []Transaction{
		{Type: "claim", TeamID: "team-1", Week: 2, At: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)},
	}
	order := applyInPeriodPenalties(base, transactions, 1) // week (W) == 1; claim's Week (2) > W
	want := []string{"team-2", "team-3", "team-4", "team-1"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("order = %v, want %v (team-1 moved to the back after its win)", order, want)
		}
	}
}

func TestApplyInPeriodPenaltyExpiresAtNextRecompute(t *testing.T) {
	base := []string{"team-1", "team-2", "team-3", "team-4"}
	// The claim's Week (2) no longer exceeds the new W (2) once the
	// period advances — the penalty set empties automatically.
	transactions := []Transaction{
		{Type: "claim", TeamID: "team-1", Week: 2, At: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)},
	}
	order := applyInPeriodPenalties(base, transactions, 2)
	for i, id := range base {
		if order[i] != id {
			t.Fatalf("order = %v, want the untouched base %v (penalty expired)", order, base)
		}
	}
}

func TestApplyInPeriodPenaltyMultipleWinsReplayInAtOrder(t *testing.T) {
	base := []string{"team-1", "team-2", "team-3", "team-4"}
	transactions := []Transaction{
		{Type: "claim", TeamID: "team-3", Week: 2, At: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)},
		{Type: "claim", TeamID: "team-1", Week: 2, At: time.Date(2026, 9, 20, 9, 1, 0, 0, time.UTC)},
	}
	order := applyInPeriodPenalties(base, transactions, 1)
	want := []string{"team-2", "team-4", "team-3", "team-1"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("order = %v, want %v (both wins replayed in At order)", order, want)
		}
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
	want := "your roster is full; choose a player to drop"
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
	want := "your bid exceeds your remaining budget ($100 left)"
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
	_, err := svc.AddPlayer(request, "team-2", "wv-open", "")
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
	_, err := svc.AddPlayer(request, "team-2", "wv-open", "")
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
	want := "your roster is full; choose a player to drop"
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
			want := "your bid exceeds your remaining budget ($20 left)"
			if r.Reason != want {
				t.Fatalf("failure reason = %q, want %q", r.Reason, want)
			}
		}
	}
	if won != 1 || failed != 1 {
		t.Fatalf("results = %+v, want exactly one win and one budget failure", results)
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

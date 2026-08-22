package league

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gridiron-2000/internal/notify"
)

// ---------------------------------------------------------------------
// tradeVetoThreshold
// ---------------------------------------------------------------------

func TestTradeVetoThreshold(t *testing.T) {
	cases := []struct {
		seats int
		want  int
	}{
		{seats: 8, want: 3}, // the reference league: "3 of 6" non-party seats
		{seats: 6, want: 2},
		{seats: 4, want: 1},
		{seats: 2, want: 0},
	}
	for _, c := range cases {
		if got := tradeVetoThreshold(c.seats); got != c.want {
			t.Errorf("tradeVetoThreshold(%d) = %d, want %d", c.seats, got, c.want)
		}
	}
}

func TestTradesDataRequiresSeatAndOnlyListsManagedPartners(t *testing.T) {
	seatless := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/trades?counterparty=team-2", nil)
	seatlessData := seatless.TradesData(request)
	if seatlessData["can_edit"] != false || seatlessData["compose_active"] != false {
		t.Fatalf("seatless trade controls = can_edit:%v compose_active:%v", seatlessData["can_edit"], seatlessData["compose_active"])
	}

	service := newTestService(t, true)
	first, _, err := service.store.AssignMember("one@example.com", "One")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.store.AssignMember("two@example.com", "Two")
	if err != nil {
		t.Fatal(err)
	}
	if first.TeamID != "team-1" || second.TeamID != "team-2" {
		t.Fatalf("fixture seats = %q and %q, want team-1 and team-2", first.TeamID, second.TeamID)
	}

	managedRequest, _ := http.NewRequest(http.MethodGet, "/trades?counterparty=team-2", nil)
	managedData := service.TradesData(managedRequest)
	partners, ok := managedData["counterparties"].([]TradeCounterparty)
	if !ok || len(partners) != 1 || partners[0].ID != "team-2" {
		t.Fatalf("managed partners = %#v, want only team-2", managedData["counterparties"])
	}
	if managedData["counterparties_empty"] != false || managedData["compose_active"] != true {
		t.Fatalf("managed compose state = empty:%v active:%v", managedData["counterparties_empty"], managedData["compose_active"])
	}

	staleRequest, _ := http.NewRequest(http.MethodGet, "/trades?counterparty=team-3", nil)
	staleData := service.TradesData(staleRequest)
	if staleData["compose_active"] != false || staleData["compose_counterparty_id"] != "" {
		t.Fatalf("unmanaged counterparty rendered as active: id=%v active=%v", staleData["compose_counterparty_id"], staleData["compose_active"])
	}
}

// ---------------------------------------------------------------------
// validateTradeAssets — T4-T8, T12, direct pure-function fixtures
// ---------------------------------------------------------------------

func tradeAssetsFixtureState() PersistedState {
	return PersistedState{
		Transactions: []Transaction{
			{Type: "add", TeamID: "team-1", Adds: []TransactionPlayer{
				{PlayerID: "t1-a"}, {PlayerID: "t1-b"}, {PlayerID: "t1-locked"},
			}},
			{Type: "add", TeamID: "team-2", Adds: []TransactionPlayer{
				{PlayerID: "t2-a"}, {PlayerID: "t2-b"}, {PlayerID: "t2-c"},
			}},
		},
	}
}

func tradeAssetsFixturePool() map[string]Player {
	return map[string]Player{
		"t1-a":      {ID: "t1-a", Name: "Team1 A", Position: "RB", NFLTeam: "PIT"},
		"t1-b":      {ID: "t1-b", Name: "Team1 B", Position: "WR", NFLTeam: "PIT"},
		"t1-locked": {ID: "t1-locked", Name: "Team1 Locked", Position: "RB", NFLTeam: "TB"},
		"t2-a":      {ID: "t2-a", Name: "Team2 A", Position: "RB", NFLTeam: "PIT"},
		"t2-b":      {ID: "t2-b", Name: "Team2 B", Position: "WR", NFLTeam: "PIT"},
		"t2-c":      {ID: "t2-c", Name: "Team2 C", Position: "TE", NFLTeam: "PIT"},
	}
}

func tradeAssetsFixtureGames(now time.Time) []GameInfo {
	return []GameInfo{
		// PIT's kickoff sits 48h out — well past every accept-to-execution
		// window this file's Service-level tests drive (up to +25h) — so a
		// PIT player stays unlocked through the whole review window; TB's
		// kickoff already passed, so a TB player (t1-locked) is locked at
		// every instant these tests use, for T6's direct fixture checks.
		{ID: "g-pit", Week: 1, Kickoff: now.Add(48 * time.Hour), Away: "PIT", Home: "NYJ"},
		{ID: "g-tb", Week: 1, Kickoff: now.Add(-time.Hour), Away: "TB", Home: "ATL"},
	}
}

func TestValidateTradeAssetsExactMessages(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	state := tradeAssetsFixtureState()
	pool := tradeAssetsFixturePool()
	games := tradeAssetsFixtureGames(now)
	baseCfg := DefaultConfig()

	cases := []struct {
		name         string
		offer        TradeOffer
		cfg          Config
		starterCount int
		rosterCap    int
		want         string
	}{
		{
			name:  "T12 pick assets rejected",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: []string{"t2-a"}, Picks: []string{"pick-1"}},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "pick trading opens after the first season rollover",
		},
		{
			name:  "T4 give empty",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: nil, Get: []string{"t2-a"}},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "offer at least one player from each side",
		},
		{
			name:  "T4 get empty",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: nil},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "offer at least one player from each side",
		},
		{
			name:  "T5 ownership (give not on from roster)",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t2-a"}, Get: []string{"t2-b"}},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "Team2 A is not on East 1's roster",
		},
		{
			name:  "T5 ownership (get not on to roster)",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: []string{"t1-b"}},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "Team1 B is not on East 2's roster",
		},
		{
			name:  "T6 locked give player",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-locked"}, Get: []string{"t2-a"}},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "Team1 Locked is locked until the week closes",
		},
		{
			name:  "T7 upper bound (from team gains too many)",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: []string{"t2-a", "t2-b", "t2-c"}},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "this trade would leave East 1 with 5 players; rosters must hold 1 to 3",
		},
		{
			name:  "T7 lower bound (from team dips below starters)",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a", "t1-b"}, Get: []string{"t2-a"}},
			cfg:   baseCfg, starterCount: 3, rosterCap: 3,
			want: "this trade would leave East 1 with 2 players; rosters must hold 3 to 3",
		},
		{
			name:         "T8 deadline passed",
			offer:        TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: []string{"t2-a"}},
			cfg:          Config{Trades: TradesBlock{Deadline: now.Add(-time.Hour).Format(time.RFC3339)}, Timezone: "America/New_York"},
			starterCount: 1, rosterCap: 3,
			want: fmt.Sprintf("the trade deadline (%s) has passed", formatResolvesAt(Config{Timezone: "America/New_York"}, now.Add(-time.Hour))),
		},
		{
			name:  "valid offer passes",
			offer: TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: []string{"t2-a"}},
			cfg:   baseCfg, starterCount: 1, rosterCap: 3,
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateTradeAssets(state, c.cfg, games, pool, now, c.offer, c.starterCount, c.rosterCap)
			if c.want == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != c.want {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------
// Zones/Limits at trade execution (roster-ops SK spec)
// ---------------------------------------------------------------------

// TestValidateTradeAssetsIRExcludedFromCapMath pins T7's IR-exclusion
// rule: an IR occupant never counts against the [starterCount, rosterCap]
// bound (SK spec: "placing a player in IR frees a general roster spot").
// team-1 raw-owns 4 players (one, t1-ir, parked on IR) — a 1-for-1 trade
// that keeps team-1 at its true 3-player effective size must pass, even
// though the raw ownership count (4) would otherwise read as already
// over a 3-player cap before the trade even starts.
func TestValidateTradeAssetsIRExcludedFromCapMath(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	state := tradeAssetsFixtureState()
	state.Transactions = append(state.Transactions, Transaction{
		Type: "add", TeamID: "team-1", Adds: []TransactionPlayer{{PlayerID: "t1-ir"}},
	})
	state.RosterZones = map[string]map[string]ZoneAssignment{
		"team-1": {"t1-ir": {Zone: zoneIR, Position: "RB"}},
	}
	pool := tradeAssetsFixturePool()
	pool["t1-ir"] = Player{ID: "t1-ir", Name: "Team1 IR", Position: "RB", NFLTeam: "PIT"}
	games := tradeAssetsFixtureGames(now)

	offer := TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-a"}, Get: []string{"t2-a"}}
	if err := validateTradeAssets(state, DefaultConfig(), games, pool, now, offer, 1, 3); err != nil {
		t.Fatalf("a 1-for-1 trade must pass once the IR occupant is excluded from the cap count: %v", err)
	}
}

// TestValidateTradeAssetsLimitsBlocks pins the optional Limits knob's
// trade-execution enforcement point: an incoming player that would push a
// position over its configured cap fails with the shared limitMessage
// pattern, even though the trade's raw player counts fit comfortably
// within [starterCount, rosterCap].
func TestValidateTradeAssetsLimitsBlocks(t *testing.T) {
	setRosterShape(RosterPreset{Name: "limits-fixture", Slots: map[string]int{"RB": 1}, Bench: 2, Limits: map[string]int{"RB": 2}})
	t.Cleanup(clearRosterShape)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	state := tradeAssetsFixtureState()
	pool := tradeAssetsFixturePool()
	games := tradeAssetsFixtureGames(now)

	// team-1 already owns 2 RBs (t1-a, t1-locked); trading its WR (t1-b)
	// for team-2's RB (t2-a) would make 3, over the RB:2 limit.
	offer := TradeOffer{FromTeamID: "team-1", ToTeamID: "team-2", Give: []string{"t1-b"}, Get: []string{"t2-a"}}
	err := validateTradeAssets(state, DefaultConfig(), games, pool, now, offer, 1, 3)
	want := limitMessage("RB", 2)
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// ---------------------------------------------------------------------
// Service-level fixture: 8 seated teams, tiny roster shape, a completed
// draft giving team-1 and team-2 three players each.
// ---------------------------------------------------------------------

func tradesFixturePoolSlice() []Player {
	byID := tradeAssetsFixturePool()
	out := make([]Player, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	return out
}

// newTradesTestService builds a demo-mode, notify-wired Service: 8 seated
// teams (team-1@example.com..team-8@example.com, in defaultTeams() order)
// unless skipManagerTeamID names one team to leave unmanned (the T3
// fixture); a tiny 3-spot roster shape (1 starter [RB], 2 bench —
// starterCount 1, rosterCap 3); a two-game week-1 schedule (PIT unlocked
// 48h out, TB locked an hour in the past); and a completed draft giving
// team-1 {t1-a, t1-b, t1-locked} and team-2 {t2-a, t2-b, t2-c}, every
// other team filler picks.
func newTradesTestService(t *testing.T, skipManagerTeamID string) (svc *Service, now time.Time) {
	t.Helper()
	now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	clock := now
	svc = &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		feed:     newLiveFeed(nil),
		draftAt:  now.Add(-time.Hour),
		demoMode: true,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		cfg:      DefaultConfig(),
		presence: newPresenceTracker(now.Add(-24 * time.Hour)),
		now:      func() time.Time { return clock },
	}
	svc.store.draftLifecycleBypass = true
	queue := notify.New(func(notify.Message) error { return nil }, func(string, ...any) {})
	svc.SetNotifier(queue, true)

	games := tradeAssetsFixtureGames(now)
	svc.SetScheduleSource(func() []GameInfo { return games })
	pool := tradesFixturePoolSlice()
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "test" })
	setRosterShape(RosterPreset{Name: "trade-fixture", Slots: map[string]int{"RB": 1}, Bench: 2})
	t.Cleanup(clearRosterShape)

	// AssignMember auto-fills the first *unused* team in defaultTeams()
	// order, so it cannot target one specific team by skipping its call —
	// skipping team N mid-loop would just shift every later email onto
	// team N instead. Seat everyone first, then ReleaseSeat the one team
	// that should stay unmanned (T3's fixture) — ReleaseSeat deletes
	// exactly that team's Members row and leaves every other mapping
	// alone.
	for _, team := range defaultTeams() {
		if _, _, err := svc.store.AssignMember(team.ID+"@example.com", team.ID); err != nil {
			t.Fatalf("assign member for %s: %v", team.ID, err)
		}
	}
	if skipManagerTeamID != "" {
		if err := svc.store.ReleaseSeat(skipManagerTeamID); err != nil {
			t.Fatalf("release seat for %s: %v", skipManagerTeamID, err)
		}
	}

	teamPicks := map[string][]string{
		"team-1": {"t1-a", "t1-b", "t1-locked"},
		"team-2": {"t2-a", "t2-b", "t2-c"},
	}
	cursor := map[string]int{}
	total := len(defaultTeams()) * CurrentDraftRounds()
	for number := 1; number <= total; number++ {
		teamID := teamOnClock(nil, number)
		id := fmt.Sprintf("filler-%d", number)
		if ids, ok := teamPicks[teamID]; ok && cursor[teamID] < len(ids) {
			id = ids[cursor[teamID]]
			cursor[teamID]++
		}
		if _, err := svc.store.MakePick(teamID, id, "manager", now, time.Time{}); err != nil {
			t.Fatalf("pick %d (%s, %s): %v", number, teamID, id, err)
		}
	}
	return svc, now
}

// ---------------------------------------------------------------------
// T1, T2, T3, T14 — propose-only validation
// ---------------------------------------------------------------------

func TestProposeTradeRequiresSignIn(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	svc.demoMode = false
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	_, err := svc.ProposeTrade(request, "team-1", "team-2", []string{"t1-a"}, []string{"t2-a"}, nil, "")
	want := "Google sign-in is required for league actions"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestProposeTradeRejectsOwnTeam(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	_, err := svc.ProposeTrade(request, "team-1", "team-1", []string{"t1-a"}, []string{"t1-b"}, nil, "")
	want := "you cannot trade with your own team"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestProposeTradeRejectsUnmannedSeat(t *testing.T) {
	svc, _ := newTradesTestService(t, "team-2")
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	_, err := svc.ProposeTrade(request, "team-1", "team-2", []string{"t1-a"}, []string{"t2-a"}, nil, "")
	want := "that seat has no manager yet"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestProposeTradeRejectsLongNote(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	longNote := ""
	for i := 0; i < 281; i++ {
		longNote += "x"
	}
	_, err := svc.ProposeTrade(request, "team-1", "team-2", []string{"t1-a"}, []string{"t2-a"}, nil, longNote)
	want := "trade notes must be 280 characters or fewer"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestProposeTradeSuccessFiresN15(t *testing.T) {
	svc, now := newTradesTestService(t, "")
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	message, err := svc.ProposeTrade(request, "team-1", "team-2", []string{"t1-a"}, []string{"t2-a"}, nil, "let's talk")
	if err != nil {
		t.Fatalf("ProposeTrade: %v", err)
	}
	if message != "Offer sent to East 2." {
		t.Fatalf("message = %q", message)
	}
	state := svc.store.Snapshot()
	if len(state.TradeOffers) != 1 {
		t.Fatalf("TradeOffers = %d, want 1", len(state.TradeOffers))
	}
	offer := state.TradeOffers[0]
	if offer.Status != TradeStatusOpen || offer.FromTeamID != "team-1" || offer.ToTeamID != "team-2" {
		t.Fatalf("offer = %+v", offer)
	}
	if !offer.CreatedAt.Equal(now.UTC()) {
		t.Fatalf("CreatedAt = %v, want %v", offer.CreatedAt, now)
	}
	key := keyTradeOffer(offer.ID, "team-2@example.com")
	if _, sent := state.SentLog[key]; !sent {
		t.Fatalf("N15 was not recorded under key %q; SentLog = %+v", key, state.SentLog)
	}
}

// ---------------------------------------------------------------------
// Lifecycle matrix: T9, T10, T11, and every transition
// ---------------------------------------------------------------------

func proposeFixtureOffer(t *testing.T, svc *Service) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.ProposeTrade(request, "team-1", "team-2", []string{"t1-a"}, []string{"t2-a"}, nil, ""); err != nil {
		t.Fatalf("propose fixture offer: %v", err)
	}
	state := svc.store.Snapshot()
	return state.TradeOffers[0].ID
}

func TestTradeDeclineWithdrawActorChecks(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)

	if _, err := svc.DeclineTrade(request, "team-3", offerID); err == nil || err.Error() != "only the receiving manager can decline this offer" {
		t.Fatalf("decline by a non-party team: err = %v", err)
	}
	if _, err := svc.WithdrawTrade(request, "team-3", offerID); err == nil || err.Error() != "only the proposing manager can withdraw this offer" {
		t.Fatalf("withdraw by a non-party team: err = %v", err)
	}
	if _, err := svc.WithdrawTrade(request, "team-2", offerID); err == nil || err.Error() != "only the proposing manager can withdraw this offer" {
		t.Fatalf("withdraw by the receiving team: err = %v", err)
	}

	message, err := svc.DeclineTrade(request, "team-2", offerID)
	if err != nil || message != "Offer declined." {
		t.Fatalf("decline by the receiving team: message=%q err=%v", message, err)
	}
	state := svc.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusDeclined {
		t.Fatalf("status = %q, want declined", state.TradeOffers[0].Status)
	}

	// T9: a second decline on the now-declined offer fails.
	if _, err := svc.DeclineTrade(request, "team-2", offerID); err == nil || err.Error() != "this offer is no longer open" {
		t.Fatalf("decline on a resolved offer: err = %v", err)
	}
}

func TestTradeWithdrawSucceeds(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	message, err := svc.WithdrawTrade(request, "team-1", offerID)
	if err != nil || message != "Offer withdrawn." {
		t.Fatalf("message=%q err=%v", message, err)
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusWithdrawn {
		t.Fatalf("status = %q, want withdrawn", got)
	}
}

func TestTradeCounterSwapsSidesAndOpensOneChain(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)

	if _, err := svc.CounterTrade(request, "team-3", offerID, []string{"t2-b"}, []string{"t1-b"}, nil, ""); err == nil ||
		err.Error() != "only the receiving manager can counter this offer" {
		t.Fatalf("counter by a non-party team: err = %v", err)
	}

	message, err := svc.CounterTrade(request, "team-2", offerID, []string{"t2-b"}, []string{"t1-b"}, nil, "counter note")
	if err != nil {
		t.Fatalf("CounterTrade: %v", err)
	}
	if message != "Counter sent to East 1." {
		t.Fatalf("message = %q", message)
	}
	state := svc.store.Snapshot()
	if len(state.TradeOffers) != 2 {
		t.Fatalf("TradeOffers = %d, want 2 (original + counter)", len(state.TradeOffers))
	}
	original := state.TradeOffers[0]
	counter := state.TradeOffers[1]
	if original.Status != TradeStatusCountered {
		t.Fatalf("original status = %q, want countered", original.Status)
	}
	if counter.Status != TradeStatusOpen || counter.ParentID != original.ID {
		t.Fatalf("counter = %+v, want open with ParentID %q", counter, original.ID)
	}
	if counter.FromTeamID != "team-2" || counter.ToTeamID != "team-1" {
		t.Fatalf("counter sides = %+v, want swapped", counter)
	}
	if counter.Give[0] != "t2-b" || counter.Get[0] != "t1-b" {
		t.Fatalf("counter assets = %+v", counter)
	}
}

func TestTradeAcceptActorAndStatusChecks(t *testing.T) {
	svc, now := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)

	if _, err := svc.AcceptTrade(request, "team-1", offerID); err == nil || err.Error() != "only the receiving manager can accept this offer" {
		t.Fatalf("accept by the proposing team: err = %v", err)
	}

	message, err := svc.AcceptTrade(request, "team-2", offerID)
	if err != nil {
		t.Fatalf("AcceptTrade: %v", err)
	}
	if message != "Offer accepted; it now enters review." {
		t.Fatalf("message = %q", message)
	}
	state := svc.store.Snapshot()
	offer := state.TradeOffers[0]
	if offer.Status != TradeStatusAccepted {
		t.Fatalf("status = %q, want accepted", offer.Status)
	}
	if !offer.AcceptedAt.Equal(now.UTC()) {
		t.Fatalf("AcceptedAt = %v, want %v", offer.AcceptedAt, now)
	}

	// T9: accepting an already-accepted offer fails.
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err == nil || err.Error() != "this offer is no longer open" {
		t.Fatalf("accept on a resolved offer: err = %v", err)
	}
}

// ---------------------------------------------------------------------
// Veto mode "none": accept executes immediately, no window
// ---------------------------------------------------------------------

func TestTradeAcceptUnderNoneModeExecutesImmediately(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	svc.cfg.Trades.Veto = "none"
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)

	message, err := svc.AcceptTrade(request, "team-2", offerID)
	if err != nil {
		t.Fatalf("AcceptTrade: %v", err)
	}
	if message != "Trade executed: Team2 A for Team1 A." {
		t.Fatalf("message = %q", message)
	}
	state := svc.store.Snapshot()
	offer := state.TradeOffers[0]
	if offer.Status != TradeStatusExecuted {
		t.Fatalf("status = %q, want executed", offer.Status)
	}
	if len(state.Transactions) != 1 || state.Transactions[0].Type != "trade" {
		t.Fatalf("Transactions = %+v, want one trade record", state.Transactions)
	}
	rosters := currentRosters(state)
	if !containsID(rosters["team-1"], "t2-a") || containsID(rosters["team-1"], "t1-a") {
		t.Fatalf("team-1 roster = %v, want t2-a in, t1-a out", rosters["team-1"])
	}
	if !containsID(rosters["team-2"], "t1-a") || containsID(rosters["team-2"], "t2-a") {
		t.Fatalf("team-2 roster = %v, want t1-a in, t2-a out", rosters["team-2"])
	}
}

func containsID(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------
// T-exec (tick), one-persist atomicity, currentRosters end-to-end, N16
// ---------------------------------------------------------------------

func TestTradeTickExecutesAfterReviewWindowAndFiresN16(t *testing.T) {
	svc, now := newTradesTestService(t, "")
	svc.cfg.Trades.Veto = "commissioner"
	svc.cfg.Trades.ReviewHours = 24
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}

	// Before the window closes, the tick must not execute.
	svc.rosterOpsTick(now.Add(23 * time.Hour))
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusAccepted {
		t.Fatalf("status before the window closes = %q, want accepted", got)
	}

	execAt := now.Add(25 * time.Hour)
	svc.rosterOpsTick(execAt)
	state := svc.store.Snapshot()
	offer := state.TradeOffers[0]
	if offer.Status != TradeStatusExecuted {
		t.Fatalf("status = %q, want executed (FailReason=%q)", offer.Status, offer.FailReason)
	}
	if !offer.ResolvedAt.Equal(execAt.UTC()) {
		t.Fatalf("ResolvedAt = %v, want %v", offer.ResolvedAt, execAt)
	}
	// One-persist atomicity: the trade Transaction and the offer's
	// executed status are both visible in this one snapshot.
	if len(state.Transactions) != 1 || state.Transactions[0].Type != "trade" || state.Transactions[0].OfferID != offerID {
		t.Fatalf("Transactions = %+v", state.Transactions)
	}
	rosters := currentRosters(state)
	if !containsID(rosters["team-2"], "t1-a") {
		t.Fatalf("team-2 roster after execution = %v, want t1-a", rosters["team-2"])
	}

	for _, email := range []string{"team-1@example.com", "team-2@example.com"} {
		key := keyTradeExecuted(offerID, email)
		if _, sent := state.SentLog[key]; !sent {
			t.Fatalf("N16 was not recorded for %s under key %q", email, key)
		}
	}
}

func TestTradeTickExpiresOpenOfferAfterSevenDays(t *testing.T) {
	svc, now := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)

	svc.rosterOpsTick(now.Add(6*24*time.Hour + 23*time.Hour))
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusOpen {
		t.Fatalf("status before 7 days = %q, want open", got)
	}

	svc.rosterOpsTick(now.Add(7*24*time.Hour + time.Minute))
	state := svc.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusExpired {
		t.Fatalf("status = %q, want expired", state.TradeOffers[0].Status)
	}
	_ = offerID
}

func TestTradeTickExpiresAtDeadline(t *testing.T) {
	svc, now := newTradesTestService(t, "")
	svc.cfg.Trades.Deadline = now.Add(2 * time.Hour).Format(time.RFC3339)
	proposeFixtureOffer(t, svc)

	svc.rosterOpsTick(now.Add(time.Hour))
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusOpen {
		t.Fatalf("status before the deadline = %q, want open", got)
	}
	svc.rosterOpsTick(now.Add(3 * time.Hour))
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusExpired {
		t.Fatalf("status after the deadline = %q, want expired", got)
	}
}

// ---------------------------------------------------------------------
// Execution re-validation failure after a mid-window roster change
// ---------------------------------------------------------------------

func TestTradeExecutionFailsClosedAfterMidWindowRosterChange(t *testing.T) {
	svc, now := newTradesTestService(t, "")
	svc.cfg.Trades.Veto = "commissioner"
	svc.cfg.Trades.ReviewHours = 24
	offerID := proposeFixtureOffer(t, svc) // team-1 gives t1-a, gets t2-a from team-2
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}

	// Mid-window: t1-a leaves team-1's roster via an independent drop.
	dropTxn := Transaction{
		ID: "txn-drop-1", Season: 2026, Week: 1, Type: "drop", TeamID: "team-1",
		Drops: []TransactionPlayer{{PlayerID: "t1-a", Name: "Team1 A", Position: "RB", NFLTeam: "PIT"}},
		By:    "manager", At: now.Add(time.Hour),
	}
	if err := svc.store.RecordTransaction(dropTxn, 3); err != nil {
		t.Fatalf("mid-window drop: %v", err)
	}
	transactionsBefore := len(svc.store.Snapshot().Transactions)

	execAt := now.Add(25 * time.Hour)
	svc.rosterOpsTick(execAt)
	state := svc.store.Snapshot()
	offer := state.TradeOffers[0]
	if offer.Status != TradeStatusFailed {
		t.Fatalf("status = %q, want failed", offer.Status)
	}
	want := "Team1 A is not on East 1's roster"
	if offer.FailReason != want {
		t.Fatalf("FailReason = %q, want %q", offer.FailReason, want)
	}
	// No trade Transaction was appended — "never half-applies."
	if len(state.Transactions) != transactionsBefore {
		t.Fatalf("Transactions grew from %d to %d; execution must not append on failure", transactionsBefore, len(state.Transactions))
	}
	rosters := currentRosters(state)
	if containsID(rosters["team-2"], "t1-a") {
		t.Fatal("team-2 must not gain t1-a when execution fails closed")
	}
}

// ---------------------------------------------------------------------
// Veto mode "commissioner": approve, veto, T13
// ---------------------------------------------------------------------

func TestApproveTradeRequiresCommissioner(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	svc.demoMode = false // demo mode grants commissioner unconditionally; flip it off after the (demo-only) accept
	_, err := svc.ApproveTrade(request, offerID)
	want := "commissioner access is required"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestApproveTradeExecutesUnderCommissionerMode(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	message, err := svc.ApproveTrade(request, offerID)
	if err != nil {
		t.Fatalf("ApproveTrade: %v", err)
	}
	if message != "Trade approved and executed: Team2 A for Team1 A." {
		t.Fatalf("message = %q", message)
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusExecuted {
		t.Fatalf("status = %q, want executed", got)
	}
}

func TestApproveTradeRejectedUnderVoteMode(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	svc.cfg.Trades.Veto = "vote"
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ApproveTrade(request, offerID)
	want := "commissioner review is not part of this league's trade policy"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestCommissionerVetoTradeFiresN17(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	message, err := svc.CommissionerVetoTrade(request, offerID)
	if err != nil || message != "Trade vetoed." {
		t.Fatalf("message=%q err=%v", message, err)
	}
	state := svc.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusVetoed {
		t.Fatalf("status = %q, want vetoed", state.TradeOffers[0].Status)
	}
	for _, email := range []string{"team-1@example.com", "team-2@example.com"} {
		key := keyTradeVetoed(offerID, email)
		if _, sent := state.SentLog[key]; !sent {
			t.Fatalf("N17 was not recorded for %s", email)
		}
	}
}

// ---------------------------------------------------------------------
// Veto mode "vote": T13's vote variant, threshold
// ---------------------------------------------------------------------

func TestVoteVetoTradeRejectsPartyMembers(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	svc.cfg.Trades.Veto = "vote"
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.VoteVetoTrade(request, "team-1", offerID)
	want := "only managers outside the trade can vote"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestVoteVetoTradeCrossesThresholdAndFiresN17(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	svc.cfg.Trades.Veto = "vote"
	offerID := proposeFixtureOffer(t, svc)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}

	// 8-team fixture: threshold = tradeVetoThreshold(8) = 3.
	voters := []string{"team-3", "team-4", "team-5"}
	for i, voter := range voters {
		message, err := svc.VoteVetoTrade(request, voter, offerID)
		if err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
		if i < len(voters)-1 {
			if message != "Your veto vote is recorded." {
				t.Fatalf("vote %d message = %q", i, message)
			}
			if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusAccepted {
				t.Fatalf("vote %d: status = %q, want still accepted", i, got)
			}
		} else {
			if message != "Your vote vetoed the trade." {
				t.Fatalf("final vote message = %q", message)
			}
		}
	}
	state := svc.store.Snapshot()
	offer := state.TradeOffers[0]
	if offer.Status != TradeStatusVetoed || len(offer.Vetoes) != 3 {
		t.Fatalf("offer = %+v, want vetoed with 3 votes", offer)
	}
	// A second vote from the same team is a harmless no-op.
	if _, err := svc.VoteVetoTrade(request, "team-3", offerID); err == nil {
		t.Fatal("voting on an already-vetoed offer must fail")
	}
}

func TestVoteVetoRejectedOutsideVoteAndBothMode(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc) // default veto mode: commissioner
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.VoteVetoTrade(request, "team-3", offerID)
	want := "league vote is not part of this league's trade policy"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// ---------------------------------------------------------------------
// Veto mode "both": either authority kills the trade; the store-lock-
// order race between approve and a threshold-crossing veto
// ---------------------------------------------------------------------

func acceptedBothModeOffer(t *testing.T) (svc *Service, offerID string, request *http.Request) {
	t.Helper()
	svc, _ = newTradesTestService(t, "")
	svc.cfg.Trades.Veto = "both"
	offerID = proposeFixtureOffer(t, svc)
	request, _ = http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	return svc, offerID, request
}

// TestBothModeCommissionerApproveExecutes checks that commissioner
// early-approve works under "both" mode when no community veto has
// crossed threshold.
func TestBothModeCommissionerApproveExecutes(t *testing.T) {
	svc, offerID, request := acceptedBothModeOffer(t)
	if _, err := svc.ApproveTrade(request, offerID); err != nil {
		t.Fatalf("ApproveTrade: %v", err)
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusExecuted {
		t.Fatalf("status = %q, want executed", got)
	}
}

// TestBothModeVoteThresholdVetoes checks that a manager-majority veto
// alone kills the trade under "both" mode, with no commissioner action.
func TestBothModeVoteThresholdVetoes(t *testing.T) {
	svc, offerID, _ := acceptedBothModeOffer(t)
	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	for _, voter := range []string{"team-3", "team-4", "team-5"} {
		if _, err := svc.VoteVetoTrade(request, voter, offerID); err != nil {
			t.Fatal(err)
		}
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusVetoed {
		t.Fatalf("status = %q, want vetoed", got)
	}
}

// TestBothModeRaceVetoThenApprove: the community veto crosses threshold
// first; the commissioner's later approve must not override it — "approve
// does not override a completed community veto."
func TestBothModeRaceVetoThenApprove(t *testing.T) {
	svc, offerID, request := acceptedBothModeOffer(t)
	for _, voter := range []string{"team-3", "team-4", "team-5"} {
		if _, err := svc.VoteVetoTrade(request, voter, offerID); err != nil {
			t.Fatal(err)
		}
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusVetoed {
		t.Fatalf("status after 3 votes = %q, want vetoed", got)
	}
	if _, err := svc.ApproveTrade(request, offerID); err == nil {
		t.Fatal("approve after a completed community veto must fail")
	} else if want := "this trade is no longer under review"; err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusVetoed {
		t.Fatalf("status must remain vetoed, got %q", got)
	}
}

// TestBothModeRaceApproveThenVeto: the commissioner's approve persists
// first (executing the trade); a later vote that would have crossed
// threshold must not veto an already-executed trade. Whichever mutation
// persists first wins, deterministically (roster-ops spec section 6.1's
// owner amendment).
func TestBothModeRaceApproveThenVeto(t *testing.T) {
	svc, offerID, request := acceptedBothModeOffer(t)
	if _, err := svc.ApproveTrade(request, offerID); err != nil {
		t.Fatalf("ApproveTrade: %v", err)
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusExecuted {
		t.Fatalf("status after approve = %q, want executed", got)
	}
	for i, voter := range []string{"team-3", "team-4", "team-5"} {
		_, err := svc.VoteVetoTrade(request, voter, offerID)
		if err == nil {
			t.Fatalf("vote %d against an already-executed trade must fail", i)
		}
	}
	if got := svc.store.Snapshot().TradeOffers[0].Status; got != TradeStatusExecuted {
		t.Fatalf("status must remain executed, got %q", got)
	}
}

// ---------------------------------------------------------------------
// Config: veto "both" is now a valid mode
// ---------------------------------------------------------------------

func TestValidateTradesAcceptsBothMode(t *testing.T) {
	if err := validateTrades(TradesBlock{Veto: "both", ReviewHours: 24}); err != nil {
		t.Fatalf("validateTrades(both) = %v, want nil", err)
	}
}

func TestValidateTradesRejectsUnknownMode(t *testing.T) {
	err := validateTrades(TradesBlock{Veto: "coinflip", ReviewHours: 24})
	want := "league config: trades.veto must be one of commissioner, vote, both, none"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// ---------------------------------------------------------------------
// Old-state-file load and cloneState deep copy
// ---------------------------------------------------------------------

// seedTradeOwnership appends two "add" Transactions giving team-1 p-1 and
// team-2 p-2, so a hand-built TradeOffer{Give: []string{"p-1"}, Get:
// []string{"p-2"}} passes ProposeTradeOffer's T5 re-validation without
// pulling in the full draft-fixture machinery these store-level tests
// don't otherwise need.
func seedTradeOwnership(t *testing.T, store *Store) {
	t.Helper()
	if err := store.RecordTransaction(Transaction{
		ID: "txn-seed-1", TeamID: "team-1", Type: "add",
		Adds: []TransactionPlayer{{PlayerID: "p-1"}}, By: "manager", At: time.Now(),
	}, 100); err != nil {
		t.Fatalf("seed team-1 ownership of p-1: %v", err)
	}
	if err := store.RecordTransaction(Transaction{
		ID: "txn-seed-2", TeamID: "team-2", Type: "add",
		Adds: []TransactionPlayer{{PlayerID: "p-2"}}, By: "manager", At: time.Now(),
	}, 100); err != nil {
		t.Fatalf("seed team-2 ownership of p-2: %v", err)
	}
}

func TestTradeOffersDecodeFromOldStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	oldJSON := `{
		"ready": {},
		"picks": [],
		"members": {},
		"invites": [],
		"boards": {},
		"teamNames": {},
		"draftOrder": [],
		"scoring": {},
		"pickems": {}
	}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	state := store.Snapshot()
	if state.TradeOffers == nil || len(state.TradeOffers) != 0 {
		t.Fatalf("TradeOffers = %#v, want an empty, non-nil slice", state.TradeOffers)
	}
	seedTradeOwnership(t, store)
	offer := TradeOffer{
		ID: "trd-1", FromTeamID: "team-1", ToTeamID: "team-2",
		Give: []string{"p-1"}, Get: []string{"p-2"}, Status: TradeStatusOpen, CreatedAt: time.Now(),
	}
	if err := store.ProposeTradeOffer(offer, DefaultConfig(), nil, nil, 0, 100); err != nil {
		t.Fatalf("a trade proposed after loading an old file must succeed: %v", err)
	}
}

func TestCloneStateDeepCopiesTradeOffers(t *testing.T) {
	store := newTestStore(t)
	seedTradeOwnership(t, store)
	offer := TradeOffer{
		ID: "trd-1", FromTeamID: "team-1", ToTeamID: "team-2",
		Give: []string{"p-1"}, Get: []string{"p-2"}, Status: TradeStatusOpen, CreatedAt: time.Now(),
	}
	if err := store.ProposeTradeOffer(offer, DefaultConfig(), nil, nil, 0, 100); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	snapshot.TradeOffers[0].Give[0] = "tampered"
	snapshot.TradeOffers[0].Status = "tampered"
	fresh := store.Snapshot()
	if fresh.TradeOffers[0].Give[0] != "p-1" || fresh.TradeOffers[0].Status != TradeStatusOpen {
		t.Fatalf("mutating a snapshot leaked into the store's own state: %+v", fresh.TradeOffers[0])
	}
}

// ---------------------------------------------------------------------
// Reset paths
// ---------------------------------------------------------------------

func TestResetDraftClearsTradeOffersAndPrunesSentLog(t *testing.T) {
	store := newTestStore(t)
	seedTradeOwnership(t, store)
	offer := TradeOffer{
		ID: "trd-1", FromTeamID: "team-1", ToTeamID: "team-2",
		Give: []string{"p-1"}, Get: []string{"p-2"}, Status: TradeStatusOpen, CreatedAt: time.Now(),
	}
	if err := store.ProposeTradeOffer(offer, DefaultConfig(), nil, nil, 0, 100); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, key := range []string{"tradeoffer:trd-1:a@example.com", "tradedone:trd-1:a@example.com", "tradeveto:trd-1:a@example.com", "draftrem:24:x:a@example.com"} {
		if _, err := store.FirstSend(key, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if len(state.TradeOffers) != 0 {
		t.Fatalf("TradeOffers after ResetDraft = %+v, want empty", state.TradeOffers)
	}
	for _, key := range []string{"tradeoffer:trd-1:a@example.com", "tradedone:trd-1:a@example.com", "tradeveto:trd-1:a@example.com"} {
		if _, ok := state.SentLog[key]; ok {
			t.Fatalf("%s must be pruned by ResetDraft", key)
		}
	}
	if _, ok := state.SentLog["draftrem:24:x:a@example.com"]; !ok {
		t.Fatal("draftrem: must survive ResetDraft (regression)")
	}
}

func TestResetLeagueClearsTradeOffers(t *testing.T) {
	store := newTestStore(t)
	seedTradeOwnership(t, store)
	offer := TradeOffer{
		ID: "trd-1", FromTeamID: "team-1", ToTeamID: "team-2",
		Give: []string{"p-1"}, Get: []string{"p-2"}, Status: TradeStatusOpen, CreatedAt: time.Now(),
	}
	if err := store.ProposeTradeOffer(offer, DefaultConfig(), nil, nil, 0, 100); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().TradeOffers; len(got) != 0 {
		t.Fatalf("TradeOffers after ResetLeague = %+v, want empty", got)
	}
}

// ---------------------------------------------------------------------
// Notification idempotency
// ---------------------------------------------------------------------

func TestTradeNotificationsFireExactlyOnce(t *testing.T) {
	svc, _ := newTradesTestService(t, "")
	offerID := proposeFixtureOffer(t, svc)
	state := svc.store.Snapshot()
	offer := state.TradeOffers[0]

	// N15 already fired once inside ProposeTrade; fire it again directly
	// and confirm the ledger key stays committed exactly once (FirstSend's
	// idempotency, exercised through the trade-specific hook).
	svc.notifyTradeOffer(offer)
	svc.notifyTradeOffer(offer)
	sentAt := svc.store.Snapshot().SentLog[keyTradeOffer(offerID, "team-2@example.com")]
	if sentAt.IsZero() {
		t.Fatal("N15 was never recorded")
	}

	request, _ := http.NewRequest(http.MethodPost, "/trades", nil)
	if _, err := svc.AcceptTrade(request, "team-2", offerID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveTrade(request, offerID); err != nil {
		t.Fatal(err)
	}
	executedOffer := svc.store.Snapshot().TradeOffers[0]
	txn := svc.store.Snapshot().Transactions[0]
	svc.notifyTradeExecuted(executedOffer, txn)
	svc.notifyTradeExecuted(executedOffer, txn)
	if key := keyTradeExecuted(offerID, "team-1@example.com"); svc.store.Snapshot().SentLog[key].IsZero() {
		t.Fatal("N16 was never recorded for the proposing manager")
	}
}

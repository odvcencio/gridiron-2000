package league

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func resetFixtureTeamIDs() []string {
	ids := make([]string, 8)
	for i := range ids {
		ids[i] = fmt.Sprintf("team-%d", i+1)
	}
	return ids
}

func seedResetFixture(t *testing.T, store *Store) {
	t.Helper()
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	ids := resetFixtureTeamIDs()
	schedule, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: ids, StartWeek: 1, Weeks: 2, Seed: 41,
	})
	if err != nil {
		t.Fatalf("fixture schedule: %v", err)
	}
	for week := range schedule.Weeks {
		for matchup := range schedule.Weeks[week].Matchups {
			schedule.Weeks[week].Matchups[matchup].Final = true
			schedule.Weeks[week].Matchups[matchup].AwayScore = 101
			schedule.Weeks[week].Matchups[matchup].HomeScore = 99
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	state := &store.state
	state.Picks = []DraftPick{{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "p-1", MadeAt: now, MadeBy: "manager"}}
	state.Ready = map[string]bool{"team-1": true}
	state.Members = map[string]Member{
		"manager@example.com": {TeamID: "team-1", Name: "Manager", Email: "manager@example.com"},
		"co@example.com":      {TeamID: "team-1", Name: "Co", Email: "co@example.com", Role: "co"},
	}
	state.CoInvites = map[string]string{"pending@example.com": "team-2"}
	state.Invites = []string{"invite@example.com"}
	state.Boards = map[string][]string{"manager@example.com": {"p-2"}}
	state.TeamNames = map[string]string{"team-1": "Renamed Franchise"}
	state.DraftOrder = append([]string(nil), ids...)
	state.Scoring = map[string]float64{"passTD": 5}
	state.DraftStarted = true
	state.DraftStartedAt = now.Add(-time.Hour)
	state.DraftAtOverride = now.Add(24 * time.Hour)
	state.Pickems = map[string]map[string]string{"manager@example.com": {"game-1": "CIN"}}
	state.PickemEnteredAt = map[string]time.Time{"manager@example.com": now.Add(-2 * time.Hour)}
	state.PickemMarkets = map[string]PickemMarket{"game-1": {
		Week: 1, Kickoff: now.Add(48 * time.Hour), Away: "CIN", Home: "CLE",
		LockAt: now.Add(47 * time.Hour), LineTenths: 35, LinePresent: true,
		ObservedAt: now, SourceUpdatedAt: now,
	}}
	state.BlitzEntries = map[string]map[string]BlitzEntry{"manager@example.com": {
		"pre2": {Players: []string{"p-1", "p-2"}, UpdatedAt: now},
	}}
	state.ClockDeadline = now.Add(time.Hour)
	state.ClockPaused = true
	state.ClockRemainingSec = 42
	state.ClockDurationSec = 90
	state.Autopick = map[string]bool{"team-1": true}
	state.SentLog = map[string]time.Time{
		"onclock:team-1": now, "seat:team-1": now, "order:keep": now,
	}
	state.NotifyPrefs = map[string]map[string]bool{"manager@example.com": {"draft_live": false}}
	state.Schedule = &schedule
	state.Playoffs = &PlayoffState{
		Config:         PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1},
		ChampionTeamID: "team-1", RunnerUpTeamID: "team-2",
	}
	state.Phase = PhaseSeasonComplete
	state.BadgeClaims = map[string]string{"team-1": "wolf"}
	state.AvatarRefs = map[string]string{"team-2": strings.Repeat("a", 64)}
	state.RosterOverride = &RosterOverride{
		Slots: map[string]int{"QB": 1, "RB": 2}, Bench: 4,
	}
	state.Announcements = []Announcement{{
		ID: "ann-fixture", Body: "Season complete.", PostedAt: now, PostedBy: "commissioner",
	}}
	state.Lineups = map[string]map[int]map[string]string{"team-1": {1: {"QB1": "p-1"}}}
	state.Transactions = []Transaction{{
		ID: "txn-fixture", Season: 2026, Week: 1, Type: "add", TeamID: "team-1",
		Adds: []TransactionPlayer{{PlayerID: "p-1", Name: "Player One", Position: "QB"}},
		At:   now,
	}}
	state.WaiverClaims = []WaiverClaim{{
		ID: "claim-fixture", TeamID: "team-1", AddID: "p-3", Priority: 1, FiledAt: now,
	}}
	state.WaiverReceipts = []WaiverReceipt{{
		ClaimID: "claim-old", Season: 2026, Week: 1, TeamID: "team-1",
		Add:     TransactionPlayer{PlayerID: "p-4", Name: "Player Four", Position: "RB"},
		Outcome: "won", FiledAt: now.Add(-time.Hour), ResolvedAt: now,
	}}
	state.WaiversProcessedThrough = now
	state.TradeOffers = []TradeOffer{{
		ID: "trade-fixture", FromTeamID: "team-1", ToTeamID: "team-2",
		Give: []string{"p-1"}, Get: []string{"p-2"}, Status: TradeStatusOpen, CreatedAt: now,
	}}
	state.RosterZones = map[string]map[string]ZoneAssignment{
		"team-1": {"p-1": {Zone: zoneReserve, Position: "QB", PlacedAt: now}},
	}
	state.TrimmedTeamIDs = []string{"team-8"}

	if err := store.persistLocked(allCollections()...); err != nil {
		t.Fatalf("persist reset fixture: %v", err)
	}
}

func assertResetFixtureConfigAndHistory(t *testing.T, state PersistedState) {
	t.Helper()
	if len(state.Invites) != 1 || state.Invites[0] != "invite@example.com" {
		t.Fatalf("invites were not preserved: %#v", state.Invites)
	}
	if state.TeamNames["team-1"] != "Renamed Franchise" || state.Scoring["passTD"] != 5 {
		t.Fatalf("operator config was not preserved: names=%#v scoring=%#v", state.TeamNames, state.Scoring)
	}
	if len(state.Announcements) != 1 || state.Announcements[0].ID != "ann-fixture" {
		t.Fatalf("announcements were not preserved: %#v", state.Announcements)
	}
	if len(state.NotifyPrefs) != 1 || state.NotifyPrefs["manager@example.com"]["draft_live"] {
		t.Fatalf("notification preferences were not preserved: %#v", state.NotifyPrefs)
	}
	if state.SentLog["order:keep"].IsZero() {
		t.Fatalf("unrelated sent receipt was not preserved: %#v", state.SentLog)
	}
}

func assertResetFixtureDraftStateCleared(t *testing.T, state PersistedState) {
	t.Helper()
	if len(state.Picks) != 0 || len(state.Ready) != 0 || len(state.Transactions) != 0 ||
		len(state.Lineups) != 0 || len(state.WaiverClaims) != 0 || len(state.WaiverReceipts) != 0 ||
		len(state.TradeOffers) != 0 || len(state.RosterZones) != 0 {
		t.Fatalf("roster/draft state survived: picks=%#v ready=%#v txns=%#v lineups=%#v waivers=%#v/%#v trades=%#v zones=%#v",
			state.Picks, state.Ready, state.Transactions, state.Lineups, state.WaiverClaims, state.WaiverReceipts, state.TradeOffers, state.RosterZones)
	}
	if !state.WaiversProcessedThrough.IsZero() || state.DraftStarted || !state.DraftStartedAt.IsZero() ||
		!state.ClockDeadline.IsZero() || state.ClockPaused ||
		state.ClockRemainingSec != 0 || state.ClockDurationSec != 0 || len(state.Autopick) != 0 {
		t.Fatalf("draft lifecycle survived: %+v", state)
	}
	if !state.SentLog["onclock:team-1"].IsZero() {
		t.Fatalf("sent-log reset receipts were not pruned: %#v", state.SentLog)
	}
}

func TestResetDraftContractRoundTripsAndPreservesLeagueTopology(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	seedResetFixture(t, store)

	if err := store.ResetDraft(); err != nil {
		t.Fatalf("ResetDraft: %v", err)
	}
	state := store.Snapshot()
	assertResetFixtureDraftStateCleared(t, state)
	if len(state.Members) != 2 || len(state.CoInvites) != 1 || len(state.Boards) != 1 ||
		len(state.DraftOrder) != 8 || state.Schedule == nil || state.Playoffs == nil ||
		state.Phase != PhaseSeasonComplete || len(state.Pickems) != 1 || len(state.BlitzEntries) != 1 ||
		len(state.BadgeClaims) != 1 || len(state.AvatarRefs) != 1 {
		t.Fatalf("ResetDraft changed preserved topology: %+v", state)
	}
	if state.RosterOverride == nil || state.RosterOverride.Bench != 4 ||
		len(state.TrimmedTeamIDs) != 1 || state.TrimmedTeamIDs[0] != "team-8" {
		t.Fatalf("ResetDraft changed preserved roster topology: override=%#v trim=%#v", state.RosterOverride, state.TrimmedTeamIDs)
	}
	if state.DraftAtOverride.IsZero() {
		t.Fatal("ResetDraft cleared the preserved DraftAtOverride")
	}
	assertResetFixtureConfigAndHistory(t, state)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(path)
	t.Cleanup(func() { _ = restarted.Close() })
	state = restarted.Snapshot()
	assertResetFixtureDraftStateCleared(t, state)
	if len(state.Members) != 2 || len(state.DraftOrder) != 8 || state.Schedule == nil ||
		state.Playoffs == nil || state.Phase != PhaseSeasonComplete {
		t.Fatalf("ResetDraft topology did not survive restart: %+v", state)
	}
	if state.RosterOverride == nil || state.RosterOverride.Bench != 4 ||
		len(state.TrimmedTeamIDs) != 1 || state.TrimmedTeamIDs[0] != "team-8" {
		t.Fatalf("ResetDraft roster topology did not survive restart: override=%#v trim=%#v", state.RosterOverride, state.TrimmedTeamIDs)
	}
	if state.DraftAtOverride.IsZero() {
		t.Fatal("ResetDraft DraftAtOverride did not survive restart")
	}
	assertResetFixtureConfigAndHistory(t, state)
}

func TestResetLeagueContractRoundTripsAndRestoresTruthfulPreDraftHQ(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	seedResetFixture(t, store)

	if err := store.ResetLeague(); err != nil {
		t.Fatalf("ResetLeague: %v", err)
	}
	state := store.Snapshot()
	assertResetFixtureDraftStateCleared(t, state)
	if len(state.Members) != 0 || len(state.CoInvites) != 0 || len(state.Boards) != 0 ||
		len(state.Pickems) != 0 || len(state.PickemEnteredAt) != 0 || len(state.PickemMarkets) != 0 ||
		len(state.BlitzEntries) != 0 || len(state.DraftOrder) != 0 || state.Schedule != nil ||
		state.Playoffs != nil || state.Phase != "" || len(state.BadgeClaims) != 0 || len(state.AvatarRefs) != 0 || !state.DraftAtOverride.IsZero() ||
		state.RosterOverride != nil || len(state.TrimmedTeamIDs) != 0 {
		t.Fatalf("ResetLeague left competitive or seat-bound state: %+v", state)
	}
	if !state.SentLog["seat:team-1"].IsZero() {
		t.Fatalf("ResetLeague left seat-bound sent receipt: %#v", state.SentLog)
	}
	assertResetFixtureConfigAndHistory(t, state)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(path)
	t.Cleanup(func() { _ = restarted.Close() })
	state = restarted.Snapshot()
	assertResetFixtureDraftStateCleared(t, state)
	if len(state.Members) != 0 || len(state.DraftOrder) != 0 || state.Schedule != nil ||
		state.Playoffs != nil || state.Phase != "" || len(state.BadgeClaims) != 0 || len(state.AvatarRefs) != 0 || !state.DraftAtOverride.IsZero() ||
		state.RosterOverride != nil || len(state.TrimmedTeamIDs) != 0 {
		t.Fatalf("ResetLeague truth did not survive restart: %+v", state)
	}
	if !state.SentLog["seat:team-1"].IsZero() {
		t.Fatalf("ResetLeague left seat-bound sent receipt after restart: %#v", state.SentLog)
	}
	assertResetFixtureConfigAndHistory(t, state)

	svc := newTestService(t, true)
	_ = svc.store.Close()
	svc.store = restarted
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if got := svc.SeasonPhase(now); got != "preseason" {
		t.Fatalf("SeasonPhase after full reset = %q, want preseason", got)
	}
	adminData := svc.AdminData(httptest.NewRequest(http.MethodGet, "/admin", nil))
	scheduleData, ok := adminData["schedule"].(map[string]any)
	if !ok || scheduleData["has_schedule"] != false {
		t.Fatalf("Admin schedule truth after full reset = %#v", adminData["schedule"])
	}
	summary := svc.CommissionerSummary("g2k", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{Mode: "offline"})
	if summary.Season.Phase != "preseason" || summary.Season.Schedule.Published ||
		summary.Season.Playoffs.Seeded || summary.Draft.OrderSet || summary.Membership.ClaimedSeats != 0 {
		t.Fatalf("HQ truth after full reset = season=%+v draft=%+v membership=%+v", summary.Season, summary.Draft, summary.Membership)
	}
}

func TestAdminResetLeagueRestoresRuntimeTopologyDefaults(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	custom := RosterPreset{
		Name:  "reset-test",
		Slots: map[string]int{"QB": 1, "RB": 2, "WR": 2, "FLEX": 1},
		Bench: 1,
	}
	setRosterShape(custom)
	applySeatTrim(activeTeams[:2])
	t.Cleanup(func() {
		clearRosterShape()
		clearSeatTrim()
	})
	if err := service.AdminResetLeague(request, ResetLeagueConfirmation); err != nil {
		t.Fatalf("AdminResetLeague: %v", err)
	}
	if got := CurrentRoster().Name; got != ActiveRosterPreset.Name {
		t.Fatalf("CurrentRoster after full reset = %q, want config default %q", got, ActiveRosterPreset.Name)
	}
	if got := len(defaultTeams()); got != len(activeTeams) {
		t.Fatalf("defaultTeams after full reset = %d, want %d", got, len(activeTeams))
	}
	if got := len(service.Teams()); got != len(activeTeams) {
		t.Fatalf("service teams after full reset = %d, want %d", got, len(activeTeams))
	}
}

func TestAdminResetConfirmationsAreDistinctAndServerValidated(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	service.store.state.Picks = []DraftPick{{Number: 1, TeamID: "team-1", PlayerID: "p-1"}}
	before := service.store.Snapshot()

	if err := service.AdminResetDraft(request, ResetLeagueConfirmation); err == nil {
		t.Fatal("RESET LEAGUE must not authorize ResetDraft")
	}
	if err := service.AdminResetLeague(request, ResetDraftConfirmation); err == nil {
		t.Fatal("RESET DRAFT must not authorize ResetLeague")
	}
	after := service.store.Snapshot()
	if len(after.Picks) != len(before.Picks) {
		t.Fatalf("wrong reset phrase mutated picks: before=%#v after=%#v", before.Picks, after.Picks)
	}
	if err := service.AdminResetDraft(request, ResetDraftConfirmation); err != nil {
		t.Fatalf("exact draft reset confirmation: %v", err)
	}
}

package league

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

var commissionerV1TestNow = time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)

func commissionerV1Fixture() (CommissionerSummaryV1ConfigSnapshot, PersistedState, CommissionerSummaryV1DataSnapshot, CommissionerSummaryV1ReleaseSnapshot) {
	cfg := DefaultConfig()
	cfg.Name = "Fixture League"
	cfg.ShortCode = "FIX"
	cfg.ModeLabel = "dynasty"
	cfg.Timezone = "America/New_York"
	cfg.Season = 2026
	cfg.DraftAt = commissionerV1TestNow.Add(2 * time.Hour)
	cfg.Rounds = 2
	cfg.ScoringFormat = "PPR"
	cfg.Waivers.Mode = "faab"
	cfg.Waivers.ProcessTime = "09:00"
	cfg.Trades.Deadline = commissionerV1TestNow.Add(30 * 24 * time.Hour).Format(time.RFC3339)
	teams := []Team{
		{ID: "team-1", Name: "Alpha", Abbreviation: "ALP"},
		{ID: "team-2", Name: "Bravo", Abbreviation: "BRV"},
		{ID: "team-3", Name: "Charlie", Abbreviation: "CHR"},
	}
	state := PersistedState{
		Ready: map[string]bool{"team-1": true},
		Members: map[string]Member{
			"primary-one@example.test": {TeamID: "team-1", Name: "Primary One", Email: "primary-one@example.test"},
			"co-one@example.test":      {TeamID: "team-1", Name: "Co One", Email: "co-one@example.test", Role: "co"},
			"primary-two@example.test": {TeamID: "team-2", Name: "Primary Two", Email: "primary-two@example.test"},
		},
		Invites:   []string{"pending@example.test"},
		CoInvites: map[string]string{"pending-co@example.test": "team-2"},
		Boards: map[string][]string{
			"primary-one@example.test": {"player-1"},
			"primary-two@example.test": {},
		},
		DraftOrder: []string{"team-1", "team-2"},
		Picks:      []DraftPick{},
		Pickems: map[string]map[string]string{
			"primary-one@example.test": {"game-1": "MIA"},
			"primary-two@example.test": {},
		},
		PickemMarkets: map[string]PickemMarket{
			"game-1": {Week: 1, Kickoff: commissionerV1TestNow.Add(time.Hour), Away: "MIA", Home: "BUF", LinePresent: true},
		},
		WaiverClaims:            []WaiverClaim{{ID: "clm-1", TeamID: "team-1", AddID: "player-2", Priority: 1}},
		WaiversProcessedThrough: commissionerV1TestNow.Add(-24 * time.Hour),
		TradeOffers: []TradeOffer{
			{ID: "trd-open", FromTeamID: "team-1", ToTeamID: "team-2", Status: TradeStatusOpen},
			{ID: "trd-review", FromTeamID: "team-2", ToTeamID: "team-1", Status: TradeStatusAccepted},
		},
		Transactions: []Transaction{{
			ID: "txn-1", Type: "add", TeamID: "team-1", At: commissionerV1TestNow.Add(-20 * time.Minute),
			Adds: []TransactionPlayer{{PlayerID: "player-3", Name: "Safe Player", Position: "RB"}},
		}},
	}
	issues := 2
	players := 340
	data := CommissionerSummaryV1DataSnapshot{
		Quality: "healthy", SourceMode: "live", SourceState: "live", PlayerCount: &players,
		LastSuccessAt: commissionerV1TestNow.Add(-2 * time.Minute), AsOf: commissionerV1TestNow.Add(-time.Minute),
		Games:            []GameInfo{{ID: "game-1", Week: 1, Kickoff: commissionerV1TestNow.Add(time.Hour), Away: "MIA", Home: "BUF"}},
		LineupIssueCount: &issues, NextLineupLockAt: commissionerV1TestNow.Add(30 * time.Minute),
	}
	release := CommissionerSummaryV1ReleaseSnapshot{
		GitSHA:      "1234567890abcdef1234567890abcdef12345678",
		BuiltAt:     commissionerV1TestNow.Add(-time.Hour),
		ImageDigest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	return CommissionerSummaryV1ConfigSnapshot{InstanceID: "fixture", LeagueID: "fixture-league", Config: cfg, Teams: teams, BlitzEnabled: true}, state, data, release
}

func TestCommissionerSummaryV1BoardGapsUseCanonicalSeatOwner(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()

	// Mixed-case persisted member data still resolves to the normalized board
	// key. The co-manager shares the primary's order; an unclaimed team's
	// unrelated board-like key is never included in claimed-seat readiness.
	primary := state.Members["primary-one@example.test"]
	delete(state.Members, "primary-one@example.test")
	primary.Email = "PRIMARY-ONE@EXAMPLE.TEST"
	state.Members["PRIMARY-ONE@EXAMPLE.TEST"] = primary
	state.Boards["co-one@example.test"] = []string{"legacy-co-player"}
	state.Boards["team-3"] = []string{"unclaimed-player"}

	summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{
		config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := *summary.Draft.BoardGapCount; got != 1 {
		t.Fatalf("board gap count = %d, want only claimed team-2's empty board", got)
	}

	delete(state.Boards, "primary-one@example.test")
	summary, err = projectCommissionerSummaryV1(commissionerSummaryV1Tuple{
		config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := *summary.Draft.BoardGapCount; got != 2 {
		t.Fatalf("board gap count = %d, want both claimed seats empty; co legacy board must not mask the gap", got)
	}
}

func TestCommissionerSummaryV1CapturesOneCoherentTuple(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	source, err := NewCommissionerSummaryV1Source(CommissionerSummaryV1Captures{
		Config: func(context.Context) (CommissionerSummaryV1ConfigSnapshot, error) {
			counts["config"]++
			return cfg, nil
		},
		Store: func(context.Context) (PersistedState, error) { counts["store"]++; return state, nil },
		Data:  func(context.Context) (CommissionerSummaryV1DataSnapshot, error) { counts["data"]++; return data, nil },
		Release: func(context.Context) (CommissionerSummaryV1ReleaseSnapshot, error) {
			counts["release"]++
			return release, nil
		},
		Clock: func() time.Time { counts["clock"]++; return commissionerV1TestNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := source(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config", "store", "data", "release", "clock"} {
		if counts[name] != 1 {
			t.Errorf("%s captures = %d, want 1", name, counts[name])
		}
	}
	after, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("projection mutated the supplied Store snapshot")
	}
	if got := *summary.Competition.Teams.Occupied; got != 2 {
		t.Errorf("occupied teams = %d, want 2", got)
	}
	if got := *summary.Competition.Teams.Vacant; got != 1 {
		t.Errorf("vacant teams = %d, want 1", got)
	}
	if *summary.Membership.PrimaryManagers != 2 || *summary.Membership.CoManagers != 1 || *summary.Membership.PendingInvites != 2 {
		t.Errorf("membership counts = %+v", summary.Membership)
	}
	if *summary.Draft.ReadyTeams != 1 || *summary.Draft.ExpectedTeams != 3 || *summary.Draft.BoardGapCount != 1 || *summary.Draft.PickCapacity != 6 {
		t.Errorf("draft counts = %+v", summary.Draft)
	}
	if *summary.Lineup.IssueCount != 2 || *summary.Waivers.OpenClaims != 1 || *summary.Trades.PendingCount != 2 || *summary.Trades.CommissionerDecisions != 1 {
		t.Errorf("weekly counts lineup=%+v waivers=%+v trades=%+v", summary.Lineup, summary.Waivers, summary.Trades)
	}
	if *summary.Pickem.Unpicked != 1 || summary.Pickem.NextDeadlineAt == nil {
		t.Errorf("pickem = %+v, want one open gap", summary.Pickem)
	}
	if summary.Calendar.NextDeadline == nil || summary.Calendar.NextDeadline.Code != "waiver_run" {
		t.Errorf("next deadline = %+v, want earliest due waiver_run", summary.Calendar.NextDeadline)
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hqv1.Decode(payload)
	if err != nil {
		t.Fatalf("frozen v1 decoder rejected projection: %v", err)
	}
	if decoded.Instance.LeagueID != "fixture-league" {
		t.Fatalf("decoded league ID = %q", decoded.Instance.LeagueID)
	}
}

func TestCommissionerSummaryV1DraftAndPhasePrecedence(t *testing.T) {
	for _, test := range []struct {
		name      string
		draftAt   time.Time
		picks     int
		started   bool
		schedule  bool
		seasonNow bool
		rawPhase  string
		wantDraft string
		wantPhase string
	}{
		{"unscheduled", time.Time{}, 0, false, false, false, "", "unscheduled", "pre-draft"},
		{"scheduled", commissionerV1TestNow.Add(time.Hour), 0, false, false, false, "", "scheduled", "pre-draft"},
		{"missed meeting stays scheduled", commissionerV1TestNow.Add(-time.Hour), 0, false, false, false, "", "scheduled", "pre-draft"},
		{"commissioner early start", commissionerV1TestNow.Add(time.Hour), 0, true, false, false, "", "open", "draft"},
		{"in progress", commissionerV1TestNow.Add(-time.Hour), 1, true, false, false, "", "in_progress", "draft"},
		{"complete beats lifecycle", commissionerV1TestNow.Add(time.Hour), 6, true, false, false, "", "complete", "preseason"},
		{"derived regular season", commissionerV1TestNow.Add(time.Hour), 6, true, true, true, "", "complete", "regular-season"},
		{"regular season", commissionerV1TestNow.Add(time.Hour), 6, true, false, false, PhaseRegularSeason, "complete", "regular-season"},
		{"postseason", commissionerV1TestNow.Add(time.Hour), 6, true, false, false, PhasePlayoffs, "complete", "post-season"},
		{"season complete", commissionerV1TestNow.Add(time.Hour), 6, true, false, false, PhaseSeasonComplete, "complete", "complete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, state, data, release := commissionerV1Fixture()
			cfg.Config.DraftAt = test.draftAt
			if test.seasonNow {
				cfg.Config.SeasonStartAt = commissionerV1TestNow.Add(-time.Hour)
			} else {
				cfg.Config.SeasonStartAt = commissionerV1TestNow.Add(time.Hour)
			}
			state.Phase = test.rawPhase
			state.DraftStarted = test.started
			if test.schedule {
				state.Schedule = &SeasonSchedule{Season: cfg.Config.Season, Weeks: []ScheduleWeek{{Week: 1}}}
			}
			state.Picks = make([]DraftPick, test.picks)
			for index := range state.Picks {
				state.Picks[index] = DraftPick{Number: index + 1, TeamID: state.DraftOrder[index%len(state.DraftOrder)], PlayerID: "player", MadeAt: commissionerV1TestNow.Add(-time.Minute)}
			}
			summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow})
			if err != nil {
				t.Fatal(err)
			}
			if summary.Draft.State != test.wantDraft || summary.Competition.Phase != test.wantPhase {
				t.Errorf("draft/phase = %s/%s, want %s/%s", summary.Draft.State, summary.Competition.Phase, test.wantDraft, test.wantPhase)
			}
			if test.wantDraft == "complete" && summary.Draft.OnClockTeamID != nil {
				t.Error("complete pick count must clear on-clock team")
			}
		})
	}
}

func TestCommissionerSummaryV1UsesRosterOverrideAndSnakeOrder(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()
	state.TrimmedTeamIDs = []string{"team-3"}
	state.RosterOverride = &RosterOverride{Slots: map[string]int{"QB": 1}, Bench: 2}
	state.DraftStarted = true
	state.Picks = []DraftPick{
		{Number: 1, TeamID: "team-1", PlayerID: "player-1", MadeAt: commissionerV1TestNow.Add(-2 * time.Minute)},
		{Number: 2, TeamID: "team-2", PlayerID: "player-2", MadeAt: commissionerV1TestNow.Add(-time.Minute)},
	}
	summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow})
	if err != nil {
		t.Fatal(err)
	}
	if *summary.Draft.DraftRounds != 3 || *summary.Draft.PickCapacity != 6 {
		t.Fatalf("override draft capacity = rounds:%v capacity:%v, want 3/6", summary.Draft.DraftRounds, summary.Draft.PickCapacity)
	}
	if summary.Draft.OnClockTeamID == nil || *summary.Draft.OnClockTeamID != "team-2" {
		t.Fatalf("snake pick 3 on clock = %v, want team-2", summary.Draft.OnClockTeamID)
	}
}

func TestCommissionerSummaryV1TrimmedTeamsAndCoManagerDenominators(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()
	state.TrimmedTeamIDs = []string{"team-3"}
	state.Members["second-co@example.test"] = Member{TeamID: "team-2", Role: "co", Email: "second-co@example.test"}
	summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow})
	if err != nil {
		t.Fatal(err)
	}
	if *summary.Competition.Teams.Total != 2 || *summary.Draft.ExpectedTeams != 2 || *summary.Membership.CoManagers != 2 {
		t.Fatalf("trim/co-manager counts = teams:%+v draft:%+v membership:%+v", summary.Competition.Teams, summary.Draft, summary.Membership)
	}
}

func TestCommissionerSummaryV1SourceFailureIsReportedWithoutLosingSnapshot(t *testing.T) {
	cfg, state, _, release := commissionerV1Fixture()
	source, err := NewCommissionerSummaryV1Source(CommissionerSummaryV1Captures{
		Config: func(context.Context) (CommissionerSummaryV1ConfigSnapshot, error) { return cfg, nil },
		Store:  func(context.Context) (PersistedState, error) { return state, nil },
		Data: func(context.Context) (CommissionerSummaryV1DataSnapshot, error) {
			return CommissionerSummaryV1DataSnapshot{}, errors.New("raw upstream secret")
		},
		Release: func(context.Context) (CommissionerSummaryV1ReleaseSnapshot, error) { return release, nil },
		Clock:   func() time.Time { return commissionerV1TestNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := source(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.DataHealth.Quality != "not_reported" || summary.DataHealth.AsOf != nil || len(summary.Warnings) == 0 {
		t.Fatalf("source failure truth = data:%+v warnings:%+v", summary.DataHealth, summary.Warnings)
	}
	if summary.Configuration.BlitzEnabled == nil || !*summary.Configuration.BlitzEnabled || summary.Links.Blitz == nil {
		t.Fatalf("source failure changed immutable Blitz configuration: config=%+v links=%+v", summary.Configuration, summary.Links)
	}
}

func TestCommissionerSummaryV1NormalizesSourceTruthAcrossSurfaces(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()
	data.SourceMode = "offline"
	data.SourceState = "live"
	summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow})
	if err != nil {
		t.Fatal(err)
	}
	if summary.DataHealth.SourceState == nil || *summary.DataHealth.SourceState != "unreachable" {
		t.Fatalf("normalized source state = %v, want unreachable", summary.DataHealth.SourceState)
	}
	if len(summary.Warnings) == 0 || summary.Warnings[0].Code != "source_not_reported" {
		t.Fatalf("warnings did not share normalized source truth: %+v", summary.Warnings)
	}
	foundAttention, foundReadiness := false, false
	for _, item := range summary.AttentionItems {
		foundAttention = foundAttention || item.Code == "source_not_reported"
	}
	for _, item := range summary.Readiness.Items {
		foundReadiness = foundReadiness || item.Code == "data_stale"
	}
	if !foundAttention || !foundReadiness {
		t.Fatalf("normalized source truth missing from attention/readiness: attention=%+v readiness=%+v", summary.AttentionItems, summary.Readiness.Items)
	}
}

func TestCommissionerSummaryV1NormalizesFantasyRuntimeStates(t *testing.T) {
	for _, test := range []struct {
		state string
		want  string
	}{
		{state: "cached", want: "stale"},
		{state: "offline", want: "unreachable"},
		{state: "unavailable", want: "unreachable"},
		{state: "degraded", want: "degraded"},
		{state: "live", want: "live"},
	} {
		t.Run(test.state, func(t *testing.T) {
			data := commissionerV1NormalizeData(CommissionerSummaryV1DataSnapshot{SourceMode: "live", SourceState: test.state})
			if data.SourceState != test.want {
				t.Fatalf("normalized state = %q, want %q", data.SourceState, test.want)
			}
		})
	}
}

func TestCommissionerSummaryV1PrivacySentinelsNeverSerialize(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()
	state.Members["victim@private.test"] = Member{TeamID: "team-1", Name: "oauth-user-secret", Email: "victim@private.test", Role: "co"}
	state.Transactions = append(state.Transactions, Transaction{
		ID: "txn-hostile", Type: "add", TeamID: "team-1", At: commissionerV1TestNow.Add(-time.Minute),
		Adds: []TransactionPlayer{{PlayerID: "private-player", Name: "victim@private.test", Position: "RB"}},
	})
	cfg.Config.Source = "/private/operator/path/league.json"
	data.DegradationCode = "credential-secret /private/operator/path"
	summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	for _, sentinel := range []string{"victim@private.test", "oauth-user-secret", "credential-secret", "/private/operator/path"} {
		if strings.Contains(serialized, sentinel) {
			t.Errorf("serialized provider response leaked %q", sentinel)
		}
	}
}

func TestCommissionerSummaryV1CalendarOmitsElapsedOneShotEvents(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()
	cfg.Config.DraftAt = commissionerV1TestNow.Add(-24 * time.Hour)
	cfg.Config.Trades.Deadline = commissionerV1TestNow.Add(-time.Hour).Format(time.RFC3339)
	cfg.Config.SeasonStartAt = commissionerV1TestNow.Add(-7 * 24 * time.Hour)
	state.DraftStarted = false
	summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Calendar.NextDeadline == nil || summary.Calendar.NextDeadline.Code != "waiver_run" {
		t.Fatalf("next deadline = %+v, want future/overdue recurring waiver work", summary.Calendar.NextDeadline)
	}
	calendarJSON, err := json.Marshal(summary.Calendar)
	if err != nil {
		t.Fatal(err)
	}
	for _, elapsed := range []string{"trade_deadline", "season_start", `"code":"draft"`} {
		if strings.Contains(string(calendarJSON), elapsed) {
			t.Errorf("calendar retained elapsed one-shot event %q: %s", elapsed, calendarJSON)
		}
	}
}

func TestCommissionerSummaryV1SignedProviderSuccessAndStoreFailure(t *testing.T) {
	cfg, state, data, release := commissionerV1Fixture()
	storeFailure := false
	source, err := NewCommissionerSummaryV1Source(CommissionerSummaryV1Captures{
		Config: func(context.Context) (CommissionerSummaryV1ConfigSnapshot, error) { return cfg, nil },
		Store: func(context.Context) (PersistedState, error) {
			if storeFailure {
				return PersistedState{}, errors.New("database unavailable")
			}
			return state, nil
		},
		Data:    func(context.Context) (CommissionerSummaryV1DataSnapshot, error) { return data, nil },
		Release: func(context.Context) (CommissionerSummaryV1ReleaseSnapshot, error) { return release, nil },
		Clock:   func() time.Time { return commissionerV1TestNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := v1transport.NewCredentials("fixture-key", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	provider, err := v1transport.NewProvider(v1transport.ProviderOptions{
		Keys: []v1transport.Credentials{credentials}, Clock: func() time.Time { return commissionerV1TestNow },
		RequestID: func() string { return "req_fixture_provider_0001" },
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(provider)
	defer server.Close()
	target, err := v1transport.NewTarget(server.URL, "fixture-league", credentials)
	if err != nil {
		t.Fatal(err)
	}
	client, err := v1transport.NewClient(v1transport.ClientOptions{
		Transport: server.Client().Transport, Timeout: time.Second,
		Clock:     func() time.Time { return commissionerV1TestNow },
		RequestID: func() string { return "req_fixture_client_00001" },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Fetch(context.Background(), target)
	if err != nil || got.Instance.LeagueID != "fixture-league" {
		t.Fatalf("signed provider success = summary:%+v err:%v", got.Instance, err)
	}
	storeFailure = true
	_, err = client.Fetch(context.Background(), target)
	if !v1transport.FailureIs(err, v1transport.FailureUnreachable) {
		t.Fatalf("Store capture failure = %v, want exact typed 503/unreachable envelope", err)
	}
}

func TestReadableSnapshotCouplesHealthAndClone(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	defer store.Close()
	snapshot, err := store.ReadableSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Ready["team-1"] = true
	if store.Snapshot().Ready["team-1"] {
		t.Fatal("mutating ReadableSnapshot leaked into Store state")
	}
	store.mu.Lock()
	store.persistenceWriteError = errors.New("write health unavailable")
	store.mu.Unlock()
	if _, err := store.ReadableSnapshot(); err == nil {
		t.Fatal("ReadableSnapshot must fail closed with persistence health")
	}
}

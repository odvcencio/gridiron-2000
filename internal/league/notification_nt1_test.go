package league

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/notify"
)

func nt1FinalGame(now time.Time) GameInfo {
	return GameInfo{
		ID: "nt1-game", Week: 1, Kickoff: now.Add(-time.Hour),
		Away: "BUF", Home: "MIA", AwayScore: 17, HomeScore: 24,
		Final: true, ScoresPresent: true, SpreadLineTenths: 30,
		SpreadLinePresent: true, SourceObservedAt: now.Add(-5 * 24 * time.Hour),
	}
}

func nt1Schedule(season int, generatedAt time.Time, final bool) SeasonSchedule {
	return SeasonSchedule{
		Season: season, GeneratedAt: generatedAt, StartWeek: 1,
		Weeks: []ScheduleWeek{{Week: 1, Matchups: []LeagueMatchup{{
			ID: "nt1-matchup", Week: 1, HomeTeamID: "team-1", AwayTeamID: "team-2",
			HomeScore: 121.5, AwayScore: 99.25, Final: final,
		}}}},
	}
}

func TestNT1PickemResultsIsTerminalFreshAndIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 20, 22, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(-24*time.Hour), now)
	if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	game := nt1FinalGame(now)
	svc.SetScheduleSource(func() []GameInfo { return []GameInfo{game} })
	if err := svc.store.ReconcilePickemMarkets(now, []GameInfo{game}, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetPickem("a@example.com", game.ID, game.Away, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	svc.evalPickemResults(svc.store.Snapshot(), now)
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("fresh terminal results queue depth = %d, want 1", got)
	}
	if got := sentLogCount(svc.store.Snapshot(), "pickem-results:"); got != 1 {
		t.Fatalf("fresh terminal results ledger = %d, want 1", got)
	}
	message := svc.buildPickemResults(Member{Email: "a@example.com"}, 1, []GameInfo{game}, svc.store.Snapshot().PickemMarkets, map[string]string{game.ID: game.Away}, now.Add(-2*time.Hour), now)
	if !strings.Contains(message.Text, "pickem?week=1") {
		t.Fatalf("results message omitted safe week link: %q", message.Text)
	}
	if got := svc.buildPickemResults(Member{Email: "a@example.com"}, 1, []GameInfo{game}, svc.store.Snapshot().PickemMarkets, map[string]string{game.ID: game.Away}, now.Add(-2*time.Hour), now).Key; got != keyPickemResults("2026", 1, "a@example.com") {
		t.Fatalf("results key = %q, want canonical key", got)
	}

	svc.evalPickemResults(svc.store.Snapshot(), now.Add(time.Hour))
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("repeat terminal results queue depth = %d, want 1", got)
	}

	open := game
	open.ID = "nt1-open"
	open.Final = false
	svc.SetScheduleSource(func() []GameInfo { return []GameInfo{open} })
	state := svc.store.Snapshot()
	state.PickemMarkets = map[string]PickemMarket{open.ID: {Week: 1, Kickoff: open.Kickoff, Frozen: true, LinePresent: true}}
	state.Pickems = map[string]map[string]string{"a@example.com": {open.ID: open.Away}}
	state.PickemEnteredAt = map[string]time.Time{"a@example.com": now.Add(-2 * time.Hour)}
	svc.store.mu.Lock()
	svc.store.state = cloneState(state)
	svc.store.mu.Unlock()
	svc.evalPickemResults(state, now)
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("open results queue depth = %d, want unchanged 1", got)
	}
}

func TestNT1ScoringChangesCoalesceAndUseStableHash(t *testing.T) {
	now := time.Date(2026, 9, 20, 22, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(-24*time.Hour), now)
	if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetScoringValue("passTD", 5, now); err != nil {
		t.Fatal(err)
	}
	state := svc.store.Snapshot()
	if !state.ScoringChangedAt.Equal(now) {
		t.Fatalf("scoring changed at = %v, want %v", state.ScoringChangedAt, now)
	}
	svc.evalScoringChanges(state, now.Add(14*time.Minute))
	if got := svc.notifyQueue.Depth(); got != 0 {
		t.Fatalf("pre-coalescing queue depth = %d, want 0", got)
	}
	svc.evalScoringChanges(state, now.Add(15*time.Minute))
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("coalesced queue depth = %d, want 1", got)
	}
	hash := scoringHash8(scoringValuesForState(state))
	if got := sentLogCount(state, "scoring:"); got != 0 {
		t.Fatalf("stale state unexpectedly carried scoring ledger = %d", got)
	}
	if got := sentLogCount(svc.store.Snapshot(), "scoring:"); got != 1 || !strings.Contains(keyScoringChange(hash, "a@example.com"), hash) {
		t.Fatalf("scoring ledger/hash = %d/%q", got, hash)
	}
	svc.evalScoringChanges(svc.store.Snapshot(), now.Add(30*time.Minute))
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("repeat scoring queue depth = %d, want 1", got)
	}

	message := svc.buildScoringChange(Member{Email: "a@example.com"}, hash, scoringChangeRows(scoringValuesForState(state)))
	if !strings.Contains(message.Text, "scoring") || !strings.Contains(message.Text, "/scoring") {
		t.Fatalf("scoring message omitted truthful route/copy: %q", message.Text)
	}
}

func TestNT1SeasonKickoffIsScheduleGatedAndFreshOnly(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	t.Setenv("SEASON_START_AT", start.Format(time.RFC3339))
	svc, _ := newNotifyTestService(t, now.Add(-24*time.Hour), now)
	if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	svc.evalSeasonKickoff(svc.store.Snapshot(), now)
	if got := svc.notifyQueue.Depth(); got != 0 {
		t.Fatalf("schedule-gated kickoff queue depth without schedule = %d", got)
	}
	if err := svc.store.SetSchedule(nt1Schedule(svc.cfg.Season, now, false)); err != nil {
		t.Fatal(err)
	}
	svc.evalSeasonKickoff(svc.store.Snapshot(), now)
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("kickoff-in-window queue depth = %d, want 1", got)
	}
	svc.evalSeasonKickoff(svc.store.Snapshot(), now.Add(2*time.Hour))
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("post-kickoff queue depth = %d, want 1", got)
	}
	if got := sentLogCount(svc.store.Snapshot(), "kickoff:"); got != 1 {
		t.Fatalf("kickoff ledger = %d, want one durable key", got)
	}
}

func TestNT1MatchupRecapUsesFinalWeekAndRouteAwareLink(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(-24*time.Hour), now)
	if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	week := nt1Schedule(svc.cfg.Season, now, true).Weeks[0]
	if err := svc.store.SetSchedule(SeasonSchedule{Season: svc.cfg.Season, GeneratedAt: now, StartWeek: 1, Weeks: []ScheduleWeek{week}}); err != nil {
		t.Fatal(err)
	}
	state := svc.store.Snapshot()
	svc.evalMatchupRecaps(state, now)
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("final recap queue depth = %d, want 1", got)
	}
	svc.evalMatchupRecaps(svc.store.Snapshot(), now)
	if got := svc.notifyQueue.Depth(); got != 1 {
		t.Fatalf("repeat recap queue depth = %d, want 1", got)
	}
	message := svc.buildMatchupRecap(svc.store.Snapshot(), Member{Email: "a@example.com", TeamID: "team-1"}, week)
	if !strings.Contains(message.Text, "week=1") || !strings.Contains(message.Text, "/matchups") {
		t.Fatalf("recap message omitted safe matchup route: %q", message.Text)
	}
}

func TestNT1DisabledTransportDoesNotRecordDerivedEvents(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	svc, _ := newNotifyTestService(t, now.Add(-24*time.Hour), now)
	if _, _, err := svc.store.AssignMember("a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetScoringValue("passTD", 5, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	svc.SetNotifier(nil, false)
	svc.notifierTick(now)
	if got := len(svc.store.Snapshot().SentLog); got != 0 {
		t.Fatalf("disabled notifier wrote %d ledger entries", got)
	}
}

func TestNT1QueueReceiptStillDistinguishesQueuedFromDelivered(t *testing.T) {
	svc := &Service{store: NewStore(filepath.Join(t.TempDir(), "state.json"))}
	queue := notify.New(func(notify.Message) error { return nil }, nil)
	svc.SetNotifier(queue, true)
	receipt := svc.recordAndSend(PersistedState{}, "a@example.com", categoryLeagueNews, "nt1-key", time.Now(), func() renderedNotification {
		return renderedNotification{Key: "nt1-key", Category: categoryLeagueNews, To: "a@example.com"}
	})
	if receipt.Queued != 1 || receipt.QueueDrops != 0 || receipt.TransportNotWired || receipt.TransportDisabled {
		t.Fatalf("queued receipt = %+v", receipt)
	}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(receiptJSON)), "delivered") {
		t.Fatalf("queued receipt claims delivery: %s", receiptJSON)
	}
}

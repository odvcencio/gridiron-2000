package league

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestFinalGameStatsSurviveLedgerCorrectionsAndRestart(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.MakePick("team-1", "p-09", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-1", 1, "QB", "p-09", now); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "bal-buf", Week: 1, Away: "BAL", Home: "BUF", Kickoff: now.Add(-4 * time.Hour), Final: true}}
	})
	allen := normalizePlayerKey("Josh Allen", "QB")
	lamar := normalizePlayerKey("Lamar Jackson", "QB")
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{{Key: allen, Stats: map[string]float64{"passTD": 1}}}, false); err != nil {
		t.Fatal(err)
	}
	// A later vendor revision and a weekly ledger import both disagree.
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{{Key: allen, Stats: map[string]float64{"passTD": 3}}}, false); err != nil {
		t.Fatal(err)
	}
	ledger := []WeekStatLine{
		{Key: allen, Stats: map[string]float64{"passTD": 2, "twoPt": 1}, Source: StatSourceLedger},
		{Key: lamar, Stats: map[string]float64{"passTD": 4, "twoPt": 1}, Source: StatSourceLedger},
		{Key: DSTStatKey("BUF"), Stats: map[string]float64{"dstSack": 2}, Source: StatSourceLedger},
	}
	assertFrozen := func(lines []WeekStatLine) {
		t.Helper()
		byKey := weekStatLinesByKey(lines)
		if got := byKey[allen]; got.Source != StatSourceLiveFinal || got.Stats["passTD"] != 1 || got.Stats["twoPt"] != 1 {
			t.Fatalf("final Josh Allen row changed: %+v", got)
		}
		if got := byKey[lamar]; got.Source != StatSourceLiveFinal || len(got.Stats) != 1 || got.Stats["twoPt"] != 1 {
			t.Fatalf("missing final-box player acquired later points: %+v", got)
		}
		if got := byKey[DSTStatKey("BUF")]; got.Source != StatSourceLedger || got.Stats["dstSack"] != 2 {
			t.Fatalf("absent final D/ST unit blocked mirror scoring: %+v", got)
		}
	}
	assertFrozen(svc.ApplyFinalGameStats(1, ledger))
	svc.SetWeekStatsSource(func(int) []WeekStatLine { return svc.ApplyFinalGameStats(1, ledger) })
	before, _, err := svc.matchupScorer(nil).TeamWeekScore("team-1", 1)
	if err != nil || before != 6 {
		t.Fatalf("final-box score before week close = %v, err %v; want 6", before, err)
	}
	closed, _, err := svc.closeWeek(1, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, matchup := range closed.Matchups {
		if matchup.HomeTeamID == "team-1" && matchup.HomeScore != before || matchup.AwayTeamID == "team-1" && matchup.AwayScore != before {
			t.Fatalf("week close changed final-box score: %+v", matchup)
		}
	}

	// The SQL reload is the real restart boundary; a fresh Store must
	// recover the exact same immutable rows from its scalar collection.
	path := svc.store.filePath
	if err := svc.store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(filepath.Clean(path))
	defer reloaded.Close()
	if err := reloaded.StartupError(); err != nil {
		t.Fatal(err)
	}
	stored := weekStatLinesByKey(reloaded.FinalGameLines(1))
	if stored[allen].Stats["passTD"] != 1 || len(stored[lamar].Stats) != 0 {
		t.Fatalf("reloaded final box changed: %+v", stored)
	}
}

func TestFinalBoxKeepsMirrorOnlyPuntAndDefenseRules(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: "punter|P", Stats: map[string]float64{"puntIn20": 2, "puntYards": 51}},
		{Key: DSTStatKey("BUF"), Stats: map[string]float64{"dstSack": 3}},
	}, false); err != nil {
		t.Fatal(err)
	}
	got := weekStatLinesByKey(svc.ApplyFinalGameStats(1, []WeekStatLine{
		{Key: "punter|P", Stats: map[string]float64{"puntIn20": 1, "puntYards": 140, "puntLong50": 2, "coffinCorner": 1}},
		{Key: DSTStatKey("BUF"), Stats: map[string]float64{"dstSack": 4, "dstForcedFumble": 1}},
	}))
	if punter := got["punter|P"].Stats; punter["puntIn20"] != 2 || punter["puntYards"] != 140 || punter["puntLong50"] != 2 || punter["coffinCorner"] != 1 {
		t.Fatalf("punter lost mirror-only per-punt scoring: %v", punter)
	}
	if dst := got[DSTStatKey("BUF")].Stats; dst["dstSack"] != 3 || dst["dstForcedFumble"] != 1 {
		t.Fatalf("D/ST lost mirror-only forced fumble: %v", dst)
	}
}

func TestCompleteFinalBoxFreezesEveryScoringCategory(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	allen := normalizePlayerKey("Josh Allen", "QB")
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: allen, Stats: map[string]float64{"passTD": 1, "twoPt": 1}},
		{Key: DSTStatKey("BUF"), Stats: map[string]float64{"dstSack": 3, "dstBlockedKick": 1}},
	}, true); err != nil {
		t.Fatal(err)
	}
	ledger := []WeekStatLine{
		{Key: allen, Stats: map[string]float64{"passTD": 2, "twoPt": 0, "returnTD": 1}},
		{Key: DSTStatKey("BUF"), Stats: map[string]float64{"dstSack": 4, "dstBlockedKick": 0, "dstForcedFumble": 2}},
	}
	check := func(lines []WeekStatLine) {
		t.Helper()
		got := weekStatLinesByKey(lines)
		if stats := got[allen].Stats; len(stats) != 2 || stats["passTD"] != 1 || stats["twoPt"] != 1 {
			t.Fatalf("complete final player changed after ledger import: %+v", stats)
		}
		if stats := got[DSTStatKey("BUF")].Stats; len(stats) != 2 || stats["dstSack"] != 3 || stats["dstBlockedKick"] != 1 {
			t.Fatalf("complete final D/ST changed after ledger import: %+v", stats)
		}
	}
	check(svc.ApplyFinalGameStats(1, ledger))
	path := svc.store.filePath
	if err := svc.store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(filepath.Clean(path))
	defer reloaded.Close()
	if err := reloaded.StartupError(); err != nil {
		t.Fatal(err)
	}
	if records := reloaded.finalGameRecords(1); len(records) != 1 || !records[0].Complete {
		t.Fatalf("complete final marker lost after restart: %+v", records)
	}
}

// TestReopenFinalGameStatsAllowsOneMoreAutomaticWrite covers the audit
// finding: RecordFinalGameStats's write-once guard must stay intact for
// ordinary automatic writes (the poller race it exists for), but a
// commissioner-reopened game must accept exactly one more write — the
// correction the reopen exists for — and then be write-once again.
func TestReopenFinalGameStatsAllowsOneMoreAutomaticWrite(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	allen := normalizePlayerKey("Josh Allen", "QB")
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: allen, Stats: map[string]float64{"passTD": 1}},
	}, false); err != nil {
		t.Fatal(err)
	}
	// The ordinary write-once guard: a second automatic write is silently
	// ignored, exactly like TestFinalGameStatsSurviveLedgerCorrectionsAndRestart.
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: allen, Stats: map[string]float64{"passTD": 9}},
	}, false); err != nil {
		t.Fatal(err)
	}
	if got := weekStatLinesByKey(svc.store.FinalGameLines(1))[allen]; got.Stats["passTD"] != 1 {
		t.Fatalf("write-once guard did not hold before reopening: %+v", got)
	}

	if err := svc.store.ReopenFinalGameStats(1, "bal-buf"); err != nil {
		t.Fatalf("ReopenFinalGameStats: %v", err)
	}
	if got := svc.store.FinalGameLines(1); len(got) != 0 {
		t.Fatalf("reopened game still has a frozen box: %+v", got)
	}

	// The correction: exactly one more write must now win.
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: allen, Stats: map[string]float64{"passTD": 2}},
	}, false); err != nil {
		t.Fatal(err)
	}
	if got := weekStatLinesByKey(svc.store.FinalGameLines(1))[allen]; got.Stats["passTD"] != 2 {
		t.Fatalf("corrected write after reopen = %+v, want passTD 2", got)
	}
	// The write-once guard is back in force immediately after the
	// correction lands: a further automatic write does not win.
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: allen, Stats: map[string]float64{"passTD": 99}},
	}, false); err != nil {
		t.Fatal(err)
	}
	if got := weekStatLinesByKey(svc.store.FinalGameLines(1))[allen]; got.Stats["passTD"] != 2 {
		t.Fatalf("write-once guard did not re-arm after the correction: %+v", got)
	}
}

func TestReopenFinalGameStatsRequiresAnExistingRecord(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.ReopenFinalGameStats(1, "no-such-game"); err == nil {
		t.Fatal("expected an error reopening a game with no recorded final box")
	}
}

func TestReopenFinalGameStatsRefusesAPostedWeek(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 1}},
	}, false); err != nil {
		t.Fatal(err)
	}
	before := svc.store.FinalGameLines(1)
	week := schedule.Weeks[0]
	for i := range week.Matchups {
		week.Matchups[i].Final = true
	}
	if err := svc.store.CommitScheduleWeekClose(week, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.ReopenFinalGameStats(1, "bal-buf"); err == nil {
		t.Fatal("expected a refusal reopening a final box in an already-posted week")
	}
	after := svc.store.FinalGameLines(1)
	if len(after) != len(before) {
		t.Fatalf("posted week's final box changed after a refused reopen: before=%+v after=%+v", before, after)
	}
}

func TestAdminReopenFinalGameStatsRequiresCommissioner(t *testing.T) {
	svc := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if err := svc.AdminReopenFinalGameStats(request, 1, "bal-buf"); err == nil {
		t.Fatal("expected a refusal for a non-commissioner request")
	}
}

func TestAdminReopenFinalGameStatsRecordsAuditEvent(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", []WeekStatLine{
		{Key: normalizePlayerKey("Josh Allen", "QB"), Stats: map[string]float64{"passTD": 1}},
	}, false); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if err := svc.AdminReopenFinalGameStats(request, 1, "bal-buf"); err != nil {
		t.Fatalf("AdminReopenFinalGameStats: %v", err)
	}
	if got := svc.store.FinalGameLines(1); len(got) != 0 {
		t.Fatalf("box still frozen after AdminReopenFinalGameStats: %+v", got)
	}
	events := svc.store.Snapshot().CommissionerEvents
	if len(events) != 1 {
		t.Fatalf("commissioner events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Kind != "finalstats.reopen" {
		t.Errorf("event kind = %q, want finalstats.reopen", event.Kind)
	}
	if event.Refs.Week != 1 {
		t.Errorf("event refs.week = %d, want 1", event.Refs.Week)
	}
	if event.Summary == "" {
		t.Error("event summary is empty")
	}
}

func TestFinalGameStatsCannotChangePostedWeek(t *testing.T) {
	svc := newTestService(t, true)
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	week := schedule.Weeks[0]
	for i := range week.Matchups {
		week.Matchups[i].Final = true
	}
	if err := svc.store.CommitScheduleWeekClose(week, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordFinalGameStats(1, "bal-buf", "BAL", "BUF", nil, false); err != nil {
		t.Fatal(err)
	}
	if got := svc.store.FinalGameLines(1); len(got) != 0 {
		t.Fatalf("late final box changed posted week: %+v", got)
	}
}

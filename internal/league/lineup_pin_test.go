package league

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

// ---------------------------------------------------------------------
// WP-R2: materialize-at-close (roster-ops spec section 4.2), the
// lineupScorer closed-week short-circuit (section 4.6 step 1), and the
// N18 warning email (tested separately in notifications_test.go).
// ---------------------------------------------------------------------

// pinTestService builds on newLineupTestService's small fixture (team-1
// drafts rb-open, rb-locked, wr-open, wr-bench, qb-open, in that order —
// qb-open is the LAST pick, which lets a test remove exactly that player
// via Store.UndoLastPick) and adds a one-week schedule pairing team-1
// against some opponent, so closeWeek has a target week to close. Returns
// the service, the week number (1), and the fixed clock instant
// newLineupTestService armed (before PIT's kickoff, after TB's).
func pinTestService(t *testing.T) (svc *Service, week int, now time.Time) {
	t.Helper()
	svc, _, now = newLineupTestService(t)
	sched, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: teamIDList(svc.teams), Divisions: teamDivisionMap(svc.teams),
		StartWeek: 1, Weeks: 1, Seed: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(sched); err != nil {
		t.Fatal(err)
	}
	return svc, 1, now
}

// pinFixtureStats scores qb-open's single passTD (4 points by default) —
// the only starter these tests' stat source recognizes.
func pinFixtureStats(week int) []WeekStatLine {
	return []WeekStatLine{
		{Key: normalizePlayerKey("Open Passer", "QB"), Stats: map[string]float64{"passTD": 1}},
	}
}

// TestCloseWeekMaterializesLineupPin pins the persisted shape (roster-ops
// spec section 4.2): closeWeek writes Lineups[teamID][week] for every
// matchup team, matching effectiveLineup's resolution at close time
// exactly, and marks the matchup final.
func TestCloseWeekMaterializesLineupPin(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}

	// The expected pin: effectiveLineup's own resolution, computed
	// independently against the same inputs closeWeek uses.
	preset := CurrentRoster()
	state := svc.store.Snapshot()
	roster, _ := svc.rosterForTeam(state, "team-1")
	games := svc.schedule()
	want := effectiveLineup(preset, roster, state.Lineups["team-1"], week, games, now)

	svc.SetWeekStatsSource(pinFixtureStats)
	updated, _, err := svc.closeWeek(week, now)
	if err != nil {
		t.Fatal(err)
	}

	pinned := svc.store.Snapshot().Lineups["team-1"][week]
	if pinned == nil {
		t.Fatal("expected a materialized pin for team-1's closed week")
	}
	wantSlots := 0
	for _, a := range want.Slots {
		if !a.HasPlayer {
			continue
		}
		wantSlots++
		if pinned[a.Slot.ID] != a.Player.ID {
			t.Errorf("pin[%s] = %q, want %q", a.Slot.ID, pinned[a.Slot.ID], a.Player.ID)
		}
	}
	if len(pinned) != wantSlots {
		t.Errorf("pin has %d entries, want %d (exactly the filled slots)", len(pinned), wantSlots)
	}
	if pinned["QB"] != "qb-open" {
		t.Fatalf("pin[QB] = %q, want qb-open", pinned["QB"])
	}

	foundFinal := false
	for _, m := range updated.Matchups {
		if m.HomeTeamID == "team-1" || m.AwayTeamID == "team-1" {
			if !m.Final {
				t.Errorf("team-1's matchup was not marked final: %+v", m)
			}
			foundFinal = true
		}
	}
	if !foundFinal {
		t.Fatal("team-1 did not appear in any closed matchup")
	}
}

// TestClosedWeekScoreSurvivesPostCloseRosterMutation is the immutability
// property the pin exists for: once a week closes, dropping the pinned
// starter from the roster (Store.UndoLastPick removes qb-open, team-1's
// last pick in this fixture's draft order) must not change that closed
// week's TeamWeekScore.
func TestClosedWeekScoreSurvivesPostCloseRosterMutation(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(pinFixtureStats)
	if _, _, err := svc.closeWeek(week, now); err != nil {
		t.Fatal(err)
	}

	before, _, err := svc.matchupScorer(nil).TeamWeekScore("team-1", week)
	if err != nil {
		t.Fatal(err)
	}
	if before != 4 { // qb-open's one passTD * the default 4-point value
		t.Fatalf("pre-mutation closed-week score = %v, want 4", before)
	}

	// Roster mutation after close: qb-open leaves team-1's roster entirely.
	if err := svc.store.UndoLastPick(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if roster, _ := svc.rosterForTeam(svc.store.Snapshot(), "team-1"); len(roster) != 4 {
		t.Fatalf("roster after UndoLastPick = %d players, want 4 (qb-open removed)", len(roster))
	}

	after, _, err := svc.matchupScorer(nil).TeamWeekScore("team-1", week)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("closed-week score changed after a roster mutation: before=%v after=%v", before, after)
	}
}

// TestOpenWeekScoringResolvesLiveRoster is the contrapositive: for a week
// that has not closed, the same roster mutation DOES change the score —
// proving the pin's short-circuit is closed-week-only, not a blanket
// freeze.
func TestOpenWeekScoringResolvesLiveRoster(t *testing.T) {
	svc, week, _ := pinTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(pinFixtureStats)

	before, _, err := svc.matchupScorer(nil).TeamWeekScore("team-1", week)
	if err != nil {
		t.Fatal(err)
	}
	if before != 4 {
		t.Fatalf("pre-mutation open-week score = %v, want 4", before)
	}

	if err := svc.store.UndoLastPick(time.Time{}); err != nil {
		t.Fatal(err)
	}

	after, _, err := svc.matchupScorer(nil).TeamWeekScore("team-1", week)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatalf("open-week score must resolve live: still %v after qb-open left the roster", after)
	}
	if after != 0 {
		t.Fatalf("open-week score after the QB left with no replacement = %v, want 0", after)
	}
}

// TestCloseWeekIsIdempotent pins section 13's "closing an already-closed
// week is a no-op" requirement: a second close leaves both the persisted
// scores and the pin's bytes unchanged, byte-for-byte.
func TestCloseWeekIsIdempotent(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(pinFixtureStats)

	first, _, err := svc.closeWeek(week, now)
	if err != nil {
		t.Fatal(err)
	}
	firstPin := svc.store.Snapshot().Lineups["team-1"][week]

	second, _, err := svc.closeWeek(week, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Matchups, second.Matchups) {
		t.Fatalf("a second close changed the matchups: first=%+v second=%+v", first.Matchups, second.Matchups)
	}
	secondPin := svc.store.Snapshot().Lineups["team-1"][week]
	if !reflect.DeepEqual(firstPin, secondPin) {
		t.Fatalf("a second close changed the pin: first=%v second=%v", firstPin, secondPin)
	}
}

// TestCloseWeekIsIdempotentAcrossRosterMutation strengthens the idempotent
// guard: even when the roster changes between two close calls, the second
// close must not rescore or re-pin against the mutated roster.
func TestCloseWeekIsIdempotentAcrossRosterMutation(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(pinFixtureStats)

	first, _, err := svc.closeWeek(week, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.UndoLastPick(time.Time{}); err != nil {
		t.Fatal(err)
	}

	second, _, err := svc.closeWeek(week, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Matchups, second.Matchups) {
		t.Fatalf("a second close after a roster mutation must not rescore: first=%+v second=%+v", first.Matchups, second.Matchups)
	}
	pin := svc.store.Snapshot().Lineups["team-1"][week]
	if pin["QB"] != "qb-open" {
		t.Fatalf("the pin must survive both the roster mutation and the re-close: QB = %q, want qb-open", pin["QB"])
	}
}

// TestWeekIsFinalInSchedule pins the pure "is this week closed" predicate
// scorer.go's pinnedStarters gates on: nil schedule, an absent week, a
// week with no matchups, a partially-final week, and a fully final week.
func TestWeekIsFinalInSchedule(t *testing.T) {
	if weekIsFinalInSchedule(nil, 1) {
		t.Error("a nil schedule must never read as closed")
	}
	sch := &SeasonSchedule{Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{Final: true}, {Final: true}}},
		{Week: 2, Matchups: []LeagueMatchup{{Final: true}, {Final: false}}},
		{Week: 3, Matchups: nil},
	}}
	if !weekIsFinalInSchedule(sch, 1) {
		t.Error("week 1 (every matchup final) must read as closed")
	}
	if weekIsFinalInSchedule(sch, 2) {
		t.Error("week 2 (one matchup not final) must not read as closed")
	}
	if weekIsFinalInSchedule(sch, 3) {
		t.Error("week 3 (no matchups) must not read as closed")
	}
	if weekIsFinalInSchedule(sch, 99) {
		t.Error("a week absent from the schedule must not read as closed")
	}
}

// TestPinnedStartersIgnoresRosterChurn is the pinnedStarters unit-level
// proof: once week is final, it resolves the pin against the whole player
// pool, not against currentRosters — a player absent from the roster
// argument (simulating a post-close drop/trade) still resolves as long as
// the pool still carries the ID.
func TestPinnedStartersIgnoresRosterChurn(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return lineupFixturePlayers(), 1, "test" })
	if _, err := svc.store.MakePick("team-1", "qb-open", "manager", time.Now(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-1", 1, "QB", "qb-open", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Undo the pick — team-1's roster no longer carries qb-open — but the
	// pool (buildPool keys every configured player by ID regardless of
	// roster ownership) still resolves the ID.
	if err := svc.store.UndoLastPick(time.Time{}); err != nil {
		t.Fatal(err)
	}

	sch := &SeasonSchedule{Weeks: []ScheduleWeek{
		{Week: 1, Matchups: []LeagueMatchup{{HomeTeamID: "team-1", AwayTeamID: "team-2", Final: true}}},
	}}
	if err := svc.store.SetSchedule(*sch); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetScheduleWeek((*sch).Weeks[0]); err != nil {
		t.Fatal(err)
	}

	starters, ok := svc.pinnedStarters(svc.store.Snapshot(), "team-1", 1)
	if !ok {
		t.Fatal("pinnedStarters must report ok once the week is final")
	}
	if len(starters) != 1 || starters[0].ID != "qb-open" {
		t.Fatalf("starters = %+v, want exactly qb-open despite the roster no longer carrying it", starters)
	}
}

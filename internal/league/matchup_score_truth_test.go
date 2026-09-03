package league

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTeamWeekLedgerTotalEqualsTeamWeekScore(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	state := svc.store.Snapshot()
	lineup, _ := svc.matchupLineup(state, "team-1", 1)
	stats := make([]WeekStatLine, 0, len(lineup.Slots))
	for _, assignment := range lineup.Slots {
		if !assignment.HasPlayer {
			continue
		}
		line := WeekStatLine{Key: normalizePlayerKey(assignment.Player.Name, assignment.Player.Position), Stats: map[string]float64{}}
		if assignment.Player.ID == "p-01" {
			line.Stats["recTD"] = 1
		}
		stats = append(stats, line)
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return stats
	})
	ledger := svc.teamWeekLedger(state, "team-1", 1)
	got, _, err := svc.matchupScorer(nil).TeamWeekScore("team-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !ledger.Known || ledger.Total != got || ledger.TotalText != "6.0" {
		t.Fatalf("ledger = %+v, TeamWeekScore = %v; want one shared 6-point calculation", ledger, got)
	}
	rowTotal := 0.0
	for _, row := range ledger.Rows {
		rowTotal += row.Points
	}
	if rowTotal != ledger.Total {
		t.Fatalf("starter row total = %v, ledger total = %v", rowTotal, ledger.Total)
	}
	for _, row := range ledger.Rows {
		if row.PlayerID == "p-01" {
			if row.JoinState != "matched" || row.Provenance != "auto-filled" {
				t.Fatalf("p-01 row = %+v, want matched auto-filled provenance", row)
			}
			return
		}
	}
	t.Fatal("ledger omitted the effective p-01 starter")
}

func TestTeamWeekLedgerReportsMissingJoinInsteadOfSilentZero(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Another Player", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	if ledger.Known || ledger.SourceState != "partial" {
		t.Fatalf("partial ledger availability = known:%v source:%q, want unknown partial", ledger.Known, ledger.SourceState)
	}
	for _, row := range ledger.Rows {
		if row.PlayerID == "p-01" {
			if row.JoinState != "missing-join" || !strings.Contains(row.Detail, "No matching player-stat row") || row.PointsText != "0.0" {
				t.Fatalf("missing join row = %+v", row)
			}
			return
		}
	}
	t.Fatal("ledger omitted p-01 while reporting the join miss")
}

// TestStarterGameKnownZeroSoFar covers rider item 1 (review of ae1a525):
// a missing-join starter's game must read as an honest, KNOWN 0.0 only
// when a healthy signal affirmatively places it pre-kickoff or in
// progress, and false — leaving the team total UNKNOWN — for a Final
// game, a degraded poller (even one that otherwise has an in-progress
// entry for the team), or no signal at all. It also covers rider item 3
// (review of ff2a9b3): a true bye (the team absent from a loaded,
// non-empty schedule) is a known 0.0 regardless of poller health, and
// the two residuals from the review of eb549b6: N1, a stale ByeWeek that
// happens to equal week must never claim BYE (or bypass the poller's own
// Degraded check) while the team's real game sits in the loaded
// schedule; N2, a pool with no bye data at all (ByeWeek == 0) must still
// read a genuine bye correctly — the loaded schedule's own team presence
// is the one signal that matters, not ByeWeek.
func TestStarterGameKnownZeroSoFar(t *testing.T) {
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	player := Player{NFLTeam: "CIN"}
	byePlayer := Player{NFLTeam: "CIN", ByeWeek: 1}
	// staleByeWeekPlayer's ByeWeek (1) matches the week under test even
	// though CIN's real game sits in the loaded schedule (N1) — the
	// opposite shape from byePlayer, which has no game in the loaded
	// schedule at all.
	staleByeWeekPlayer := Player{NFLTeam: "CIN", ByeWeek: 1}
	// noByeDataPlayer models a pool with no bye field populated at all
	// (ByeWeek's zero value, the offline/fallback pool's own shape, N2):
	// CIN is genuinely absent from the loaded schedule below, and that
	// absence alone — not ByeWeek — must still read as a known bye.
	noByeDataPlayer := Player{NFLTeam: "CIN"}
	cases := []struct {
		name     string
		player   Player
		snapshot matchupStatsSnapshot
		want     bool
	}{
		{
			name:     "schedule pre-kickoff, no poller wired",
			player:   player,
			snapshot: matchupStatsSnapshot{games: []GameInfo{{Away: "CIN", Home: "CLE", Kickoff: now.Add(time.Hour)}}},
			want:     true,
		},
		{
			name:     "schedule final, no poller wired",
			player:   player,
			snapshot: matchupStatsSnapshot{games: []GameInfo{{Away: "CIN", Home: "CLE", Kickoff: now.Add(-3 * time.Hour), Final: true}}},
			want:     false,
		},
		{
			name:     "live in progress, healthy poller",
			player:   player,
			snapshot: matchupStatsSnapshot{hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{"CIN": {InProgress: true}}}},
			want:     true,
		},
		{
			name:     "live final",
			player:   player,
			snapshot: matchupStatsSnapshot{hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{"CIN": {Final: true}}}},
			want:     false,
		},
		{
			name:     "poller degraded, even with an in-progress entry",
			player:   player,
			snapshot: matchupStatsSnapshot{hasLive: true, live: LiveStatus{Degraded: true, Games: map[string]LiveGameState{"CIN": {InProgress: true}}}},
			want:     false,
		},
		{
			name:     "no signal at all",
			player:   player,
			snapshot: matchupStatsSnapshot{},
			want:     false,
		},
		{
			name:     "true bye: loaded schedule omits the team, healthy poller",
			player:   byePlayer,
			snapshot: matchupStatsSnapshot{games: []GameInfo{{Away: "BUF", Home: "MIA", Kickoff: now.Add(-time.Hour)}}, hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{"BUF": {InProgress: true}, "MIA": {InProgress: true}}}},
			want:     true,
		},
		{
			name:     "true bye: known even while the poller reports degraded",
			player:   byePlayer,
			snapshot: matchupStatsSnapshot{games: []GameInfo{{Away: "BUF", Home: "MIA", Kickoff: now.Add(-time.Hour)}}, hasLive: true, live: LiveStatus{Degraded: true}},
			want:     true,
		},
		{
			name:     "bye-week match but no schedule loaded at all: not a league-wide bye",
			player:   byePlayer,
			snapshot: matchupStatsSnapshot{},
			want:     false,
		},
		{
			name:     "N1: stale ByeWeek matches week, but the team's real game sits in the loaded schedule, degraded poller",
			player:   staleByeWeekPlayer,
			snapshot: matchupStatsSnapshot{games: []GameInfo{{Away: "CIN", Home: "CLE", Kickoff: now.Add(-time.Hour)}}, hasLive: true, live: LiveStatus{Degraded: true, Games: map[string]LiveGameState{"CIN": {InProgress: true}}}},
			want:     false,
		},
		{
			name:     "N2: no bye data at all (ByeWeek zero value), but the team is genuinely absent from the loaded schedule",
			player:   noByeDataPlayer,
			snapshot: matchupStatsSnapshot{games: []GameInfo{{Away: "BUF", Home: "MIA", Kickoff: now.Add(-time.Hour)}}, hasLive: true, live: LiveStatus{Games: map[string]LiveGameState{"BUF": {InProgress: true}, "MIA": {InProgress: true}}}},
			want:     true,
		},
		{
			name:     "N1 abbreviation normalization: player carries Tank01-style LAR, schedule already normalized to LA",
			player:   Player{NFLTeam: "LAR", ByeWeek: 1},
			snapshot: matchupStatsSnapshot{games: []GameInfo{{Away: "LA", Home: "SF", Kickoff: now.Add(-time.Hour)}}, hasLive: true, live: LiveStatus{Degraded: true}},
			want:     false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := starterGameKnownZeroSoFar(c.player, 1, c.snapshot, now); got != c.want {
				t.Fatalf("starterGameKnownZeroSoFar = %v, want %v", got, c.want)
			}
		})
	}
}

// TestTeamHasGameNormalizesTank01Abbreviations covers the abbreviation
// normalization residual N1 names explicitly: teamHasGame must match a
// Tank01-style team code (LAR, WSH, JAC) against an nflverse-style
// schedule entry (LA, WAS, JAX) and vice versa, the same three-entry
// correction internal/livescore.NormalizeTeam applies, kept as its own
// copy here since internal/league must not import internal/livescore.
func TestTeamHasGameNormalizesTank01Abbreviations(t *testing.T) {
	games := []GameInfo{{Away: "LA", Home: "SF"}, {Away: "BUF", Home: "MIA"}}
	cases := []struct {
		team string
		want bool
	}{
		{"LAR", true},  // Tank01-style, schedule already normalized to LA
		{"LA", true},   // already nflverse-style
		{"lar", true},  // case-insensitive
		{"SF", true},   // untouched by the three-entry map
		{"WSH", false}, // not in this schedule at all
		{"DAL", false},
	}
	for _, c := range cases {
		if got := teamHasGame(c.team, games); got != c.want {
			t.Errorf("teamHasGame(%q) = %v, want %v", c.team, got, c.want)
		}
	}
}

// TestStarterGameStateNormalizesTank01Abbreviations is item 2's own
// regression test (2026-08-31 post-wave audit): a LAR player's game clock
// (the kickoff-time/live-clock text /team, /players, and /board render
// beside the opponent) must resolve against an nflverse-normalized "LA"
// schedule entry, the same normalization teamHasGame already applies
// (TestTeamHasGameNormalizesTank01Abbreviations, above) but
// starterGameState/starterGameNotStarted did not.
func TestStarterGameStateNormalizesTank01Abbreviations(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	location := time.UTC
	kickoff := now.Add(2 * time.Hour)
	player := Player{NFLTeam: "LAR"} // Tank01-style abbreviation, real pool shape
	snapshot := matchupStatsSnapshot{
		games: []GameInfo{{Away: "LA", Home: "SF", Week: 1, Kickoff: kickoff}}, // nflverse-normalized
	}
	want := strings.ToUpper(kickoff.In(location).Format("Mon 3:04 PM"))
	if got := starterGameState(player, 1, snapshot, location); got != want {
		t.Fatalf("starterGameState(LAR) = %q, want %q (schedule carries LA, not LAR)", got, want)
	}
	if !starterGameNotStarted("LAR", snapshot, now) {
		t.Fatalf("starterGameNotStarted(LAR) = false, want true (kickoff is 2h in the future)")
	}
}

// TestStarterGameStateRendersBYEOnlyForATrueBye covers the "no BYE chip"
// half of residual N1 (review of eb549b6): starterGameState must not
// render "BYE" for a starter whose ByeWeek happens to equal week when
// their team's real game sits in the loaded schedule, and must render
// "BYE" for a starter whose team is genuinely absent from a loaded
// schedule, independent of ByeWeek (residual N2).
func TestStarterGameStateRendersBYEOnlyForATrueBye(t *testing.T) {
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	location := time.UTC
	staleByeWeekPlayer := Player{NFLTeam: "CIN", ByeWeek: 1}
	snapshotTeamPresent := matchupStatsSnapshot{
		games:   []GameInfo{{Away: "CIN", Home: "CLE", Kickoff: now.Add(-time.Hour)}},
		hasLive: true,
		live:    LiveStatus{Degraded: true, Games: map[string]LiveGameState{"CIN": {InProgress: true}}},
	}
	if got := starterGameState(staleByeWeekPlayer, 1, snapshotTeamPresent, location); got == "BYE" {
		t.Fatalf("starterGameState (N1, stale ByeWeek, team present) = %q, want anything but BYE", got)
	}

	noByeDataPlayer := Player{NFLTeam: "CIN"}
	snapshotTeamAbsent := matchupStatsSnapshot{
		games: []GameInfo{{Away: "BUF", Home: "MIA", Kickoff: now.Add(-time.Hour)}},
	}
	if got := starterGameState(noByeDataPlayer, 1, snapshotTeamAbsent, location); got != "BYE" {
		t.Fatalf("starterGameState (N2, no ByeWeek data, team genuinely absent) = %q, want BYE", got)
	}
}

// draftSecondPickForTeam1 advances the (bypassed-lifecycle) draft with
// arbitrary filler picks for every other team until team-1's own second
// pick comes up (the last slot of snake round 2, pick 2*len(defaultTeams())),
// then makes that pick for team-1 with playerID. Store.MakePick enforces
// strict turn order regardless of draftLifecycleBypass, so team-1 cannot
// receive a second roster player without every intervening pick landing
// somewhere first; Store.MakePick never validates a playerID against any
// player pool, so the filler IDs below are never looked up.
func draftSecondPickForTeam1(t *testing.T, svc *Service, now time.Time, playerID string) {
	t.Helper()
	target := 2 * len(defaultTeams())
	for number := 2; number < target; number++ {
		teamID := teamOnClock(nil, number)
		if _, err := svc.store.MakePick(teamID, fmt.Sprintf("filler-%02d", number), "manager", now, time.Time{}); err != nil {
			t.Fatalf("filler pick %d for %s: %v", number, teamID, err)
		}
	}
	if _, err := svc.store.MakePick("team-1", playerID, "manager", now, time.Time{}); err != nil {
		t.Fatalf("team-1 second pick %s: %v", playerID, err)
	}
}

// TestTeamWeekLedgerCountsPreKickoffMissingJoinAsKnownZero is rider test
// (a): a lone starter whose game has not kicked off yet, with no ledger
// join, still yields a KNOWN 0.0 team total — not the hours-long "—" the
// pre-rider rule produced for every game of the slate.
func TestTeamWeekLedgerCountsPreKickoffMissingJoinAsKnownZero(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil { // Ja'Marr Chase, CIN
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "cin-game", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "CIN", Home: "CLE"}}
	})
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Someone Else", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	if !ledger.Known || ledger.TotalText != "0.0" || ledger.Total != 0 {
		t.Fatalf("pre-kickoff ledger = %+v, want a known 0.0 total", ledger)
	}
	for _, row := range ledger.Rows {
		if row.PlayerID == "p-01" {
			if row.JoinState != "missing-join" {
				t.Fatalf("p-01 row = %+v, want missing-join", row)
			}
			return
		}
	}
	t.Fatal("ledger omitted p-01")
}

// TestTeamWeekLedgerSumsMatchedAndInProgressKnownZeroRows is rider test
// (b): one matched starter plus one missing-join starter whose game a
// healthy poller reports in progress still yields a KNOWN total equal to
// the matched sum alone (the missing-join row's honest 0.0 changes
// nothing numerically, only whether the aggregate itself is trusted).
func TestTeamWeekLedgerSumsMatchedAndInProgressKnownZeroRows(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil { // Ja'Marr Chase, CIN
		t.Fatal(err)
	}
	draftSecondPickForTeam1(t, svc, now, "p-02") // Bijan Robinson, ATL
	svc.SetLiveStatusSource(func() LiveStatus {
		return LiveStatus{Enabled: true, Games: map[string]LiveGameState{"ATL": {GameID: "atl-game", Period: "Q2", Clock: "5:00", InProgress: true}}}
	})
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	if !ledger.Known || ledger.TotalText != "6.0" || ledger.Total != 6 {
		t.Fatalf("mixed matched/in-progress ledger = %+v, want a known 6.0 total", ledger)
	}
	var sawMatched, sawKnownZero bool
	for _, row := range ledger.Rows {
		switch row.PlayerID {
		case "p-01":
			sawMatched = true
			if row.JoinState != "matched" || row.Points != 6 {
				t.Fatalf("p-01 row = %+v, want a matched 6-point row", row)
			}
		case "p-02":
			sawKnownZero = true
			if row.JoinState != "missing-join" {
				t.Fatalf("p-02 row = %+v, want missing-join", row)
			}
		}
	}
	if !sawMatched || !sawKnownZero {
		t.Fatalf("rows seen: matched=%v knownZero=%v (%+v)", sawMatched, sawKnownZero, ledger.Rows)
	}
}

// TestTeamWeekLedgerStaysUnknownOnDegradedPoller is rider test (c): a
// degraded poller must keep the team total UNKNOWN even for a starter
// whose game the schedule alone would otherwise place pre-kickoff — a
// known outage is not the same claim as "affirmatively known" — and the
// resulting winProbabilityText for that side must render the same
// honest dash, never a borrowed percentage.
func TestTeamWeekLedgerStaysUnknownOnDegradedPoller(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil { // Ja'Marr Chase, CIN
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "cin-game", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "CIN", Home: "CLE"}}
	})
	svc.SetLiveStatusSource(func() LiveStatus {
		return LiveStatus{Enabled: true, Degraded: true, Reason: "daily budget exhausted"}
	})
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Someone Else", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	if ledger.Known || ledger.TotalText != "—" {
		t.Fatalf("degraded ledger = %+v, want an unknown dash total", ledger)
	}
	if got := winProbabilityText(50, 40, ledger.Known, true); got != "—" {
		t.Fatalf("winProbabilityText for the degraded/unknown side = %q, want the dash", got)
	}
	if got := projectedText(50, ledger.Known); got != "—" {
		t.Fatalf("projectedText for the degraded/unknown side = %q, want the dash, never a proj figure beside the dash score", got)
	}
}

// TestTeamWeekLedgerCountsByeWeekStarterAsKnownZeroWithBadge is rider
// item 3 (review of ff2a9b3): a starter on a true bye — no matched stat
// line, healthy poller, a loaded week-1 schedule that never mentions
// their team — still yields a KNOWN, numeric team total and renders
// GameState "BYE" for that row (which .state:empty never hides, since
// "BYE" is non-empty text).
func TestTeamWeekLedgerCountsByeWeekStarterAsKnownZeroWithBadge(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	byePlayer := Player{ID: "p-bye", Name: "Bye Week Wideout", Position: "WR", NFLTeam: "CIN", ByeWeek: 1}
	pool := append(append([]Player{}, defaultPlayers()...), byePlayer)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "test" })
	if _, err := svc.store.MakePick("team-1", "p-bye", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	// A loaded, non-empty week-1 schedule that never mentions CIN: the
	// starterOnBye guard (len(snapshot.games) > 0) requires a real
	// schedule, not merely "no signal at all" for this player.
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "other-game", Week: 1, Kickoff: now.Add(-time.Hour), Away: "BUF", Home: "MIA"}}
	})
	svc.SetLiveStatusSource(func() LiveStatus {
		return LiveStatus{Enabled: true, Games: map[string]LiveGameState{"BUF": {InProgress: true}, "MIA": {InProgress: true}}}
	})
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Someone Else", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	if !ledger.Known || ledger.TotalText != "0.0" {
		t.Fatalf("bye-week ledger = %+v, want a known 0.0 total", ledger)
	}
	for _, row := range ledger.Rows {
		if row.PlayerID == "p-bye" {
			if row.GameState != "BYE" || row.JoinState != "missing-join" {
				t.Fatalf("bye-week row = %+v, want GameState \"BYE\" and missing-join", row)
			}
			return
		}
	}
	t.Fatal("ledger omitted the bye-week starter")
}

func TestMatchupsDataCarriesStarterLedgerAndUnavailableScoreState(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 19})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "active", Week: 1, Kickoff: now.Add(-time.Hour)}}
	})
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0
	data := svc.MatchupsData(context.Background(), matchupDataRequest(t, "/matchups"))
	matchups, ok := data["matchups"].([]map[string]any)
	if !ok || len(matchups) == 0 {
		t.Fatalf("matchups = %#v, want a populated typed map slice", data["matchups"])
	}
	for _, matchup := range matchups {
		for _, side := range []string{"away", "home"} {
			team, _ := matchup[side].(map[string]any)
			if _, ok := team["score_known"].(bool); !ok {
				t.Fatalf("%s side omitted score_known: %#v", side, team)
			}
			if _, ok := team["starters"].([]map[string]any); !ok {
				t.Fatalf("%s side omitted typed starters: %#v", side, team["starters"])
			}
		}
	}
	// No WeekStatsSource is wired: an active matchup must expose an unknown
	// score marker, never claim an official 0.0 while rows are unavailable.
	firstAway, _ := matchups[0]["away"].(map[string]any)
	if firstAway["score"] != "—" {
		t.Fatalf("unavailable active score = %#v, want an honest em dash", firstAway["score"])
	}
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 1}}}
	})
	view := svc.LiveScoresView(context.Background())
	points, ok := view["starterPoints"].(map[string]string)
	if !ok {
		t.Fatalf("live starterPoints = %#v, want typed one-level binding map", view["starterPoints"])
	}
	found := false
	for key, value := range points {
		if strings.HasPrefix(key, "team-1_") && value == "6.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("live starterPoints = %#v, want team-1's matched starter to update", points)
	}
}

type starterRowSequenceProvider struct {
	snapshots []LiveSnapshot
	index     int
}

func (p *starterRowSequenceProvider) Snapshot(context.Context, time.Time) (LiveSnapshot, error) {
	if len(p.snapshots) == 0 {
		return LiveSnapshot{}, nil
	}
	index := p.index
	if index >= len(p.snapshots) {
		index = len(p.snapshots) - 1
	}
	p.index++
	return p.snapshots[index], nil
}

func TestLiveScoresViewStarterRowsUpdateEveryFieldForIdentityAndJoinTransitions(t *testing.T) {
	row := func(slot, playerID, playerName, position, nflTeam, points, provenance, joinState, detail string) StarterLedgerRow {
		return StarterLedgerRow{
			LiveKey:    "team-1_" + slot,
			Slot:       slot,
			PlayerID:   playerID,
			PlayerName: playerName,
			Position:   position,
			NFLTeam:    nflTeam,
			PointsText: points,
			Provenance: provenance,
			JoinState:  joinState,
			Detail:     detail,
		}
	}
	snapshot := func(qb, rb StarterLedgerRow) LiveSnapshot {
		return LiveSnapshot{
			OK:          true,
			Source:      "test",
			SourceLabel: "Test source",
			Week:        1,
			WeekLabel:   "Week 1",
			State:       MatchupStateInProgress,
			Status:      "LIVE",
			Matchups: []ScoreMatchup{{
				ID:   "matchup-1",
				Away: ScoreTeam{ID: "team-1", StarterLedger: []StarterLedgerRow{qb, rb}},
				Home: ScoreTeam{ID: "team-2"},
			}},
		}
	}
	provider := &starterRowSequenceProvider{snapshots: []LiveSnapshot{
		snapshot(
			row("QB", "p-a", "Player A", "QB", "AAA", "0.0", "explicit", "missing-join", "No matching player-stat row for Player A."),
			row("RB1", "", "Empty slot", "RB", "", "0.0", "empty", "empty", "No player configured in this starting slot."),
		),
		snapshot(
			row("QB", "p-b", "Player B", "QB", "BBB", "6.0", "auto-filled", "matched", "Matched current player-stat row."),
			row("RB1", "p-c", "Player C", "RB", "CCC", "3.0", "explicit", "matched", "Matched current player-stat row."),
		),
	}}
	svc := newTestService(t, true)
	svc.feed = newLiveFeed(provider, svc)
	svc.feed.cacheFor = 0

	fieldValues := func(view map[string]any, key string) map[string]string {
		values, ok := view[key].(map[string]string)
		if !ok {
			t.Fatalf("live view field %q = %#v, want a typed one-level binding map", key, view[key])
		}
		return values
	}
	assertView := func(view map[string]any, want map[string][2]string) {
		for field, expected := range want {
			values := fieldValues(view, field)
			if values["team-1_QB"] != expected[0] || values["team-1_RB1"] != expected[1] {
				t.Errorf("live view %s = %#v, want QB=%q RB1=%q", field, values, expected[0], expected[1])
			}
		}
	}
	// starterProvenance/starterJoinState stay the RAW StarterLedgerRow
	// tokens this fixture's row() helper stores (a caller that matches
	// join provenance verbatim, e.g. league.StatSourceLive, needs the
	// exact token); starterProvenanceText/starterJoinStateText carry
	// ledgerLineupText/ledgerStatsText's already-labelled words instead
	// (wave-8 audit item 3) — the page renders the *Text fields, never
	// these raw ones.
	wantFields := map[string][2]string{
		"starterPoints":         {"0.0", "0.0"},
		"starterPlayerName":     {"Player A", "Empty slot"},
		"starterPosition":       {"QB", "RB"},
		"starterNFLTeam":        {"AAA", ""},
		"starterProvenance":     {"explicit", "empty"},
		"starterJoinState":      {"missing-join", "empty"},
		"starterProvenanceText": {"Lineup: set by the manager", "Lineup: no player in this slot"},
		"starterJoinStateText":  {" · Stats: no stat row yet", ""},
		"starterDetail":         {"No matching player-stat row for Player A.", "No player configured in this starting slot."},
	}
	assertView(svc.LiveScoresView(context.Background()), wantFields)
	wantFields = map[string][2]string{
		"starterPoints":         {"6.0", "3.0"},
		"starterPlayerName":     {"Player B", "Player C"},
		"starterPosition":       {"QB", "RB"},
		"starterNFLTeam":        {"BBB", "CCC"},
		"starterProvenance":     {"auto-filled", "explicit"},
		"starterJoinState":      {"matched", "matched"},
		"starterProvenanceText": {"Lineup: auto-filled", "Lineup: set by the manager"},
		"starterJoinStateText":  {" · Stats: scored", " · Stats: scored"},
		"starterDetail":         {"Matched current player-stat row.", "Matched current player-stat row."},
	}
	assertView(svc.LiveScoresView(context.Background()), wantFields)
	if provider.index != 2 {
		t.Fatalf("live snapshot calls = %d, want one authoritative snapshot per poll", provider.index)
	}
}

func TestScheduleProviderSharesOneWeeklyStatsSnapshotAcrossScoresAndLedgers(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "active", Week: 1, Kickoff: now.Add(-time.Hour)}}
	})
	state := svc.store.Snapshot()
	makeStats := func(recTD float64) []WeekStatLine {
		lines := make([]WeekStatLine, 0)
		for _, team := range svc.teams {
			lineup, _ := svc.matchupLineup(state, team.ID, 1)
			for _, assignment := range lineup.Slots {
				if !assignment.HasPlayer {
					continue
				}
				stats := map[string]float64{}
				if assignment.Player.ID == "p-01" {
					stats["recTD"] = recTD
				}
				lines = append(lines, WeekStatLine{Key: normalizePlayerKey(assignment.Player.Name, assignment.Player.Position), Stats: stats})
			}
		}
		return lines
	}
	calls := 0
	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		calls++
		if calls == 1 {
			return makeStats(1)
		}
		// A second read would represent a different upstream slice. Returning
		// a different value makes a per-team re-query visible even if the call
		// count assertion is accidentally weakened later.
		return makeStats(2)
	})

	snapshot, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("weekly stats source calls = %d, want one shared snapshot for the whole page", calls)
	}
	for _, matchup := range snapshot.Matchups {
		for _, team := range []ScoreTeam{matchup.Home, matchup.Away} {
			if !team.ScoreKnown || len(team.StarterLedger) == 0 {
				t.Fatalf("team %s score/ledger = %+v, want one known score and starter ledger", team.ID, team)
			}
			rowTotal := 0.0
			for _, row := range team.StarterLedger {
				rowTotal += row.Points
			}
			if rowTotal != team.Score {
				t.Fatalf("team %s score = %v, starter rows = %v; score and ledger drifted", team.ID, team.Score, rowTotal)
			}
		}
	}
	var teamOne ScoreTeam
	for _, matchup := range snapshot.Matchups {
		if matchup.Home.ID == "team-1" {
			teamOne = matchup.Home
			break
		}
		if matchup.Away.ID == "team-1" {
			teamOne = matchup.Away
			break
		}
	}
	if teamOne.ID != "team-1" {
		t.Fatal("schedule omitted team-1")
	}
	if teamOne.Score != 6 || teamOne.ScoreText != "6.0" {
		t.Fatalf("team-1 score = %v (%q), want the first shared slice's 6.0 points", teamOne.Score, teamOne.ScoreText)
	}
	for _, row := range teamOne.StarterLedger {
		if row.PlayerID == "p-01" {
			if row.Points != 6 || row.JoinState != "matched" {
				t.Fatalf("team-1 p-01 ledger row = %+v, want the same 6-point matched slice", row)
			}
			return
		}
	}
	t.Fatal("shared team-1 ledger omitted p-01")
}

func TestFinalScoreKeepsPostedTotalAndLabelsLedgerDelta(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, err := http.NewRequest(http.MethodPost, "/team", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	openLedger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", week)
	for _, row := range openLedger.Rows {
		if row.Slot == "QB" {
			if row.PlayerID != "qb-open" || row.Provenance != "explicit" {
				t.Fatalf("open QB row = %+v, want effective explicit qb-open", row)
			}
			break
		}
	}
	svc.SetWeekStatsSource(pinFixtureStats)
	if _, _, err := svc.closeWeek(week, now); err != nil {
		t.Fatal(err)
	}
	state := svc.store.Snapshot()
	var posted float64
	for _, matchup := range state.Schedule.Weeks[0].Matchups {
		if matchup.HomeTeamID == "team-1" {
			posted = matchup.HomeScore
			break
		}
		if matchup.AwayTeamID == "team-1" {
			posted = matchup.AwayScore
			break
		}
	}
	if posted != 4 {
		t.Fatalf("posted team-1 score = %v, want pin fixture's 4 points", posted)
	}
	// Return a complete, corrected source slice: every configured starter has
	// a join, but qb-open now has two pass TDs. This isolates the final-score
	// authority check from the partial-join check above.
	prior := svc.teamWeekLedger(state, "team-1", week)
	correction := make([]WeekStatLine, 0, len(prior.Rows))
	for _, row := range prior.Rows {
		if row.PlayerID == "" {
			continue
		}
		stats := map[string]float64{}
		if row.PlayerID == "qb-open" {
			stats["passTD"] = 2
		}
		correction = append(correction, WeekStatLine{Key: normalizePlayerKey(row.PlayerName, row.Position), Stats: stats})
	}
	svc.SetWeekStatsSource(func(int) []WeekStatLine { return correction })
	snapshot, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, matchup := range snapshot.Matchups {
		var team ScoreTeam
		switch {
		case matchup.Home.ID == "team-1":
			team = matchup.Home
		case matchup.Away.ID == "team-1":
			team = matchup.Away
		default:
			continue
		}
		if team.Score != posted || team.ScoreText != "4.0" || team.ScoreBasis != "posted-final" {
			t.Fatalf("final team score = %+v, want persisted posted 4.0", team)
		}
		if !team.LedgerKnown || team.LedgerTotal != 8 || !strings.Contains(team.ScoreNote, "delta +4.0") || !strings.Contains(team.ScoreNote, "authoritative") {
			t.Fatalf("final score note = %+v, want corrected 8.0 ledger and explicit posted-total delta", team)
		}
		return
	}
	t.Fatal("closed schedule omitted team-1")
}

func TestScheduleProviderSeparatesStatsFreshnessFromCheckedTime(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	statsAt := now.Add(-7 * time.Hour)
	svc.now = func() time.Time { return now }
	svc.SetStatsUpdatedSource(func() time.Time { return statsAt })
	schedule, err := GenerateSchedule(ScheduleParams{Season: 2026, TeamIDs: teamIDList(svc.teams), StartWeek: 1, Weeks: 1, Seed: 9})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (scheduleProvider{svc: svc}).Snapshot(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CheckedAt.Equal(now) || !snapshot.StatsUpdatedAt.Equal(statsAt) {
		t.Fatalf("freshness = checked:%s stats:%s, want checked:%s stats:%s", snapshot.CheckedAt, snapshot.StatsUpdatedAt, now, statsAt)
	}
	if !snapshot.LastUpdated.Equal(statsAt) {
		t.Fatalf("legacy LastUpdated = %s, want the mirrored ledger instant %s", snapshot.LastUpdated, statsAt)
	}
	svc.feed = newLiveFeed(scheduleProvider{svc: svc}, svc)
	svc.feed.cacheFor = 0
	view := svc.LiveScoresView(context.Background())
	if view["checkedAt"] != svc.formatMatchupUpdate(now) || view["statsUpdatedAt"] != svc.formatMatchupUpdate(statsAt) {
		t.Fatalf("live view freshness = checked:%v stats:%v", view["checkedAt"], view["statsUpdatedAt"])
	}
	// liveStatus itself names the Ledger clock; the Checked clock is the
	// freshness clause's own separate "checkedAt" bind (page.gsx), named
	// only once in the rendered sentence — wave-8 audit item 4 retired
	// liveStatus's own "Checked" clause, which used to duplicate it.
	if strings.Contains(view["liveStatus"].(string), "Checked") {
		t.Fatalf("live status still names Checked itself, want that left to the freshness clause's own checkedAt bind: %q", view["liveStatus"])
	}
	if !strings.Contains(view["liveStatus"].(string), "Ledger") {
		t.Fatalf("live status does not name the ledger clock: %q", view["liveStatus"])
	}
}

// TestLiveStatusTextOpensAfterFirstGameBeforeKickoff covers wave-8 audit
// item 4: before this week's (or the season's) first kickoff, with no
// stats ever posted, liveStatusText must say the ledger simply has not
// opened yet ("Weekly ledger opens after the first game") rather than
// "Ledger Unavailable" — a phrase that read as an outage sitting right
// beside the status line's own "Weekly ledger (nflverse)" source clause.
func TestLiveStatusTextOpensAfterFirstGameBeforeKickoff(t *testing.T) {
	svc := newTestService(t, true)
	presentation := matchupPresentation(MatchupStateScheduled)
	cases := []struct {
		name  string
		state string
	}{
		{"scheduled (mid-season, before this week's kickoff)", MatchupStateScheduled},
		{"preseason (before the season's first kickoff)", MatchupStatePreseason},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			live := LiveSnapshot{State: c.state}
			got := svc.liveStatusText(live, presentation)
			if strings.Contains(got, "Unavailable") {
				t.Fatalf("liveStatusText(%s) = %q, want no \"Unavailable\" before kickoff", c.name, got)
			}
			if !strings.Contains(got, "Weekly ledger opens after the first game") {
				t.Fatalf("liveStatusText(%s) = %q, want the friendly pre-kickoff ledger phrase", c.name, got)
			}
		})
	}
}

// TestLiveStatusTextKeepsUnavailableOutsideThePreKickoffWindow covers the
// same wave-8 audit item 4 fix's other edge: once the week is actually
// underway (or degraded) and stats still never posted, that IS a genuine
// "cannot read this" state, so the honest "Ledger Unavailable" wording
// stays — only the pre-kickoff window gets the friendlier phrase.
func TestLiveStatusTextKeepsUnavailableOutsideThePreKickoffWindow(t *testing.T) {
	svc := newTestService(t, true)
	presentation := matchupPresentation(MatchupStateDegraded)
	live := LiveSnapshot{State: MatchupStateDegraded}
	got := svc.liveStatusText(live, presentation)
	if !strings.Contains(got, "Ledger Unavailable") {
		t.Fatalf("liveStatusText(degraded) = %q, want the honest Ledger Unavailable wording outside the pre-kickoff window", got)
	}
}

func TestClosedWeekLedgerUsesPinnedProvenance(t *testing.T) {
	svc, week, now := pinTestService(t)
	request, err := http.NewRequest(http.MethodPost, "/team", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	svc.SetWeekStatsSource(pinFixtureStats)
	if _, _, err := svc.closeWeek(week, now); err != nil {
		t.Fatal(err)
	}
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", week)
	for _, row := range ledger.Rows {
		if row.Slot == "QB" {
			if row.PlayerID != "qb-open" || row.Provenance != "pinned" {
				t.Fatalf("closed QB row = %+v, want pinned qb-open", row)
			}
			return
		}
	}
	t.Fatal("closed ledger omitted its pinned QB slot")
}

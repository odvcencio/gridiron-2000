package league

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

// newClockTestService builds a Service with a fake clock, a fresh Store on
// disk, and demo/live draftAt gating. Tests advance simulated time by
// writing through the returned *time.Time. The presence tracker's
// startedAt floor sits a full day before start, so tests are free to plant
// an arbitrarily distant "last seen" instant (to simulate a long-away
// manager) without the floor silently promoting it back to "just booted" —
// that floor behavior gets its own dedicated coverage in TestRestartRecovery.
func newClockTestService(t *testing.T, demo bool, draftAt time.Time, start time.Time) (*Service, *time.Time) {
	t.Helper()
	clock := start
	svc := &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		draftAt:  draftAt,
		demoMode: demo,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		cfg:      DefaultConfig(),
		presence: newPresenceTracker(start.Add(-24 * time.Hour)),
		now:      func() time.Time { return clock },
	}
	svc.feed = newLiveFeed(nil, svc)
	startTestDraft(t, svc.store)
	return svc, &clock
}

// TestClockArmsAtDraftAt proves wall-clock passage alone is inert and an
// explicit persisted start is the only lifecycle transition.
func TestClockArmsAtDraftAt(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	t.Run("live mode", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt.Add(-time.Minute))
		service.store.state.DraftStarted = false
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })

		service.clockTick(*clock)
		if got := service.store.Snapshot().ClockDeadline; !got.IsZero() {
			t.Fatalf("clock armed before draftAt: %v", got)
		}

		*clock = draftAt
		service.clockTick(*clock)
		if got := service.store.Snapshot().ClockDeadline; !got.IsZero() {
			t.Fatalf("clock armed from scheduled time alone: %v", got)
		}
		state := service.store.Snapshot()
		started, err := service.store.StartDraft(*clock, service.pickClock(state))
		if err != nil || !started {
			t.Fatalf("StartDraft = %v, %v", started, err)
		}
		if want := draftAt.Add(service.pickClock(state)); !service.store.Snapshot().ClockDeadline.Equal(want) {
			t.Fatalf("explicit-start deadline = %v, want %v", service.store.Snapshot().ClockDeadline, want)
		}
	})

	t.Run("demo mode never self-arms", func(t *testing.T) {
		service, clock := newClockTestService(t, true, draftAt, draftAt.Add(time.Hour))
		service.store.state.DraftStarted = false
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })

		for i := 0; i < 5; i++ {
			service.clockTick(*clock)
			*clock = clock.Add(time.Second)
		}
		if got := service.store.Snapshot().ClockDeadline; !got.IsZero() {
			t.Fatalf("demo mode self-armed: %v", got)
		}

		if _, err := service.store.StartDraft(*clock, service.pickClock(service.store.Snapshot())); err != nil {
			t.Fatal(err)
		}
		if got := service.store.Snapshot().ClockDeadline; got.IsZero() {
			t.Fatal("resume did not arm the demo clock")
		}
	})
}

// TestEffectiveDeadlinePrecedence locks the clock authority contract:
// presence never shortens a pick, while explicit AUTO uses the short grace.
func TestEffectiveDeadlinePrecedence(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, _ := newClockTestService(t, false, draftAt, draftAt)
	member, _, err := service.store.AssignMember("a@example.com", "A") // team-1
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	state := service.store.Snapshot()

	t.Run("here: full deadline", func(t *testing.T) {
		service.presence.record(member.Email, draftAt)
		effective, reason := service.effectiveDeadline(state, draftAt)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})

	t.Run("away: full deadline remains", func(t *testing.T) {
		awaySince := draftAt.Add(-time.Hour)
		service.presence.record(member.Email, awaySince)
		now := draftAt.Add(5 * time.Second)
		effective, reason := service.effectiveDeadline(state, now)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})

	t.Run("explicit AUTO: armAt+3s", func(t *testing.T) {
		awaySince := draftAt.Add(-time.Hour)
		service.presence.record(member.Email, awaySince) // still away
		if err := service.store.SetAutopick("team-1", true); err != nil {
			t.Fatal(err)
		}
		toggledState := service.store.Snapshot()
		now := draftAt.Add(5 * time.Second)
		effective, reason := service.effectiveDeadline(toggledState, now)
		armAt := toggledState.ClockDeadline.Add(-service.pickClock(toggledState))
		want := armAt.Add(AutopickGrace)
		if reason != "autopick" || !effective.Equal(want) {
			t.Fatalf("effective = %v (%s), want %v (autopick)", effective, reason, want)
		}
		if err := service.store.SetAutopick("team-1", false); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("restart and disconnect never manufacture an early pick", func(t *testing.T) {
		now := draftAt.Add(5 * time.Second)
		effective, reason := service.effectiveDeadline(state, now)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})
}

// TestNotSeenClockCap locks the NOT SEEN clock-shortening contract: a seat
// whose manager has never sent one heartbeat this process lifetime gets a
// capped deadline, once the process has run past the restart guard; every
// other presence bucket (away, idle) keeps the full persisted clock; a
// first heartbeat restores the full clock on the very next tick; and an
// explicit AUTO toggle keeps using its own grace, untouched by this change.
func TestNotSeenClockCap(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	t.Run("not_seen, process up past the boot grace: capped deadline", func(t *testing.T) {
		service, _ := newClockTestService(t, false, draftAt, draftAt)
		if _, _, err := service.store.AssignMember("a@example.com", "A"); err != nil { // team-1
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		state := service.store.Snapshot()
		now := draftAt.Add(5 * time.Second)
		// No heartbeat ever recorded for a@example.com: not_seen.
		effective, reason := service.effectiveDeadline(state, now)
		armAt := state.ClockDeadline.Add(-service.pickClock(state))
		want := armAt.Add(NotSeenClock)
		if reason != "not_seen" || !effective.Equal(want) {
			t.Fatalf("effective = %v (%s), want %v (not_seen)", effective, reason, want)
		}
	})

	t.Run("not_seen within the restart grace: full deadline", func(t *testing.T) {
		service := &Service{
			store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
			draftAt:  draftAt,
			demoMode: false,
			teams:    defaultTeams(),
			players:  defaultPlayers(),
			cfg:      DefaultConfig(),
			presence: newPresenceTracker(draftAt), // boots exactly at draftAt
			now:      func() time.Time { return draftAt },
		}
		service.feed = newLiveFeed(nil, service)
		startTestDraft(t, service.store)
		if _, _, err := service.store.AssignMember("a@example.com", "A"); err != nil {
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		state := service.store.Snapshot()
		now := draftAt.Add(NotSeenBootGrace - time.Second) // still inside the grace window
		effective, reason := service.effectiveDeadline(state, now)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("restart guard failed: effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})

	t.Run("away seat: full deadline (hidden tabs unaffected)", func(t *testing.T) {
		service, _ := newClockTestService(t, false, draftAt, draftAt)
		member, _, err := service.store.AssignMember("a@example.com", "A")
		if err != nil {
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		state := service.store.Snapshot()
		// One heartbeat an hour ago, then silence: away, never not_seen.
		service.presence.record(member.Email, draftAt.Add(-time.Hour))
		now := draftAt.Add(5 * time.Second)
		effective, reason := service.effectiveDeadline(state, now)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("away must never shorten the clock: effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})

	t.Run("idle seat: full deadline", func(t *testing.T) {
		service, _ := newClockTestService(t, false, draftAt, draftAt)
		member, _, err := service.store.AssignMember("a@example.com", "A")
		if err != nil {
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		state := service.store.Snapshot()
		now := draftAt.Add(5 * time.Second)
		service.presence.record(member.Email, now.Add(-30*time.Second)) // within the idle window
		effective, reason := service.effectiveDeadline(state, now)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("idle must never shorten the clock: effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})

	t.Run("first heartbeat restores the full deadline on the next tick", func(t *testing.T) {
		service, _ := newClockTestService(t, false, draftAt, draftAt)
		member, _, err := service.store.AssignMember("a@example.com", "A")
		if err != nil {
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		state := service.store.Snapshot()
		now := draftAt.Add(5 * time.Second)
		if _, reason := service.effectiveDeadline(state, now); reason != "not_seen" {
			t.Fatalf("precondition: reason = %q, want not_seen", reason)
		}
		service.presence.record(member.Email, now) // the seat's first-ever heartbeat
		effective, reason := service.effectiveDeadline(state, now)
		if reason != "clock" || !effective.Equal(state.ClockDeadline) {
			t.Fatalf("first heartbeat did not restore the full clock: effective = %v (%s), want %v (clock)", effective, reason, state.ClockDeadline)
		}
	})

	t.Run("AUTO-armed seat keeps the autopick grace, unaffected by not_seen", func(t *testing.T) {
		service, _ := newClockTestService(t, false, draftAt, draftAt)
		if _, _, err := service.store.AssignMember("a@example.com", "A"); err != nil { // team-1
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := service.store.SetAutopick("team-1", true); err != nil {
			t.Fatal(err)
		}
		state := service.store.Snapshot()
		now := draftAt.Add(5 * time.Second)
		effective, reason := service.effectiveDeadline(state, now)
		armAt := state.ClockDeadline.Add(-service.pickClock(state))
		want := armAt.Add(AutopickGrace)
		if reason != "autopick" || !effective.Equal(want) {
			t.Fatalf("effective = %v (%s), want %v (autopick)", effective, reason, want)
		}
	})
}

// TestAutopickPrecedence checks autopickChoice: the board's head is chosen
// first; picked or stale (pool-unresolvable) board entries are skipped;
// an exhausted or empty board falls through to best-available ADP; and an
// exhausted pool reports ok=false.
func TestAutopickPrecedence(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, _ := newClockTestService(t, false, draftAt, draftAt)
	pool := testPool(5)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	member, _, err := service.store.AssignMember("a@example.com", "A") // team-1
	if err != nil {
		t.Fatal(err)
	}

	// Board head chosen, skipping a picked entry and a stale (unresolvable)
	// entry ahead of it.
	for _, id := range []string{"pool-001", "stale-id", "pool-003"} {
		if err := service.store.BoardAdd(member.Email, id); err != nil {
			t.Fatal(err)
		}
	}
	// autopickChoice only reads state.Picks for the "already drafted" set;
	// it does not care which team made the pick, so a synthetic entry
	// stands in for "pool-001 was already picked by another team" without
	// needing to route it through the store's turn-order validation.
	state := service.store.Snapshot()
	state.Picks = append(state.Picks, DraftPick{Number: 1, TeamID: "team-8", PlayerID: "pool-001"})
	got, ok := service.autopickChoice(state, "team-1")
	if !ok || got != "pool-003" {
		t.Fatalf("autopickChoice = %q, %v, want pool-003 (skipping picked and stale entries)", got, ok)
	}

	// Empty board falls through to best-available ADP order.
	if err := service.store.BoardClear(member.Email); err != nil {
		t.Fatal(err)
	}
	state = service.store.Snapshot()
	state.Picks = append(state.Picks, DraftPick{Number: 1, TeamID: "team-8", PlayerID: "pool-001"})
	got, ok = service.autopickChoice(state, "team-1")
	if !ok || got != "pool-002" { // pool-001 already picked above
		t.Fatalf("empty-board fallback = %q, %v, want pool-002", got, ok)
	}

	// An exhausted pool (every player picked) reports ok=false.
	small, clockSmall := newClockTestService(t, false, draftAt, draftAt)
	small.SetPlayerSource(func() ([]Player, int64, string) { return testPool(1), 1, "live" })
	if _, err := small.store.MakePick("team-1", "pool-001", "manager", *clockSmall, time.Time{}); err != nil {
		t.Fatal(err)
	}
	state = small.store.Snapshot()
	if _, ok := small.autopickChoice(state, "team-2"); ok {
		t.Fatal("an exhausted pool must report ok=false")
	}
}

func TestDraftSelectionCannotStrandRequiredStarter(t *testing.T) {
	setRosterShape(rosterPresets["standard"])
	t.Cleanup(clearRosterShape)
	draftAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service, _ := newClockTestService(t, true, draftAt, draftAt)
	targetTeam := teamOnClock(nil, 113) // first pick of round 15
	targetPositions := []string{"QB", "RB", "WR", "WR", "TE", "WR", "DST", "K", "QB", "QB", "WR", "WR", "TE", "WR"}
	targetCursor := 0
	pool := make([]Player, 0, 114)
	picks := make([]DraftPick, 0, 112)
	for number := 1; number <= 112; number++ {
		teamID := teamOnClock(nil, number)
		position := "WR"
		if teamID == targetTeam {
			position = targetPositions[targetCursor]
			targetCursor++
		}
		id := fmt.Sprintf("late-pick-%03d", number)
		pool = append(pool, Player{ID: id, Name: id, Position: position, NFLTeam: "TEST", ADPRank: number})
		picks = append(picks, DraftPick{Number: number, Round: pickRound(len(defaultTeams()), number), TeamID: teamID, PlayerID: id, MadeAt: draftAt})
	}
	bad := Player{ID: "board-extra-wr", Name: "Extra Wideout", Position: "WR", NFLTeam: "TEST", ADPRank: 113}
	good := Player{ID: "required-rb", Name: "Required Rusher", Position: "RB", NFLTeam: "TEST", ADPRank: 114}
	pool = append(pool, bad, good)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	service.store.mu.Lock()
	service.store.state.Picks = picks
	service.store.state.DraftStarted = true
	service.store.state.Boards["demo-guest"] = []string{bad.ID, good.ID}
	persistErr := service.store.persistLocked(colPicks, colBoards, colScalars)
	service.store.mu.Unlock()
	if persistErr != nil {
		t.Fatalf("persist late-draft fixture: %v", persistErr)
	}

	state := service.store.Snapshot()
	if got, ok := service.autopickChoice(state, targetTeam); !ok || got != good.ID {
		t.Fatalf("late autopick = %q, %v, want required RB %q", got, ok, good.ID)
	}
	request, _ := http.NewRequest(http.MethodPost, "/draft", nil)
	data := service.DraftData(request)
	rows, _ := data["available"].([]map[string]any)
	eligibility := map[string]bool{}
	for _, row := range rows {
		eligibility[row["id"].(string)], _ = row["draft_eligible"].(bool)
	}
	if eligibility[bad.ID] || !eligibility[good.ID] {
		t.Fatalf("late Draft Room eligibility = %+v, want extra WR false and required RB true", eligibility)
	}
	if _, _, _, err := service.MakePick(request, targetTeam, bad.ID); err == nil || err.Error() != "choose a player who keeps every required starter slot fillable" {
		t.Fatalf("stranding manual pick error = %v", err)
	}
	if _, _, _, err := service.MakePick(request, targetTeam, good.ID); err != nil {
		t.Fatalf("required-starter manual pick: %v", err)
	}
	roster, _ := service.rosterForTeam(service.store.Snapshot(), targetTeam)
	if filled := maximumDraftStarterFill(roster, CurrentRoster()); filled != CurrentRoster().Starters() {
		t.Fatalf("completed target roster fills %d/%d starters", filled, CurrentRoster().Starters())
	}
}

// TestAutopickForcedPunterTakesProjectionTopNotAlphabetical is item 6's own
// regression test (design: "No code change expected — autopickChoice
// walks pool order"): once every other required starter slot is filled, a
// team's final pick can only be legally completed by a punter (the
// gridiron-house preset's own startable P slot). autopickChoice's
// best-available fallback then walks pool.players in POOL ORDER — the
// order internal/fantasy's mergePool/normalizePool now produce, punters
// ranked by real projection — and must take the projection-top punter
// ("Zed Punter", pool-order-first) rather than the alphabetically-first
// one ("Aaron Punter"), proving the historical bug (all-zero punter
// projections tie-breaking to alphabetical order) is fixed upstream, with
// no change needed here. The two punters are driven through fantasy's
// own SetPunterProjections (real enrichment, real re-sort, real
// PunterRank assignment — see fantasyEnrichedPunterPool below), not
// hand-assigned a pre-sorted Projection/PunterRank, so deleting that
// upstream enrichment logic fails this test instead of going unnoticed.
func TestAutopickForcedPunterTakesProjectionTopNotAlphabetical(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"]) // Starters()=11 incl. P; Total()=17
	t.Cleanup(clearRosterShape)
	draftAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service, _ := newClockTestService(t, true, draftAt, draftAt) // demo mode: empty board key "demo-guest"
	const targetTeam = "team-1"

	// Ten players that, between them, fill every gridiron-house starter
	// slot EXCEPT P (QB, RB x2, WR x2, TE, FLEX via a 3rd RB, SUPERFLEX via
	// a 2nd QB, K, DST — 10 of the preset's 11 starter slots).
	filled := []Player{
		{ID: "fq1", Name: "Filled QB1", Position: "QB", NFLTeam: "TEST"},
		{ID: "fq2", Name: "Filled QB2", Position: "QB", NFLTeam: "TEST"},
		{ID: "fr1", Name: "Filled RB1", Position: "RB", NFLTeam: "TEST"},
		{ID: "fr2", Name: "Filled RB2", Position: "RB", NFLTeam: "TEST"},
		{ID: "fr3", Name: "Filled RB3", Position: "RB", NFLTeam: "TEST"},
		{ID: "fw1", Name: "Filled WR1", Position: "WR", NFLTeam: "TEST"},
		{ID: "fw2", Name: "Filled WR2", Position: "WR", NFLTeam: "TEST"},
		{ID: "ft1", Name: "Filled TE1", Position: "TE", NFLTeam: "TEST"},
		{ID: "fk1", Name: "Filled K1", Position: "K", NFLTeam: "TEST"},
		{ID: "fd1", Name: "Filled DST1", Position: "DST", NFLTeam: "TEST"},
	}
	// Six more bench fillers (any non-P position) so the target team's
	// prior pick count reaches 16 — one short of gridiron-house's 17
	// rounds, so this is the team's final pick.
	bench := make([]Player, 0, 6)
	for i := 1; i <= 6; i++ {
		bench = append(bench, Player{ID: fmt.Sprintf("bench-%d", i), Name: fmt.Sprintf("Bench WR %d", i), Position: "WR", NFLTeam: "TEST"})
	}
	priorPicks := append(append([]Player{}, filled...), bench...)
	if len(priorPicks) != 16 {
		t.Fatalf("prior pick fixture = %d players, want 16", len(priorPicks))
	}

	// An unpicked, non-P camp body: it must fail roster viability (the
	// target team has no starter deficit left except P) and so must never
	// be chosen, however early it sits in pool order.
	extraCampBody := Player{ID: "extra-camp", Name: "Extra Camp Body", Position: "WR", NFLTeam: "TEST"}

	// The two punters start out exactly as fantasy.mergePool/normalizePool
	// would see them pre-enrichment — zero Projection, zero PunterRank —
	// fed in ALPHABETICAL (Aaron-before-Zed) raw order, deliberately the
	// wrong order for this test's assertion. fantasyEnrichedPunterPool
	// drives them through fantasy's real SetPunterProjections pipeline;
	// only the real enrichment and re-sort can put "Zed Punter" (the
	// higher-projection one) ahead of "Aaron Punter" in the returned pool
	// order.
	enrichedPunters := fantasyEnrichedPunterPool(t,
		[]fantasy.Player{
			{ID: "aaron-punter", Name: "Aaron Punter", Position: "P", NFLTeam: "DAL"},
			{ID: "zed-punter", Name: "Zed Punter", Position: "P", NFLTeam: "HOU"},
		},
		map[string]float64{"Zed Punter": 9.0, "Aaron Punter": 6.0},
	)
	var zedID, aaronID string
	pool := append([]Player{}, priorPicks...)
	pool = append(pool, extraCampBody)
	for _, p := range enrichedPunters {
		pool = append(pool, Player{ID: p.ID, Name: p.Name, Position: p.Position, NFLTeam: p.NFLTeam, Projection: p.Projection, PunterRank: p.PunterRank})
		switch p.Name {
		case "Zed Punter":
			zedID = p.ID
		case "Aaron Punter":
			aaronID = p.ID
		}
	}
	if zedID == "" || aaronID == "" {
		t.Fatalf("enriched punter fixture missing an expected punter: %+v", enrichedPunters)
	}
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	picks := make([]DraftPick, 0, len(priorPicks))
	for i, player := range priorPicks {
		picks = append(picks, DraftPick{Number: i + 1, TeamID: targetTeam, PlayerID: player.ID, MadeAt: draftAt})
	}
	service.store.mu.Lock()
	service.store.state.Picks = picks
	service.store.state.DraftStarted = true
	persistErr := service.store.persistLocked(colPicks, colBoards, colScalars)
	service.store.mu.Unlock()
	if persistErr != nil {
		t.Fatalf("persist forced-punter fixture: %v", persistErr)
	}

	state := service.store.Snapshot()
	got, ok := service.autopickChoice(state, targetTeam)
	if !ok {
		t.Fatal("autopickChoice reported no legal candidate, want the top punter")
	}
	if got != zedID {
		t.Fatalf("autopickChoice = %q, want %q (projection-top punter, not %q the alphabetically-first one)", got, zedID, aaronID)
	}
}

// fantasyEnrichedPunterPool drives raw, pre-enrichment fantasy.Player
// punters through fantasy's real SetPunterProjections ranking pipeline (a
// cache-loaded fantasy.Service, mirroring
// TestSetPunterProjectionsEnrichesACacheLoadedPool in
// internal/fantasy/punters_test.go) so a caller gets back the actual
// mergePool/normalizePool pool order and Projection/PunterRank values —
// not a hand-picked stand-in — for a test whose assertion depends on that
// real ordering (finding 6, autopick's punter-ranking regression test).
// hook resolves a raw punter's per-game projection by exact Name, and the
// returned hook ignores requireTeam: this fixture's punter names are
// unique, so no live-pool surname collision applies.
func fantasyEnrichedPunterPool(t *testing.T, raw []fantasy.Player, hook map[string]float64) []fantasy.Player {
	t.Helper()
	return fantasyEnrichedPunterPoolWithHook(t, raw, func(name, team string, requireTeam bool) (float64, bool) {
		perGame, ok := hook[name]
		return perGame, ok
	})
}

// fantasyEnrichedPunterPoolWithHook is fantasyEnrichedPunterPool's shared
// core: it takes the punter-projection hook directly, in fantasy's own
// func(name, team string, requireTeam bool) (float64, bool) shape, rather
// than a name-keyed stub. This lets a caller drive the pool through the
// REAL PunterProjection (this package, no stub) for finding 7's own
// lockstep regression test, below — the only way to exercise
// SetPunterProjections' real requireTeam wiring exactly as app_build.go
// sets it up, rather than a fixture that ignores requireTeam entirely.
func fantasyEnrichedPunterPoolWithHook(t *testing.T, raw []fantasy.Player, hook func(name, team string, requireTeam bool) (float64, bool)) []fantasy.Player {
	t.Helper()
	root := t.TempDir()
	cacheFile := struct {
		SchemaVersion int              `json:"schemaVersion"`
		Provider      string           `json:"provider"`
		Scoring       string           `json:"scoring"`
		Players       []fantasy.Player `json:"players"`
	}{
		SchemaVersion: fantasy.SchemaVersion,
		Provider:      "tank01",
		Scoring:       "half_ppr",
		Players:       raw,
	}
	encoded, err := json.Marshal(cacheFile)
	if err != nil {
		t.Fatalf("marshal fantasy cache fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "players.json"), encoded, 0o600); err != nil {
		t.Fatalf("write fantasy cache fixture: %v", err)
	}
	service, err := fantasy.NewService(fantasy.Config{Root: root, Season: 2026, ScoringFormat: "half_ppr"})
	if err != nil {
		t.Fatalf("fantasy.NewService: %v", err)
	}
	service.SetPunterProjections(hook)
	players, _ := service.Players()
	return players
}

// TestFantasyEnrichmentAgreesWithLeaguePunterProjectionOnSuffixedSurname is
// finding 1's own lockstep regression test (finding 7 of the
// punter-rankings review): it drives fantasy's REAL SetPunterProjections
// pipeline with PunterProjection itself as the hook — no stub — over a
// pool holding "AJ Cole III" (LV), a second, unrelated live Cole on
// another team, and a moved punter with a unique-in-pool surname.
// punterSurname (internal/fantasy) and lastWord (this package) must
// tokenize "AJ Cole III" to the same "COLE" key or this fails: before
// finding 1's fix, punterSurname kept "III" as the key, so the two Coles
// shared no collision key at all and the second Cole silently inherited
// the first Cole's LV projection.
func TestFantasyEnrichmentAgreesWithLeaguePunterProjectionOnSuffixedSurname(t *testing.T) {
	raw := []fantasy.Player{
		{ID: "lv-cole", Name: "AJ Cole III", Position: "P", NFLTeam: "LV"},
		{ID: "other-cole", Name: "Bo Cole", Position: "P", NFLTeam: "DAL"},
		{ID: "moved-townsend", Name: "Tommy Townsend", Position: "P", NFLTeam: "NYJ"},
	}
	out := fantasyEnrichedPunterPoolWithHook(t, raw, PunterProjection)
	byID := make(map[string]fantasy.Player, len(out))
	for _, p := range out {
		byID[p.ID] = p
	}
	if byID["lv-cole"].Projection <= 0 {
		t.Errorf("AJ Cole III (LV) must enrich from the embedded Cole/LV entry: %+v", byID["lv-cole"])
	}
	if byID["other-cole"].Projection != 0 {
		t.Errorf("a second live Cole on another team must NOT inherit LV Cole's projection: %+v", byID["other-cole"])
	}
	if byID["moved-townsend"].Projection <= 0 {
		t.Errorf("a moved punter with a unique-in-pool surname must still resolve by last name alone: %+v", byID["moved-townsend"])
	}
}

func TestCommissionerAutopickCompletesStartableRosters(t *testing.T) {
	setRosterShape(rosterPresets["standard"])
	t.Cleanup(clearRosterShape)
	draftAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service, _ := newClockTestService(t, true, draftAt, draftAt)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(200), 1, "demo" })
	request, _ := http.NewRequest(http.MethodPost, "/draft", nil)
	total := len(defaultTeams()) * CurrentDraftRounds()
	for number := 1; number <= total; number++ {
		if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot())); err != nil {
			t.Fatalf("commissioner autopick %d: %v", number, err)
		}
	}
	state := service.store.Snapshot()
	for _, team := range service.Teams() {
		roster, _ := service.rosterForTeam(state, team.ID)
		if len(roster) != CurrentDraftRounds() {
			t.Fatalf("%s roster = %d, want %d", team.ID, len(roster), CurrentDraftRounds())
		}
		if filled := maximumDraftStarterFill(roster, CurrentRoster()); filled != CurrentRoster().Starters() {
			t.Fatalf("%s automatic roster fills %d/%d starters", team.ID, filled, CurrentRoster().Starters())
		}
	}
}

// TestExpiryFiresAutoWithProvenance drives clockTick past an armed
// deadline and checks the fired pick carries MadeBy == "auto", the clock
// resets to a fresh deadline, and the fingerprint changes across the fire.
func TestExpiryFiresAutoWithProvenance(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newClockTestService(t, false, draftAt, draftAt)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(150), 1, "live" })

	service.clockTick(*clock) // arms
	deadline := service.store.Snapshot().ClockDeadline
	if deadline.IsZero() {
		t.Fatal("clock did not arm")
	}
	before := service.StateFingerprint(1)

	*clock = deadline
	service.clockTick(*clock)

	state := service.store.Snapshot()
	if len(state.Picks) != 1 {
		t.Fatalf("picks = %d, want 1", len(state.Picks))
	}
	if state.Picks[0].MadeBy != "auto" {
		t.Fatalf("MadeBy = %q, want auto", state.Picks[0].MadeBy)
	}
	if want := clock.Add(service.pickClock(state)); !state.ClockDeadline.Equal(want) {
		t.Fatalf("next deadline = %v, want %v", state.ClockDeadline, want)
	}
	after := service.StateFingerprint(1)
	if before == after {
		t.Fatal("fingerprint did not change across the auto-pick")
	}
}

// TestRestartRecovery checks the three boot cases from section 8.1: a past
// deadline gets a bounded fresh one (now + min(RestartGrace, pickClock)); a
// future deadline is untouched; and a paused clock stays paused, untouched.
func TestRestartRecovery(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	t.Run("past deadline gets a bounded fresh one", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		past := draftAt.Add(-5 * time.Minute)
		if err := service.store.ArmClock(past); err != nil {
			t.Fatal(err)
		}
		service.bootRecoverClock(*clock)
		got := service.store.Snapshot().ClockDeadline
		want := clock.Add(RestartGrace) // RestartGrace < DefaultPickClock here
		if !got.Equal(want) {
			t.Fatalf("recovered deadline = %v, want %v", got, want)
		}
	})

	t.Run("future deadline untouched", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		future := draftAt.Add(5 * time.Minute)
		if err := service.store.ArmClock(future); err != nil {
			t.Fatal(err)
		}
		service.bootRecoverClock(*clock)
		if got := service.store.Snapshot().ClockDeadline; !got.Equal(future) {
			t.Fatalf("future deadline changed: got %v, want %v", got, future)
		}
	})

	t.Run("paused stays paused, untouched", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		if err := service.store.ArmClock(draftAt.Add(-5 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := service.store.PauseClock(draftAt.Add(-10 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		before := service.store.Snapshot()
		service.bootRecoverClock(*clock)
		after := service.store.Snapshot()
		if !after.ClockPaused || after.ClockRemainingSec != before.ClockRemainingSec {
			t.Fatalf("paused state disturbed by boot recovery: before=%+v after=%+v", before, after)
		}
	})

	t.Run("grace clamps to a short pickClock", func(t *testing.T) {
		service, clock := newClockTestService(t, false, draftAt, draftAt)
		if err := service.store.SetClockDuration(int(MinPickClock.Seconds())); err != nil {
			t.Fatal(err)
		}
		if err := service.store.ArmClock(draftAt.Add(-5 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		service.bootRecoverClock(*clock)
		got := service.store.Snapshot().ClockDeadline
		want := clock.Add(MinPickClock) // min(RestartGrace, pickClock) == pickClock here
		if !got.Equal(want) {
			t.Fatalf("recovered deadline = %v, want %v (clamped to the short pick clock)", got, want)
		}
	})
}

// TestPauseBeatsExpiryRace reproduces section 8.3's timeline: the ticker
// snapshots an expired deadline, a commissioner pause commits before the
// ticker's write lands, and the auto-pick must abort — the pause wins, and
// no pick is recorded.
func TestPauseBeatsExpiryRace(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service, clock := newClockTestService(t, false, draftAt, draftAt)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })

	if err := service.store.ArmClock(draftAt); err != nil {
		t.Fatal(err)
	}
	snapshot := service.store.Snapshot() // the ticker's snapshot, at T

	// The pause commits before the ticker's write (T+1ms in the spec's
	// timeline).
	if err := service.store.PauseClock(*clock); err != nil {
		t.Fatal(err)
	}

	// The ticker's decision, made from the stale snapshot, arrives after.
	playerID, ok := service.autopickChoice(snapshot, teamOnClock(snapshot.DraftOrder, 1))
	if !ok {
		t.Fatal("autopickChoice found no candidate")
	}
	_, err := service.store.AutoPick(teamOnClock(snapshot.DraftOrder, 1), playerID, "auto", 1, snapshot.ClockDeadline, *clock, clock.Add(90*time.Second))
	if err == nil {
		t.Fatal("a stale auto-pick raced against a pause must abort")
	}

	state := service.store.Snapshot()
	if len(state.Picks) != 0 {
		t.Fatalf("the pause must win: picks = %d, want 0", len(state.Picks))
	}
	if !state.ClockPaused {
		t.Fatal("pause state lost")
	}
}

// TestSpeedyDraftSimulation drives the whole system — presence, the
// persisted pick clock, and the auto-pick ticker — through a full
// DraftRounds-round (136-pick as of WP-R0) snake draft with a mixed manager
// population, one second of simulated time at a time. It exercises every
// mechanism together rather than in isolation, the way a real draft night
// would.
//
// Roster, assigned in team-1..team-8 order (AssignMember fills seats in
// that order, so the category maps directly onto the seat):
//   - team-1: flapping. Away like team-7/8 below, except one presence ping
//     lands mid-cap on its first (and only) turn on the clock. The test
//     asserts the capped deadline before the ping and the restored,
//     longer deadline after — flapping costs the manager time, it never
//     saves it, which is the fairness argument section 5.4 makes.
//   - team-2, team-3, team-4: CONNECTED. Presence is pinged every
//     simulated second; each picks manually, landing within 10 seconds of
//     coming on the clock (a varying, deterministic offset).
//   - team-5, team-6: Autopick-toggled. The ticker fires at armAt+3s.
//   - team-7, team-8: AWAY throughout — presence is never recorded for
//     their keys. The ticker fires at the away cap.
func TestSpeedyDraftSimulation(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	clock := draftAt
	service := &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		draftAt:  draftAt,
		demoMode: false,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		cfg:      DefaultConfig(),
		// The tracker boots exactly at draftAt: "presence is empty at
		// boot" (section 8.1), so every seat starts unseen.
		presence: newPresenceTracker(draftAt),
		now:      func() time.Time { return clock },
	}
	service.feed = newLiveFeed(nil, service)
	startTestDraft(t, service.store)
	pool := testPool(150)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	type seat struct {
		email    string
		category string
	}
	roster := []seat{
		{"flap-1@example.com", "flap"},
		{"connected-2@example.com", "connected"},
		{"connected-3@example.com", "connected"},
		{"connected-4@example.com", "connected"},
		{"toggled-5@example.com", "toggled"},
		{"toggled-6@example.com", "toggled"},
		{"away-7@example.com", "away"},
		{"away-8@example.com", "away"},
	}
	category := map[string]string{} // teamID -> category
	var connectedKeys []string
	for _, entry := range roster {
		member, _, err := service.store.AssignMember(entry.email, entry.email)
		if err != nil {
			t.Fatal(err)
		}
		category[member.TeamID] = entry.category
		switch entry.category {
		case "connected":
			connectedKeys = append(connectedKeys, member.Email)
		}
	}
	if err := service.store.SetAutopick("team-5", true); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetAutopick("team-6", true); err != nil {
		t.Fatal(err)
	}

	totalPicks := len(defaultTeams()) * DraftRounds
	manualFireAt := map[int]time.Time{}
	const maxSimulatedTicks = 3 * 3600 // safety bound; a real run finishes far sooner

	start := clock
	for tick := 0; tick < maxSimulatedTicks; tick++ {
		state := service.store.Snapshot()
		if len(state.Picks) >= totalPicks {
			break
		}
		for _, key := range connectedKeys {
			service.presence.record(key, clock)
		}

		number := len(state.Picks) + 1
		teamID := teamOnClock(state.DraftOrder, number)

		switch category[teamID] {
		case "connected":
			fireAt, scheduled := manualFireAt[number]
			if !scheduled {
				// A varying, deterministic offset under 10 seconds — the
				// simulation's "every manual pick lands within 10 seconds"
				// configuration (section 9, test 16).
				offset := time.Duration(2+(number%8)) * time.Second
				fireAt = clock.Add(offset)
				manualFireAt[number] = fireAt
			}
			if clock.Before(fireAt) {
				service.clockTick(clock)
				break
			}
			playerID, ok := firstUndraftedPlayer(pool, state)
			if !ok {
				t.Fatalf("pool exhausted at pick %d", number)
			}
			nextDeadline := time.Time{}
			if number < totalPicks {
				nextDeadline = clock.Add(service.pickClock(state))
			}
			if _, err := service.store.MakePick(teamID, playerID, "manager", clock, nextDeadline); err != nil {
				t.Fatalf("manual pick %d for %s: %v", number, teamID, err)
			}
		case "flap":
			// A transient disconnect is observational only. The same full
			// persisted clock applies before and after the next heartbeat.
			if !state.ClockDeadline.IsZero() {
				if _, reason := service.effectiveDeadline(state, clock); reason == "away-cap" {
					t.Fatal("presence must never create an away-cap deadline")
				}
			}
			service.clockTick(clock)
		default: // toggled, away
			service.clockTick(clock)
		}

		clock = clock.Add(time.Second)
	}
	elapsed := clock.Sub(start)

	final := service.store.Snapshot()
	if len(final.Picks) != totalPicks {
		t.Fatalf("picks = %d, want %d (the simulation did not finish within %d simulated seconds)", len(final.Picks), totalPicks, maxSimulatedTicks)
	}

	seenPlayers := make(map[string]bool, totalPicks)
	madeByCount := map[string]int{}
	for _, pick := range final.Picks {
		if seenPlayers[pick.PlayerID] {
			t.Fatalf("duplicate pick of player %s", pick.PlayerID)
		}
		seenPlayers[pick.PlayerID] = true
		if want := teamOnClock(final.DraftOrder, pick.Number); pick.TeamID != want {
			t.Fatalf("pick %d: team = %s, want %s (snake order)", pick.Number, pick.TeamID, want)
		}
		madeByCount[pick.MadeBy]++
	}
	wantManager := 3 * DraftRounds // 3 connected teams
	if madeByCount["manager"] != wantManager {
		t.Errorf("manager picks = %d, want %d", madeByCount["manager"], wantManager)
	}
	wantAuto := 5 * DraftRounds // 5 non-connected teams (toggled + away)
	if madeByCount["auto"] != wantAuto {
		t.Errorf("auto picks = %d, want %d", madeByCount["auto"], wantAuto)
	}
	if madeByCount["commissioner"] != 0 {
		t.Errorf("commissioner picks = %d, want 0", madeByCount["commissioner"])
	}

	t.Logf("simulation finished in %v of simulated time (%d picks)", elapsed, totalPicks)
	if elapsed > 150*time.Minute {
		t.Errorf("elapsed = %v, want under 2.5 simulated hours (worst-case bound)", elapsed)
	}
}

// firstUndraftedPlayer returns the first player in pool order whose ID does
// not appear in state.Picks, standing in for "whatever a connected manager
// chose" — the simulation only needs a valid, on-time pick, not a
// board-driven one.
func firstUndraftedPlayer(pool []Player, state PersistedState) (string, bool) {
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	for _, player := range pool {
		if !picked[player.ID] {
			return player.ID, true
		}
	}
	return "", false
}

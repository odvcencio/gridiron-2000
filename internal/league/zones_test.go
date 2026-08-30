package league

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// zonesFixturePlayers is the pool newZonesTestService drafts from: one
// reserve-eligible QB pair (qb-1, qb-2 — for the reserve-capacity test), a
// starting RB/WR pair, a bench RB, an injured RB (inj-1) for IR placement,
// and a free agent (fa-1) for the "IR frees a spot" and limits tests.
func zonesFixturePlayers() []Player {
	return []Player{
		{ID: "qb-1", Name: "Reserve QB One", Position: "QB", NFLTeam: "PIT", Projection: 20},
		{ID: "qb-2", Name: "Reserve QB Two", Position: "QB", NFLTeam: "PIT", Projection: 18},
		{ID: "rb-1", Name: "Starting Rusher", Position: "RB", NFLTeam: "PIT", Projection: 14},
		{ID: "rb-2", Name: "Bench Rusher", Position: "RB", NFLTeam: "PIT", Projection: 9},
		{ID: "wr-1", Name: "Starting Wideout", Position: "WR", NFLTeam: "PIT", Projection: 11},
		{ID: "inj-1", Name: "Injured Rusher", Position: "RB", NFLTeam: "PIT", Projection: 13},
		{ID: "fa-1", Name: "Free Agent Rusher", Position: "RB", NFLTeam: "PIT", Projection: 8},
	}
}

// zonesTestRoster is the tiny fixture shape every zones_test.go test
// shares: RB/WR starters, one bench spot, a 1-slot QB reserve (counted in
// the cap), a 1-player IR (outside the cap), and an RB limit of 2 —
// deliberately small so a limit breach is reachable with two RB adds.
func zonesTestRoster() RosterPreset {
	return RosterPreset{
		Name:    "zones-fixture",
		Slots:   map[string]int{"RB": 1, "WR": 1},
		Bench:   1,
		Reserve: map[string]int{"QB": 1},
		IR:      1,
		Limits:  map[string]int{"RB": 2},
	}
}

// newZonesTestService builds a demo-mode service on zonesTestRoster (cap 4:
// RB+WR starters, 1 bench, 1 reserve), with rb-1/wr-1/rb-2/qb-1 drafted
// onto team-1 — exactly at cap, one reserve slot still open (qb-1 starts
// in the general pool; individual tests place it as needed).
func newZonesTestService(t *testing.T) (svc *Service, now time.Time) {
	t.Helper()
	svc = newTestService(t, true)
	now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "g-pit-1", Week: 1, Kickoff: now.Add(48 * time.Hour), Away: "PIT", Home: "NYJ"},
		{ID: "g-pit-2", Week: 2, Kickoff: now.Add(24*7*time.Hour + 48*time.Hour), Away: "PIT", Home: "CIN"},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return zonesFixturePlayers(), 1, "test" })
	setRosterShape(zonesTestRoster())
	t.Cleanup(clearRosterShape)
	draftFixtureOntoTeam1(t, svc, now, []string{"rb-1", "wr-1", "rb-2", "qb-1"})
	return svc, now
}

func zonesRequest() *http.Request {
	r, _ := http.NewRequest(http.MethodPost, "/team", nil)
	return r
}

// ---------------------------------------------------------------------
// Zone validation matrix
// ---------------------------------------------------------------------

func TestPlaceInReserveWrongPosition(t *testing.T) {
	svc, _ := newZonesTestService(t)
	_, err := svc.PlaceInReserve(zonesRequest(), "team-1", "rb-1")
	want := "RB does not qualify for the reserve zone"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestPlaceInReserveHappyPath(t *testing.T) {
	svc, _ := newZonesTestService(t)
	msg, err := svc.PlaceInReserve(zonesRequest(), "team-1", "qb-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "Reserve QB One moved to reserve."; msg != want {
		t.Errorf("message = %q, want %q", msg, want)
	}
	state := svc.store.Snapshot()
	if zoneOfPlayer(state, "team-1", "qb-1") != zoneReserve {
		t.Fatal("qb-1 must sit in the reserve zone after placement")
	}
	// Reserve still counts toward the cap: the team is still exactly at
	// its 4-spot Total(), no spot freed.
	if got := effectiveRosterSize(state, "team-1"); got != 4 {
		t.Errorf("effectiveRosterSize = %d, want 4 (reserve counts toward the cap)", got)
	}
}

func TestPlaceInReserveCapacityFull(t *testing.T) {
	svc, now := newZonesTestService(t)
	// Draft a second QB onto team-1 so the 1-slot QB reserve can be
	// contested (the fixture cap is 4; this pushes team-1 to 5, fine for
	// this test since it seeds via a direct transaction, not the draft
	// round cap).
	txn := Transaction{
		ID: "txn-seed-qb2", Type: "add", TeamID: "team-1", Season: svc.cfg.Season, Week: 1,
		Adds: []TransactionPlayer{{PlayerID: "qb-2", Name: "Reserve QB Two", Position: "QB", NFLTeam: "PIT"}},
		By:   "manager", At: now,
	}
	if err := svc.store.RecordTransaction(txn, 99); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PlaceInReserve(zonesRequest(), "team-1", "qb-1"); err != nil {
		t.Fatalf("first reserve placement: %v", err)
	}
	_, err := svc.PlaceInReserve(zonesRequest(), "team-1", "qb-2")
	want := "the reserve zone is full for QB"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestPlaceInIRRequiresQualifyingDesignation(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	// No injury source wired at all.
	_, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1")
	want := "Injured Rusher does not carry a qualifying injury designation"
	if err == nil || err.Error() != want {
		t.Fatalf("(no source) err = %v, want %q", err, want)
	}
	// A non-qualifying designation ("Questionable" — still expected to
	// play, per irQualifyingDesignations' documented set).
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) {
		return "Questionable", true
	})
	_, err = svc.PlaceInIR(zonesRequest(), "team-1", "inj-1")
	if err == nil || err.Error() != want {
		t.Fatalf("(Questionable) err = %v, want %q", err, want)
	}
}

// seedTeam1WithInjured adds inj-1 to team-1 via a direct transaction
// (bypassing the cap, like newWaiversTestService's drop-provenance seed).
// Used only by tests that do not care about exact cap arithmetic (the
// designation gate, and the Limits-exemption check) — see
// newZonesTestServiceWithInjuryAtCap for the cap-accurate IR fixture the
// cap-math tests use instead.
func seedTeam1WithInjured(t *testing.T, svc *Service, now time.Time) {
	t.Helper()
	txn := Transaction{
		ID: "txn-seed-inj", Type: "add", TeamID: "team-1", Season: svc.cfg.Season, Week: 1,
		Adds: []TransactionPlayer{{PlayerID: "inj-1", Name: "Injured Rusher", Position: "RB", NFLTeam: "PIT"}},
		By:   "manager", At: now,
	}
	if err := svc.store.RecordTransaction(txn, 99); err != nil {
		t.Fatal(err)
	}
}

// newZonesTestServiceWithInjuryAtCap is newZonesTestService's variant for
// the IR cap-math tests: team-1 drafts rb-1, wr-1, inj-1 (instead of
// rb-2), and qb-1 — exactly at the 4-spot cap, with inj-1 already a
// rostered (not yet IR-zoned) player, modeling "an already-owned player
// who gets hurt mid-season."
func newZonesTestServiceWithInjuryAtCap(t *testing.T) (svc *Service, now time.Time) {
	t.Helper()
	svc = newTestService(t, true)
	now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "g-pit-1", Week: 1, Kickoff: now.Add(48 * time.Hour), Away: "PIT", Home: "NYJ"},
		{ID: "g-pit-2", Week: 2, Kickoff: now.Add(24*7*time.Hour + 48*time.Hour), Away: "PIT", Home: "CIN"},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return zonesFixturePlayers(), 1, "test" })
	setRosterShape(zonesTestRoster())
	t.Cleanup(clearRosterShape)
	draftFixtureOntoTeam1(t, svc, now, []string{"rb-1", "wr-1", "inj-1", "qb-1"})
	return svc, now
}

func TestPlaceInIRFreesARosterSpot(t *testing.T) {
	svc, _ := newZonesTestServiceWithInjuryAtCap(t)
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) {
		return "Out", true
	})
	state := svc.store.Snapshot()
	if got := effectiveRosterSize(state, "team-1"); got != 4 {
		t.Fatalf("effectiveRosterSize before IR placement = %d, want 4 (at cap)", got)
	}
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatalf("PlaceInIR: %v", err)
	}
	state = svc.store.Snapshot()
	if zoneOfPlayer(state, "team-1", "inj-1") != zoneIR {
		t.Fatal("inj-1 must sit in the IR zone")
	}
	if got := effectiveRosterSize(state, "team-1"); got != 3 {
		t.Errorf("effectiveRosterSize after IR placement = %d, want 3 (IR frees the spot)", got)
	}
	// The freed spot means AddPlayer's W6 cap check now passes.
	if _, err := svc.AddPlayer(zonesRequest(), "team-1", "fa-1", "", ""); err != nil {
		t.Fatalf("AddPlayer after IR placement must succeed (the freed spot): %v", err)
	}
}

func TestActivateFromIRNoDropNeededWhenRoomExists(t *testing.T) {
	svc, _ := newZonesTestServiceWithInjuryAtCap(t)
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "Out", true })
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatal(err)
	}
	// IR placement freed a spot (3/4); activating without a drop should
	// succeed since there is room for inj-1 to re-enter the general pool.
	msg, err := svc.ActivateFromIR(zonesRequest(), "team-1", "inj-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "Injured Rusher activated from IR."; msg != want {
		t.Errorf("message = %q, want %q", msg, want)
	}
	state := svc.store.Snapshot()
	if zoneOfPlayer(state, "team-1", "inj-1") != "" {
		t.Fatal("inj-1 must leave the IR zone after activation")
	}
}

func TestActivateFromIRRequiresDropWhenAtCap(t *testing.T) {
	svc, _ := newZonesTestServiceWithInjuryAtCap(t)
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "Out", true })
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatal(err)
	}
	// Fill the freed spot with a real add, so activation now needs a drop.
	if _, err := svc.AddPlayer(zonesRequest(), "team-1", "fa-1", "", ""); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if _, err := svc.ActivateFromIR(zonesRequest(), "team-1", "inj-1", ""); err == nil {
		t.Fatal("activation without a drop must fail once the team is back at cap")
	}
	// Drop fa-1 (not wr-1): inj-1 is itself an RB, so the drop must free
	// an RB spot too, or the RB:2 limit blocks the activation.
	msg, err := svc.ActivateFromIR(zonesRequest(), "team-1", "inj-1", "fa-1")
	if err != nil {
		t.Fatalf("activation with a drop: %v", err)
	}
	if want := "Injured Rusher activated from IR; Free Agent Rusher dropped."; msg != want {
		t.Errorf("message = %q, want %q", msg, want)
	}
	state := svc.store.Snapshot()
	if zoneOfPlayer(state, "team-1", "inj-1") != "" {
		t.Fatal("inj-1 must leave the IR zone")
	}
	owner := rosterOwner(currentRosters(state))
	if owner["fa-1"] == "team-1" {
		t.Fatal("fa-1 must have been dropped")
	}
}

// TestAddPlayerRejectsIRDropAsFreeingARosterSpot pins F3 (roster-ops audit
// probe 2, ported to AddPlayer's realistic, non-artificial cap math):
// naming an IR occupant as the drop in an add must not bypass the roster
// cap. effectiveRosterSize already excludes an IR occupant, so dropping
// one frees no additional spot on top of the one IR already gave — the
// old `else if dropID == ""` gate skipped the cap check entirely whenever
// any drop was named, letting exactly this combination push the team
// over cap.
func TestAddPlayerRejectsIRDropAsFreeingARosterSpot(t *testing.T) {
	svc, _ := newZonesTestServiceWithInjuryAtCap(t)
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "Out", true })
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatal(err)
	}
	// Fill the spot IR freed with a real add — the team is back at its
	// true effective cap (4), with inj-1 still parked on IR.
	if _, err := svc.AddPlayer(zonesRequest(), "team-1", "fa-1", "", ""); err != nil {
		t.Fatalf("AddPlayer to fill the freed spot: %v", err)
	}
	state := svc.store.Snapshot()
	if got := effectiveRosterSize(state, "team-1"); got != 4 {
		t.Fatalf("effectiveRosterSize before the exploit attempt = %d, want 4 (at cap)", got)
	}

	// Naming the IR occupant as the drop must not bypass the cap: it
	// credits zero spots, so this add must fail exactly like a no-drop
	// add at cap would.
	if _, err := svc.AddPlayer(zonesRequest(), "team-1", "qb-2", "inj-1", "add-drop-player"); err == nil {
		t.Fatal("AddPlayer with an IR-occupant drop must not bypass the roster cap")
	}
	state = svc.store.Snapshot()
	if got := effectiveRosterSize(state, "team-1"); got > 4 {
		t.Fatalf("effectiveRosterSize after the rejected add = %d, want no more than 4", got)
	}
	if zoneOfPlayer(state, "team-1", "inj-1") != zoneIR {
		t.Fatal("inj-1 must remain on IR after the rejected add")
	}
}

// TestFileClaimRejectsIRDropAsFreeingARosterSpot pins F3's filing-time
// fail-fast (Service.FileClaim's W6 pre-check): naming an IR occupant as
// the drop must not let a claim file past the roster cap either.
func TestFileClaimRejectsIRDropAsFreeingARosterSpot(t *testing.T) {
	svc, _ := newZonesTestServiceWithInjuryAtCap(t)
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "Out", true })
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddPlayer(zonesRequest(), "team-1", "fa-1", "", ""); err != nil {
		t.Fatalf("AddPlayer to fill the freed spot: %v", err)
	}
	if _, err := svc.FileClaim(zonesRequest(), "team-1", "qb-2", "inj-1", 0); err == nil {
		t.Fatal("FileClaim with an IR-occupant drop must not bypass the roster cap")
	}
	state := svc.store.Snapshot()
	if len(state.WaiverClaims) != 0 {
		t.Fatalf("WaiverClaims = %+v, want no claim filed", state.WaiverClaims)
	}
}

// ---------------------------------------------------------------------
// Limits enforcement (optional knob, default off)
// ---------------------------------------------------------------------

func TestLimitsBlockAddPlayer(t *testing.T) {
	svc, _ := newZonesTestService(t)
	// team-1 holds rb-1 and rb-2 (2 RBs, at the RB:2 limit) plus wr-1 and
	// qb-1 (4/4, at cap). Drop qb-1 to free a cap spot without touching
	// the RB count, then try to add fa-1 (a third RB) — Limits, not cap,
	// must be what blocks it.
	if _, err := svc.DropPlayer(zonesRequest(), "team-1", "qb-1", playerDropConfirmation); err != nil {
		t.Fatalf("DropPlayer: %v", err)
	}
	_, err := svc.AddPlayer(zonesRequest(), "team-1", "fa-1", "", "")
	want := limitMessage("RB", 2)
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestLimitsIRExempt(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "Out", true })
	// inj-1 is a 3rd RB, over the RB:2 limit, but IR is exempt — placement
	// itself must not be blocked by Limits.
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatalf("IR placement must not be blocked by Limits (IR exempt): %v", err)
	}
}

func TestLimitsBlockDraftPick(t *testing.T) {
	svc, now := newZonesTestService(t)
	// team-1 already holds rb-1 and rb-2 (2 RBs, at the RB:2 limit).
	// Walk the snake order forward, filling every other team's pick, until
	// team-1 is back on the clock, then try to draft a third RB.
	state := svc.store.Snapshot()
	number := len(state.Picks) + 1
	for teamOnClock(nil, number) != "team-1" {
		filler := fmt.Sprintf("filler-limits-%d", number)
		if _, err := svc.store.MakePick(teamOnClock(nil, number), filler, "manager", now, time.Time{}); err != nil {
			t.Fatal(err)
		}
		number++
	}
	_, _, _, err := svc.MakePick(zonesRequest(), "team-1", "fa-1")
	want := limitMessage("RB", 2)
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// ---------------------------------------------------------------------
// Scorer exclusion of zone occupants
// ---------------------------------------------------------------------

func TestScorerExcludesZoneOccupants(t *testing.T) {
	svc, now := newZonesTestService(t)
	if _, err := svc.PlaceInReserve(zonesRequest(), "team-1", "qb-1"); err != nil {
		t.Fatal(err)
	}
	state := svc.store.Snapshot()
	starters := svc.lineupStarters(state, "team-1", 1)
	for _, p := range starters {
		if p.ID == "qb-1" {
			t.Fatal("a reserve occupant must never appear in lineupStarters")
		}
	}
	roster, _ := svc.rosterForTeam(state, "team-1")
	general, reserve, _ := splitRosterZones(state, "team-1", roster)
	for _, p := range general {
		if p.ID == "qb-1" {
			t.Fatal("qb-1 must not appear in the general (bench/starter) pool once reserved")
		}
	}
	if len(reserve) != 1 || reserve[0].ID != "qb-1" {
		t.Fatalf("reserve = %+v, want [qb-1]", reserve)
	}
	_ = now
}

// ---------------------------------------------------------------------
// Old-state-file load (RosterZones absent decodes safely)
// ---------------------------------------------------------------------

func TestOldStateFileLoadsWithoutRosterZones(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/state.json"
	// A state file predating RosterZones: no "rosterZones" key at all.
	body := `{"schemaVersion":2,"ready":{},"picks":[],"members":{},"invites":[],"boards":{},"teamNames":{},"draftOrder":[],"scoring":{}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatalf("StartupError = %v, want nil", err)
	}
	state := store.Snapshot()
	if state.RosterZones == nil {
		t.Fatal("RosterZones must normalize to a non-nil empty map on load")
	}
	if len(state.RosterZones) != 0 {
		t.Fatalf("RosterZones = %+v, want empty", state.RosterZones)
	}
	// The store must still accept a fresh zone placement after loading an
	// old-shaped file.
	if err := store.PlaceInZone("team-1", "some-player", zoneReserve, "QB", time.Now()); err == nil {
		t.Fatal("PlaceInZone must still validate ownership (some-player is not on team-1's roster)")
	}
}

// ---------------------------------------------------------------------
// Healed-IR lifecycle, end to end on the roster-ops ticker: warn -> auto-
// cut -> deferred waiver clearing -> claimable next week, idempotent at
// every step.
// ---------------------------------------------------------------------

func TestHealedIRLifecycleEndToEnd(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	svc, clock := newNotifyTestService(t, start.Add(-time.Hour), start)
	svc.cfg.Timezone = "UTC" // keep the 09:00 process_time math in UTC
	svc.cfg.Waivers.ClearDays = 2

	week1Kickoff := start.Add(5*24*time.Hour + 17*time.Hour)  // 2026-09-06 17:00 UTC
	week2Kickoff := start.Add(12*24*time.Hour + 17*time.Hour) // 2026-09-13 17:00 UTC
	games := []GameInfo{
		{ID: "g-week1", Week: 1, Kickoff: week1Kickoff, Away: "PIT", Home: "NYJ"},
		{ID: "g-week2", Week: 2, Kickoff: week2Kickoff, Away: "BUF", Home: "MIA"}, // anchors "the following week", not PIT-specific
	}
	svc.SetScheduleSource(func() []GameInfo { return games })

	pool := []Player{{ID: "inj-1", Name: "Injured Rusher", Position: "RB", NFLTeam: "PIT", Projection: 10}}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "test" })
	if err := svc.store.SetDraftOrder(defaultTeamIDs()); err != nil {
		t.Fatal(err)
	}
	member, _, err := svc.store.AssignMember("manager@example.com", "Manager One") // seats team-1 first
	if err != nil {
		t.Fatal(err)
	}
	if member.TeamID != "team-1" {
		t.Fatalf("AssignMember seated %q, want team-1", member.TeamID)
	}
	if _, err := svc.store.MakePick("team-1", "inj-1", "manager", start, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.PlaceInZone("team-1", "inj-1", zoneIR, "RB", start); err != nil {
		t.Fatal(err)
	}

	// Healed: the injury source now reports a non-qualifying designation
	// (an empty report — cleared entirely — is the common case).
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "", false })

	assertNoSend := func(label string) {
		t.Helper()
		if n := sentLogCount(svc.store.Snapshot(), "irheal:"); n != 0 {
			t.Fatalf("%s: SentLog carries %d irheal: entries, want 0", label, n)
		}
	}
	assertZone := func(label, want string) {
		t.Helper()
		if got := zoneOfPlayer(svc.store.Snapshot(), "team-1", "inj-1"); got != want {
			t.Fatalf("%s: zone = %q, want %q", label, got, want)
		}
	}

	// 1. Before the due window: nothing happens.
	*clock = week1Kickoff.Add(-30 * time.Hour)
	svc.rosterOpsTick(*clock)
	assertNoSend("before window")
	assertZone("before window", zoneIR)

	// 2. Inside the due window: the warning fires exactly once.
	*clock = week1Kickoff.Add(-12 * time.Hour)
	svc.rosterOpsTick(*clock)
	if n := sentLogCount(svc.store.Snapshot(), "irheal:"); n != 1 {
		t.Fatalf("after first in-window tick: SentLog carries %d irheal: entries, want 1", n)
	}
	assertZone("in window", zoneIR)

	// 3. Idempotent: a repeat tick at the same instant does not re-send.
	svc.rosterOpsTick(*clock)
	if n := sentLogCount(svc.store.Snapshot(), "irheal:"); n != 1 {
		t.Fatalf("after repeat in-window tick: SentLog carries %d irheal: entries, want 1 (idempotent)", n)
	}

	// 4. At kickoff, unresolved: the platform auto-cuts him.
	*clock = week1Kickoff.Add(time.Hour)
	svc.rosterOpsTick(*clock)
	assertZone("after kickoff", "")
	state := svc.store.Snapshot()
	var cut *Transaction
	for i := range state.Transactions {
		if state.Transactions[i].Type == "auto-drop" {
			cut = &state.Transactions[i]
		}
	}
	if cut == nil {
		t.Fatal("no auto-drop transaction was recorded")
	}
	if cut.By != "system" || len(cut.Drops) != 1 || cut.Drops[0].PlayerID != "inj-1" {
		t.Fatalf("auto-drop transaction = %+v, want By=system, Drops=[inj-1]", cut)
	}
	if owner := rosterOwner(currentRosters(state)); owner["inj-1"] != "" {
		t.Fatal("inj-1 must no longer be owned by any team after the auto-cut")
	}

	// 5. Idempotent: a repeat tick past kickoff never appends a second cut.
	svc.rosterOpsTick(*clock)
	cutCount := 0
	for _, txn := range svc.store.Snapshot().Transactions {
		if txn.Type == "auto-drop" {
			cutCount++
		}
	}
	if cutCount != 1 {
		t.Fatalf("auto-drop transaction count = %d, want 1 (idempotent)", cutCount)
	}

	// 6. Deferred clearing: not an instant free agent. The clear instant
	// is the first daily run at or after the FOLLOWING week's earliest
	// kickoff (week 2's game, at 09:00 UTC the day after) — never the
	// ordinary clear_days=2 formula, which would resolve on 2026-09-08.
	wantClears := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	status := playerWaiverStatus(svc.store.Snapshot(), svc.cfg, games, "inj-1", "PIT", week1Kickoff.Add(2*time.Hour))
	if status.State != AvailabilityOnWaivers || status.Reason != "dropped" {
		t.Fatalf("status = %+v, want ON WAIVERS/dropped", status)
	}
	if !status.ResolvesAt.Equal(wantClears) {
		t.Fatalf("ResolvesAt = %v, want %v (the following week's run, not the ordinary clear_days instant)", status.ResolvesAt, wantClears)
	}
	ordinaryClears := clearsAt(svc.cfg, week1Kickoff.Add(time.Hour))
	if status.ResolvesAt.Equal(ordinaryClears) {
		t.Fatal("an auto-cut must not clear on the ordinary clear_days schedule")
	}

	// 7. Still on waivers the day after the cut, well before the deferred
	// clear instant — never an instant free agent.
	dayAfter := playerWaiverStatus(svc.store.Snapshot(), svc.cfg, games, "inj-1", "PIT", week1Kickoff.Add(24*time.Hour))
	if dayAfter.State != AvailabilityOnWaivers {
		t.Fatalf("status the day after the cut = %+v, want ON WAIVERS (not an instant free agent)", dayAfter)
	}

	// 8. At/after the deferred clear instant, claimable — mark the
	// original PIT game Final so the unrelated kickoff-lock condition
	// does not also hold the player hostage, isolating the drop-clear
	// mechanism this test exercises.
	games[0].Final = true
	*clock = wantClears.Add(time.Minute)
	status = playerWaiverStatus(svc.store.Snapshot(), svc.cfg, games, "inj-1", "PIT", *clock)
	if status.State != AvailabilityFreeAgent {
		t.Fatalf("status at/after the deferred clear = %+v, want FREE AGENT", status)
	}
}

package league

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// withPoolInjury swaps the zones fixture pool for one where inj-1 carries
// designation in the Tank01 pool field, with a source label of label.
func withPoolInjury(svc *Service, designation, label string) {
	svc.SetPlayerSource(func() ([]Player, int64, string) {
		players := zonesFixturePlayers()
		for i := range players {
			if players[i].ID == "inj-1" {
				players[i].Injury = designation
			}
		}
		return players, 2, label
	})
}

// TestPlaceInIRAcceptsPoolOnlyDesignations is the 2026-09-29 IR review's
// main finding: a player the NFL has moved to injured reserve, PUP, NFI,
// or the suspended list is off the weekly practice report, so only the
// pool designation says why he is out. The gate used to read the weekly
// report alone and refused exactly these players.
func TestPlaceInIRAcceptsPoolOnlyDesignations(t *testing.T) {
	for _, designation := range []string{"Injured Reserve", "Physically Unable to Perform", "Non-Football Injury", "Suspended", "Out"} {
		t.Run(designation, func(t *testing.T) {
			svc, now := newZonesTestService(t)
			seedTeam1WithInjured(t, svc, now)
			withPoolInjury(svc, designation, "live")
			// The weekly report does not list him at all.
			svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "", false })
			msg, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1")
			if err != nil {
				t.Fatalf("PlaceInIR(%s): %v", designation, err)
			}
			if msg != "Injured Rusher moved to IR." {
				t.Fatalf("message = %q", msg)
			}
			if zoneOfPlayer(svc.store.Snapshot(), "team-1", "inj-1") != zoneIR {
				t.Fatal("inj-1 is not on IR")
			}
		})
	}
}

// TestPlaceInIRFollowsLeagueEligibleSet: roster.ir_eligible decides, in
// both directions.
func TestPlaceInIRFollowsLeagueEligibleSet(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	withPoolInjury(svc, "Out", "live")
	svc.cfg.IREligible = []string{"IR"}
	_, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1")
	want := "Injured Rusher is listed as Out, which does not qualify for IR. This league's IR takes: Injured reserve."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}

	withPoolInjury(svc, "Questionable", "live")
	svc.cfg.IREligible = []string{"q", "IR"}
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatalf("a league that lists Q must accept a questionable player: %v", err)
	}
}

// TestWorseFeedDecidesIREligibility: the canonical status is the more
// serious of the two feeds, so a stale "Questionable" in the pool does not
// hide an "Out" on the weekly report, and the reverse.
func TestWorseFeedDecidesIREligibility(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	withPoolInjury(svc, "Questionable", "live")
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) {
		if name == "Injured Rusher" {
			return "Out", true
		}
		return "", false
	})
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatalf("weekly Out over pool Questionable must qualify: %v", err)
	}
}

func TestPlaceInIRFullNamesTheCount(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	svc.SetPlayerSource(func() ([]Player, int64, string) {
		players := zonesFixturePlayers()
		for i := range players {
			players[i].Injury = "Out"
		}
		return players, 3, "live"
	})
	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.PlaceInIR(zonesRequest(), "team-1", "rb-2")
	if want := "the IR zone is full (1 of 1); activate a player first"; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestValidateIREligible(t *testing.T) {
	if err := validateIREligible(nil, "league config"); err != nil {
		t.Fatalf("empty list: %v", err)
	}
	if err := validateIREligible([]string{"ir", " O ", "NFI", "SUS", "PUP", "D", "Q"}, "league config"); err != nil {
		t.Fatalf("every known code: %v", err)
	}
	err := validateIREligible([]string{"IR", "COVID"}, "league config")
	if want := `league config: roster.ir_eligible: unknown status "COVID"; valid statuses: IR, O, D, Q, PUP, NFI, SUS`; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	err = validateIREligible([]string{"O", "o"}, "league config")
	if want := `league config: roster.ir_eligible lists "O" twice`; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if got := strings.Join(normalizeIREligible(nil), ","); got != "IR,O,D,PUP,NFI,SUS" {
		t.Fatalf("default = %s", got)
	}
	if got := strings.Join(normalizeIREligible([]string{"sus", "IR"}), ","); got != "IR,SUS" {
		t.Fatalf("normalized = %s, want IR,SUS", got)
	}
}

func TestLeagueConfigReadsIREligible(t *testing.T) {
	body := strings.Replace(instanceBLeagueConfigJSON, `"ir": 2`, `"ir": 2, "ir_eligible": ["IR", "O"]`, 1)
	cfg, err := loadConfigFromEnvFile(t, body)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Join(cfg.IREligible, ",") != "IR,O" {
		t.Fatalf("IREligible = %v", cfg.IREligible)
	}
	bad := strings.Replace(instanceBLeagueConfigJSON, `"ir": 2`, `"ir": 2, "ir_eligible": ["BENCHED"]`, 1)
	if _, err := loadConfigFromEnvFile(t, bad); err == nil || !strings.Contains(err.Error(), `unknown status "BENCHED"`) {
		t.Fatalf("bad ir_eligible err = %v", err)
	}
}

// TestNFLInjuredReserveIsNotHealedOffTheWeeklyReport: a player on the
// NFL's IR is not on the weekly report. His silence there must not start
// the healed clock that ends in an automatic drop.
func TestNFLInjuredReserveIsNotHealedOffTheWeeklyReport(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	withPoolInjury(svc, "Injured Reserve", "live")
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "", false })
	if err := svc.store.PlaceInZone("team-1", "inj-1", zoneIR, "RB", now); err != nil {
		t.Fatal(err)
	}
	pool := svc.pool()
	if svc.irOccupantHealed(pool, pool.byID["inj-1"]) {
		t.Fatal("an NFL injured-reserve player was called healed")
	}
	svc.rosterOpsTick(now.Add(49 * time.Hour)) // past his week-1 kickoff
	if zoneOfPlayer(svc.store.Snapshot(), "team-1", "inj-1") != zoneIR {
		t.Fatal("an NFL injured-reserve player was auto-cut")
	}
}

// TestHealedIRWaitsForTrustworthyFeeds: an outage must never read as a
// recovery.
func TestHealedIRWaitsForTrustworthyFeeds(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	if err := svc.store.PlaceInZone("team-1", "inj-1", zoneIR, "RB", now); err != nil {
		t.Fatal(err)
	}
	// An offline pool carries no designations; with no weekly report
	// wired, nothing is known, so nobody is healed.
	withPoolInjury(svc, "", "offline")
	pool := svc.pool()
	if svc.injuryFeedsReady(pool) {
		t.Fatal("an offline pool with no weekly report must not count as ready")
	}
	if svc.irOccupantHealed(pool, pool.byID["inj-1"]) {
		t.Fatal("healed with no trustworthy feed")
	}
	svc.rosterOpsTick(now.Add(49 * time.Hour))
	if zoneOfPlayer(svc.store.Snapshot(), "team-1", "inj-1") != zoneIR {
		t.Fatal("auto-cut with no trustworthy feed")
	}
	// A weekly source wired over an empty or failed mirror lists nobody;
	// that silence is not a recovery (PR #92 review).
	svc.SetInjuryDesignationSource(func(name, position, nflTeam string) (string, bool) { return "", false })
	if svc.injuryFeedsReady(svc.pool()) {
		t.Fatal("a wired weekly source with no mirror data must not count as ready")
	}
	svc.SetInjuryReportReady(func() bool { return false })
	pool = svc.pool()
	if svc.irOccupantHealed(pool, pool.byID["inj-1"]) {
		t.Fatal("healed from an empty weekly mirror")
	}
	svc.rosterOpsTick(now.Add(49 * time.Hour))
	if zoneOfPlayer(svc.store.Snapshot(), "team-1", "inj-1") != zoneIR {
		t.Fatal("auto-cut from an empty weekly mirror")
	}
	// The same offline pool with the weekly report loaded can decide.
	svc.SetInjuryReportReady(func() bool { return true })
	if !svc.injuryFeedsReady(svc.pool()) {
		t.Fatal("an offline pool with the weekly report loaded must count as ready")
	}
	// A live pool that recovered him is healed.
	withPoolInjury(svc, "", "live")
	pool = svc.pool()
	if !svc.irOccupantHealed(pool, pool.byID["inj-1"]) {
		t.Fatal("a live pool listing no designation must read as healed")
	}
}

func TestTeamDataIRSectionListsOnlyEligiblePlayers(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	svc.SetPlayerSource(func() ([]Player, int64, string) {
		players := zonesFixturePlayers()
		for i := range players {
			switch players[i].ID {
			case "inj-1":
				players[i].Injury = "Injured Reserve"
			case "rb-2":
				players[i].Injury = "Questionable"
			}
		}
		return players, 4, "live"
	})
	data := svc.teamDataForTest(t, "team-1")
	options, _ := data["ir_place_options"].([]map[string]any)
	if len(options) != 1 || options[0]["id"] != "inj-1" || options[0]["label"] != "Injured Rusher (RB) · Injured reserve" {
		t.Fatalf("ir_place_options = %#v, want only inj-1 labelled with its status", options)
	}
	if data["ir_can_place"] != true || data["ir_full"] != false || data["ir_none_eligible"] != false {
		t.Fatalf("flags = can_place %v full %v none %v", data["ir_can_place"], data["ir_full"], data["ir_none_eligible"])
	}

	if _, err := svc.PlaceInIR(zonesRequest(), "team-1", "inj-1"); err != nil {
		t.Fatal(err)
	}
	data = svc.teamDataForTest(t, "team-1")
	if data["ir_full"] != true || data["ir_can_place"] != false {
		t.Fatalf("after filling IR: full %v can_place %v", data["ir_full"], data["ir_can_place"])
	}
	occupants, _ := data["ir_occupants"].([]map[string]any)
	if len(occupants) != 1 || occupants[0]["ir_status"] != "Injured reserve" || occupants[0]["healed"] != false {
		t.Fatalf("ir_occupants = %#v", occupants)
	}
	if data["has_ir_healed"] != false {
		t.Fatal("no healed alert expected while he still qualifies")
	}

	// He recovers: the team page and Home both say so, with the deadline.
	svc.SetPlayerSource(func() ([]Player, int64, string) { return zonesFixturePlayers(), 5, "live" })
	data = svc.teamDataForTest(t, "team-1")
	occupants, _ = data["ir_occupants"].([]map[string]any)
	if occupants[0]["healed"] != true || occupants[0]["has_deadline"] != true {
		t.Fatalf("healed occupant = %#v", occupants[0])
	}
	alerts, _ := data["ir_healed"].([]map[string]any)
	if data["has_ir_healed"] != true || len(alerts) != 1 || !strings.Contains(alerts[0]["text"].(string), "Injured Rusher is off the injury report and no longer qualifies for IR.") {
		t.Fatalf("ir_healed = %#v", data["ir_healed"])
	}
}

func TestIRActionCenterItem(t *testing.T) {
	deadline := time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC)
	f := ActionCenterFacts{HasSeat: true, Admitted: true, DraftComplete: true, DraftStarted: true, SeasonPhase: "regular-season",
		IR: ActionCenterIRFacts{Healed: 1, FirstName: "Injured Rusher", Deadline: deadline, HasDeadline: true}}
	a := irAction(f)
	if a == nil || a.ID != "ir-healed" || a.Href != "/team#team-ir" || !a.Urgent || !a.DueAt.Equal(deadline) {
		t.Fatalf("irAction = %+v", a)
	}
	if !strings.Contains(a.Detail, "Injured Rusher no longer qualifies for IR") {
		t.Fatalf("detail = %q", a.Detail)
	}
	f.IR = ActionCenterIRFacts{}
	if irAction(f) != nil {
		t.Fatal("no IR action without a healed player")
	}
}

// TestCommissionerCanMoveAnotherTeamsIR: dead-manager insurance reaches IR
// the same way it reaches the lineup, with an audit row; ordinary
// managers stay scoped to their own seat.
func TestCommissionerCanMoveAnotherTeamsIR(t *testing.T) {
	service, commissioner, manager, _ := claimedLineupViewService(t)
	now := time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "ir-comm", Week: 3, Kickoff: now.Add(72 * time.Hour), Away: "PIT", Home: "NYJ"}}
	})
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return []Player{
			{ID: "ir-comm-hurt", Name: "Hurt Runner", Position: "RB", NFLTeam: "PIT", Injury: "Injured Reserve"},
			{ID: "ir-comm-filler", Name: "Filler", Position: "WR", NFLTeam: "PIT"},
		}, 1, "live"
	})
	setRosterShape(RosterPreset{Name: "ir-comm", Slots: map[string]int{"RB": 1}, Bench: 1, IR: 1})
	t.Cleanup(clearRosterShape)
	if _, err := service.store.MakePick("team-1", "ir-comm-filler", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.MakePick("team-2", "ir-comm-hurt", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PlaceInIR(manager, "team-2", "ir-comm-hurt"); err == nil {
		t.Fatal("an ordinary manager moved another team's player to IR")
	}
	if _, err := service.PlaceInIR(commissioner, "team-2", "ir-comm-hurt"); err != nil {
		t.Fatalf("commissioner IR placement: %v", err)
	}
	if zoneOfPlayer(service.store.Snapshot(), "team-2", "ir-comm-hurt") != zoneIR {
		t.Fatal("the commissioner's placement did not land")
	}
	if _, err := service.ActivateFromIR(commissioner, "team-2", "ir-comm-hurt", ""); err != nil {
		t.Fatalf("commissioner IR activation: %v", err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[0].Kind != "roster.intervention_ir" || events[0].Refs.TeamID != "team-2" || events[1].Kind != "roster.intervention_ir" {
		t.Fatalf("commissioner events = %+v, want two roster.intervention_ir rows for team-2", events)
	}
}

// teamDataForTest renders the demo-mode team view for teamID.
func (s *Service) teamDataForTest(t *testing.T, teamID string) map[string]any {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, "/team?team="+teamID, nil)
	data := s.TeamData(request)
	if team, _ := data["team"].(map[string]any); team == nil || team["id"] != teamID {
		t.Fatalf("TeamData rendered team %#v, want %s", data["team"], teamID)
	}
	return data
}

// TestTeamDataIRSaysWhenEligiblePlayersAreLocked: after kickoff, a player
// who qualifies but is locked must not read as "nobody qualifies".
func TestTeamDataIRSaysWhenEligiblePlayersAreLocked(t *testing.T) {
	svc, now := newZonesTestService(t)
	seedTeam1WithInjured(t, svc, now)
	withPoolInjury(svc, "Out", "live")
	later := now.Add(49 * time.Hour) // the PIT week-1 game has kicked off
	svc.now = func() time.Time { return later }
	data := svc.teamDataForTest(t, "team-1")
	if data["ir_eligible_locked"] != true || data["ir_none_eligible"] != false || data["ir_can_place"] != false {
		t.Fatalf("flags = locked %v none %v can_place %v", data["ir_eligible_locked"], data["ir_none_eligible"], data["ir_can_place"])
	}
}

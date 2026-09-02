package league

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
)

// playersFixturePool is the WP-R3 add/drop fixture pool: three players
// draftable onto team-1 up to a tiny 3-spot cap (rb-open unlocked, wr-open
// unlocked, rb-locked already kicked off), one player draftable onto
// team-2 (for the "already on a roster" case), and two free agents left
// undrafted.
func playersFixturePool() []Player {
	return []Player{
		{ID: "rb-open", Name: "Open Rusher", Position: "RB", NFLTeam: "PIT", Projection: 12},
		{ID: "wr-open", Name: "Open Wideout", Position: "WR", NFLTeam: "PIT", Projection: 11},
		{ID: "rb-locked", Name: "Locked Rusher", Position: "RB", NFLTeam: "TB", Projection: 14},
		{ID: "other-team-player", Name: "Other Team Player", Position: "TE", NFLTeam: "PIT", Projection: 7},
		{ID: "fa-open", Name: "Free Agent Open", Position: "RB", NFLTeam: "PIT", Projection: 9},
		{ID: "fa-two", Name: "Free Agent Two", Position: "WR", NFLTeam: "PIT", Projection: 8},
	}
}

// newPlayersTestService builds a demo-mode service with a fixed clock, the
// same two-game week-1 schedule newLineupTestService uses (PIT unlocked an
// hour out, TB locked an hour in the past), a tiny 3-spot roster shape,
// and a completed draft: team-1 holds rb-open, wr-open, and rb-locked
// (exactly at cap); team-2 holds other-team-player; fa-open and fa-two sit
// undrafted as free agents.
func newPlayersTestService(t *testing.T) (svc *Service, now time.Time) {
	t.Helper()
	return newPlayersTestServiceWithPool(t, playersFixturePool())
}

// newPlayersTestServiceWithPool is newPlayersTestService's pool-parametric
// core: WP-R4's waivers_test.go reuses the exact same fixture team/roster
// shape against a pool that adds waiver-relevant players (waiversFixturePool,
// waivers_test.go) without duplicating the draft-fixture setup.
func newPlayersTestServiceWithPool(t *testing.T, pool []Player) (svc *Service, now time.Time) {
	t.Helper()
	return newPlayersTestServiceWithPicks(t, pool, nil)
}

// newPlayersTestServiceWithPicks extends newPlayersTestServiceWithPool with
// extra team -> drafted-player-ID assignments, merged after the base
// fixture's own picks (team-1/team-2). waivers_test.go uses this to draft
// a player onto a third team before dropping it, so a seeded "already on
// waivers" fixture player has real drop provenance instead of an
// impossible drop-from-nobody.
func newPlayersTestServiceWithPicks(t *testing.T, pool []Player, extraPicks map[string][]string) (svc *Service, now time.Time) {
	t.Helper()
	svc = newTestService(t, true)
	now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "g-pit", Week: 1, Kickoff: now.Add(time.Hour), Away: "PIT", Home: "NYJ"},
		{ID: "g-tb", Week: 1, Kickoff: now.Add(-time.Hour), Away: "TB", Home: "ATL"},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "test" })
	setRosterShape(RosterPreset{Name: "tiny", Slots: map[string]int{"RB": 1, "WR": 1}, Bench: 1})
	t.Cleanup(clearRosterShape)

	teamPicks := map[string][]string{
		"team-1": {"rb-open", "wr-open", "rb-locked"},
		"team-2": {"other-team-player"},
	}
	for teamID, ids := range extraPicks {
		teamPicks[teamID] = append(teamPicks[teamID], ids...)
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

func TestAddPlayerRequiresSignIn(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	svc.demoMode = false
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "", "")
	want := "Google sign-in is required for league actions"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerUnknownPoolMember(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "ghost", "", "")
	want := "choose an available player"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerAlreadyRostered(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "other-team-player", "", "")
	want := "Other Team Player is already on a roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerRosterFullRequiresDrop(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "", "")
	want := "your roster is full; choose a player to drop"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerDropNotOnRoster(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "fa-two", playerAddDropConfirmation)
	want := "that player is not on your roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerDropLocked(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "rb-locked", playerAddDropConfirmation)
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerSwapSucceeds(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	message, err := svc.AddPlayer(request, "team-1", "fa-open", "wr-open", playerAddDropConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	want := "Free Agent Open signed; Open Wideout dropped."
	if message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
	state := svc.store.Snapshot()
	owner := rosterOwner(currentRosters(state))
	if owner["fa-open"] != "team-1" {
		t.Fatal("fa-open must now belong to team-1")
	}
	if _, stillRostered := owner["wr-open"]; stillRostered {
		t.Fatal("wr-open must no longer belong to any roster")
	}
	if len(state.Transactions) != 1 {
		t.Fatalf("len(Transactions) = %d, want 1 (one atomic add+drop record)", len(state.Transactions))
	}
	txn := state.Transactions[0]
	if txn.Type != "add" || len(txn.Adds) != 1 || len(txn.Drops) != 1 {
		t.Fatalf("txn = %+v, want one add-type record carrying both sides", txn)
	}
}

func TestAddPlayerFreeAgencyClosedPreDraft(t *testing.T) {
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo { return nil })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return playersFixturePool(), 1, "test" })
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "", "")
	want := "free agency opens once the draft is complete"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func newInProgressPlayersTestService(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.SetScheduleSource(func() []GameInfo { return nil })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return playersFixturePool(), 1, "test" })
	if _, err := svc.store.StartDraft(now, DefaultPickClock); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	if _, err := svc.store.MakePick(teamOnClock(nil, 1), "rb-open", "manager", now, now.Add(DefaultPickClock)); err != nil {
		t.Fatalf("first pick: %v", err)
	}
	return svc
}

func TestDropPlayerFreeAgencyClosedDuringDraft(t *testing.T) {
	svc := newInProgressPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.DropPlayer(request, "team-1", "rb-open", playerDropConfirmation)
	want := "free agency opens once the draft is complete"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if owner := rosterOwner(currentRosters(svc.store.Snapshot())); owner["rb-open"] != "team-1" {
		t.Fatalf("drafted player owner = %q, want team-1 after rejected drop", owner["rb-open"])
	}
}

func TestFileClaimFreeAgencyClosedDuringDraft(t *testing.T) {
	svc := newInProgressPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.FileClaim(request, "team-1", "fa-open", "", 0)
	want := "free agency opens once the draft is complete"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if claims := svc.store.Snapshot().WaiverClaims; len(claims) != 0 {
		t.Fatalf("waiver claims = %+v, want none while draft is live", claims)
	}
}

func TestDropPlayerRequiresSignIn(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	svc.demoMode = false
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.DropPlayer(request, "team-1", "rb-open", playerDropConfirmation)
	want := "Google sign-in is required for league actions"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDropPlayerNotOnRoster(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.DropPlayer(request, "team-1", "fa-open", playerDropConfirmation)
	want := "that player is not on your roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDropPlayerLocked(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.DropPlayer(request, "team-1", "rb-locked", playerDropConfirmation)
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDropPlayerSucceedsAndAppendsOneTransaction(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	message, err := svc.DropPlayer(request, "team-1", "rb-open", playerDropConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Open Rusher dropped."; message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
	state := svc.store.Snapshot()
	if len(state.Transactions) != 1 || state.Transactions[0].Type != "drop" {
		t.Fatalf("Transactions = %+v, want one drop record", state.Transactions)
	}
	if owner := rosterOwner(currentRosters(state)); owner["rb-open"] != "" {
		t.Fatal("rb-open must be a free agent after the drop")
	}
}

// TestPlayersDataPreDraftRendersRosterMutationsDisabled checks the
// free-agency gate at the page-loader level: during a live, incomplete
// draft, adds, claims, and drops must all remain unavailable.
func TestPlayersDataPreDraftRendersRosterMutationsDisabled(t *testing.T) {
	svc := newInProgressPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	if open, _ := data["free_agency_open"].(bool); open {
		t.Fatal("free_agency_open must be false pre-draft")
	}
	rows, _ := data["players"].([]map[string]any)
	if len(rows) == 0 {
		t.Fatal("expected rows in the pre-draft pool")
	}
	for _, row := range rows {
		if canAdd, _ := row["can_add"].(bool); canAdd {
			t.Fatalf("row %v: can_add must be false pre-draft", row["name"])
		}
		if canClaim, _ := row["can_claim"].(bool); canClaim {
			t.Fatalf("row %v: can_claim must be false pre-draft", row["name"])
		}
		if canDrop, _ := row["can_drop"].(bool); canDrop {
			t.Fatalf("row %v: can_drop must be false pre-draft", row["name"])
		}
	}
}

// TestPlayersDataPostDraftAvailability checks rostered rows show the
// owning team's abbreviation and free-agent rows are addable.
func TestPlayersDataPostDraftAvailability(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	if canEdit, _ := data["can_edit"].(bool); !canEdit {
		t.Fatal("the demo viewer owns a seat and must be allowed to manage players")
	}
	if open, _ := data["free_agency_open"].(bool); !open {
		t.Fatal("free_agency_open must be true once the draft completes")
	}
	rows, _ := data["players"].([]map[string]any)
	byID := map[string]map[string]any{}
	for _, row := range rows {
		id, _ := row["id"].(string)
		byID[id] = row
	}
	if got := byID["rb-open"]["owner_abbr"]; got != svc.teamByID("team-1").Abbreviation {
		t.Fatalf("rb-open owner_abbr = %v, want %s", got, svc.teamByID("team-1").Abbreviation)
	}
	// owner_name is item 9's own regression check (2026-08-31 post-wave
	// audit): the owner chip (page.gsx's position-chip--locked span) used
	// to carry only the bare abbreviation with no title attribute at all
	// — a manager had no way to learn which team "W4" even was.
	if got := byID["rb-open"]["owner_name"]; got != svc.teamByID("team-1").Name {
		t.Fatalf("rb-open owner_name = %v, want %s", got, svc.teamByID("team-1").Name)
	}
	if canAdd, _ := byID["fa-open"]["can_add"].(bool); !canAdd {
		t.Fatal("fa-open must be addable")
	}
	if needsDrop, _ := byID["fa-open"]["needs_drop"].(bool); !needsDrop {
		t.Fatal("needs_drop must be true once team-1 is at cap")
	}
	if atCap, _ := data["at_cap"].(bool); !atCap {
		t.Fatal("at_cap must be true for team-1 (3 of 3 spots filled)")
	}
}

// TestPlayersDataOwnerChipCarriesDraftedRoundAndPick is wave 7's item 2:
// a rostered row's own draft pick (the fixture drafts rb-open through a
// real store.MakePick call, newPlayersTestServiceWithPicks) surfaces its
// round/pick as drafted_round/drafted_pick/drafted_label ("R# · P#") for
// the /players owner chip — the same playerMap keys draftedByPlayerID
// feeds. other-team-player is drafted too, onto team-2, so this also
// checks the label is not accidentally pinned to team-1's own picks.
func TestPlayersDataOwnerChipCarriesDraftedRoundAndPick(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	rows, _ := data["players"].([]map[string]any)
	byID := map[string]map[string]any{}
	for _, row := range rows {
		id, _ := row["id"].(string)
		byID[id] = row
	}
	for _, id := range []string{"rb-open", "other-team-player"} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("%s must appear in the Player Pool", id)
		}
		if row["is_drafted"] != true {
			t.Fatalf("%s is_drafted = %v, want true", id, row["is_drafted"])
		}
		round, _ := row["drafted_round"].(int)
		pick, _ := row["drafted_pick"].(int)
		if round < 1 || pick < 1 {
			t.Fatalf("%s drafted_round/drafted_pick = %d/%d, want both >= 1", id, round, pick)
		}
		want := fmt.Sprintf("R%d · P%d", round, pick)
		if row["drafted_label"] != want {
			t.Fatalf("%s drafted_label = %v, want %q", id, row["drafted_label"], want)
		}
	}
	// fa-open sits undrafted (a free agent in the fixture pool), so it
	// must carry the same false/zero/"" default playerMap sets for any
	// caller that never passes drafted at all.
	freeAgent := byID["fa-open"]
	if freeAgent["is_drafted"] != false || freeAgent["drafted_label"] != "" {
		t.Fatalf("fa-open drafted fields = is_drafted %v label %q, want false/\"\"", freeAgent["is_drafted"], freeAgent["drafted_label"])
	}
}

// TestPlayersDataPlayerDetailContextKeepsDecisionFields checks that the
// native Player Pool disclosure can explain the same roster, projection,
// matchup, and availability decisions already carried by PlayersData.
func TestPlayersDataPlayerDetailContextKeepsDecisionFields(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	rows, _ := data["players"].([]map[string]any)
	byID := map[string]map[string]any{}
	for _, row := range rows {
		id, _ := row["id"].(string)
		byID[id] = row
	}
	freeAgent, ok := byID["fa-open"]
	if !ok {
		t.Fatal("fa-open must appear in the Player Pool")
	}
	for _, key := range []string{
		"position", "nfl_team", "projection", "has_breakdown", "has_opponent",
		"opponent", "has_matchup", "matchup_detail", "has_hist", "hist",
		"rostered", "free_agent", "on_waivers", "waiver_resolves", "owner_abbr",
		"claimed_by_me", "needs_drop", "can_add", "can_claim", "can_drop",
	} {
		if _, exists := freeAgent[key]; !exists {
			t.Errorf("fa-open detail context lost data key %q", key)
		}
	}
	if freeAgent["projection"] != "9.0" || freeAgent["free_agent"] != true {
		t.Fatalf("fa-open detail context = %+v, want projection 9.0 and free_agent=true", freeAgent)
	}
	if freeAgent["needs_drop"] != true {
		t.Fatal("fa-open detail context must retain the full-roster drop guidance")
	}
	rostered, ok := byID["rb-open"]
	if !ok {
		t.Fatal("rb-open must appear in the Player Pool")
	}
	if rostered["rostered"] != true || rostered["owner_abbr"] == "" {
		t.Fatalf("rb-open detail context = %+v, want rostered owner abbreviation", rostered)
	}
}

// TestPlayersDataSignedInWithoutSeatCanBrowseButNotManage checks the
// invitation-to-seat gap explicitly: authentication grants pool visibility,
// while roster and waiver controls require an actual franchise seat.
func TestPlayersDataSignedInWithoutSeatCanBrowseButNotManage(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	svc.demoMode = false
	svc.store.mu.Lock()
	svc.store.state.WaiverReceipts = []WaiverReceipt{{ClaimID: "private", TeamID: "team-1", Outcome: "won"}}
	svc.store.mu.Unlock()

	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: "invited-viewer", Email: "invited@example.com", Name: "Invited Viewer"}, true
		}),
	})
	var data map[string]any
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data = svc.PlayersData(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/players", nil))

	viewer, _ := data["viewer"].(map[string]any)
	if signedIn, _ := viewer["signed_in"].(bool); !signedIn {
		t.Fatal("fixture must exercise a signed-in viewer")
	}
	if hasSeat, _ := viewer["has_seat"].(bool); hasSeat {
		t.Fatal("an invited viewer without a claimed franchise must remain seatless")
	}
	if canEdit, _ := data["can_edit"].(bool); canEdit {
		t.Fatal("authentication alone must not grant roster or waiver controls")
	}
	if teamID, _ := viewer["team_id"].(string); teamID != "" {
		t.Fatalf("seatless team_id = %q, want empty", teamID)
	}

	rows, _ := data["players"].([]map[string]any)
	if len(rows) == 0 {
		t.Fatal("seatless viewers must still be able to browse the player pool")
	}
	for _, row := range rows {
		for _, capability := range []string{"can_add", "can_claim", "mine", "claimed_by_me"} {
			if enabled, _ := row[capability].(bool); enabled {
				t.Fatalf("seatless row %v unexpectedly enables %s", row["name"], capability)
			}
		}
	}
	order, _ := data["waiver_order"].([]map[string]any)
	for _, row := range order {
		if mine, _ := row["mine"].(bool); mine {
			t.Fatal("seatless viewers must not be assigned a place in the waiver order")
		}
	}
	if receipts, _ := data["my_waiver_receipts"].([]map[string]any); len(receipts) != 0 {
		t.Fatalf("seatless viewer leaked team-private receipts: %+v", receipts)
	}
}

// TestPlayersDataUnavailablePoolFailsClosed ensures an explicitly wired
// production source with no rows cannot fall through to the embedded demo
// player list or advertise roster/waiver controls to a seated manager.
func TestPlayersDataUnavailablePoolFailsClosed(t *testing.T) {
	svc := newTestService(t, false)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return nil, 7, "live" })
	const email = "pool-outage-manager@example.com"
	if _, err := svc.AssignManager(email, "Pool Outage Manager"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: "Pool Outage Manager"}, true
		}),
	})
	var data map[string]any
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data = svc.PlayersData(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/players", nil))

	if data["pool_unavailable"] != true {
		t.Fatalf("pool_unavailable = %v, want true", data["pool_unavailable"])
	}
	if data["can_edit"] != false {
		t.Fatalf("can_edit = %v, want false while the source is unavailable", data["can_edit"])
	}
	rows, _ := data["players"].([]map[string]any)
	if len(rows) != 0 {
		t.Fatalf("unavailable source exposed %d player rows, want none", len(rows))
	}
	status, _ := data["pool_status"].(map[string]any)
	if status["state"] != "unavailable" || status["label"] != "PLAYER DATA UNAVAILABLE" {
		t.Fatalf("pool status = %+v, want unavailable", status)
	}
}

// TestAddPlayerUnavailablePoolIsRejected pins the mutation boundary: a
// zero-row production source must reject adds before any roster transaction
// can be written, even when the caller has a valid franchise seat.
func TestAddPlayerUnavailablePoolIsRejected(t *testing.T) {
	const email = "pool-outage-manager@example.com"
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: "Pool Outage Manager"}, true
		}),
	})

	tests := []struct {
		name string
		call func(*Service, *http.Request) error
	}{
		{name: "add", call: func(svc *Service, r *http.Request) error {
			_, err := svc.AddPlayer(r, "team-1", "fa-open", "", "")
			return err
		}},
		{name: "drop", call: func(svc *Service, r *http.Request) error {
			_, err := svc.DropPlayer(r, "team-1", "rb-open", playerDropConfirmation)
			return err
		}},
		{name: "claim", call: func(svc *Service, r *http.Request) error {
			_, err := svc.FileClaim(r, "team-1", "fa-open", "", 0)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, false)
			svc.SetPlayerSource(func() ([]Player, int64, string) { return nil, 7, "live" })
			if _, err := svc.AssignManager(email, "Pool Outage Manager"); err != nil {
				t.Fatalf("AssignManager: %v", err)
			}
			before := svc.store.Snapshot()
			var actionErr error
			handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				actionErr = tt.call(svc, r)
				w.WriteHeader(http.StatusNoContent)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/players", nil))
			if actionErr == nil || !strings.Contains(actionErr.Error(), "player data is unavailable") {
				t.Fatalf("error = %v, want player-data-unavailable rejection", actionErr)
			}
			after := svc.store.Snapshot()
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("unavailable %s changed persisted state\nbefore: %+v\nafter: %+v", tt.name, before, after)
			}
		})
	}
}

// TestWaiverRunUnavailablePoolLeavesStateUntouched ensures the background
// processor fails closed too: an outage must defer a due run, not turn a
// missing player lookup into a failed claim or advance the retry cursor.
func TestWaiverRunUnavailablePoolLeavesStateUntouched(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.SetPlayerSource(func() ([]Player, int64, string) { return nil, 7, "live" })
	svc.cfg.Timezone = "UTC"
	if err := svc.store.FileClaim(WaiverClaim{
		ID: "unavailable-claim", TeamID: "team-1", AddID: "missing-player", FiledAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("FileClaim: %v", err)
	}
	svc.store.mu.Lock()
	svc.store.state.WaiversProcessedThrough = now.Add(-24 * time.Hour)
	svc.store.mu.Unlock()
	before := svc.store.Snapshot()
	svc.rosterOpsTick(now)
	after := svc.store.Snapshot()
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("unavailable waiver run changed persisted state\nbefore: %+v\nafter: %+v", before, after)
	}
}

// TestPlayersDataPositionFilterAndSearch checks the server-side GET
// filters (?pos=, ?q=) — no client-side filtering.
func TestPlayersDataPositionFilterAndSearch(t *testing.T) {
	svc, _ := newPlayersTestService(t)

	request, _ := http.NewRequest(http.MethodGet, "/players?pos=WR", nil)
	data := svc.PlayersData(request)
	rows, _ := data["players"].([]map[string]any)
	for _, row := range rows {
		if row["position"] != "WR" {
			t.Fatalf("pos=WR filter leaked a %v row", row["position"])
		}
	}

	request, _ = http.NewRequest(http.MethodGet, "/players?q=locked", nil)
	data = svc.PlayersData(request)
	rows, _ = data["players"].([]map[string]any)
	if len(rows) != 1 || rows[0]["name"] != "Locked Rusher" {
		t.Fatalf("q=locked rows = %+v, want exactly Locked Rusher", rows)
	}
}

// TestPlayersDataPositionFilterPunterRankOrder checks /players?pos=P:
// punters render ordered by projection (pool order — PlayersData never
// re-sorts, it only filters, matching mergePool/normalizePool's existing
// rest-tier order), each carrying its "P##" rank label, and a punter the
// embedded projection lookup missed renders "—" instead of a false rank.
func TestPlayersDataPositionFilterPunterRankOrder(t *testing.T) {
	pool := []Player{
		{ID: "wr1", Name: "Some Receiver", Position: "WR", NFLTeam: "PIT", ADPRank: 1, Projection: 15},
		{ID: "p-high", Name: "High Punter", Position: "P", NFLTeam: "HOU", Projection: 9.0, PunterRank: 1},
		{ID: "p-low", Name: "Low Punter", Position: "P", NFLTeam: "DAL", Projection: 6.0, PunterRank: 2},
		{ID: "p-missed", Name: "Unmatched Punter", Position: "P", NFLTeam: "NYJ"},
	}
	svc, _ := newPlayersTestServiceWithPool(t, pool)

	request, _ := http.NewRequest(http.MethodGet, "/players?pos=P", nil)
	data := svc.PlayersData(request)
	rows, _ := data["players"].([]map[string]any)
	if len(rows) != 3 {
		t.Fatalf("pos=P rows = %d, want 3: %+v", len(rows), rows)
	}
	wantOrder := []struct {
		id   string
		rank string
	}{
		{"p-high", "P01"},
		{"p-low", "P02"},
		{"p-missed", "—"},
	}
	for i, want := range wantOrder {
		if rows[i]["id"] != want.id {
			t.Fatalf("row %d id = %v, want %v (rows: %+v)", i, rows[i]["id"], want.id, rows)
		}
		if rows[i]["rank"] != want.rank {
			t.Fatalf("row %d rank = %v, want %v", i, rows[i]["rank"], want.rank)
		}
		if rows[i]["position"] != "P" {
			t.Fatalf("pos=P filter leaked a %v row", rows[i]["position"])
		}
	}
}

func TestPlayersDataPaginatesFilteredRowsAndKeepsNavigationState(t *testing.T) {
	svc := newTestService(t, true)
	pool := make([]Player, 0, 123)
	for index, player := range testPool(123) {
		player.Name = fmt.Sprintf("Roster Pool %03d", index+1)
		pool = append(pool, player)
	}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/players?pos=WR&page=2", nil)
	data := svc.PlayersData(request)
	rows, _ := data["players"].([]map[string]any)
	if len(rows) != 21 {
		t.Fatalf("clamped filtered page = %d rows, want 21", len(rows))
	}
	if data["pool_total"] != 21 || data["pool_page"] != 1 || data["pool_pages"] != 1 {
		t.Fatalf("filtered pagination = total %v page %v/%v, want 21 page 1/1", data["pool_total"], data["pool_page"], data["pool_pages"])
	}
	if data["pool_previous_href"] != "/players?pos=WR" {
		t.Fatalf("clamped filtered previous href = %v", data["pool_previous_href"])
	}

	request, _ = http.NewRequest(http.MethodGet, "/players?page=2", nil)
	data = svc.PlayersData(request)
	rows, _ = data["players"].([]map[string]any)
	if len(rows) != poolPageSize {
		t.Fatalf("second page = %d rows, want %d", len(rows), poolPageSize)
	}
	if data["pool_total"] != 123 || data["pool_page"] != 2 || data["pool_pages"] != 3 {
		t.Fatalf("second-page pagination = total %v page %v/%v, want 123 page 2/3", data["pool_total"], data["pool_page"], data["pool_pages"])
	}
	if data["pool_previous_href"] != "/players" || data["pool_next_href"] != "/players?page=3" {
		t.Fatalf("second-page hrefs = previous %v next %v", data["pool_previous_href"], data["pool_next_href"])
	}
}

// ---------------------------------------------------------------------
// PlayersData — MY CLAIMS and WAIVER ORDER panels (roster-ops spec 8.2)
// ---------------------------------------------------------------------

// TestPlayersDataOnWaiversRowRendersClaimNotAdd checks the row-state
// contract: an ON WAIVERS row offers CLAIM, never ADD, and a rostered
// row offers neither. PlayersData's unauthenticated-demo viewer always
// resolves to team-1 (Viewer's own default, service.go); team-1 owns
// other-team-player through the fixture's cross-team draft, keeping the
// "rostered row offers neither" half of the assertion meaningful.
func TestPlayersDataOnWaiversRowRendersClaimNotAdd(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	rows, _ := data["players"].([]map[string]any)
	byID := map[string]map[string]any{}
	for _, row := range rows {
		id, _ := row["id"].(string)
		byID[id] = row
	}
	wv, ok := byID["wv-open"]
	if !ok {
		t.Fatal("wv-open must appear in the pool rows")
	}
	if canAdd, _ := wv["can_add"].(bool); canAdd {
		t.Fatal("an ON WAIVERS row must never offer ADD")
	}
	if canClaim, _ := wv["can_claim"].(bool); !canClaim {
		t.Fatal("an ON WAIVERS row must offer CLAIM")
	}
	if onWaivers, _ := wv["on_waivers"].(bool); !onWaivers {
		t.Fatal("wv-open must render on_waivers = true")
	}
	if resolves, _ := wv["waiver_resolves"].(string); resolves == "" {
		t.Fatal("an ON WAIVERS row must carry a non-empty waiver_resolves time")
	}

	other, ok := byID["other-team-player"]
	if !ok {
		t.Fatal("other-team-player must appear in the pool rows")
	}
	if canAdd, _ := other["can_add"].(bool); canAdd {
		t.Fatal("a rostered row must never offer ADD")
	}
	if canClaim, _ := other["can_claim"].(bool); canClaim {
		t.Fatal("a rostered row must never offer CLAIM")
	}
}

// TestPlayersDataWaiverResolvesAppendsRelativeTime is item C's regression
// test (2026-08-31 gap audit): the /players pool row chip read "ON WAIVERS
// · Sep 4, 9:00 AM EDT" with no relative label, while the MY CLAIMS row for
// the same deadline showed "(in 2 days)" via the same deadlineRelativeTime
// helper (lineup_deadline.go). The pool row must append " · " plus that same
// helper's output, so the two surfaces never disagree.
func TestPlayersDataWaiverResolvesAppendsRelativeTime(t *testing.T) {
	svc, now := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	rows, _ := data["players"].([]map[string]any)
	var wv map[string]any
	for _, row := range rows {
		if row["id"] == "wv-open" {
			wv = row
			break
		}
	}
	if wv == nil {
		t.Fatal("wv-open must appear in the pool rows")
	}
	resolves, _ := wv["waiver_resolves"].(string)

	state := svc.store.Snapshot()
	status := playerWaiverStatus(state, svc.cfg, svc.schedule(), "wv-open", "PIT", now)
	if status.Reason == "kickoff" {
		t.Fatal("this fixture's wv-open must resolve on the plain waiver-window path, not the kickoff estimate — the appended-relative-time claim doesn't apply to that branch")
	}
	want := formatResolvesAt(svc.cfg, status.ResolvesAt) + " · " + deadlineRelativeTime(now, status.ResolvesAt)
	if resolves != want {
		t.Fatalf("waiver_resolves = %q, want %q (the absolute time plus the shared relative-time suffix)", resolves, want)
	}
}

// TestPlayersDataClaimedByMeSuppressesTheClaimButton checks that an
// already-claimed row stops offering a second CLAIM form. team-1 (the
// demo default viewer) is at cap, so its claim names its own rb-open as
// the drop.
func TestPlayersDataClaimedByMeSuppressesTheClaimButton(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.FileClaim(request, "team-1", "wv-open", "rb-open", 0); err != nil {
		t.Fatal(err)
	}
	getRequest, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(getRequest)
	rows, _ := data["players"].([]map[string]any)
	for _, row := range rows {
		if row["id"] != "wv-open" {
			continue
		}
		if canClaim, _ := row["can_claim"].(bool); canClaim {
			t.Fatal("a row team-1 already claimed must not offer a second CLAIM form")
		}
		if claimed, _ := row["claimed_by_me"].(bool); !claimed {
			t.Fatal("claimed_by_me must be true for team-1's own open claim")
		}
	}
}

// TestPlayersDataMyClaimsPanel checks the MY CLAIMS panel's row shape in
// perf-priority mode: the add player's name, the drop label, and the
// private filing order and the separate public waiverOrder position.
func TestPlayersDataMyClaimsPanel(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.FileClaim(request, "team-1", "wv-open", "rb-open", 0); err != nil {
		t.Fatal(err)
	}
	getRequest, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(getRequest)

	if empty, _ := data["my_claims_empty"].(bool); empty {
		t.Fatal("my_claims_empty must be false once a claim is filed")
	}
	claims, _ := data["my_claims"].([]map[string]any)
	if len(claims) != 1 {
		t.Fatalf("my_claims = %+v, want exactly 1", claims)
	}
	claim := claims[0]
	if claim["add_name"] != "Waived Wideout" {
		t.Fatalf("add_name = %v, want Waived Wideout", claim["add_name"])
	}
	if hasDrop, _ := claim["has_drop"].(bool); !hasDrop {
		t.Fatal("has_drop must be true (a drop accompanies this claim)")
	}
	if faab, _ := data["waivers_faab"].(bool); faab {
		t.Fatal("waivers_faab must be false under the default perf-priority mode")
	}
	pos, _ := data["my_waiver_position"].(int)
	if pos < 1 || pos > 8 {
		t.Fatalf("my_waiver_position = %v, want a position in [1,8]", pos)
	}
	if got := claim["priority"]; got != 1 {
		t.Fatalf("claim priority = %v, want private filing order 1", got)
	}
	if got := claim["waiver_position"]; got != pos {
		t.Fatalf("claim waiver_position = %v, want public position %d", got, pos)
	}
}

// TestPlayersDataWaiverTimesUseLeagueZoneWithRelative is the gap-audit
// finding for players.go:153/:369: a waiver receipt's resolved_at and an
// open claim's filed_at used to format the stored instant directly
// (whatever zone it carried) with no relative text. newWaiversTestService's
// fixture clock (2026-09-13T12:00:00Z) falls in Eastern daylight time.
func TestPlayersDataWaiverTimesUseLeagueZoneWithRelative(t *testing.T) {
	svc, now := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	if _, err := svc.FileClaim(request, "team-1", "wv-open", "rb-open", 0); err != nil {
		t.Fatal(err)
	}
	svc.store.mu.Lock()
	svc.store.state.WaiverReceipts = []WaiverReceipt{
		{
			ClaimID: "receipt-time", Season: 2026, Week: 1, TeamID: "team-1",
			Add:     TransactionPlayer{Name: "Receipt Player", Position: "WR"},
			Outcome: "won", Reason: "Claim awarded.", ResolvedAt: now.Add(-90 * time.Minute).UTC(),
		},
	}
	svc.store.mu.Unlock()

	getRequest, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(getRequest)

	claims, _ := data["my_claims"].([]map[string]any)
	if len(claims) != 1 {
		t.Fatalf("my_claims = %+v, want exactly 1", claims)
	}
	if got := claims[0]["filed_at"]; got != "Sep 13, 8:00 AM EDT · just now" {
		t.Fatalf("filed_at = %v, want the league-zone stamp plus relative suffix", got)
	}

	receipts, _ := data["my_waiver_receipts"].([]map[string]any)
	if len(receipts) != 1 {
		t.Fatalf("my_waiver_receipts = %+v, want exactly 1", receipts)
	}
	if got := receipts[0]["resolved_at"]; got != "Sep 13, 6:30 AM EDT · 1 hour ago" {
		t.Fatalf("resolved_at = %v, want the league-zone stamp plus relative suffix", got)
	}
}

func TestPlayersDataWaiverReceiptsAreTeamPrivateAndIgnoreEmailPrefs(t *testing.T) {
	svc, now := newWaiversTestService(t)
	svc.store.mu.Lock()
	svc.store.state.NotifyPrefs["manager@example.com"] = map[string]bool{"waivers": false}
	svc.store.state.WaiverReceipts = []WaiverReceipt{
		{ClaimID: "other", Season: 2026, Week: 1, TeamID: "team-2", Add: TransactionPlayer{Name: "Other Player", Position: "WR"}, Outcome: "won", Reason: "Claim awarded.", ResolvedAt: now},
		{ClaimID: "mine", Season: 2026, Week: 1, TeamID: "team-1", Add: TransactionPlayer{Name: "My Player", Position: "RB"}, Mode: "faab", Outcome: "beaten", WinningTeamID: "team-2", WinningBid: 14, WinningBidKnown: true, Reason: "outbid", ResolvedAt: now},
	}
	svc.store.mu.Unlock()
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	receipts, _ := data["my_waiver_receipts"].([]map[string]any)
	if len(receipts) != 1 || receipts[0]["claim_id"] != "mine" || receipts[0]["add_name"] != "My Player" {
		t.Fatalf("private receipts = %+v, want only team-1 receipt", receipts)
	}
	// F8: the beaten team's own private receipt never discloses the
	// amount that outbid it — only the commissioner's all-teams overlay
	// (F5) may ever surface a beaten claim's true winning bid.
	if receipts[0]["has_winning_bid"] != false || receipts[0]["winning_bid"] != 0 {
		t.Fatalf("receipt winning bid projection = %+v, want the winning bid withheld from the beaten team", receipts[0])
	}
}

// TestPlayersDataCommissionerSeesAllReceiptsIncludingWinningBid pins F5
// (commissioner receipts are not team-private) and F8 (the winning FAAB
// bid may surface for the commissioner's trusted, audited overlay, the
// one place it ever does).
func TestPlayersDataCommissionerSeesAllReceiptsIncludingWinningBid(t *testing.T) {
	svc, now := newWaiversTestService(t)
	svc.demoMode = true // demo mode grants IsCommissioner
	svc.store.mu.Lock()
	svc.store.state.WaiverReceipts = []WaiverReceipt{
		{ClaimID: "other", Season: 2026, Week: 1, TeamID: "team-2", Add: TransactionPlayer{Name: "Other Player", Position: "WR"}, Outcome: "won", Reason: "Claim awarded.", ResolvedAt: now},
		{ClaimID: "mine", Season: 2026, Week: 1, TeamID: "team-1", Add: TransactionPlayer{Name: "My Player", Position: "RB"}, Mode: "faab", Outcome: "beaten", WinningTeamID: "team-2", WinningBid: 14, WinningBidKnown: true, Reason: "outbid", ResolvedAt: now},
	}
	svc.store.mu.Unlock()
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	if data["is_commissioner"] != true {
		t.Fatal("demo mode must grant is_commissioner")
	}
	receipts, _ := data["commissioner_waiver_receipts"].([]map[string]any)
	if len(receipts) != 2 {
		t.Fatalf("commissioner receipts = %+v, want both teams' receipts", receipts)
	}
	var mine map[string]any
	for _, r := range receipts {
		if r["claim_id"] == "mine" {
			mine = r
		}
	}
	if mine == nil {
		t.Fatal("commissioner view is missing team-1's receipt")
	}
	if mine["team_abbr"] == "" {
		t.Fatal("commissioner view must name the owning team")
	}
	if mine["has_winning_bid"] != true || mine["winning_bid"] != 14 {
		t.Fatalf("commissioner receipt winning bid projection = %+v, want the true winning bid disclosed", mine)
	}
}

func TestMoveClaimUsesAuthenticatedSeatInsteadOfSubmittedTeam(t *testing.T) {
	svc, now := newWaiversTestService(t)
	svc.demoMode = false
	primary, err := svc.AssignManager("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.AssignManager("other@example.com", "Other")
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range []WaiverClaim{
		{ID: "auth-a", TeamID: primary.TeamID, AddID: "auth-player-a", FiledAt: now},
		{ID: "auth-b", TeamID: primary.TeamID, AddID: "auth-player-b", FiledAt: now.Add(time.Minute)},
	} {
		if err := svc.store.FileClaim(claim); err != nil {
			t.Fatal(err)
		}
	}
	currentEmail := other.Email
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: currentEmail, Email: currentEmail, Name: "Manager"}, true
	})})
	invoke := func() (string, error) {
		var message string
		var moveErr error
		handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			message, moveErr = svc.MoveClaim(r, primary.TeamID, "auth-b", "up")
			w.WriteHeader(http.StatusNoContent)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/players", nil))
		return message, moveErr
	}
	if _, err := invoke(); err == nil || err.Error() != "claim is no longer open" {
		t.Fatalf("other manager spoofed submitted team: %v", err)
	}
	currentEmail = primary.Email
	if message, err := invoke(); err != nil || message != "Claim moved up one position." {
		t.Fatalf("primary move = %q, %v", message, err)
	}
}

// TestPlayersDataWaiverOrderStrip checks the WAIVER ORDER strip: every
// team appears exactly once, and exactly one row is marked "mine" for
// the viewing team.
// TestPlayersDataExposesViewerDemoForTheRehearsalModeDisclosure is item
// 13 (coordinator-added, 2026-09-01 post-wave audit): /players had no
// REHEARSAL MODE disclosure for an anonymous demo visitor at all, unlike
// /team, /admin, and /draft — page.gsx now renders one gated on
// data.viewer.demo, the same key/semantic /team's own disclosure uses
// (Viewer, service.go): true only for a genuinely anonymous visitor to a
// demo-mode league, never merely "this league runs in demo mode" (that
// broader signal is the separate demo_mode key /board, /locker, and
// /trades use — see demo_disclosure_test.go).
func TestPlayersDataExposesViewerDemoForTheRehearsalModeDisclosure(t *testing.T) {
	demo, _ := newPlayersTestService(t)
	anonymous, _ := http.NewRequest(http.MethodGet, "/players", nil)
	viewer, ok := demo.PlayersData(anonymous)["viewer"].(map[string]any)
	if !ok || viewer["demo"] != true {
		t.Fatalf("anonymous demo viewer = %#v, want demo=true", viewer)
	}

	real := newTestService(t, false)
	real.SetPlayerSource(func() ([]Player, int64, string) { return playersFixturePool(), 1, "test" })
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	realViewer, ok := real.PlayersData(request)["viewer"].(map[string]any)
	if !ok || realViewer["demo"] != false {
		t.Fatalf("non-demo league viewer = %#v, want demo=false", realViewer)
	}
}

func TestPlayersDataWaiverOrderStrip(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	order, _ := data["waiver_order"].([]map[string]any)
	if len(order) != len(defaultTeamIDs()) {
		t.Fatalf("waiver_order has %d rows, want %d (one per team)", len(order), len(defaultTeamIDs()))
	}
	mineCount := 0
	for _, row := range order {
		if mine, _ := row["mine"].(bool); mine {
			mineCount++
		}
	}
	if mineCount != 1 {
		t.Fatalf("exactly one waiver_order row must be marked mine, got %d", mineCount)
	}
	if count, _ := data["waiver_team_count"].(int); count != len(defaultTeamIDs()) {
		t.Fatalf("waiver_team_count = %v, want %d", count, len(defaultTeamIDs()))
	}
}

// TestPlayersDataFaabModePanelFields checks the faab-mode panel variant:
// waivers_faab true and my_faab_remaining populated instead of a
// priority position.
func TestPlayersDataFaabModePanelFields(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	svc.cfg.Waivers.Mode = "faab"
	svc.cfg.Waivers.FAABBudget = 100
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
	if faab, _ := data["waivers_faab"].(bool); !faab {
		t.Fatal("waivers_faab must be true under faab mode")
	}
	if remaining, _ := data["my_faab_remaining"].(int); remaining != 100 {
		t.Fatalf("my_faab_remaining = %v, want 100 (no claims filed yet)", remaining)
	}
}
func TestPlayersDataLockedDropExplainsAvailability(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	data := svc.PlayersData(deadlineTestGET("/players"))
	rows, _ := data["players"].([]map[string]any)
	for _, row := range rows {
		if row["id"] != "rb-locked" {
			continue
		}
		if canDrop, _ := row["can_drop"].(bool); canDrop {
			t.Fatal("locked roster row must not offer DROP")
		}
		if locked, _ := row["drop_locked"].(bool); !locked {
			t.Fatal("locked roster row must expose drop_locked")
		}
		reason, _ := row["drop_lock_reason"].(string)
		if !strings.Contains(reason, "kicked off") {
			t.Fatalf("drop lock reason = %q, want kickoff explanation", reason)
		}
		return
	}
	t.Fatal("rb-locked row not found")
}

// TestPlayersDataLockedDropNamesHistoricalWeek keeps the Player Pool's
// explanation tied to the kicked-but-unfinalized scoring week even after the
// action selector advances to the next NFL week.
func TestPlayersDataLockedDropNamesHistoricalWeek(t *testing.T) {
	svc, now := newPlayersTestService(t)
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "w1-pit", Week: 1, Kickoff: now.Add(-6 * time.Hour), Away: "PIT", Home: "NYJ"},
			{ID: "w1-tb", Week: 1, Kickoff: now.Add(-6 * time.Hour), Away: "TB", Home: "ATL"},
			{ID: "w2-tb", Week: 2, Kickoff: now.Add(2 * time.Hour), Away: "TB", Home: "CIN"},
		}
	})
	data := svc.PlayersData(deadlineTestGET("/players"))
	rows, _ := data["players"].([]map[string]any)
	for _, row := range rows {
		if row["id"] != "rb-locked" {
			continue
		}
		if locked, _ := row["drop_locked"].(bool); !locked {
			t.Fatal("historically kicked player must remain drop locked")
		}
		reason, _ := row["drop_lock_reason"].(string)
		if !strings.Contains(reason, "week 1") {
			t.Fatalf("historical drop lock reason = %q, want actual Week 1", reason)
		}
		if strings.Contains(reason, "week 2") {
			t.Fatalf("historical drop lock reason used later selector week: %q", reason)
		}
		return
	}
	t.Fatal("rb-locked row not found")
}

// TestPlayersDataCapacityBreakdownSeparatesGeneralReserveAndIR keeps the
// effective cap and each roster zone visible when an IR stash makes raw
// ownership exceed the draftable cap.
func TestPlayersDataCapacityBreakdownSeparatesGeneralReserveAndIR(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	setRosterShape(RosterPreset{
		Name:    "tiny-zones",
		Slots:   map[string]int{"RB": 1, "WR": 1},
		Bench:   0,
		Reserve: map[string]int{"RB": 1},
		IR:      2,
	})
	state := svc.store.Snapshot()
	svc.store.mu.Lock()
	svc.store.state.Picks = append(svc.store.state.Picks, DraftPick{
		Number: len(state.Picks) + 1, TeamID: "team-1", PlayerID: "fa-two",
	})
	svc.store.state.RosterZones["team-1"] = map[string]ZoneAssignment{
		"rb-open":   {Zone: zoneReserve, Position: "RB"},
		"rb-locked": {Zone: zoneIR, Position: "RB"},
	}
	svc.store.mu.Unlock()

	data := svc.PlayersData(deadlineTestGET("/players"))
	for key, want := range map[string]int{
		"roster_size":         3,
		"roster_cap":          3,
		"roster_general_size": 2,
		"roster_general_cap":  2,
		"roster_reserve_size": 1,
		"roster_reserve_cap":  1,
		"roster_ir_size":      1,
		"roster_ir_cap":       2,
	} {
		if got, _ := data[key].(int); got != want {
			t.Errorf("%s = %v, want %d", key, data[key], want)
		}
	}
	if atCap, _ := data["at_cap"].(bool); !atCap {
		t.Fatal("effective general+reserve roster must be at cap")
	}
	if got, _ := data["roster_capacity_summary"].(string); got != "GENERAL 2 / 2 · RESERVE 1 / 1 · IR 1 / 2" {
		t.Fatalf("roster_capacity_summary = %q, want explicit zone breakdown", got)
	}
	rows, _ := data["players"].([]map[string]any)
	for _, row := range rows {
		if row["id"] != "fa-open" {
			continue
		}
		if needsDrop, _ := row["needs_drop"].(bool); !needsDrop {
			t.Fatal("full effective roster must require a drop for fa-open")
		}
		if canAdd, _ := row["can_add"].(bool); !canAdd {
			t.Fatal("full effective roster should retain add-and-drop eligibility")
		}
		return
	}
	t.Fatal("fa-open row not found")
}

// TestPlayersDataKickoffLockedResolveTimeAgreesBetweenPoolRowAndMyClaims
// pins F7: the pool row and MY CLAIMS panel must render the exact same
// answer for one kickoff-locked player's resolve time. Before the fix,
// the pool row rendered a kickoff-plus-five-hours estimate
// (formatResolvesAt(status.ResolvesAt)) while MY CLAIMS said the timing
// was unavailable — two different answers for the same claim.
func TestPlayersDataKickoffLockedResolveTimeAgreesBetweenPoolRowAndMyClaims(t *testing.T) {
	svc, now := newWaiversTestService(t)
	if err := svc.store.FileClaim(WaiverClaim{ID: "clm-kick", TeamID: "team-1", AddID: "fa-open", FiledAt: now}); err != nil {
		t.Fatal(err)
	}
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "in-progress", Week: 1, Kickoff: now.Add(-time.Hour), Away: "PIT", Home: "NYJ"}}
	})
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)

	rows, _ := data["players"].([]map[string]any)
	var poolResolves any
	found := false
	for _, row := range rows {
		if row["id"] == "fa-open" {
			poolResolves = row["waiver_resolves"]
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fa-open not found in the pool rows")
	}

	claims, _ := data["my_claims"].([]map[string]any)
	if len(claims) != 1 {
		t.Fatalf("my_claims = %+v, want exactly one open claim", claims)
	}
	if poolResolves != claims[0]["resolution_label"] {
		t.Fatalf("pool row waiver_resolves = %q, MY CLAIMS resolution_label = %q; the two surfaces must agree", poolResolves, claims[0]["resolution_label"])
	}
	if poolResolves != waiverKickoffPendingLabel {
		t.Fatalf("waiver_resolves = %q, want the shared kickoff-pending label %q", poolResolves, waiverKickoffPendingLabel)
	}
}

func TestPlayersDataClaimResolutionStates(t *testing.T) {
	claimRow := func(svc *Service) map[string]any {
		data := svc.PlayersData(deadlineTestGET("/players"))
		claims, _ := data["my_claims"].([]map[string]any)
		if len(claims) != 1 {
			return nil
		}
		return claims[0]
	}

	futureSvc, _ := newWaiversTestService(t)
	if _, err := futureSvc.FileClaim(deadlineTestPOST("/players"), "team-1", "wv-open", "rb-open", 0); err != nil {
		t.Fatalf("seed future claim: %v", err)
	}
	future := claimRow(futureSvc)
	if future == nil || future["resolution_state"] != "scheduled" || future["resolution_at"] == "" || future["resolution_relative"] == "" {
		t.Fatalf("future claim resolution = %+v, want scheduled exact+relative state", future)
	}

	overdueSvc, overdueNow := newWaiversTestService(t)
	if _, err := overdueSvc.FileClaim(deadlineTestPOST("/players"), "team-1", "wv-open", "rb-open", 0); err != nil {
		t.Fatalf("seed overdue claim: %v", err)
	}
	overdueState := overdueSvc.store.Snapshot()
	overduePlayer := overdueSvc.pool().byID["wv-open"]
	clearRun := playerWaiverStatus(overdueState, overdueSvc.cfg, overdueSvc.schedule(), overduePlayer.ID, overduePlayer.NFLTeam, overdueNow).ResolvesAt
	overdueSvc.store.mu.Lock()
	overdueSvc.store.state.WaiversProcessedThrough = clearRun.Add(-24 * time.Hour)
	overdueSvc.store.mu.Unlock()
	overdueSvc.SetScheduleSource(func() []GameInfo { return nil })
	overdueSvc.now = func() time.Time { return clearRun.Add(time.Hour) }
	overdue := claimRow(overdueSvc)
	if overdue == nil || overdue["resolution_state"] != "overdue" || overdue["resolution_at"] == "" || overdue["resolution_relative"] == "" {
		t.Fatalf("overdue claim resolution = %+v, want overdue exact+relative state", overdue)
	}

	unknownSvc, unknownNow := newWaiversTestService(t)
	if err := unknownSvc.store.FileClaim(WaiverClaim{ID: "unknown-claim", TeamID: "team-1", AddID: "missing-player", FiledAt: unknownNow}); err != nil {
		t.Fatalf("seed unknown claim: %v", err)
	}
	// F6: an AddID absent from the pool is deferred, not "unknown" — the
	// claim stays open and resolves once the player returns to the pool.
	unknown := claimRow(unknownSvc)
	if unknown == nil || unknown["resolution_state"] != "deferred" || unknown["resolution_at"] != "" || unknown["resolution_relative"] != "" {
		t.Fatalf("deferred claim resolution = %+v, want no invented time", unknown)
	}

	degradedSvc, degradedNow := newWaiversTestService(t)
	if err := degradedSvc.store.FileClaim(WaiverClaim{ID: "degraded-claim", TeamID: "team-1", AddID: "fa-open", FiledAt: degradedNow}); err != nil {
		t.Fatalf("seed degraded claim: %v", err)
	}
	degradedSvc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "in-progress", Week: 1, Kickoff: degradedNow.Add(-time.Hour), Away: "PIT", Home: "NYJ"}}
	})
	degraded := claimRow(degradedSvc)
	if degraded == nil || degraded["resolution_state"] != "degraded" || degraded["resolution_at"] != "" || degraded["resolution_relative"] != "" {
		t.Fatalf("degraded claim resolution = %+v, want explicit degraded state", degraded)
	}
}

func TestPlayersDataClaimFiledAfterProcessedRunUsesNextRun(t *testing.T) {
	svc, _ := newWaiversTestService(t)
	svc.cfg.Timezone = "UTC"
	svc.cfg.Waivers.ClearDays = 0
	svc.cfg.Waivers.ProcessTime = "09:00"
	processedThrough := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	filedAt := processedThrough.Add(time.Minute)
	svc.now = func() time.Time { return filedAt }

	foundDrop := false
	svc.store.mu.Lock()
	svc.store.state.WaiversProcessedThrough = processedThrough
	for index := range svc.store.state.Transactions {
		if svc.store.state.Transactions[index].ID == "txn-seed" {
			svc.store.state.Transactions[index].At = processedThrough.Add(-time.Hour)
			foundDrop = true
		}
	}
	svc.store.state.WaiverClaims = []WaiverClaim{{
		ID: "after-run", TeamID: "team-1", AddID: "wv-open", DropID: "rb-open", FiledAt: filedAt,
	}}
	svc.store.mu.Unlock()
	if !foundDrop {
		t.Fatal("seed drop transaction not found")
	}

	data := svc.PlayersData(deadlineTestGET("/players"))
	claims, _ := data["my_claims"].([]map[string]any)
	if len(claims) != 1 {
		t.Fatalf("my_claims = %+v, want one claim", claims)
	}
	wantRun := firstRunStrictlyAfter(svc.cfg, processedThrough)
	if claims[0]["resolution_state"] != "scheduled" || claims[0]["resolution_at"] != formatResolvesAt(svc.cfg, wantRun) {
		t.Fatalf("resolution = state:%v exact:%v, want scheduled at %s", claims[0]["resolution_state"], claims[0]["resolution_at"], formatResolvesAt(svc.cfg, wantRun))
	}
	if claims[0]["resolution_at"] == formatResolvesAt(svc.cfg, processedThrough) {
		t.Fatal("claim incorrectly reused the already-processed 09:00 drop-clear instant")
	}
}

func deadlineTestGET(path string) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, path, nil)
	return request
}

func deadlineTestPOST(path string) *http.Request {
	request, _ := http.NewRequest(http.MethodPost, path, nil)
	return request
}

func TestPlayersDataPublicEntryMatrixAndPrivacy(t *testing.T) {
	type stateCase struct {
		name         string
		email        string
		wantState    PublicEntryState
		wantClaim    bool
		wantAction   string
		commissioner bool
		setup        func(*testing.T, *Service, string)
	}
	cases := []stateCase{
		{
			name:       "anonymous",
			wantState:  PublicEntryAnonymous,
			wantAction: "/login",
		},
		{
			name:       "authenticated pending",
			email:      "pending-surface@example.com",
			wantState:  PublicEntryAuthenticatedPending,
			wantAction: "/guide#identity",
		},
		{
			name:       "admitted seatless open",
			email:      "open-surface@example.com",
			wantState:  PublicEntryAdmittedSeatlessOpen,
			wantClaim:  true,
			wantAction: "/join",
			setup: func(t *testing.T, service *Service, email string) {
				if _, err := service.EnsureMember(email, "Open Surface"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "co-manager pending",
			email:      "pending-co-surface@example.com",
			wantState:  PublicEntryCoManagerPending,
			wantAction: "/guide#identity",
			setup: func(t *testing.T, service *Service, email string) {
				primary, _, err := service.store.AssignMember("surface-primary@example.com", "Surface Primary")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.store.InviteCoManager(primary.TeamID, email); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "admitted seatless full",
			email:      "full-surface@example.com",
			wantState:  PublicEntryAdmittedSeatlessFull,
			wantAction: "/pickem",
			setup: func(t *testing.T, service *Service, email string) {
				for _, team := range service.Teams() {
					if _, _, err := service.store.AssignMember(team.ID+"@surface.example.com", team.Name); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := service.EnsureMember(email, "Full Surface"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "primary",
			email:      "primary-surface@example.com",
			wantState:  PublicEntryPrimary,
			wantAction: "/team",
			setup: func(t *testing.T, service *Service, email string) {
				if _, _, err := service.store.AssignMember(email, "Primary Surface"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "co-manager",
			email:      "co-surface@example.com",
			wantState:  PublicEntryCoManager,
			wantAction: "/team",
			setup: func(t *testing.T, service *Service, email string) {
				primary, _, err := service.store.AssignMember("co-primary-surface@example.com", "Co Primary Surface")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.store.InviteCoManager(primary.TeamID, email); err != nil {
					t.Fatal(err)
				}
				if _, bound, err := service.BindCoManagerOnSignIn(email, "Co Surface"); err != nil || !bound {
					t.Fatalf("bind co-manager = bound %v, err %v", bound, err)
				}
			},
		},
		{
			name:         "commissioner overlay",
			email:        "commissioner-surface@example.com",
			wantState:    PublicEntryAdmittedSeatlessOpen,
			wantClaim:    true,
			wantAction:   "/join",
			commissioner: true,
			setup: func(t *testing.T, service *Service, email string) {
				if _, err := service.EnsureMember(email, "Commissioner Surface"); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, false)
			if tt.commissioner {
				t.Setenv("COMMISSIONER_EMAILS", tt.email)
			}
			if tt.setup != nil {
				tt.setup(t, service, tt.email)
			}
			check := func(r *http.Request) {
				data := service.PlayersData(r)
				entry, ok := data["public_entry"].(map[string]any)
				if !ok {
					t.Fatalf("public_entry = %#v, want map", data["public_entry"])
				}
				if entry["state"] != string(tt.wantState) || entry["can_claim"] != tt.wantClaim || entry["action_href"] != tt.wantAction {
					t.Fatalf("public entry = %#v, want state=%s claim=%v action=%s", entry, tt.wantState, tt.wantClaim, tt.wantAction)
				}
				if entry["is_commissioner"] != tt.commissioner {
					t.Fatalf("commissioner overlay = %v, want %v", entry["is_commissioner"], tt.commissioner)
				}
				canonical := service.publicEntryDataForViewerState(r, service.Viewer(r), service.store.Snapshot())
				if got, want := entry["detail"], canonical["detail"]; got != want {
					t.Fatalf("detail = %v, want canonical %v", got, want)
				}
				encoded, err := json.Marshal(entry)
				if err != nil {
					t.Fatal(err)
				}
				if tt.wantState == PublicEntryCoManagerPending {
					if got := entry["action_label"]; got != "Complete co-manager invitation →" {
						t.Fatalf("pending co-manager action label = %v, want invitation guidance", got)
					}
					if strings.Contains(string(encoded), "/auth/google/start?next=%2Fteam") {
						t.Fatalf("pending co-manager player entry exposed stale reauthentication: %s", encoded)
					}
				}
				for _, secret := range []string{"pending-surface@example.com", "commissioner-surface@example.com", "surface-primary@example.com"} {
					if strings.Contains(string(encoded), secret) {
						t.Fatalf("public_entry leaked private identity %q: %s", secret, encoded)
					}
				}
			}
			if tt.email == "" {
				check(httptest.NewRequest(http.MethodGet, "/players", nil))
				return
			}
			withPublicEntryRequest(t, service, tt.email, check)
		})
	}
}

// TestPlayersDataNamesTheAddLockReason covers the contract's disabled-
// control rule at the loader level: the 2026-09-01 UX audit found fifty
// disabled "Add" buttons on /players with no adjacent reason anywhere in
// a row. The page data must carry the plain-language reason whenever
// adds are locked, and no reason once free agency opens.
func TestPlayersDataNamesTheAddLockReason(t *testing.T) {
	preDraft := newInProgressPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := preDraft.PlayersData(request)
	if got, _ := data["add_locked_reason"].(string); got != "Roster moves open after the draft." {
		t.Fatalf("pre-draft add_locked_reason = %q, want the draft explanation", got)
	}

	postDraft, _ := newPlayersTestService(t)
	data = postDraft.PlayersData(request)
	if got, _ := data["add_locked_reason"].(string); got != "" {
		t.Fatalf("post-draft add_locked_reason = %q, want empty (adds are live)", got)
	}
}

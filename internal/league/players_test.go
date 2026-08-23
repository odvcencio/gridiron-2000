package league

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	if receipts[0]["has_winning_bid"] != true || receipts[0]["winning_bid"] != 14 {
		t.Fatalf("receipt winning bid projection = %+v", receipts[0])
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

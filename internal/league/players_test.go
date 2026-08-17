package league

import (
	"fmt"
	"net/http"
	"testing"
	"time"
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
	svc = newTestService(t, true)
	now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	games := []GameInfo{
		{ID: "g-pit", Week: 1, Kickoff: now.Add(time.Hour), Away: "PIT", Home: "NYJ"},
		{ID: "g-tb", Week: 1, Kickoff: now.Add(-time.Hour), Away: "TB", Home: "ATL"},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return playersFixturePool(), 1, "test" })
	setRosterShape(RosterPreset{Name: "tiny", Slots: map[string]int{"RB": 1, "WR": 1}, Bench: 1})
	t.Cleanup(clearRosterShape)

	teamPicks := map[string][]string{
		"team-1": {"rb-open", "wr-open", "rb-locked"},
		"team-2": {"other-team-player"},
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
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "")
	want := "Google sign-in is required for league actions"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerUnknownPoolMember(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "ghost", "")
	want := "choose an available player"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerAlreadyRostered(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "other-team-player", "")
	want := "Other Team Player is already on a roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerRosterFullRequiresDrop(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "")
	want := "your roster is full; choose a player to drop"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerDropNotOnRoster(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "fa-two")
	want := "that player is not on your roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerDropLocked(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "rb-locked")
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAddPlayerSwapSucceeds(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	message, err := svc.AddPlayer(request, "team-1", "fa-open", "wr-open")
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
	_, err := svc.AddPlayer(request, "team-1", "fa-open", "")
	want := "free agency opens once the draft is complete"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDropPlayerRequiresSignIn(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	svc.demoMode = false
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.DropPlayer(request, "team-1", "rb-open")
	want := "Google sign-in is required for league actions"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDropPlayerNotOnRoster(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.DropPlayer(request, "team-1", "fa-open")
	want := "that player is not on your roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDropPlayerLocked(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	_, err := svc.DropPlayer(request, "team-1", "rb-locked")
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDropPlayerSucceedsAndAppendsOneTransaction(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/players", nil)
	message, err := svc.DropPlayer(request, "team-1", "rb-open")
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

// TestPlayersDataPreDraftRendersAddsDisabled checks the free-agency gate
// at the page-loader level: before the draft completes, every row must
// render with can_add false and the page's free_agency_open flag false.
func TestPlayersDataPreDraftRendersAddsDisabled(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetScheduleSource(func() []GameInfo { return nil })
	svc.SetPlayerSource(func() ([]Player, int64, string) { return playersFixturePool(), 1, "test" })
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
	}
}

// TestPlayersDataPostDraftAvailability checks rostered rows show the
// owning team's abbreviation and free-agent rows are addable.
func TestPlayersDataPostDraftAvailability(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)
	data := svc.PlayersData(request)
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

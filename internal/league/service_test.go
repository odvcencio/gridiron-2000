package league

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func newTestService(t *testing.T, demo bool) *Service {
	t.Helper()
	return &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		feed:     newLiveFeed(nil),
		draftAt:  time.Now().Add(-time.Hour),
		demoMode: demo,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		roster:   defaultRoster(),
	}
}

func testPool(size int) []Player {
	players := make([]Player, 0, size)
	positions := []string{"QB", "RB", "WR", "TE", "K", "DST"}
	for index := 0; index < size; index++ {
		players = append(players, Player{
			ID:         fmt.Sprintf("pool-%03d", index+1),
			Name:       fmt.Sprintf("Pool Player %03d", index+1),
			Position:   positions[index%len(positions)],
			NFLTeam:    "CIN",
			ADP:        float64(index + 1),
			ADPRank:    index + 1,
			ByeWeek:    10,
			Projection: 20 - float64(index)*0.1,
		})
	}
	return players
}

func TestDraftDataUsesPlayerSource(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(150)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 7, "live" })

	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	data := service.DraftData(request)
	available, ok := data["available"].([]map[string]any)
	if !ok || len(available) != 150 {
		t.Fatalf("available = %d, want 150", len(available))
	}
	if available[0]["rank"] != "001" || available[0]["name"] != "Pool Player 001" {
		t.Errorf("head of pool wrong: %+v", available[0])
	}
	if data["pool_label"] != "live" || data["pool_live"] != true {
		t.Errorf("pool labels wrong: %v %v", data["pool_label"], data["pool_live"])
	}
	detail, _ := available[0]["detail"].(string)
	if detail != "CIN · BYE 10" {
		t.Errorf("detail = %q", detail)
	}
}

func TestEmptySourceFallsBackToDemoPool(t *testing.T) {
	service := newTestService(t, true)
	service.SetPlayerSource(func() ([]Player, int64, string) { return nil, 0, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	data := service.DraftData(request)
	available, _ := data["available"].([]map[string]any)
	if len(available) != len(defaultPlayers()) {
		t.Fatalf("fallback pool = %d players", len(available))
	}
	if data["pool_label"] != "demo" {
		t.Errorf("pool_label = %v", data["pool_label"])
	}
}

func TestFullSnakeDraftAndRosters(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(150)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	teams := defaultTeams()
	totalPicks := len(teams) * 15
	for number := 1; number <= totalPicks; number++ {
		onClock := teamOnClock(number)
		data := service.DraftData(request)
		available, _ := data["available"].([]map[string]any)
		if len(available) == 0 {
			t.Fatalf("pool exhausted at pick %d", number)
		}
		playerID, _ := available[0]["id"].(string)
		pick, player, team, err := service.MakePick(request, onClock, playerID)
		if err != nil {
			t.Fatalf("pick %d (%s): %v", number, onClock, err)
		}
		if pick.Number != number || player.ID != playerID || team.ID != onClock {
			t.Fatalf("pick %d mismatch: %+v %s %s", number, pick, player.ID, team.ID)
		}
	}

	state := service.store.Snapshot()
	if len(state.Picks) != totalPicks {
		t.Fatalf("picks = %d, want %d", len(state.Picks), totalPicks)
	}
	for _, team := range teams {
		roster, drafted := service.rosterForTeam(state, team.ID)
		if !drafted || len(roster) != 15 {
			t.Fatalf("team %s roster = %d drafted=%v", team.ID, len(roster), drafted)
		}
	}

	// Round 2 must reverse the order (snake).
	if state.Picks[8].TeamID != teams[len(teams)-1].ID {
		t.Errorf("pick 9 should belong to the last team, got %s", state.Picks[8].TeamID)
	}

	data := service.DraftData(request)
	if available, _ := data["available"].([]map[string]any); len(available) != 150-totalPicks {
		t.Errorf("available after draft = %d", len(available))
	}
	picks, _ := data["picks"].([]map[string]any)
	if len(picks) != totalPicks {
		t.Fatalf("pick tape = %d", len(picks))
	}
	lastPlayer, _ := picks[totalPicks-1]["player"].(map[string]any)
	if name, _ := lastPlayer["name"].(string); name == "" {
		t.Error("pick tape lost player identity")
	}
}

func TestEmailAllowedMergesEnvAndInvites(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "")
	if !service.EmailAllowed("anyone@example.com") {
		t.Error("empty lists must leave the league open")
	}
	t.Setenv("LEAGUE_ALLOWED_EMAILS", "env@example.com")
	if !service.EmailAllowed("ENV@example.com") {
		t.Error("env allowlist match failed")
	}
	if service.EmailAllowed("other@example.com") {
		t.Error("non-listed email allowed")
	}
	if err := service.store.AddInvite("other@example.com"); err != nil {
		t.Fatal(err)
	}
	if !service.EmailAllowed("other@example.com") {
		t.Error("stored invite not honored")
	}
}

func TestBoardFlowThroughService(t *testing.T) {
	service := newTestService(t, true) // demo mode: guest board key
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board", nil)

	if _, err := service.BoardAdd(request, "pool-003"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BoardAdd(request, "pool-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BoardAdd(request, "does-not-exist"); err == nil {
		t.Error("unknown player accepted onto board")
	}
	if err := service.BoardMove(request, "pool-001", "up"); err != nil {
		t.Fatal(err)
	}

	data := service.BoardData(request)
	board, _ := data["board"].([]map[string]any)
	if len(board) != 2 || board[0]["id"] != "pool-001" {
		t.Fatalf("board order wrong: %+v", board)
	}
	available, _ := data["available"].([]map[string]any)
	if len(available) != 18 {
		t.Fatalf("available should exclude board entries: %d", len(available))
	}

	// The draft room surfaces the board, minus already-picked players.
	if _, _, _, err := service.MakePick(request, teamOnClock(1), "pool-001"); err != nil {
		t.Fatal(err)
	}
	draft := service.DraftData(request)
	panel, _ := draft["board"].([]map[string]any)
	if len(panel) != 1 || panel[0]["id"] != "pool-003" {
		t.Fatalf("draft board panel wrong: %+v", panel)
	}
}

func TestAdminGuardsAndControls(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")
	if err := service.AdminAddInvite(request, "x@example.com"); err == nil {
		t.Fatal("unauthenticated admin action must fail")
	}

	demo := newTestService(t, true) // demo mode grants commissioner
	if err := demo.AdminAddInvite(request, "x@example.com"); err != nil {
		t.Fatal(err)
	}
	data := demo.AdminData(request)
	invites, _ := data["invites"].([]map[string]any)
	found := false
	for _, invite := range invites {
		if invite["email"] == "x@example.com" && invite["removable"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("invite missing from admin data: %+v", invites)
	}

	if _, err := demo.store.AssignMember("x@example.com", "X"); err != nil {
		t.Fatal(err)
	}
	team, err := demo.AdminReleaseSeat(request, "team-1")
	if err != nil {
		t.Fatal(err)
	}
	if team.Manager != "" {
		t.Fatalf("released seat still shows a manager: %+v", team)
	}
	seats, _ := demo.AdminData(request)["seats"].([]map[string]any)
	if seats[0]["claimed"] != false || seats[0]["manager"] != "UNCLAIMED" {
		t.Fatalf("seat display wrong after release: %+v", seats[0])
	}
}

func TestUnclaimedTeamDisplay(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/team", nil)
	data := service.TeamData(request)
	team, _ := data["team"].(map[string]any)
	if team["claimed"] != false || team["manager"] != "UNCLAIMED" || team["record"] != "0–0" {
		t.Fatalf("unclaimed team display wrong: %+v", team)
	}
	if data["drafted"] != false {
		t.Error("fresh team must have an empty roster")
	}
	if roster, _ := data["roster"].([]map[string]any); len(roster) != 0 {
		t.Fatalf("roster should be empty, got %d", len(roster))
	}
}

func TestRehearsalPicksSurviveLivePoolSwap(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)

	// Rehearsal pick from the demo pool, before any live pool exists.
	if _, _, _, err := service.MakePick(request, teamOnClock(1), "p-01"); err != nil {
		t.Fatalf("demo pick: %v", err)
	}
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(150), 3, "live" })

	data := service.DraftData(request)
	picks, _ := data["picks"].([]map[string]any)
	if len(picks) != 1 {
		t.Fatalf("picks = %d", len(picks))
	}
	player, _ := picks[0]["player"].(map[string]any)
	if name, _ := player["name"].(string); name != "Ja'Marr Chase" {
		t.Errorf("rehearsal pick lost its name after pool swap: %q", name)
	}
}

package league

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func newTestService(t *testing.T, demo bool) *Service {
	t.Helper()
	avatarAnchor := t.TempDir()
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.draftLifecycleBypass = true
	return &Service{
		store:             store,
		feed:              newLiveFeed(nil),
		draftAt:           time.Now().Add(-time.Hour),
		demoMode:          demo,
		teams:             defaultTeams(),
		players:           defaultPlayers(),
		cfg:               DefaultConfig(),
		avatarRoot:        filepath.Join(avatarAnchor, "avatars"),
		avatarDurableRoot: avatarAnchor,
		defaultBadgeRoot:  filepath.Join(t.TempDir(), "avatar-defaults"),
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
	if !ok || len(available) != poolPageSize {
		t.Fatalf("available = %d, want first page size %d", len(available), poolPageSize)
	}
	if data["pool_total"] != 150 || data["pool_page"] != 1 || data["pool_pages"] != 3 {
		t.Fatalf("pagination = total %v page %v/%v, want 150 page 1/3", data["pool_total"], data["pool_page"], data["pool_pages"])
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

// TestCacheModePoolIsNotReportedLive proves the second freshness-signal
// defect: a pool loaded from the on-disk snapshot at boot ("cache") is
// explicitly not live, so every page that surfaces pool_live must report
// false for it — otherwise the OFFLINE POOL warning never fires and the
// masthead live dot renders as if a fresh sync just landed.
func TestCacheModePoolIsNotReportedLive(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(150)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 7, "cache" })
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)

	draft := service.DraftData(request)
	if draft["pool_label"] != "cache" || draft["pool_live"] != false {
		t.Errorf("DraftData pool labels wrong: label=%v live=%v", draft["pool_label"], draft["pool_live"])
	}

	players := service.PlayersData(request)
	if players["pool_label"] != "cache" || players["pool_live"] != false {
		t.Errorf("PlayersData pool labels wrong: label=%v live=%v", players["pool_label"], players["pool_live"])
	}

	board := service.BoardData(request)
	if board["pool_live"] != false {
		t.Errorf("BoardData pool_live wrong: %v", board["pool_live"])
	}
}

func TestDraftDataSurfacesViewerReadyAndAutopickState(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)

	initial := service.DraftData(request)
	if initial["viewer_ready"] != false || initial["viewer_autopick"] != false {
		t.Fatalf("initial controls = ready:%v autopick:%v", initial["viewer_ready"], initial["viewer_autopick"])
	}

	if _, err := service.store.ToggleReady("team-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetAutopick("team-1", true); err != nil {
		t.Fatal(err)
	}
	updated := service.DraftData(request)
	if updated["viewer_ready"] != true || updated["viewer_autopick"] != true {
		t.Fatalf("updated controls = ready:%v autopick:%v", updated["viewer_ready"], updated["viewer_autopick"])
	}
}

func TestEmptySourceFallsBackToDemoPool(t *testing.T) {
	service := newTestService(t, true)
	service.SetPlayerSource(func() ([]Player, int64, string) { return nil, 0, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	data := service.DraftData(request)
	available, _ := data["available"].([]map[string]any)
	if len(available) != len(defaultPlayers()) {
		t.Fatalf("fallback pool first page = %d players, want %d", len(available), len(defaultPlayers()))
	}
	if data["pool_total"] != len(defaultPlayers()) {
		t.Fatalf("fallback pool total = %v, want %d", data["pool_total"], len(defaultPlayers()))
	}
	if data["pool_label"] != "demo" {
		t.Errorf("pool_label = %v", data["pool_label"])
	}
}

func TestDraftDataServerFiltersAndClampsPoolPages(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(123)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 7, "live" })

	request, _ := http.NewRequest(http.MethodGet, "/draft?pos=WR&q=pool&page=99", nil)
	data := service.DraftData(request)
	available, ok := data["available"].([]map[string]any)
	if !ok {
		t.Fatal("available must be a typed player row slice")
	}
	if len(available) != 21 {
		t.Fatalf("filtered final page = %d rows, want 21", len(available))
	}
	if data["pool_total"] != 21 || data["pool_page"] != 1 || data["pool_pages"] != 1 {
		t.Fatalf("filtered pagination = total %v page %v/%v, want 21 page 1/1", data["pool_total"], data["pool_page"], data["pool_pages"])
	}
	for _, row := range available {
		if row["position"] != "WR" {
			t.Fatalf("filtered row leaked position %v", row["position"])
		}
	}

	request, _ = http.NewRequest(http.MethodGet, "/draft?page=2", nil)
	data = service.DraftData(request)
	available, _ = data["available"].([]map[string]any)
	if len(available) != poolPageSize {
		t.Fatalf("second page = %d rows, want %d", len(available), poolPageSize)
	}
	if data["pool_total"] != 123 || data["pool_page"] != 2 || data["pool_pages"] != 3 {
		t.Fatalf("second-page pagination = total %v page %v/%v, want 123 page 2/3", data["pool_total"], data["pool_page"], data["pool_pages"])
	}
}

func TestFullSnakeDraftAndRosters(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(150)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)
	teams := defaultTeams()
	totalPicks := len(teams) * DraftRounds
	for number := 1; number <= totalPicks; number++ {
		onClock := teamOnClock(nil, number)
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
		if !drafted || len(roster) != DraftRounds {
			t.Fatalf("team %s roster = %d drafted=%v", team.ID, len(roster), drafted)
		}
	}

	// Round 2 must reverse the order (snake).
	if state.Picks[8].TeamID != teams[len(teams)-1].ID {
		t.Errorf("pick 9 should belong to the last team, got %s", state.Picks[8].TeamID)
	}

	// The draft is complete; pick 121 must be rejected.
	if _, _, _, err := service.MakePick(request, teamOnClock(nil, totalPicks+1), "pool-149"); err == nil {
		t.Error("pick past the final round must be rejected")
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
	if _, _, _, err := service.MakePick(request, teamOnClock(nil, 1), "pool-001"); err != nil {
		t.Fatal(err)
	}
	draft := service.DraftData(request)
	panel, _ := draft["board"].([]map[string]any)
	if len(panel) != 1 || panel[0]["id"] != "pool-003" {
		t.Fatalf("draft board panel wrong: %+v", panel)
	}
}

func TestBoardKeyForViewerSharesPrimaryBoardWithCoManager(t *testing.T) {
	state := PersistedState{Members: map[string]Member{
		"primary@example.com": {TeamID: "team-1", Email: "primary@example.com", Name: "Primary"},
		"co@example.com":      {TeamID: "team-1", Email: "co@example.com", Name: "Co", Role: "co"},
		"free@example.com":    {Email: "free@example.com", Name: "Free agent"},
	}, Boards: map[string][]string{
		"primary@example.com": {"pool-003", "pool-001"},
		"co@example.com":      {"pool-002"},
	}}

	if got := boardKeyForViewer(state, "CO@EXAMPLE.COM"); got != "primary@example.com" {
		t.Fatalf("co-manager board key = %q, want primary manager key", got)
	}
	if got := boardKeyForViewer(state, "primary@example.com"); got != "primary@example.com" {
		t.Fatalf("primary board key = %q", got)
	}
	if got := boardKeyForViewer(state, "free@example.com"); got != "free@example.com" {
		t.Fatalf("unseated board key = %q", got)
	}
	if got := boardKeyForViewer(state, "demo-guest"); got != "demo-guest" {
		t.Fatalf("demo board key = %q", got)
	}

	service := newTestService(t, false)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(10), 1, "live" })
	if got, ok := service.autopickChoice(state, "team-1"); !ok || got != "pool-003" {
		t.Fatalf("seat autopick = %q, ok=%v; want primary/shared board head pool-003", got, ok)
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

	if _, _, err := demo.store.AssignMember("x@example.com", "X"); err != nil {
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

func TestAdminDataPoolStatusSeam(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	pool, _ := service.AdminData(request)["pool"].(map[string]any)
	if pool["mode"] != "unknown" || pool["last_sync"] != "never" {
		t.Fatalf("default pool status wrong: %+v", pool)
	}
	service.SetPoolStatus(func() map[string]any {
		return map[string]any{"mode": "live", "players": 400}
	})
	pool, _ = service.AdminData(request)["pool"].(map[string]any)
	if pool["mode"] != "live" || pool["players"] != 400 {
		t.Fatalf("injected pool status not surfaced: %+v", pool)
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
	if data["predraft_visible"] != false {
		t.Error("an unclaimed rehearsal seat must not show post-claim setup progress")
	}
	if roster, _ := data["roster"].([]map[string]any); len(roster) != 0 {
		t.Fatalf("roster should be empty, got %d", len(roster))
	}
}

func TestTeamDataSurfacesTruthfulPredraftProgress(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/team", nil)
	if _, _, err := service.store.AssignMember("manager@example.com", "Manager"); err != nil {
		t.Fatal(err)
	}

	initial := service.TeamData(request)
	if initial["predraft_visible"] != true || initial["predraft_has_board"] != false || initial["predraft_board_count"] != 0 || initial["predraft_ready"] != false {
		t.Fatalf("initial pre-draft state = visible:%v has_board:%v count:%v ready:%v", initial["predraft_visible"], initial["predraft_has_board"], initial["predraft_board_count"], initial["predraft_ready"])
	}

	if err := service.store.BoardAdd("demo-guest", defaultPlayers()[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.ToggleReady("team-1"); err != nil {
		t.Fatal(err)
	}
	updated := service.TeamData(request)
	if updated["predraft_has_board"] != true || updated["predraft_board_count"] != 1 || updated["predraft_ready"] != true {
		t.Fatalf("updated pre-draft state = has_board:%v count:%v ready:%v", updated["predraft_has_board"], updated["predraft_board_count"], updated["predraft_ready"])
	}

	service.store.mu.Lock()
	service.store.state.DraftStarted = true
	service.store.mu.Unlock()
	started := service.TeamData(request)
	if started["predraft_visible"] != false {
		t.Fatalf("predraft_visible after start = %v, want false", started["predraft_visible"])
	}
}

func TestDivisionMaps(t *testing.T) {
	service := newTestService(t, true)
	divisions := service.divisionMaps(service.store.Snapshot())
	if len(divisions) != 2 {
		t.Fatalf("divisions = %d, want 2", len(divisions))
	}
	if divisions[0]["name"] != "EAST" || divisions[1]["name"] != "WEST" {
		t.Fatalf("division order wrong: %v then %v", divisions[0]["name"], divisions[1]["name"])
	}
	for _, division := range divisions {
		teams, ok := division["teams"].([]map[string]any)
		if !ok || len(teams) != 4 {
			t.Fatalf("division %v has %d teams, want 4", division["name"], len(teams))
		}
		for _, team := range teams {
			if team["division"] != division["name"] {
				t.Errorf("team division %v does not match group %v", team["division"], division["name"])
			}
		}
	}
}

func TestAdminRenameTeamOverridesTeamMap(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	team, err := service.AdminRenameTeam(request, "team-1", "  Commissioner's Pick  ")
	if err != nil {
		t.Fatal(err)
	}
	if team.Name != "Commissioner's Pick" {
		t.Fatalf("rename result = %q", team.Name)
	}

	view := service.teamMap(service.teamView(service.store.Snapshot(), "team-1"))
	if view["name"] != "Commissioner's Pick" {
		t.Fatalf("teamMap did not carry the rename: %v", view["name"])
	}

	divisions := service.divisionMaps(service.store.Snapshot())
	aquaTeams, _ := divisions[0]["teams"].([]map[string]any)
	if aquaTeams[0]["name"] != "Commissioner's Pick" {
		t.Fatalf("divisionMaps did not carry the rename: %v", aquaTeams[0]["name"])
	}
}

func TestRehearsalPicksSurviveLivePoolSwap(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)

	// Rehearsal pick from the demo pool, before any live pool exists.
	if _, _, _, err := service.MakePick(request, teamOnClock(nil, 1), "p-01"); err != nil {
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

func TestDraftDataFollowsCustomOrder(t *testing.T) {
	service := newTestService(t, true)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/draft", nil)

	custom := []string{"team-8", "team-7", "team-6", "team-5", "team-4", "team-3", "team-2", "team-1"}
	if err := service.store.SetDraftOrder(custom); err != nil {
		t.Fatal(err)
	}

	data := service.DraftData(request)
	teams, _ := data["teams"].([]map[string]any)
	if len(teams) != len(custom) {
		t.Fatalf("teams grid = %d entries, want %d", len(teams), len(custom))
	}
	for index, team := range teams {
		if team["id"] != custom[index] {
			t.Fatalf("team grid position %d = %v, want %s", index, team["id"], custom[index])
		}
	}
	if data["on_clock_id"] != custom[0] {
		t.Fatalf("on_clock_id = %v, want %s", data["on_clock_id"], custom[0])
	}
	if data["order_randomized"] != true {
		t.Error("order_randomized must be true once a custom order is set")
	}
	if data["league_mode"] != "DYNASTY" {
		t.Errorf("league_mode = %v, want DYNASTY", data["league_mode"])
	}

	pick, _, team, err := service.MakePick(request, custom[0], "pool-001")
	if err != nil {
		t.Fatal(err)
	}
	if pick.TeamID != custom[0] || team.ID != custom[0] {
		t.Fatalf("MakePick did not honor the custom order: %+v %s", pick, team.ID)
	}
}

func TestAdminRandomizeDraftOrder(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	if err := service.AdminRandomizeDraftOrder(request); err != nil {
		t.Fatal(err)
	}
	state := service.store.Snapshot()
	if !isPermutationOfDefaultTeams(state.DraftOrder) {
		t.Fatalf("draft order is not a permutation of the eight teams: %v", state.DraftOrder)
	}

	data := service.AdminData(request)
	if data["order_randomized"] != true {
		t.Error("order_randomized must be true after randomizing")
	}
	draftOrder, ok := data["draft_order"].([]map[string]any)
	if !ok || len(draftOrder) != 8 {
		t.Fatalf("admin draft_order = %v", data["draft_order"])
	}
}

func isPermutationOfDefaultTeams(order []string) bool {
	want := defaultTeamIDs()
	if len(order) != len(want) {
		return false
	}
	counts := map[string]int{}
	for _, id := range want {
		counts[id]++
	}
	for _, id := range order {
		counts[id]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func TestScoringDataDefaultsAndOverride(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodGet, "/scoring", nil)

	data := service.ScoringData(request)
	if data["locked"] != false || data["editable"] != true {
		t.Fatalf("scoring should be open before the season starts: %+v", data)
	}
	groups, _ := data["groups"].([]ScoringRuleGroup)
	if len(groups) == 0 || groups[0].Name != "PASSING" {
		t.Fatalf("first group = %v, want PASSING", groups)
	}
	rules := groups[0].Rules
	passYards := rules[0]
	if passYards.Key != "passYards" || passYards.Points != "0.04" || passYards.IsDefault != true {
		t.Fatalf("passYards default wrong: %+v", passYards)
	}

	rule, err := service.AdminSetScoring(request, "passYards", "0.06")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Points != 0.06 {
		t.Fatalf("returned rule points = %v, want 0.06", rule.Points)
	}

	data = service.ScoringData(request)
	groups, _ = data["groups"].([]ScoringRuleGroup)
	rules = groups[0].Rules
	passYards = rules[0]
	if passYards.Points != "0.06" || passYards.IsDefault != false {
		t.Fatalf("override not reflected: %+v", passYards)
	}

	if _, err := service.AdminSetScoring(request, "passYards", "0.04"); err != nil {
		t.Fatal(err)
	}
	data = service.ScoringData(request)
	groups, _ = data["groups"].([]ScoringRuleGroup)
	rules = groups[0].Rules
	passYards = rules[0]
	if passYards.IsDefault != true || passYards.Points != "0.04" {
		t.Fatalf("setting the default value must clear the override: %+v", passYards)
	}

	if _, err := service.AdminSetScoring(request, "passYards", "999"); err == nil {
		t.Error("out-of-range value accepted")
	}
	if _, err := service.AdminSetScoring(request, "not-a-key", "1"); err == nil {
		t.Error("unknown scoring key accepted")
	}
	if _, err := service.AdminSetScoring(request, "passYards", "not-a-number"); err == nil {
		t.Error("non-numeric value accepted")
	}
}

func TestScoringLockRejectsEdits(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodGet, "/scoring", nil)
	t.Setenv("SEASON_START_AT", time.Now().Add(-time.Hour).Format(time.RFC3339))

	data := service.ScoringData(request)
	if data["locked"] != true || data["editable"] != false {
		t.Fatalf("scoring must lock once the season has started: %+v", data)
	}
	if _, err := service.AdminSetScoring(request, "passYards", "0.06"); err == nil {
		t.Error("locked scoring accepted an edit")
	}
	if err := service.AdminResetScoring(request); err == nil {
		t.Error("locked scoring accepted a reset")
	}
}

func TestLeaderMapsRanksTopFourByProjection(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(10)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })

	leaders := service.leaderMaps()
	if len(leaders) != 4 {
		t.Fatalf("leaders = %d, want 4", len(leaders))
	}
	for index, leader := range leaders {
		want := pool[index]
		if leader["rank"] != fmt.Sprintf("%02d", index+1) {
			t.Errorf("leader %d rank = %v", index, leader["rank"])
		}
		if leader["name"] != want.Name || leader["position"] != want.Position {
			t.Errorf("leader %d identity = %+v, want %+v", index, leader, want)
		}
		if leader["points"] != fmt.Sprintf("%.1f", want.Projection) {
			t.Errorf("leader %d points = %v, want %.1f", index, leader["points"], want.Projection)
		}
		wantTrend := "—"
		if want.ADPRank > 0 {
			wantTrend = fmt.Sprintf("ADP #%d", want.ADPRank)
		}
		if leader["trend"] != wantTrend {
			t.Errorf("leader %d trend = %v, want %v", index, leader["trend"], wantTrend)
		}
	}
}

func TestDashboardDataFeaturedEmptyOnFreshService(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	data := service.DashboardData(context.Background(), request)
	if data["featured_empty"] != true {
		t.Fatalf("featured_empty = %v, want true", data["featured_empty"])
	}
	featured, _ := data["featured"].([]map[string]any)
	if len(featured) != 0 {
		t.Fatalf("featured = %d, want 0", len(featured))
	}
}

// TestDashboardDataIncludesHasSeatAndPickemHome pins build item 3's
// plumbing: DashboardData precomputes has_seat (from viewer["has_seat"])
// and always carries a pickem_home summary, so app/page.gsx can branch on
// a plain server-computed bool rather than deriving seat state itself.
// Demo mode is the only signed-in-like state reachable without forging
// auth (see TestCommissionerForceAutopick's doc comment), and demo mode
// is always fully seated (Viewer's has_seat == s.demoMode) — the seatless
// branch itself is covered directly by TestPickemHomeSummaryComputation
// and verified visually in the HQ page screenshot.
func TestDashboardDataIncludesHasSeatAndPickemHome(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	data := service.DashboardData(context.Background(), request)
	if data["has_seat"] != true {
		t.Fatalf("has_seat in demo mode = %v, want true", data["has_seat"])
	}
	home, ok := data["pickem_home"].(map[string]any)
	if !ok {
		t.Fatalf("pickem_home missing or wrong shape: %+v", data["pickem_home"])
	}
	for _, key := range []string{"week", "unpicked_count", "season_correct", "season_total", "has_record", "streak", "has_streak", "has_games_this_week"} {
		if _, ok := home[key]; !ok {
			t.Errorf("pickem_home missing key %q: %+v", key, home)
		}
	}
}

// TestPickemHomeSummaryComputation exercises pickemHomeSummary directly —
// the seatless home dashboard's data source — with a real schedule and a
// mix of picked and unpicked games, since a seatless Viewer branch is not
// reachable through the public request API in this test package.
func TestPickemHomeSummaryComputation(t *testing.T) {
	service := newTestService(t, true) // demo mode: viewerKey is "demo-guest"
	now := time.Now()
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	if err := service.store.SetPickem("demo-guest", "g-final", "BUF"); err != nil {
		t.Fatal(err)
	}
	// g-locked and g-open are both week 1 and still unpicked; g-locked has
	// already kicked off so it does not count toward "unpicked this week".
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	summary := service.pickemHomeSummary(request, service.store.Snapshot(), now)

	if summary["week"] != 1 {
		t.Fatalf("week = %v, want 1", summary["week"])
	}
	if summary["unpicked_count"] != 1 {
		t.Fatalf("unpicked_count = %v, want 1 (only g-open is still open and unpicked)", summary["unpicked_count"])
	}
	if summary["season_correct"] != 1 || summary["season_total"] != 1 {
		t.Fatalf("season record = %v/%v, want 1/1", summary["season_correct"], summary["season_total"])
	}
	if summary["has_record"] != true {
		t.Error("has_record should be true")
	}
	if summary["has_games_this_week"] != true {
		t.Error("has_games_this_week should be true")
	}
}

func TestViewerIncludesIsCommissionerInDemoMode(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	viewer := service.Viewer(request)
	if viewer["is_commissioner"] != true {
		t.Fatalf("is_commissioner = %v, want true in demo mode", viewer["is_commissioner"])
	}
}

// TestPlayerMapEmitsBreakdownJerseyAndHistKeys checks the frontend contract:
// jersey, has_breakdown, breakdown, breakdown_total, has_hist, and hist all
// appear on the rendered player map, with jersey prefixed "#" only when set.
func TestPlayerMapEmitsBreakdownJerseyAndHistKeys(t *testing.T) {
	service := newTestService(t, true)
	loaded := Player{
		ID: "p-jersey", Name: "Loaded Guy", Position: "RB", NFLTeam: "CIN",
		Jersey:    "26",
		ProjStats: map[string]float64{"rushYds": 80, "rushTD": 1},
		Hist:      "2025 · 16 G · 900 rush yds · 6 TD · 12.4 FPts",
	}
	entry := playerMap(loaded, service.currentScoringValues(), matchupIndex{})
	if entry["jersey"] != "#26" {
		t.Errorf("jersey = %v, want #26", entry["jersey"])
	}
	if entry["has_breakdown"] != true {
		t.Errorf("has_breakdown = %v, want true", entry["has_breakdown"])
	}
	rows, ok := entry["breakdown"].([]map[string]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("breakdown rows = %+v", entry["breakdown"])
	}
	if entry["breakdown_total"] != "14.0" {
		t.Errorf("breakdown_total = %v, want 14.0", entry["breakdown_total"])
	}
	if entry["has_hist"] != true || entry["hist"] != loaded.Hist {
		t.Errorf("hist fields wrong: has_hist=%v hist=%v", entry["has_hist"], entry["hist"])
	}

	bare := Player{ID: "p-bare", Name: "No Data", Position: "WR", NFLTeam: "CIN"}
	bareEntry := playerMap(bare, nil, matchupIndex{})
	if bareEntry["jersey"] != "" {
		t.Errorf("jersey = %v, want empty", bareEntry["jersey"])
	}
	if bareEntry["has_breakdown"] != false || bareEntry["has_hist"] != false {
		t.Errorf("bare player defaults wrong: has_breakdown=%v has_hist=%v", bareEntry["has_breakdown"], bareEntry["has_hist"])
	}
	if bareEntry["breakdown_total"] != "" || bareEntry["hist"] != "" {
		t.Errorf("bare player breakdown_total/hist wrong: %v %v", bareEntry["breakdown_total"], bareEntry["hist"])
	}
	bareRows, ok := bareEntry["breakdown"].([]map[string]any)
	if !ok || bareRows != nil {
		t.Errorf("bare player breakdown = %+v, want nil", bareEntry["breakdown"])
	}
}

// TestPlayerMapEmitsRookieAndDraftCapitalKeys checks the rookie chip's
// frontend contract (owner directive 2026-08-18 — "show the reasoning"):
// is_rookie, draft_capital, and has_draft_capital all appear, and a player
// with no DraftCapital set (the graceful-degradation case: main.go's
// fantasy.Player.DraftCapitalLabel found no usable Tank01 draft slot, or
// the player is not a rookie at all) renders has_draft_capital == false
// with an empty label — never a placeholder — so a row template's chip
// stays hidden rather than showing garbage.
func TestPlayerMapEmitsRookieAndDraftCapitalKeys(t *testing.T) {
	service := newTestService(t, true)
	rookie := Player{
		ID: "p-rookie", Name: "Capital Rookie", Position: "RB", NFLTeam: "ARI",
		Rookie: true, DraftCapital: "R1 · P3",
	}
	entry := playerMap(rookie, service.currentScoringValues(), matchupIndex{})
	if entry["is_rookie"] != true {
		t.Errorf("is_rookie = %v, want true", entry["is_rookie"])
	}
	if entry["draft_capital"] != "R1 · P3" {
		t.Errorf("draft_capital = %v, want %q", entry["draft_capital"], "R1 · P3")
	}
	if entry["has_draft_capital"] != true {
		t.Errorf("has_draft_capital = %v, want true", entry["has_draft_capital"])
	}

	veteran := Player{ID: "p-veteran", Name: "No Capital Veteran", Position: "WR", NFLTeam: "CIN"}
	veteranEntry := playerMap(veteran, service.currentScoringValues(), matchupIndex{})
	if veteranEntry["is_rookie"] != false {
		t.Errorf("is_rookie = %v, want false", veteranEntry["is_rookie"])
	}
	if veteranEntry["draft_capital"] != "" {
		t.Errorf("draft_capital = %v, want empty (no placeholder)", veteranEntry["draft_capital"])
	}
	if veteranEntry["has_draft_capital"] != false {
		t.Errorf("has_draft_capital = %v, want false", veteranEntry["has_draft_capital"])
	}
}

// TestHistoricalSourceAppliesInBuildPool checks that buildPool fills Hist
// from the attached HistoricalSource for matching players, on both the
// ordered players slice and the byID lookup, and leaves an existing Hist
// or an unmatched player untouched.
func TestHistoricalSourceAppliesInBuildPool(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(3)
	pool[1].Hist = "already set"
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 9, "live" })
	service.SetHistoricalSource(func(name, position string) (string, bool) {
		if name == "Pool Player 001" && position == "QB" {
			return "2025 · 17 G · 4,100 pass yds · 30 TD · 8 INT · 24.6 FPts", true
		}
		return "", false
	})

	built := service.pool()
	if built.players[0].Hist == "" {
		t.Fatalf("historical line missing on matched player: %+v", built.players[0])
	}
	if byID, ok := built.byID["pool-001"]; !ok || byID.Hist != built.players[0].Hist {
		t.Fatalf("historical line not mirrored into byID: %+v", byID)
	}
	if built.players[1].Hist != "already set" {
		t.Fatalf("existing hist overwritten: %+v", built.players[1])
	}
	if built.players[2].Hist != "" {
		t.Fatalf("unmatched player gained a hist line: %+v", built.players[2])
	}
}

// TestCountdownDHMSLabelMatchesRuntimeFormat proves the server-rendered
// initial text for a data-gosx-countdown-format="dhms" element matches the
// v0.43.0 client runtime's own compact formatter exactly (days unpadded,
// "d " separator, then zero-padded HH:MM:SS) — see gosx's
// client/runtime/host/navigation.ts formatCountdownDHMS. A mismatch would
// show a visible jump the instant the runtime's first 1-second tick fires.
func TestCountdownDHMSLabelMatchesRuntimeFormat(t *testing.T) {
	cases := []struct {
		remaining time.Duration
		want      string
	}{
		{remaining: 0, want: "0d 00:00:00"},
		{remaining: -time.Hour, want: "0d 00:00:00"},
		{remaining: 45 * time.Second, want: "0d 00:00:45"},
		{remaining: 5*24*time.Hour + 19*time.Hour + 40*time.Minute + 56*time.Second, want: "5d 19:40:56"},
	}
	for _, c := range cases {
		if got := countdownDHMSLabel(c.remaining); got != c.want {
			t.Fatalf("countdownDHMSLabel(%v) = %q, want %q", c.remaining, got, c.want)
		}
	}
}

// TestCountdownMMSSLabelMatchesRuntimeFormat proves the server-rendered
// initial text for a data-gosx-countdown-format="mm:ss" element (the pick
// clock) matches the client runtime's own compact formatter exactly
// (minutes unpadded, seconds zero-padded) — see
// client/runtime/host/navigation.ts formatCountdownMMSS.
func TestCountdownMMSSLabelMatchesRuntimeFormat(t *testing.T) {
	cases := []struct {
		remainingSeconds int
		want             string
	}{
		{remainingSeconds: 0, want: "0:00"},
		{remainingSeconds: -5, want: "0:00"},
		{remainingSeconds: 5, want: "0:05"},
		{remainingSeconds: 90, want: "1:30"},
		{remainingSeconds: 605, want: "10:05"},
	}
	for _, c := range cases {
		if got := countdownMMSSLabel(c.remainingSeconds); got != c.want {
			t.Fatalf("countdownMMSSLabel(%d) = %q, want %q", c.remainingSeconds, got, c.want)
		}
	}
}

// TestClockViewIncludesRemainingLabel proves clockView's remaining_label
// key (the pick clock's data-gosx-countdown initial text) always agrees
// with its remaining_seconds sibling, for both the armed and the
// paused/unarmed shapes.
func TestClockViewIncludesRemainingLabel(t *testing.T) {
	service := newTestService(t, false)
	now := time.Now()

	unarmed := service.clockView(PersistedState{}, now)
	if unarmed["remaining_label"] != "0:00" {
		t.Fatalf("unarmed remaining_label = %v, want 0:00", unarmed["remaining_label"])
	}

	armed := service.clockView(PersistedState{ClockDeadline: now.Add(90 * time.Second)}, now)
	if armed["remaining_label"] != "1:30" {
		t.Fatalf("armed remaining_label = %v, want 1:30 (remaining_seconds=%v)", armed["remaining_label"], armed["remaining_seconds"])
	}

	paused := service.clockView(PersistedState{ClockDeadline: now.Add(90 * time.Second), ClockPaused: true}, now)
	if paused["remaining_label"] != "0:00" {
		t.Fatalf("paused remaining_label = %v, want 0:00", paused["remaining_label"])
	}
}

func TestIdentityUnavailableViewsFailClosedForAllPickers(t *testing.T) {
	service := newTestService(t, true)
	service.store.mu.Lock()
	service.store.poisonedState = cloneState(service.store.state)
	service.store.persistencePoison = ErrPersistenceIndeterminate
	service.store.mu.Unlock()

	request, _ := http.NewRequest(http.MethodGet, "/team", nil)
	teamData := service.TeamData(request)
	if teamData["identity_available"] != false {
		t.Fatalf("TeamData identity_available = %#v, want false", teamData["identity_available"])
	}
	if teamData["identity_error"] != identityUnavailableCopy {
		t.Fatalf("TeamData identity_error = %#v, want truthful copy", teamData["identity_error"])
	}
	if grid, ok := teamData["badge_grid"].([]map[string]any); !ok || len(grid) != 0 {
		t.Fatalf("poisoned TeamData badge_grid = %#v, want empty", teamData["badge_grid"])
	}

	signupData := service.SignupData(request)
	if signupData["identity_available"] != false {
		t.Fatalf("SignupData identity_available = %#v, want false", signupData["identity_available"])
	}
	if grid, ok := signupData["badge_grid"].([]UnclaimedBadgeOption); !ok || len(grid) != 0 {
		t.Fatalf("poisoned SignupData badge_grid = %#v, want empty", signupData["badge_grid"])
	}

	adminData := service.AdminData(request)
	if adminData["identity_available"] != false {
		t.Fatalf("AdminData identity_available = %#v, want false", adminData["identity_available"])
	}
	seats, ok := adminData["seats"].([]map[string]any)
	if !ok || len(seats) == 0 {
		t.Fatalf("AdminData seats = %#v, want seats", adminData["seats"])
	}
	for _, seat := range seats {
		if seat["identity_available"] != false || seat["identity_error"] != identityUnavailableCopy {
			t.Fatalf("poisoned admin seat identity state = %#v", seat)
		}
	}
}

func TestDashboardStandingsUseFinalScheduleResults(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)

	schedule := SeasonSchedule{
		Season: service.cfg.Season,
		Seed:   17,
		Weeks: []ScheduleWeek{
			{
				Week: 1,
				Matchups: []LeagueMatchup{{
					ID:         "week-1-team-1-team-2",
					HomeTeamID: "team-1",
					AwayTeamID: "team-2",
					HomeScore:  123.5,
					AwayScore:  88,
					Final:      true,
				}},
			},
			{
				Week: 2,
				Matchups: []LeagueMatchup{{
					ID:         "week-2-team-1-team-2",
					HomeTeamID: "team-1",
					AwayTeamID: "team-2",
					Final:      false,
				}},
			},
		},
	}
	if err := service.store.SetSchedule(schedule); err != nil {
		t.Fatal(err)
	}

	data := service.DashboardData(context.Background(), request)
	if data["standings_available"] != true {
		t.Fatalf("standings_available = %v, want true", data["standings_available"])
	}
	if data["standings_title"] != "2026 standings" {
		t.Fatalf("standings_title = %v, want current-season title", data["standings_title"])
	}
	if data["standings_note"] != "Through Week 1 · Records and points reflect finalized league matchups." {
		t.Fatalf("standings_note = %v", data["standings_note"])
	}

	divisions, ok := data["divisions"].([]map[string]any)
	if !ok {
		t.Fatalf("divisions shape = %T", data["divisions"])
	}
	var teamOne map[string]any
	for _, division := range divisions {
		teams, _ := division["teams"].([]map[string]any)
		for _, team := range teams {
			if team["id"] == "team-1" {
				teamOne = team
			}
		}
	}
	if teamOne == nil {
		t.Fatal("team-1 missing from state-aware divisions")
	}
	for key, want := range map[string]any{
		"rank":       "01",
		"record":     "1–0",
		"points_for": "123.5",
		"streak":     "W1",
	} {
		if teamOne[key] != want {
			t.Errorf("team-1 %s = %v, want %v", key, teamOne[key], want)
		}
	}

	flat, ok := data["standings"].([]map[string]any)
	if !ok || len(flat) != len(service.Teams()) {
		t.Fatalf("flat standings shape = %T/%d", data["standings"], len(flat))
	}
	if flat[0]["id"] != "team-1" || flat[0]["points_for"] != "123.5" {
		t.Fatalf("flat standings head = %#v, want team-1 with scored points", flat[0])
	}
}

func TestDashboardStandingsWithoutScheduleIsExplicit(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)

	data := service.DashboardData(context.Background(), request)
	if data["standings_available"] != false {
		t.Fatalf("standings_available = %v, want false without a schedule", data["standings_available"])
	}
	if data["standings_title"] != "Standings pending" || data["standings_empty_title"] != "NO SEASON TABLE" {
		t.Fatalf("no-season copy = title:%v empty:%v", data["standings_title"], data["standings_empty_title"])
	}
	if data["standings_note"] != "The commissioner has not published a regular-season schedule yet." {
		t.Fatalf("no-season note = %v", data["standings_note"])
	}

	if err := service.store.SetSchedule(SeasonSchedule{
		Season: service.cfg.Season,
		Weeks: []ScheduleWeek{{Week: 1, Matchups: []LeagueMatchup{{
			ID:         "unscored-week-1",
			HomeTeamID: "team-1",
			AwayTeamID: "team-2",
		}}}},
	}); err != nil {
		t.Fatal(err)
	}
	data = service.DashboardData(context.Background(), request)
	if data["standings_available"] != false || data["standings_empty_title"] != "NO SCORED WEEKS" {
		t.Fatalf("preseason standings state = available:%v empty:%v", data["standings_available"], data["standings_empty_title"])
	}
	if data["standings_title"] != "2026 standings" {
		t.Fatalf("preseason standings title = %v", data["standings_title"])
	}
}

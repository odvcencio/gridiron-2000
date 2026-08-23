package league

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func terminalDraftPicks(players []Player, teams []Team, count int, teamFor func(int, Player) string) []DraftPick {
	picks := make([]DraftPick, 0, count)
	for index := 0; index < count; index++ {
		player := players[index%len(players)]
		if index >= len(players) {
			player.ID = fmt.Sprintf("%s-terminal-%d", player.ID, index)
		}
		teamID := teams[index%len(teams)].ID
		if teamFor != nil {
			teamID = teamFor(index, player)
		}
		picks = append(picks, DraftPick{
			Number:   index + 1,
			Round:    index/len(teams) + 1,
			TeamID:   teamID,
			PlayerID: player.ID,
		})
	}
	return picks
}

func TestTeamTerminalLifecyclePhases(t *testing.T) {
	teams := defaultTeams()
	players := defaultPlayers()
	capacity := CurrentRoster().Total()
	totalPicks := len(teams) * CurrentDraftRounds()

	t.Run("pre draft", func(t *testing.T) {
		lifecycle := resolveTeamTerminalLifecycle(PersistedState{}, 0, capacity)
		if lifecycle.Phase != TeamTerminalPreDraft || lifecycle.DraftStarted || lifecycle.DraftComplete {
			t.Fatalf("lifecycle = %+v, want PRE_DRAFT", lifecycle)
		}
	})

	t.Run("started while another team picked", func(t *testing.T) {
		state := PersistedState{
			DraftStarted: true,
			Picks:        []DraftPick{{Number: 1, Round: 1, TeamID: teams[1].ID, PlayerID: players[0].ID}},
		}
		rosterCount := len(currentRosters(state)[teams[0].ID])
		lifecycle := resolveTeamTerminalLifecycle(state, rosterCount, capacity)
		if lifecycle.Phase != TeamTerminalDraftLiveEmpty || lifecycle.RosterCount != 0 {
			t.Fatalf("lifecycle = %+v, want DRAFT_LIVE_EMPTY with zero occupancy", lifecycle)
		}
	})

	t.Run("one viewer pick", func(t *testing.T) {
		state := PersistedState{
			DraftStarted: true,
			Picks:        []DraftPick{{Number: 1, Round: 1, TeamID: teams[0].ID, PlayerID: players[0].ID}},
		}
		rosterCount := len(currentRosters(state)[teams[0].ID])
		lifecycle := resolveTeamTerminalLifecycle(state, rosterCount, capacity)
		if lifecycle.Phase != TeamTerminalDraftLiveBuilding || lifecycle.RosterCount != 1 {
			t.Fatalf("lifecycle = %+v, want DRAFT_LIVE_BUILDING with one occupied spot", lifecycle)
		}
	})

	t.Run("complete", func(t *testing.T) {
		state := PersistedState{
			DraftStarted: true,
			Picks:        terminalDraftPicks(players, teams, totalPicks, nil),
		}
		lifecycle := resolveTeamTerminalLifecycle(state, len(currentRosters(state)[teams[0].ID]), capacity)
		if lifecycle.Phase != TeamTerminalRosterComplete || !lifecycle.DraftComplete {
			t.Fatalf("lifecycle = %+v, want ROSTER_COMPLETE", lifecycle)
		}
	})
}

func TestTeamTerminalDataUsesFinitePhaseTruth(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/team", nil)

	service.store.mu.Lock()
	service.store.state.DraftStarted = true
	service.store.state.Picks = []DraftPick{{
		Number: 1, Round: 1, TeamID: defaultTeams()[1].ID, PlayerID: defaultPlayers()[0].ID,
	}}
	service.store.mu.Unlock()

	data := service.TeamData(request)
	if data["team_terminal_phase"] != string(TeamTerminalDraftLiveEmpty) {
		t.Fatalf("team terminal phase = %v, want %s", data["team_terminal_phase"], TeamTerminalDraftLiveEmpty)
	}
	if data["team_terminal_roster_empty"] != true || data["team_terminal_draft_live_empty"] != true {
		t.Fatalf("team terminal occupancy flags = empty:%v live-empty:%v", data["team_terminal_roster_empty"], data["team_terminal_draft_live_empty"])
	}
	if data["team_terminal_roster_count"] != 0 || data["team_terminal_roster_capacity"] != CurrentRoster().Total() {
		t.Fatalf("team terminal count/capacity = %v/%v", data["team_terminal_roster_count"], data["team_terminal_roster_capacity"])
	}
}

func TestTeamTerminalDraftRadarExcludesPickedPlayers(t *testing.T) {
	service := newTestService(t, true)
	players := defaultPlayers()
	teams := defaultTeams()
	state := service.store.Snapshot()
	state.DraftStarted = true
	state.Picks = []DraftPick{{Number: 1, Round: 1, TeamID: teams[1].ID, PlayerID: players[0].ID}}

	radar := service.teamTerminalRadar(state, TeamTerminalDraftLiveEmpty, time.Now(), 50)
	if len(radar) == 0 {
		t.Fatal("draft radar is empty with an available pool")
	}
	for _, row := range radar {
		if row["name"] == players[0].Name || row["status"] != "DRAFT TARGET" {
			t.Fatalf("picked player leaked into radar: %+v", row)
		}
		href, _ := row["href"].(string)
		if !strings.HasPrefix(href, "/draft?q=") {
			t.Fatalf("draft target href = %q, want /draft search link", href)
		}
	}
}

func TestTeamTerminalPostDraftRadarUsesRosterAndWaiverTruth(t *testing.T) {
	service := newTestService(t, true)
	players := defaultPlayers()
	teams := defaultTeams()
	totalPicks := len(teams) * CurrentDraftRounds()
	added := players[2]
	dropped := players[3]
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	state := service.store.Snapshot()
	state.DraftStarted = true
	draftPlayers := []Player{players[0], players[1], players[3]}
	draftPlayers = append(draftPlayers, players[5:]...)
	state.Picks = terminalDraftPicks(draftPlayers, teams, totalPicks, func(index int, player Player) string {
		if player.ID == dropped.ID {
			return teams[0].ID
		}
		return teams[index%len(teams)].ID
	})
	state.Transactions = []Transaction{
		{
			ID:     "txn-terminal-add",
			Type:   "add",
			TeamID: teams[0].ID,
			Adds:   []TransactionPlayer{{PlayerID: added.ID, Name: added.Name, Position: added.Position, NFLTeam: added.NFLTeam}},
			By:     "manager",
			At:     now.Add(-time.Hour),
		},
		{
			ID:     "txn-terminal-drop",
			Type:   "drop",
			TeamID: teams[0].ID,
			Drops:  []TransactionPlayer{{PlayerID: dropped.ID, Name: dropped.Name, Position: dropped.Position, NFLTeam: dropped.NFLTeam}},
			By:     "manager",
			At:     now.Add(-time.Hour),
		},
	}

	radar := service.teamTerminalRadar(state, TeamTerminalRosterComplete, now, len(players))
	byName := make(map[string]map[string]any, len(radar))
	for _, row := range radar {
		name, _ := row["name"].(string)
		byName[name] = row
	}
	if _, ok := byName[added.Name]; ok {
		t.Fatalf("transaction-added player %q leaked into post-draft radar", added.Name)
	}
	if _, ok := byName[players[0].Name]; ok {
		t.Fatalf("rostered player %q leaked into post-draft radar", players[0].Name)
	}
	droppedRow, ok := byName[dropped.Name]
	if !ok || droppedRow["status"] != "ON WAIVERS" || droppedRow["has_resolution"] != true {
		t.Fatalf("dropped player radar row = %+v, want ON WAIVERS with resolution", droppedRow)
	}
	resolution, _ := droppedRow["resolution"].(string)
	if !strings.HasPrefix(resolution, "Resolves ") {
		t.Fatalf("waiver resolution = %q, want readable resolution", resolution)
	}
	freeRow, ok := byName[players[4].Name]
	if !ok || freeRow["status"] != "FREE AGENT" {
		t.Fatalf("free-agent radar row = %+v, want FREE AGENT", freeRow)
	}
	href, _ := freeRow["href"].(string)
	if !strings.HasPrefix(href, "/players?q=") {
		t.Fatalf("free-agent href = %q, want /players search link", href)
	}
}

func TestTeamTerminalRadarCanBeHonestlyEmpty(t *testing.T) {
	service := newTestService(t, true)
	players := defaultPlayers()
	teams := defaultTeams()
	service.players = []Player{players[0]}
	totalPicks := len(teams) * CurrentDraftRounds()
	state := service.store.Snapshot()
	state.DraftStarted = true
	state.Picks = terminalDraftPicks(append([]Player{players[0]}, players[1:]...), teams, totalPicks, nil)
	// The roster source only contains p0, and p0 is the only player in the
	// service pool; every post-draft acquisition slot is therefore empty.
	state.Picks = state.Picks[:1]
	state.Picks[0].PlayerID = players[0].ID
	for len(state.Picks) < totalPicks {
		state.Picks = append(state.Picks, DraftPick{
			Number:   len(state.Picks) + 1,
			Round:    1,
			TeamID:   teams[len(state.Picks)%len(teams)].ID,
			PlayerID: fmt.Sprintf("unlisted-%d", len(state.Picks)),
		})
	}
	radar := service.teamTerminalRadar(state, TeamTerminalRosterComplete, time.Now(), 3)
	if len(radar) != 0 {
		t.Fatalf("empty radar = %+v, want no rows", radar)
	}
}

func TestTeamTerminalIdentityIDsAreUnique(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	page, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "app", "team", "page.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if count := strings.Count(source, "id=\"team-identity\""); count != 1 {
		t.Fatalf("team-identity id count = %d, want 1", count)
	}
	if count := strings.Count(source, "id=\"team-identity-hero\""); count != 1 {
		t.Fatalf("team-identity-hero id count = %d, want 1", count)
	}
}

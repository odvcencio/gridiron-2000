package team

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestTeamPageIRFlowRenders drives the manager-facing IR flow through the
// real page (2026-09-29 IR review): the place-on-IR list offers only the
// player whose status qualifies, labelled with that status; once he is on
// IR and recovers, the page leads with an action-needed banner linking to
// the IR section. Forked into a subprocess because league.Default() is a
// process-wide singleton and this fixture needs its own drafted league.
func TestTeamPageIRFlowRenders(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTeamIRFlowFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"TEAM_IR_FLOW_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=test",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("team IR flow fixture: %v\n%s", err, output)
	}
	sections := strings.Split(string(output), "===SECTION===")
	if len(sections) != 3 {
		t.Fatalf("fixture did not emit three renders: %s", output)
	}
	before, placed, healed := sections[0], sections[1], sections[2]

	placeForm := between(before, `aria-label="Place a player on IR"`, `</select>`)
	if !strings.Contains(placeForm, "Team Fixture Player 002 (RB) · Injured reserve") {
		t.Errorf("the IR list does not offer the injured player with his status: %s", placeForm)
	}
	if strings.Contains(placeForm, "Team Fixture Player 001") {
		t.Errorf("the IR list offers a healthy player: %s", placeForm)
	}
	if strings.Contains(before, "IR PLAYER NO LONGER QUALIFIES") {
		t.Error("the healed banner rendered before anyone was on IR")
	}

	if !strings.Contains(placed, "IR is full. Activate a player to open a spot.") {
		t.Error("a full IR does not say so")
	}
	if strings.Contains(placed, `aria-label="Place a player on IR"`) {
		t.Error("a full IR still offers the place form")
	}
	if !strings.Contains(placed, "Injured reserve") || !strings.Contains(placed, `id="team-ir"`) {
		t.Error("the IR occupant row does not show his status, or the IR section lost its anchor")
	}

	if !strings.Contains(healed, "IR PLAYER NO LONGER QUALIFIES") || !strings.Contains(healed, `href="#team-ir"`) {
		t.Errorf("the healed banner is missing: %s", between(healed, "team-identity-hero", "team-identity-hero"))
	}
	if !strings.Contains(healed, "Team Fixture Player 002 is off the injury report and no longer qualifies for IR.") {
		t.Error("the healed banner does not name the player and the rule")
	}
	if !strings.Contains(healed, "No longer qualifies for IR.") {
		t.Error("the IR row does not mark the healed player")
	}
	assertNoStrayCommentLines(t, "/team (IR healed)", healed)
}

func between(body, start, end string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i+len(start):], end)
	if j < 0 {
		return body[i:]
	}
	return body[i : i+len(start)+j]
}

// TestTeamIRFlowFixtureProcess is TestTeamPageIRFlowRenders' subprocess
// body.
func TestTeamIRFlowFixtureProcess(t *testing.T) {
	if os.Getenv("TEAM_IR_FLOW_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	pool := teamFixturePool(200)
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 1, "cache" })
	setupRequest := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if started, err := service.AdminStartDraft(setupRequest); err != nil || !started {
		t.Fatalf("start draft: started=%v err=%v", started, err)
	}
	data := service.AdminData(setupRequest)
	required, _ := data["draft_required_players"].(int)
	for pick := 1; pick <= required; pick++ {
		data = service.AdminData(setupRequest)
		token, _ := data["current_pick_token"].(string)
		if _, _, _, err := service.AdminForceAutopick(setupRequest, league.ForceCurrentPickConfirmation, token); err != nil {
			t.Fatalf("complete fixture pick %d/%d: %v", pick, required, err)
		}
	}
	current := league.CurrentRoster()
	if _, err := service.AdminSetRosterShape(setupRequest, league.RosterOverride{
		Slots: current.Slots, Bench: current.Bench, Reserve: map[string]int{}, Limits: map[string]int{}, IR: 1,
	}); err != nil {
		t.Fatalf("turn IR on: %v", err)
	}

	// Find a team-1 player to injure: the first rostered RB.
	team := service.TeamData(httptest.NewRequest(http.MethodGet, "/team", nil))
	injuredID := ""
	for _, row := range append(asRows(team["starters"]), asRows(team["bench"])...) {
		if id, _ := row["id"].(string); id != "" && row["position"] == "RB" {
			injuredID = id
			break
		}
	}
	if injuredID != "" && injuredID != "team-fixture-pool-002" {
		// Rename so the assertions can name him without knowing the draft.
		for i := range pool {
			if pool[i].ID == "team-fixture-pool-002" {
				pool[i].Name = "Team Fixture Player 002 (unused)"
			}
			if pool[i].ID == injuredID {
				pool[i].Name = "Team Fixture Player 002"
			}
		}
	}
	if injuredID == "" {
		t.Fatalf("team-1 has no RB to injure")
	}
	injured := make([]league.Player, len(pool))
	copy(injured, pool)
	for i := range injured {
		if injured[i].ID == injuredID {
			injured[i].Injury = "Injured Reserve"
		}
	}
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return injured, 2, "cache" })
	os.Stdout.WriteString(renderTeamPageOnce(t))
	os.Stdout.WriteString("===SECTION===")

	if _, err := service.PlaceInIR(httptest.NewRequest(http.MethodPost, "/team", nil), "team-1", injuredID); err != nil {
		t.Fatalf("PlaceInIR: %v", err)
	}
	os.Stdout.WriteString(renderTeamPageOnce(t))
	os.Stdout.WriteString("===SECTION===")

	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 3, "cache" })
	os.Stdout.WriteString(renderTeamPageOnce(t))
}

func asRows(value any) []map[string]any {
	rows, _ := value.([]map[string]any)
	return rows
}

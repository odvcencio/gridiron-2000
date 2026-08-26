package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
	"gridiron-2000/internal/league"
)

func TestStableAdminSectionsAndAllowlistedFocus(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, key := range []string{"draft-control", "schedule", "week-close", "seats", "invites", "draft-order", "data", "clock", "roster", "announcements", "danger"} {
		if !strings.Contains(page, "id="+string(rune(34))+"admin-"+key+string(rune(34))) {
			t.Errorf("admin section %q has no stable id", key)
		}
		if !strings.Contains(page, "data-admin-section="+string(rune(34))+key+string(rune(34))) {
			t.Errorf("admin section %q has no stable data key", key)
		}
	}
	for _, raw := range []string{"/admin?section=data", "/admin?section=data#admin-data", "/admin?section=../../"} {
		request := httptest.NewRequest("GET", raw, nil)
		got := adminSection(request)
		if raw == "/admin?section=data" && got != "data" {
			t.Fatalf("allowlisted section = %q", got)
		}
		if strings.Contains(raw, "..") && got != "" {
			t.Fatalf("hostile section accepted: %q", got)
		}
	}
	if adminSectionClass("data", "data") != " admin-section--focused" || adminSectionClass("data", "clock") != "" {
		t.Fatal("section focus class is not exact")
	}
}

func TestCommissionerLeagueSwitcherIsAllowlistedAndPreservesAdminSection(t *testing.T) {
	publicURL, _ := url.Parse("https://sk.example")
	service, err := commissionerhq.New(commissionerhq.Config{
		InstanceID: "g2k", Timeout: time.Second,
		Peers: []commissionerhq.Peer{{ID: "skl", PublicURL: publicURL}},
	}, func() commissionerhq.Summary {
		return commissionerhq.Summary{Instance: commissionerhq.Instance{
			ID: "g2k", Name: "GRIDIRON 2000", PublicURL: "https://gridiron.example",
		}}
	})
	if err != nil {
		t.Fatal(err)
	}

	options, visible := adminLeagueSwitcherData(service, true)
	if !visible || len(options) != 2 || options[0]["current"] != true || options[1]["id"] != "skl" {
		t.Fatalf("switcher options = %#v, visible=%v", options, visible)
	}
	if options, visible := adminLeagueSwitcherData(service, false); visible || len(options) != 0 {
		t.Fatalf("noncommissioner received switcher topology: %#v", options)
	}

	handler := switchHandler(service, func(*http.Request) bool { return true })
	request := httptest.NewRequest(http.MethodGet, "/commissioner/switch?league=skl&section=data", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://sk.example/admin?section=data#admin-data" {
		t.Fatalf("switch response = %d %q", response.Code, response.Header().Get("Location"))
	}

	for _, target := range []string{"https://attacker.example", "skl%0d%0aLocation:%20https://attacker.example"} {
		request := httptest.NewRequest(http.MethodGet, "/commissioner/switch?league="+target, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
			t.Fatalf("hostile target %q = %d %q", target, response.Code, response.Header().Get("Location"))
		}
	}

	request = httptest.NewRequest(http.MethodGet, "/commissioner/switch?league=skl&section=../../danger", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://sk.example/admin" {
		t.Fatalf("hostile section was preserved: %d %q", response.Code, response.Header().Get("Location"))
	}

	denied := switchHandler(service, func(*http.Request) bool { return false })
	response = httptest.NewRecorder()
	denied.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/commissioner/switch?league=skl", nil))
	if response.Code != http.StatusForbidden || response.Header().Get("Location") != "" {
		t.Fatalf("noncommissioner switch = %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAdminTaskNavigationGroupsAndRoutineOrder(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	quote := string(rune(34))
	navStart := strings.Index(markup, "class="+quote+"admin-task-nav")
	navEnd := strings.Index(markup[navStart:], "</nav>")
	if navStart < 0 || navEnd < 0 {
		t.Fatal("admin task navigation is missing")
	}
	nav := markup[navStart : navStart+navEnd]
	for _, group := range []string{
		"Draft preparation and live operation",
		"Season operation",
		"People and access",
		"League configuration and communication",
		"Danger Zone",
	} {
		if !strings.Contains(nav, group) {
			t.Errorf("task navigation missing group %q", group)
		}
	}
	for _, target := range []string{
		"section=draft-control#admin-draft-control",
		"section=draft-order#admin-draft-order",
		"section=data#admin-data",
		"section=clock#admin-clock",
		"section=roster#admin-roster",
		"section=schedule#admin-schedule",
		"section=week-close#admin-week-close",
		"section=seats#admin-seats",
		"section=invites#admin-invites",
		"section=announcements#admin-announcements",
		"section=danger#admin-danger",
	} {
		if !strings.Contains(nav, target) {
			t.Errorf("task navigation missing target %q", target)
		}
	}

	sectionIndex := func(id string) int {
		return strings.Index(markup, "<section id="+quote+id+quote)
	}
	danger := sectionIndex("admin-danger")
	if danger < 0 {
		t.Fatal("danger section is missing")
	}
	for _, id := range []string{
		"admin-draft-control", "admin-schedule", "admin-week-close",
		"admin-seats", "admin-invites", "admin-draft-order", "admin-data",
		"admin-clock", "admin-roster", "admin-announcements",
	} {
		if index := sectionIndex(id); index < 0 || index > danger {
			t.Errorf("routine section %q does not precede Danger Zone: %d vs %d", id, index, danger)
		}
	}
	if strings.Contains(markup, "03 // RESET") {
		t.Fatal("destructive section retained the old routine-looking 03 // RESET label")
	}
	if !strings.Contains(markup, "99 // DANGER ZONE") {
		t.Fatal("destructive section lacks isolated 99 // DANGER ZONE label")
	}

	for _, id := range []string{
		"admin-draft-control", "admin-schedule", "admin-week-close",
		"admin-seats", "admin-invites", "admin-draft-order", "admin-data",
		"admin-clock", "admin-roster", "admin-announcements", "admin-danger",
	} {
		section := "id=" + quote + id + quote + " aria-labelledby=" + quote + id + "-heading" + quote + " tabindex=" + quote + "-1" + quote
		if !strings.Contains(markup, section) {
			t.Errorf("section %q lacks a focusable labelled landmark", id)
		}
		if !strings.Contains(markup, "id="+quote+id+"-heading"+quote) {
			t.Errorf("section %q lacks its labelled heading", id)
		}
	}

	for _, action := range []string{
		"draft-start", "schedule-generate", "schedule-regenerate",
		"close-week-ready", "close-week-force", "seat-release", "team-rename",
		"clock-set-autopick", "avatar-reset", "co-detach", "invite-add",
		"invite-send", "invite-remove", "draft-reset", "draft-undo", "league-reset",
		"seat-trim", "order-randomize", "clock-pause", "clock-resume",
		"clock-force-autopick", "clock-extend", "clock-set-duration",
		"roster-shape-apply", "roster-shape-reset", "announcement-post",
		"announcement-delete",
	} {
		if !strings.Contains(markup, "actionPath("+quote+action+quote+")") {
			t.Errorf("existing admin action disappeared: %s", action)
		}
	}
}

func TestAdminTaskNavigationRendersAccessibleLinks(t *testing.T) {
	body := renderAdminPage(t)
	quote := string(rune(34))
	if !strings.Contains(body, "class="+quote+"admin-task-nav") {
		t.Fatal("rendered admin page omitted task navigation")
	}
	for _, target := range []string{
		"section=draft-control#admin-draft-control",
		"section=seats#admin-seats",
		"section=danger#admin-danger",
	} {
		if !strings.Contains(body, target) {
			t.Errorf("rendered task navigation missing %s", target)
		}
	}
	if !strings.Contains(body, "aria-labelledby="+quote+"admin-danger-heading"+quote) ||
		!strings.Contains(body, "tabindex="+quote+"-1"+quote) {
		t.Fatal("rendered admin sections lost accessible focus metadata")
	}
}

func TestAdminTaskNavigationKeepsDesktopAndNarrowColumnContracts(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	if !strings.Contains(css, ".admin-task-nav__groups {\n  display: grid;\n  grid-template-columns: repeat(2, minmax(0, 1fr));") {
		t.Fatal("commissioner task board lost its two-column desktop grid")
	}
	narrow := "@media (max-width: 60rem) {\n  .board-workspace,\n  .admin-grid,\n  .danger-grid {\n    grid-template-columns: minmax(0, 1fr);\n  }\n\n  .admin-task-nav__groups {\n    grid-template-columns: minmax(0, 1fr);\n  }\n}"
	if !strings.Contains(css, narrow) {
		t.Fatal("commissioner task board one-column override escaped the narrow-screen media query")
	}
}

func TestAdminTaskBoardDraftPhaseTruthTable(t *testing.T) {
	fixtures := []struct {
		phase     string
		want      []string
		forbidden []string
	}{
		{
			phase: "scheduled",
			want: []string{
				"Draft is not live. Confirm seats, draw order, then start it intentionally.",
				"START REQUIRED",
				"Configure roster shape",
				"OPEN",
			},
			forbidden: []string{"LIVE · operate now", "LOCKED · DRAFT STARTED", "LOCKED · DRAFT COMPLETE"},
		},
		{
			phase: "live",
			want: []string{
				"Draft is live. Operate the current pick from Draft clock.",
				"LIVE · operate now",
				"LOCKED · DRAFT STARTED",
				"DRAFT LIVE:",
			},
			forbidden: []string{"Draft is complete.", "LOCKED · DRAFT COMPLETE", "DRAFT COMPLETE:"},
		},
		{
			phase: "complete",
			want: []string{
				"Draft is complete. There is no current pick; move on to season operations.",
				"LOCKED · DRAFT COMPLETE",
				"DRAFT COMPLETE:",
			},
			forbidden: []string{
				"Draft is live. Operate the current pick from Draft clock.",
				"LIVE · operate now",
				"LOCKED · DRAFT STARTED",
				"LOCKED · DRAFT LIVE",
				"DRAFT LIVE:",
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.phase, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestAdminTaskBoardDraftPhaseFixtureProcess$")
			cmd.Env = append(os.Environ(),
				"ADMIN_TASK_DRAFT_PHASE="+fixture.phase,
				"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
				"DEMO_MODE=true",
				"GOOGLE_CLIENT_ID=",
				"APP_ENV=",
				"LEAGUE_FILE=",
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s fixture process: %v\n%s", fixture.phase, err, output)
			}
			body := string(output)
			for _, want := range fixture.want {
				if !strings.Contains(body, want) {
					t.Errorf("%s fixture missing %q", fixture.phase, want)
				}
			}
			for _, forbidden := range fixture.forbidden {
				if strings.Contains(body, forbidden) {
					t.Errorf("%s fixture contains false phase copy %q", fixture.phase, forbidden)
				}
			}
		})
	}
}

func TestAdminTaskBoardDraftPhaseFixtureProcess(t *testing.T) {
	phase := os.Getenv("ADMIN_TASK_DRAFT_PHASE")
	if phase == "" {
		t.Skip("fixture helper")
	}

	service := league.Default()
	pool := adminTaskFixturePool(200)
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 1, "demo" })
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if phase == "live" || phase == "complete" {
		if started, err := service.AdminStartDraft(request); err != nil || !started {
			t.Fatalf("start %s fixture: started=%v err=%v", phase, started, err)
		}
	}
	if phase == "complete" {
		data := service.AdminData(request)
		required, ok := data["draft_required_players"].(int)
		if !ok || required < 1 {
			t.Fatalf("draft_required_players = %#v", data["draft_required_players"])
		}
		for pick := 1; pick <= required; pick++ {
			data = service.AdminData(request)
			token, _ := data["current_pick_token"].(string)
			if _, _, _, err := service.AdminForceAutopick(request, league.ForceCurrentPickConfirmation, token); err != nil {
				t.Fatalf("complete fixture pick %d/%d: %v", pick, required, err)
			}
		}
	}

	data := service.AdminData(request)
	draft, ok := data["draft"].(map[string]any)
	if !ok {
		t.Fatalf("draft data = %#v", data["draft"])
	}
	wantStarted := phase != "scheduled"
	wantComplete := phase == "complete"
	if draft["started"] != wantStarted || data["draft_started"] != wantStarted || draft["complete"] != wantComplete {
		t.Fatalf("%s data contract: draft=%#v draft_started=%#v", phase, draft, data["draft_started"])
	}

	fmt.Print(renderAdminPage(t))
}

func adminTaskFixturePool(size int) []league.Player {
	players := make([]league.Player, 0, size)
	positions := []string{"QB", "RB", "WR", "TE", "K", "DST"}
	for index := 0; index < size; index++ {
		players = append(players, league.Player{
			ID:         fmt.Sprintf("admin-task-pool-%03d", index+1),
			Name:       fmt.Sprintf("Admin Task Player %03d", index+1),
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

func TestCommissionerLeagueSwitcherMarkupAndRouteContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	for _, want := range []string{
		`action="/commissioner/switch"`, `name="league"`, `name="section"`,
		`selected={league.current}`, `href="/commissioner"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("league switcher markup missing %q", want)
		}
	}
	mainSource, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainSource), `app.Mount("GET /commissioner/switch", adminpage.SwitchHandler(hqService))`) {
		t.Fatal("commissioner league switch route is not mounted")
	}
}

func TestResponsiveConsoleContainmentContract(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	for _, want := range []string{
		".page-masthead > *,\n.draft-masthead > *,\n.matchup-layout > * {\n  min-width: 0;\n}",
		".matchup-stage .section-heading--split {\n  align-items: flex-start;\n  flex-wrap: wrap;\n}",
		".matchup-stage .section-heading--split > * {\n  min-width: 0;\n}",
		".admin-league-switcher {\n  display: grid;\n  gap: var(--space-xs);\n  margin-block-end: var(--space-sm);\n  padding-block-end: var(--space-md);\n  border-block-end: 1px solid var(--color-border);\n  min-width: 0;\n}",
		".admin-league-switcher__heading,\n.admin-league-switcher__controls {\n  display: flex;\n  align-items: center;\n  gap: var(--space-sm);\n  min-width: 0;\n}",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("responsive containment contract missing %q", want)
		}
	}
}

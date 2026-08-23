package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderLandingPage(t *testing.T) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")

	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	t.Setenv("LEAGUE_FILE", leagueFile)

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestPublicLandingPreservesConfiguredModeAndEventTruth(t *testing.T) {
	body := renderLandingPage(t)

	for _, want := range []string{
		"STABLE KERNEL LEAGUE",
		"redraft format",
		"LEAGUE DRAFT",
		"Saturday, August 29, 2026",
		"4:00 PM EDT",
		"America/New_York",
		"SCHEDULED WINDOW",
		"The commissioner controls when the room opens.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page omitted %q; body: %s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "dynasty scoring") {
		t.Error("redraft landing page still contains dynasty scoring copy")
	}
	if !strings.Contains(body, "SIGN IN TO ENTER.") {
		t.Error("anonymous landing page must make authentication the only promise")
	}
	if strings.Contains(body, "CLAIM YOUR SEAT.") {
		t.Error("anonymous landing page retained unconditional seat-claim copy")
	}
	if strings.Contains(body, "Doors in") {
		t.Error("landing page still uses auto-start-implying Doors in label")
	}

	h1 := strings.Index(body, "<h1")
	cta := strings.Index(body, `class="button button--primary"`)
	event := strings.Index(body, `class="draft-transmission"`)
	if h1 < 0 || cta < 0 || event < 0 {
		t.Fatalf("landing page missing heading, primary CTA, or event card: h1=%d cta=%d event=%d", h1, cta, event)
	}
	if !(h1 < cta && cta < event) {
		t.Fatalf("semantic reading order must be heading, primary CTA, then event details: h1=%d cta=%d event=%d", h1, cta, event)
	}
	if got := strings.Count(body, `class="button button--primary"`); got != 1 {
		t.Fatalf("public landing page has %d primary CTAs, want exactly one", got)
	}
}

func TestPublicEntrySourceKeepsNarrowAndMotionContracts(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	stylesheet := string(css)
	for _, want := range []string{
		"@media (max-width: 20rem)",
		"@media (max-width: 38rem)",
		"@media (prefers-reduced-motion: reduce)",
		".home-action-center",
		".home-action-center__task",
		".home-action-center__task-top",
		".home-action-center__task-detail",
		".home-action-center__body",
		".home-action-center__task-marker",
		".home-action-center__task--on_clock",
		".home-action-center__task--deadline",
		".hero-command > *",
		".login-stage > *",
		"overflow-wrap: anywhere",
		"outline: 2px solid var(--color-accent-cyan)",
	} {
		if !strings.Contains(stylesheet, want) {
			t.Errorf("stylesheet omitted public-entry contract %q", want)
		}
	}
}

func TestHomepageMatchupPreviewOnlyShowsLiveIndicatorsInProgress(t *testing.T) {
	for _, fixture := range []struct {
		state string
		show  bool
	}{
		{state: "preseason"},
		{state: "scheduled"},
		{state: "in_progress", show: true},
		{state: "final"},
		{state: "degraded"},
	} {
		t.Run(fixture.state, func(t *testing.T) {
			raw := []map[string]any{{
				"id": "matchup-1", "state": fixture.state,
				"show_live_indicator": fixture.show, "status": "Status", "clock": "Clock",
				"away": map[string]any{"id": "team-1", "name": "Away", "manager": "A", "score": "0.0", "tone": "cyan", "abbreviation": "AWY"},
				"home": map[string]any{"id": "team-2", "name": "Home", "manager": "H", "score": "0.0", "tone": "red", "abbreviation": "HME"},
			}}
			cards := dashboardMatchupCards(raw)
			if len(cards) != 1 || cards[0].State != fixture.state || cards[0].ShowLiveIndicator != fixture.show {
				t.Fatalf("converted cards = %+v", cards)
			}
			program, err := route.LoadFileProgram("page.gsx")
			if err != nil {
				t.Fatal(err)
			}
			html, err := route.RenderProgramComponent(program, "MiniMatchup", route.ProgramRenderEnv{
				Values: map[string]any{"props": cards[0]},
			})
			if err != nil {
				t.Fatal(err)
			}
			hasDot := strings.Contains(html, `class="live-dot"`)
			if hasDot != fixture.show {
				t.Fatalf("state %s live-dot presence = %v, want %v: %s", fixture.state, hasDot, fixture.show, html)
			}
		})
	}

	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `<If cond={data.live.show_live_indicator}>`) {
		t.Fatal("homepage matchup preview masthead live dot is not gated by live state")
	}
}

func TestHomepagePendingCoManagerInviteRendersTruthfully(t *testing.T) {
	body := runHomepageStandingsFixture(t, "pending-co-manager")
	for _, want := range []string{
		"ADMITTED · CO-MANAGER INVITE",
		"COMPLETE YOUR SHARED SEAT.",
		"You are invited to co-manage East 1.",
		"Complete co-manager sign-in",
		"/auth/google/start?next=%2Fteam",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pending co-manager homepage missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Claim an open franchise") || strings.Contains(body, "Join a team") {
		t.Fatalf("pending co-manager homepage exposed a competing seat-claim path: %s", body)
	}
	if strings.Contains(body, `href="/auth/google/start?next=%2Fteam" data-gosx-link`) {
		t.Fatalf("pending co-manager OAuth recovery must use native navigation: %s", body)
	}
}

func TestHomepageStandingsPendingStateRendersExplicitly(t *testing.T) {
	body := runHomepageStandingsFixture(t, "pending")
	for _, want := range []string{"Standings pending", "NO SEASON TABLE", "The commissioner has not published a regular-season schedule yet."} {
		if !strings.Contains(body, want) {
			t.Errorf("pending homepage missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "standing-row") || strings.Contains(body, "Final ’25 table") {
		t.Fatalf("pending homepage rendered a fabricated standings table: %s", body)
	}
}

func TestHomepageStandingsRendersFinalizedScheduleData(t *testing.T) {
	body := runHomepageStandingsFixture(t, "scored")
	for _, want := range []string{"2026 standings", "Through Week 1", "standing-row", "0–0–1", "0.0", "Scheduled time is the meeting point", "randomizes draft order about one hour", "Draft order locks when the commissioner starts the draft"} {
		if !strings.Contains(body, want) {
			t.Errorf("scored homepage missing %q: %s", want, body)
		}
	}
	for _, stale := range []string{"Final ’25 table", "Last season’s damage report", "locks when pick 1 lands", "locks once the first pick lands"} {
		if strings.Contains(body, stale) {
			t.Fatalf("scored homepage retained stale copy %q: %s", stale, body)
		}
	}
}

func runHomepageStandingsFixture(t *testing.T, fixture string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHomepageStandingsFixtureProcess$")
	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	cmd.Env = append(os.Environ(),
		"HOME_STANDINGS_RENDER_FIXTURE="+fixture,
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"LEAGUE_FILE="+leagueFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("homepage %s fixture: %v\n%s", fixture, err, output)
	}
	return string(output)
}

func TestHomepageStandingsFixtureProcess(t *testing.T) {
	fixture := os.Getenv("HOME_STANDINGS_RENDER_FIXTURE")
	if fixture == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	if fixture == "pending-co-manager" {
		primary, err := service.AssignManager("primary@example.com", "Primary Fixture")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.EnsureMember("render@example.com", "Render Fixture"); err != nil {
			t.Fatal(err)
		}
		if err := service.InviteCoManager(request, primary.TeamID, "render@example.com"); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := service.AssignManager("render@example.com", "Render Fixture"); err != nil {
			t.Fatal(err)
		}
	}
	if fixture == "scored" {
		players := make([]league.Player, 0, 150)
		players = append(players, league.Player{ID: "p-01", Name: "Ja'Marr Chase", Position: "WR", NFLTeam: "CIN"})
		for i := 2; i <= 150; i++ {
			players = append(players, league.Player{ID: fmt.Sprintf("pool-%03d", i), Name: fmt.Sprintf("Pool Player %03d", i), Position: "WR", NFLTeam: "CIN"})
		}
		service.SetPlayerSource(func() ([]league.Player, int64, string) { return players, 1, "live" })
		schedule, err := service.AdminGenerateSchedule(request, 14, 1, 42)
		if err != nil {
			t.Fatal(err)
		}
		service.SetWeekStatsSource(func(int) []league.WeekStatLine {
			return []league.WeekStatLine{{
				Key:   "jamarrchase|WR",
				Stats: map[string]float64{"recTD": 2},
			}}
		})
		if _, _, err := service.AdminCloseWeek(request, schedule.Weeks[0].Week); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Print(renderAuthenticatedHomepage(t))
}

func renderAuthenticatedHomepage(t *testing.T) string {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			email := r.Header.Get("X-Test-User")
			return auth.User{ID: email, Email: email, Name: "Render Fixture"}, true
		}),
	})
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Test-User", "render@example.com")
	authn.Middleware(handler).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func TestHomepageActionCenterSourceContract(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<ActionCenterPanel {...data.action_center}>`,
		`home-action-center`,
		`home-action-center__task-top`,
		`home-action-center__task-detail`,
		`home-action-center__task-marker`,
		`home-action-center__status`,
		`home-action-center__body`,
		`data.public_entry`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("homepage source missing action-center contract %q", want)
		}
	}
	for _, stale := range []string{`status-cards`, `data.fantasy_card`, `data.pickem_home`, `Claim a team`} {
		if strings.Contains(page, stale) {
			t.Errorf("homepage retained replaced authenticated status-card contract %q", stale)
		}
	}
}

func TestHomepageActionCenterNavigationContract(t *testing.T) {
	pageSource, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageSource)
	taskStart := strings.Index(page, "component ActionCenterTask")
	nativeStart := strings.Index(page, "component ActionCenterNativeTask")
	panelStart := strings.Index(page, "component ActionCenterPanel")
	if taskStart < 0 || nativeStart <= taskStart || panelStart <= nativeStart {
		t.Fatalf("homepage action-center navigation components are not ordered")
	}
	managed := page[taskStart:nativeStart]
	native := page[nativeStart:panelStart]
	if !strings.Contains(managed, "data-gosx-link") {
		t.Fatal("ordinary internal action-center links must opt into GoSX managed navigation")
	}
	if !strings.Contains(managed, "href={props.Href}") {
		t.Fatal("managed action-center links must preserve the model href, including fragments")
	}
	if strings.Contains(native, "data-gosx-link") {
		t.Fatal("native action-center links must not be intercepted by managed navigation")
	}

	loginSource, err := os.ReadFile("login/page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	login := string(loginSource)
	oauthStart := strings.Index(login, "href={data.oauth_start}")
	if oauthStart < 0 {
		t.Fatal("login page omitted its OAuth start link")
	}
	oauthEnd := strings.Index(login[oauthStart:], "</a>")
	if oauthEnd < 0 {
		t.Fatal("login OAuth start link is not a complete anchor")
	}
	if strings.Contains(login[oauthStart:oauthStart+oauthEnd], "data-gosx-link") {
		t.Fatal("OAuth hand-off must stay a native top-level navigation")
	}

	hqSource, err := os.ReadFile("../internal/league/hq.go")
	if err != nil {
		t.Fatal(err)
	}
	hq := string(hqSource)
	for _, fragment := range []string{"#admin-draft-control", "#admin-clock"} {
		if !strings.Contains(hq, fragment) {
			t.Errorf("action-center model omitted commissioner fragment %q", fragment)
		}
	}
	if !strings.Contains(hq, `NativeNavigation: strings.HasPrefix(href, "/auth/")`) {
		t.Fatal("action-center model does not classify OAuth hand-offs as native")
	}
}

func TestHomepageActionCenterTypedAdapterRendersLinkOnly(t *testing.T) {
	raw := map[string]any{
		"stage": "regular_season", "stage_label": "REGULAR SEASON // TODAY",
		"heading": "KEEP THE SEASON MOVING.", "summary": "Deadlines stay visible.",
		"has_actions": true, "action_count": 1,
		"actions": []map[string]any{{
			"id": "lineup", "priority": "deadline", "priority_label": "BEFORE KICKOFF",
			"label": "Fix your lineup", "detail": "One slot needs attention.",
			"href": "/team?week=1", "has_due_at": false, "urgent": true, "primary": true,
		}},
		"has_commissioner_actions": false, "commissioner_actions": []map[string]any{},
	}
	card := dashboardActionCenter(raw)
	if card.Stage != "regular_season" || len(card.Actions) != 1 || card.Actions[0].Href != "/team?week=1" {
		t.Fatalf("typed action-center card = %+v", card)
	}
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{`<ActionCenterPanel {...data.action_center}>`, `href={props.Href}`, `home-action-center__task-detail`} {
		if !strings.Contains(page, want) {
			t.Errorf("typed action-center template missing %q", want)
		}
	}
	if strings.Contains(page, "<form") {
		t.Fatal("homepage action center template must remain link-only")
	}
}

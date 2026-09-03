package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// homeTagPattern replaces every tag with a newline, the same way this
// codebase's own manual verification command does (curl ... | sed
// 's/<[^>]*>/\n/g' | grep -c '^\s*//'): GoSX does not collapse a text
// node's own whitespace the way JSX does, so a tag boundary is where a
// rendered line actually breaks, not just where an original .gsx source
// line happened to break.
var homeTagPattern = regexp.MustCompile(`<[^>]*>`)

// assertNoStrayCommentLines fails the test when the rendered page's text,
// with every tag replaced by a newline, carries a line that starts with
// "//". A GoSX markup block never strips a Go-style "//" comment the way
// plain Go source does, so a developer comment left inside a return
// <...> block renders as visible text instead of staying source-only
// (wave-7 re-audit, item 5 — the app/team/page.gsx defect this guards
// against here on the home page instead). See
// TestNoCommentLinesInsideGSXMarkup (root package,
// gsx_markup_comment_contract_test.go) for the matching source-level
// guard.
//
// A bare "//" with nothing else on the line is allowed: page.gsx's own
// live-bound score-ticker (data.live.week_label // data.live.status //
// data.live.refresh_label) separates each independently live-bound
// <span> with the site's "LABEL // detail" divider glyph (every
// section-index span in app/*.gsx uses the same convention, traced back
// to the original "GRIDIRON 2000 // Eight seats. One trophy." tagline).
// Sitting between two independently tagged, independently live-bound
// elements, it renders on its own isolated line under this exact
// tag-to-newline check, with no way to merge it into a neighboring
// live-bound span's own text without risking a live-patch overwriting
// it. A genuine leftover developer comment is never contentless, so a
// bare "//" line is never itself a defect.
func assertNoStrayCommentLines(t *testing.T, label, html string) {
	t.Helper()
	text := homeTagPattern.ReplaceAllString(html, "\n")
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "//" || !strings.HasPrefix(trimmed, "//") {
			continue
		}
		t.Errorf("%s rendered a stray comment line: %q", label, trimmed)
	}
}

func renderLandingPage(t *testing.T) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	t.Setenv("APP_ENV", "test")

	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	t.Setenv("LEAGUE_FILE", leagueFile)

	// The fixture pins the draft window at a fixed instant
	// (sk-league.json's draft.at). Freeze the service clock to a point
	// safely before that window so this test's "SCHEDULED WINDOW" copy
	// assertion does not flip to "AWAITING COMMISSIONER" once wall time
	// walks past the fixture's pinned date. See SetClockForTest's doc
	// comment (internal/league/harnessclock.go) for the harness-only seam.
	service := league.Default()
	service.SetClockForTest(func() time.Time {
		return time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	})
	t.Cleanup(func() { service.SetClockForTest(nil) })

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

// TestHomePlayoffCardCollapsesToOneLineInPreseason is gap-audit item 2
// (wave 4 — linden): during draft week the home page's second card was
// the full 257px "PLAYOFFS NOT ACTIVE" layout even though the phase is
// PRESEASON and no bracket can exist yet. It now collapses to one status
// line while season_phase is "preseason" — the identical pattern
// app/team/page.gsx's own playoff card uses (gap-audit item 1) — and
// restores the full card once the phase moves past preseason.
func TestHomePlayoffCardCollapsesToOneLineInPreseason(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`<If cond={data.playoff_truth.season_phase == "preseason"}>`,
		`class="score-command playoff-truth-card playoff-truth-card--compact"`,
		`<If cond={data.playoff_truth.season_phase != "preseason"}>`,
		`class="score-command playoff-truth-card" aria-labelledby="home-playoff-truth-heading">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page.gsx missing preseason playoff-card collapse contract %q", want)
		}
	}
	compactAt := strings.Index(page, `playoff-truth-card--compact`)
	compactEnd := strings.Index(page[compactAt:], "</section>")
	if compactAt < 0 || compactEnd < 0 {
		t.Fatal("compact playoff card section has no closing </section>")
	}
	compactBlock := page[compactAt : compactAt+compactEnd]
	if strings.Contains(compactBlock, "section-heading--split") || strings.Contains(compactBlock, "pool-stats") {
		t.Errorf("compact preseason playoff card still carries the full card's layout: %s", compactBlock)
	}
}

func TestPublicLandingPreservesConfiguredModeAndEventTruth(t *testing.T) {
	body := renderLandingPage(t)

	for _, want := range []string{
		"STABLE KERNEL LEAGUE",
		"redraft format",
		"LEAGUE DRAFT",
		"Saturday, August 29, 2026",
		"4:00 PM EDT",
		"Eastern Time",
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
				"show_live_indicator": fixture.show, "live_indicator": map[bool]string{true: "live", false: ""}[fixture.show], "status": "Status", "clock": "Clock",
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
			if !strings.Contains(html, `data-gosx-live-bind="matchupIndicator.matchup-1"`) {
				t.Fatalf("state %s omitted matchup live indicator binding: %s", fixture.state, html)
			}
			if fixture.show && !strings.Contains(html, ">live</span>") {
				t.Fatalf("state %s omitted initial live indicator token: %s", fixture.state, html)
			}
		})
	}

	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-gosx-live-src="/api/live/week"`,
		`data-gosx-live-interval={data.live_interval}`,
		`data-gosx-live-bind="liveStatus"`,
		`data-gosx-live-bind="liveIndicator"`,
		`data-gosx-live-bind={"scores." + props.Away.ID}`,
		`data-gosx-live-bind={"scores." + props.Home.ID}`,
		`data-gosx-live-bind={"matchupStatus." + props.ID}`,
		`data-gosx-live-bind={"matchupClock." + props.ID}`,
		`data-gosx-live-bind="refreshLabel"`,
	} {
		if !strings.Contains(string(source), want) {
			t.Errorf("homepage omitted targeted live binding %q", want)
		}
	}
	if strings.Contains(string(source), "updates every 60 seconds") {
		t.Fatal("homepage still promises a fixed live refresh without binding it to live state")
	}
}

func TestHomepagePendingCoManagerInviteRendersTruthfully(t *testing.T) {
	body := runHomepageStandingsFixture(t, "pending-co-manager")
	for _, want := range []string{
		"ADMITTED · CO-MANAGER INVITE",
		"COMPLETE YOUR SHARED SEAT.",
		"You are invited to co-manage East 1.",
		"Complete co-manager invitation",
		"/guide#identity",
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
	if strings.Contains(body, "Complete co-manager sign-in") || strings.Contains(body, `/auth/google/start?next=%2Fteam`) {
		t.Fatalf("pending co-manager rendered a stale reauthentication CTA: %s", body)
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

// TestHomepageBootstrapAndHubGateOnSignedInAndSeated covers round-2
// review finding 1: the GoSX bootstrap runtime and the scores-live hub
// binding must load only for the same viewer state that unlocks
// page.gsx's own live element (data.viewer.signed_in && data.has_seat,
// ~line 386) — a signed-out landing visitor loads no runtime and opens no
// socket. "id="gosx-manifest"" and `data-gosx-script="bootstrap"` are
// the bootstrap runtime's own rendered markers (m31labs.dev/gosx's
// island.go); BindHub adds the hub's entry to that same manifest.
func TestHomepageBootstrapAndHubGateOnSignedInAndSeated(t *testing.T) {
	signedOut := renderLandingPage(t)
	for _, marker := range []string{`id="gosx-manifest"`, `data-gosx-script="bootstrap"`} {
		if strings.Contains(signedOut, marker) {
			t.Fatalf("a signed-out landing visitor must load no bootstrap runtime or hub (%q present): %s", marker, signedOut)
		}
	}
	assertNoStrayCommentLines(t, "/ (signed out)", signedOut)

	seated := runHomeBootstrapFixture(t, "seated")
	for _, marker := range []string{`id="gosx-manifest"`, `data-gosx-script="bootstrap"`, "scores-live"} {
		if !strings.Contains(seated, marker) {
			t.Fatalf("a signed-in, seated viewer must load the bootstrap runtime and the scores-live hub (missing %q): %s", marker, seated)
		}
	}
	// The seated render is the one that unlocks page.gsx's score-ticker
	// (data.viewer.signed_in && data.has_seat), so it is also the render
	// that exercises the ticker's own "// " divider glyphs.
	assertNoStrayCommentLines(t, "/ (seated)", seated)
}

func runHomeBootstrapFixture(t *testing.T, scenario string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHomeBootstrapRenderFixtureProcess$")
	leagueFile, err := filepath.Abs(filepath.Join("..", "internal", "league", "testdata", "sk-league.json"))
	if err != nil {
		t.Fatalf("league fixture path: %v", err)
	}
	cmd.Env = append(os.Environ(),
		"HOME_BOOTSTRAP_RENDER_FIXTURE="+scenario,
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"LEAGUE_FILE="+leagueFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("homepage bootstrap fixture %s: %v\n%s", scenario, err, output)
	}
	return string(output)
}

func TestHomeBootstrapRenderFixtureProcess(t *testing.T) {
	scenario := os.Getenv("HOME_BOOTSTRAP_RENDER_FIXTURE")
	if scenario == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	if scenario == "seated" {
		if _, err := service.AssignManager("render@example.com", "Render Fixture"); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Print(renderAuthenticatedHomepage(t))
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

// TestCommissionerSeatlessOverlayLeadsWithLeagueStateAndPromotesRealTask is
// the wave-1 audit fix for a seatless commissioner's home page: the
// generic seatless entry stage (internal/league/public_entry.go's
// PublicEntryAdmittedSeatlessFull) headlines "ADMITTED · WAITING FOR A
// SEAT." and its one action's Detail tells the viewer to ask the
// commissioner — nonsensical copy when the viewer IS the commissioner.
// commissionerSeatlessOverlay replaces the leading heading/actions with
// league state and the commissioner's own already-computed next job
// (hq.go's commissionerActions, promoted from the secondary aside into
// the primary action list) rather than duplicating that logic here.
func TestCommissionerSeatlessOverlayLeadsWithLeagueStateAndPromotesRealTask(t *testing.T) {
	card := ActionCenterCard{
		Stage: "entry", StageLabel: "ADMITTED · NO FRANCHISE",
		Heading:    "ADMITTED · WAITING FOR A SEAT.",
		Summary:    "Complete admission or claim a franchise before setting up the season.",
		HasActions: true, ActionCount: 1,
		Actions: []league.ActionCenterActionCard{{
			ID: "entry", Label: "Open Pick'em HQ →",
			Detail: "You are admitted, but every configured franchise is currently assigned. The commissioner must release a seat before team entry is available; Pick'em remains available while you wait.",
			Href:   "/pickem",
		}},
		HasCommissioner: true,
		CommissionerActions: []league.ActionCenterActionCard{{
			ID: "commissioner-start", Label: "Start and monitor draft",
			Detail: "0/10 seats claimed · 0/0 managers ready · draft order is not set · 0 players in pool.",
			Href:   "/admin?section=draft-control#admin-draft-control",
		}},
	}
	data := map[string]any{
		"draft": map[string]any{"status_label": "SCHEDULED WINDOW"},
		"live":  map[string]any{"week_label": "WEEK 1"},
	}
	got := commissionerSeatlessOverlay(card, data)

	if strings.Contains(got.Heading, "WAITING FOR A SEAT") {
		t.Errorf("overlay Heading still leads with the seatless entry copy: %q", got.Heading)
	}
	if len(got.Actions) == 0 {
		t.Fatal("overlay dropped every action")
	}
	for _, action := range got.Actions {
		if strings.Contains(action.Detail, "must release a seat") {
			t.Errorf("overlay action %q still tells the commissioner to ask the commissioner: %q", action.ID, action.Detail)
		}
	}
	if got.Actions[0].Href != "/admin?section=draft-control#admin-draft-control" {
		t.Errorf("overlay did not promote the real commissioner task, got Actions[0] = %+v", got.Actions[0])
	}
	if got.HasCommissioner || len(got.CommissionerActions) != 0 {
		t.Errorf("overlay left a duplicate commissioner aside after promoting it into Actions: HasCommissioner=%v CommissionerActions=%+v", got.HasCommissioner, got.CommissionerActions)
	}
}

// TestCommissionerSeatlessOverlayFallsBackWhenNoCommissionerTaskExists
// covers the case hq.go's commissionerActions returns nothing (a
// DraftComplete league with no open week-close): the overlay still names
// a real next job instead of leaving the panel with zero actions.
func TestCommissionerSeatlessOverlayFallsBackWhenNoCommissionerTaskExists(t *testing.T) {
	card := ActionCenterCard{Heading: "ADMITTED · WAITING FOR A SEAT.", HasCommissioner: false, CommissionerActions: nil}
	got := commissionerSeatlessOverlay(card, map[string]any{})
	if len(got.Actions) != 1 || got.Actions[0].Href != "/admin" {
		t.Fatalf("fallback overlay action = %+v, want exactly one action linking /admin", got.Actions)
	}
}

// TestHomepageCommissionerSeatlessOverlayRenders is the full-page render
// proof: a signed-in commissioner with no team seat sees the promoted
// commissioner task and never the "ask the commissioner" sentence.
func TestHomepageCommissionerSeatlessOverlayRenders(t *testing.T) {
	body := runHomeBootstrapFixture(t, "seatless-commissioner")
	if strings.Contains(body, "must release a seat before team entry is available") {
		t.Fatalf("seatless commissioner's home page still told them to ask the commissioner: %s", body)
	}
	if strings.Contains(body, "WAITING FOR A SEAT") {
		t.Fatalf("seatless commissioner's home page still led with the generic seatless headline: %s", body)
	}
	if !strings.Contains(body, "/admin") {
		t.Fatalf("seatless commissioner's home page omitted a real next job into League settings: %s", body)
	}
}

// TestHomepagePostDraftCardShowsTheViewerOwnOpeningPick is wave 7's item
// 3: once the draft completes, the home page carries a "Draft results"
// card linking /draft/results, and — for a viewer whose team made a
// pick — the teaser sentence names their own opening pick. A minimal
// one-of-each roster override (6 starters, the smallest legal shape,
// plus 4 bench — roster.go refuses a total roster under 10) shrinks the
// draft to ten rounds, not a full multi-round draft, so this fixture
// completes quickly.
func TestHomepagePostDraftCardShowsTheViewerOwnOpeningPick(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHomepagePostDraftCardFixtureProcess$")
	cmd.Env = append(os.Environ(), "HOME_POST_DRAFT_FIXTURE=1", "DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post-draft home fixture process: %v\n%s", err, output)
	}
	body := string(output)
	for _, want := range []string{
		`class="score-command draft-results-card"`, ">Draft results<", `href="/draft/results"`,
		"You opened with",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("post-draft home page missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Every pick is locked. See who drafted whom") {
		t.Error("the viewer holds a seat that made a pick; the card must show the teaser, not the seatless fallback sentence")
	}
}

func TestHomepagePostDraftCardFixtureProcess(t *testing.T) {
	if os.Getenv("HOME_POST_DRAFT_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	const viewerEmail = "render@example.com"
	if _, err := service.AssignManager(viewerEmail, "Render Fixture"); err != nil {
		t.Fatal(err)
	}
	// The built-in neutral league's own embedded rehearsal pool is far
	// too small for even a shrunk 10-round draft (draftStartReadiness,
	// admin.go); a real, "live"-labelled source sidesteps that
	// requirement, matching shell_render_test.go's own livePool helper
	// (app/draft) for the identical reason.
	offline := fantasy.OfflinePool()
	pool := make([]league.Player, 0, len(offline))
	for _, p := range offline {
		pool = append(pool, league.Player{ID: p.ID, Name: p.Name, Position: p.Position, NFLTeam: p.NFLTeam, ADP: p.ADP, ADPRank: p.ADPRank, ByeWeek: p.ByeWeek, Projection: p.Projection, Status: "Available"})
	}
	service.SetPlayerSource(func() ([]league.Player, int64, string) { return pool, 1, "live" })

	// authenticatedRequest captures a request the auth middleware has
	// already attached CurrentUser identity to — the same shape
	// internal/league's own authenticatedJourneyRequest helper builds —
	// so the direct Admin*/MakePick service calls below (no HTTP round
	// trip) see a real signed-in identity.
	authenticatedRequest := func(email string) *http.Request {
		authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: "Render Fixture"}, true
		})})
		var captured *http.Request
		authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = r
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
		return captured
	}
	commissioner := authenticatedRequest(viewerEmail) // demoMode makes every signed-in viewer a commissioner (IsCommissioner)

	if _, err := service.AdminSetRosterShape(commissioner, league.RosterOverride{Slots: map[string]int{"QB": 1, "RB": 1, "WR": 1, "TE": 1, "K": 1, "DST": 1}, Bench: 4}); err != nil {
		t.Fatalf("shrink the roster shape: %v", err)
	}
	if _, err := service.AdminStartDraft(commissioner); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	limit := len(service.Teams())*league.CurrentDraftRounds() + 1
	for n := 0; n < limit; n++ {
		view := service.DraftDataReadOnly(httptest.NewRequest(http.MethodGet, "/", nil))
		if complete, _ := view["draft_complete"].(bool); complete {
			break
		}
		token, _ := view["current_pick_token"].(string)
		if _, _, _, err := service.AdminForceAutopick(commissioner, "FORCE CURRENT PICK", token); err != nil {
			t.Fatalf("force pick %d: %v", n, err)
		}
	}
	view := service.DraftDataReadOnly(httptest.NewRequest(http.MethodGet, "/", nil))
	if complete, _ := view["draft_complete"].(bool); !complete {
		t.Fatalf("draft did not complete after %d forced picks", limit)
	}
	// A same-process sanity check with a clear failure message, ahead of
	// the outer test's own string-matched assertions against the
	// rendered HTML: the viewer's team made a pick in this one-round
	// draft (every team does), so ViewerFirstPickTeaser must answer true.
	if _, _, has := service.ViewerFirstPickTeaser(commissioner); !has {
		t.Fatal("ViewerFirstPickTeaser answered false after the viewer's own team completed a one-round draft")
	}
	fmt.Print(renderAuthenticatedHomepage(t))
}

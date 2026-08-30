package matchups

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
	"gridiron-2000/internal/sim/replay"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func TestMatchupsPagePreseasonAndScheduledCopyIsNotLive(t *testing.T) {
	for _, fixture := range []struct {
		name string
		want string
	}{
		{name: "preseason", want: "COMING SOON."},
		{name: "scheduled", want: "SCHEDULED."},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
			cmd.Env = append(os.Environ(),
				"MATCHUPS_RENDER_FIXTURE="+fixture.name,
				"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
				"DEMO_MODE=true", "GOOGLE_CLIENT_ID=",
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture process: %v\n%s", err, output)
			}
			body := string(output)
			if !strings.Contains(body, fixture.want) {
				t.Fatalf("fixture %s missing %q: %s", fixture.name, fixture.want, body)
			}
			for _, forbidden := range []string{"Feed connected", "Live scoring", "In progress", "LIVE LEAGUE FEED"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("fixture %s contains false live copy %q: %s", fixture.name, forbidden, body)
				}
			}
			for _, binding := range []string{
				`data-gosx-live-bind="status"`,
				`data-gosx-live-bind="headlineTop"`,
				`data-gosx-live-bind="headlineBottom"`,
				`data-gosx-live-bind="refreshLabel"`,
				`data-gosx-live-bind="noteTitle"`,
				`data-gosx-live-bind="noteBody"`,
				`data-gosx-live-bind="liveIndicator"`,
			} {
				if !strings.Contains(body, binding) {
					t.Errorf("fixture %s missing transition binding %s", fixture.name, binding)
				}
			}
			if fixture.name == "scheduled" && !strings.Contains(body, `data-gosx-live-bind="matchupIndicator.`) {
				t.Errorf("scheduled fixture omitted persistent card indicator bindings: %s", body)
			}
			if fixture.name == "scheduled" {
				if !strings.Contains(body, "Push at kickoff · 60 s fallback") {
					t.Errorf("current scheduled fixture lost live refresh copy: %s", body)
				}
				if strings.Contains(body, "Static week view") {
					t.Errorf("current scheduled fixture rendered static-week copy: %s", body)
				}
			}
			if strings.Contains(body, `data-gosx-revalidate-src="/api/league/version"`) {
				t.Errorf("fixture %s still relies on league-version revalidation", fixture.name)
			}
		})
	}

	layout, err := os.ReadFile(filepath.Join("..", "layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	layoutSource := string(layout)
	if strings.Contains(layoutSource, "LIVE LEAGUE FEED") ||
		!strings.Contains(layoutSource, `<Link href="/matchups"`) ||
		!strings.Contains(layoutSource, "Matchups") ||
		!strings.Contains(layoutSource, "data.league.matchup_footer_label") {
		t.Fatalf("shared layout still has hardcoded live matchup copy:\n%s", layout)
	}
}

// TestMatchupsPageWeekBrowserRoute's required strings were updated for
// Task 11b's page rewrite: the old "SEASON SCHEDULE // WEEK 2" /
// "WEEK 2 VIEW" / "Static schedule snapshot" copy lived in the retired
// matchup-week-controls/masthead-console sections. The new masthead's own
// degraded-state headline ("SCHEDULE" / "STATUS.", from
// matchupStaticPresentation) plus the WeekBrowser's "Back to current
// week" link cover the same regression this test guards: a non-current
// week must render its own week label and never claim a live auto-refresh
// it cannot deliver (the forbidden list below, unchanged).
func TestMatchupsPageWeekBrowserRoute(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"MATCHUPS_RENDER_FIXTURE=scheduled",
		"MATCHUPS_RENDER_QUERY=?week=2",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture process: %v\n%s", err, output)
	}
	body := string(output)
	for _, want := range []string{
		`data-gosx-live-bind="weekLabel">Week 2`,
		`data-gosx-live-bind="headlineTop">SCHEDULE`,
		`data-gosx-live-bind="headlineBottom">STATUS.`,
		"Back to current week",
		"href=\"/matchups\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("week-browser route missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{
		"60 sec",
		"60 s fallback",
		"Checks every",
		"Retrying every",
		"Scores update on their own",
		"Browser poll",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("non-current week retained auto-refresh claim %q: %s", forbidden, body)
		}
	}
}

// TestMatchupsLiveFixtureIsSummaryFirstWithOneStatusLine covers Task 11a:
// the "live" render fixture (a real in-progress BAL@BUF frame from the
// replay harness) proves the page renders the summary-first layout — the
// A6 status line in place of the retired provenance table, the viewer's
// featured matchup card before the rest of the week's matchups, and the
// bracket still below every matchup — even though the actual page.gsx
// markup this fixture drives does not exist until Task 11b.
func TestMatchupsLiveFixtureIsSummaryFirstWithOneStatusLine(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(), "MATCHUPS_RENDER_FIXTURE=live",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture process: %v\n%s", err, output)
	}
	body := string(output)
	for _, want := range []string{
		`class="matchup-status-line"`, `data-gosx-live-bind="liveState"`, `data-gosx-live-bind="sourceLine"`, `data-gosx-live-bind="gamesFinal"`,
		`class="my-matchup card"`, `data-gosx-live-bind="winProb.`, `data-gosx-live-bind="projected.`, `data-gosx-live-bind="stillToPlay.`,
		`class="matchup-pair slot-row"`, `<details class="matchup-ledger">`, `data-gosx-live-bind="starterGameState.`, `class="scorebug card"`,
		`data-gosx-live-on="scores:changed"`, "Live box scores · checked", "Q2 ", "Josh Allen", "Lamar Jackson",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("live fixture missing %q", want)
		}
	}
	for _, forbidden := range []string{"Scores from", "Browser checked", "Stats ledger updated", "View mode", "Browser poll", `class="masthead-console"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("live fixture still renders the provenance table: %q", forbidden)
		}
	}
	if strings.Index(body, `class="my-matchup card"`) > strings.Index(body, `class="matchup-grid"`) {
		t.Error("the featured matchup must render before the other matchups")
	}
	if strings.Index(body, `class="matchup-grid"`) > strings.Index(body, "playoff-truth-card") {
		t.Error("the bracket must stay below the week's matchups")
	}
}

func TestMatchupsMastheadCanShrinkBesideNavigationRail(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	const shrinkableMasthead = "grid-template-columns: minmax(0, 1fr) minmax(20rem, 0.48fr);"
	if !strings.Contains(string(styles), shrinkableMasthead) {
		t.Fatalf("page masthead lost its shrinkable primary track; authenticated pages with the desktop navigation rail can overflow")
	}
}

func TestMatchupsPageFixtureProcess(t *testing.T) {
	fixture := os.Getenv("MATCHUPS_RENDER_FIXTURE")
	if fixture == "" {
		t.Skip("fixture helper")
	}
	svc := league.Default()
	if fixture == "scheduled" {
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		if _, err := svc.AdminGenerateSchedule(request, 14, 1, 42); err != nil {
			t.Fatal(err)
		}
		kickoff := time.Now().Add(24 * time.Hour)
		svc.SetScheduleSource(func() []league.GameInfo {
			return []league.GameInfo{{ID: "future", Week: 1, Kickoff: kickoff, Away: "BUF", Home: "MIA"}}
		})
	}
	if fixture == "live" {
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		if _, err := svc.AdminGenerateSchedule(request, 14, 1, 42); err != nil {
			t.Fatal(err)
		}
		// Seat the two pinned demo-pool names as actual starters (Task 11b:
		// the page no longer carries a projection-leaders rail, so their
		// only way onto the rendered page — featured card or a scorebug —
		// is a real starting slot). This also gives the BAL@BUF frame a
		// matched, in-progress starter to resolve LiveState to LIVE for
		// real, rather than the page-wide LEDGER fallback an empty lineup
		// leaves every row at.
		if err := svc.SeedStarterForTest("team-1", 1, "QB", "p-09"); err != nil {
			t.Fatal(err)
		}
		if err := svc.SeedStarterForTest("team-2", 1, "QB", "p-06"); err != nil {
			t.Fatal(err)
		}
		// Testdata naming (decided after Task 1 review): the hyphenated
		// copy, not the fixture's own "@" name — a literal "@" in a
		// tracked test file trips privacy_contract_test.go's email-shaped
		// scan.
		game, err := replay.Load(filepath.Join("..", "..", "internal", "sim", "replay", "testdata", "box-20250907_BAL-BUF-pbp.json"))
		if err != nil {
			t.Fatal(err)
		}
		box := fantasy.ParseBoxScore(game.Frames()[70].Body) // mid second quarter
		start := time.Now().Add(-time.Hour)
		svc.SetScheduleSource(func() []league.GameInfo {
			return []league.GameInfo{{ID: "g1", Week: 1, Kickoff: start, Away: "BAL", Home: "BUF"}, {ID: "g2", Week: 1, Kickoff: start.Add(4 * time.Hour), Away: "SF", Home: "SEA"}}
		})
		snapshot := livescore.SnapshotFromBoxScores(1, start, box)
		svc.SetWeekStatsSource(func(week int) []league.WeekStatLine {
			return livescore.MergeLines(nil, week, snapshot, svc.ResolveLivePlayer)
		})
		svc.SetLiveStatusSource(func() league.LiveStatus {
			games := map[string]league.LiveGameState{}
			for _, g := range snapshot.Games {
				state := league.LiveGameState{GameID: g.ID, Away: g.Away, Home: g.Home, Period: g.Period, Clock: g.Clock, Final: g.Final, InProgress: g.InProgress, Kickoff: g.Kickoff}
				games[g.Away], games[g.Home] = state, state
			}
			return league.LiveStatus{Enabled: true, CheckedAt: time.Now().Add(-4 * time.Second), Games: games}
		})
	}
	fmt.Print(renderMatchupsPage(t))
}

func renderMatchupsPage(t *testing.T) string {
	t.Helper()
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
	target := "/"
	if query := os.Getenv("MATCHUPS_RENDER_QUERY"); query != "" {
		target += query
	}
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

// TestMatchupsPageRendersWithRealScheduleData is the regression guard for
// the production render crash this package's page.gsx used to hit:
// "render strict component TeamMark: spread source has type
// map[string]interface {}" (see ScoreTeamProps/MatchupCardProps' doc
// comments in page.gsx for the root cause and the fix).
//
// It drives a real HTTP GET through the actual file router — the same
// router.AddDir mechanism main.go uses to mount every page — against
// this package's page.gsx and page.server.go exactly as they sit on
// disk, after seeding a real generated schedule so data.matchups is
// genuinely non-empty. Before this test, nothing in the repo's suite
// rendered a .gsx template with real data at all: existing tests only
// asserted the map/struct shape MatchupsData returns
// (internal/league/service_test.go and friends), never exercised the
// render path, which is exactly why this crash shipped and reproduced
// unnoticed on an empty-schedule dev checkout.
//
// This test intentionally covers only the matchups page; it does not
// attempt to be a general render-every-page harness (see the audit
// notes in the fix's commit/PR description for why every other .gsx
// page's strict-component boundary was judged safe by inspection
// instead of duplicating this harness per page).
func TestMatchupsPageRendersWithRealScheduleData(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"MATCHUPS_RENDER_FIXTURE=scheduled",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real-schedule fixture process: %v\n%s", err, output)
	}
	body := string(output)
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("matchups page rendered the error page instead of matchup cards: %s", body)
	}
	if !strings.Contains(body, `class="my-matchup card"`) {
		t.Fatalf("expected the featured matchup card in the response, got: %s", body)
	}
	if strings.Contains(body, "NO MATCHUPS YET") {
		t.Fatalf("expected real seeded matchups, got the empty state: %s", body)
	}
	for _, want := range []string{
		"Points update as plays land",
		"tap a starter for the box score",
		`data-gosx-live-bind="starterPoints.`,
		`data-gosx-live-bind="sourceLine"`,
		`data-gosx-live-bind="gamesFinal"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("real schedule render missing matchup truth surface %q: %s", want, body)
		}
	}
}

func TestMatchupsPageLedgerDisclosureAndFreshnessLabelsAreNative(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		"<details class=\"matchup-ledger\">",
		"data-gosx-live-bind={\"starterPoints.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterPlayerName.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterPosition.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterNFLTeam.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterProvenance.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterJoinState.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterDetail.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterGameState.\" + props.LiveKey}",
		"data-gosx-live-bind={\"starterSource.\" + props.LiveKey}",
		"data-gosx-live-bind=\"sourceLine\"",
		"data-gosx-live-bind=\"statsUpdatedAt\"",
		"Configured starters only. Bench, reserve, and IR are excluded.",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("matchup page lost native disclosure/freshness contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"<span>Browser poll</span>",
		"{data.live.checked_label}",
		"{data.live.stats_updated_label}",
		"class=\"masthead-console\"",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("matchup page still carries the retired provenance table: %q", forbidden)
		}
	}
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".matchup-ledger__row", ".slot-row"} {
		if !strings.Contains(string(styles), want) {
			t.Fatalf("matchup starter-ledger styles are missing %q", want)
		}
	}
}

// TestMobileShortNameCountsRunesNotBytes is rider item 6 (review of
// ae1a525): mobileNameOverflowChars is a glyph budget, so a multi-byte
// accented name must compare against the same ~12-rune budget an equally
// long ASCII name gets, not read as artificially long because len()
// counts its UTF-8 encoding's extra bytes.
func TestMobileShortNameCountsRunesNotBytes(t *testing.T) {
	// "José Ramírez" is 12 runes (two accented) but 14 bytes: a
	// byte-length check would wrongly treat it as over budget and
	// abbreviate it, even though it fits the same budget a 12-rune ASCII
	// name like "Jayden Watts" does not need to.
	if got := mobileShortName("José Ramírez"); got != "José Ramírez" {
		t.Fatalf("mobileShortName(12-rune accented name) = %q, want it returned unabbreviated", got)
	}
	if got := mobileShortName("Jayden Watts"); got != "Jayden Watts" {
		t.Fatalf("mobileShortName(12-rune ascii name) = %q, want it returned unabbreviated", got)
	}
	// A name genuinely over budget still abbreviates, accented or not.
	if got := mobileShortName("Alejandro Domínguez"); got != "A. Domínguez" {
		t.Fatalf("mobileShortName(over-budget accented name) = %q, want the initial-abbreviated form", got)
	}
	if got := mobileShortName("Jayden Daniels"); got != "J. Daniels" {
		t.Fatalf("mobileShortName(over-budget ascii name) = %q, want the initial-abbreviated form", got)
	}
}

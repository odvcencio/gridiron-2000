package draft

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestDraftPageHasSingleH1AndMobileNavExit covers gap-audit item 8 (wave
// 3, "feel and speed"): at 390px the mobile top bar and the desktop
// command bar's Rail button both compute to no-op (see page.gsx's
// DraftMobileTabs doc comment on the League tab), leaving a phone-width
// manager with no way out of the draft room to the rest of the league,
// and /draft had no h1 at all.
func TestDraftPageHasSingleH1AndMobileNavExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftPageHasSingleH1AndMobileNavExitFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"DRAFT_NAV_EXIT_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("draft nav-exit fixture process: %v\n%s", err, output)
	}
}

func TestDraftPageHasSingleH1AndMobileNavExitFixtureProcess(t *testing.T) {
	if os.Getenv("DRAFT_NAV_EXIT_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	const email = "draft-nav-exit-render@example.com"
	if _, err := service.AssignManager(email, "Draft Nav Exit Render"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	handler := buildDraftAuthenticatedHandler(t)
	html := renderDraftForUser(t, handler, email)

	if got := strings.Count(html, "<h1"); got != 1 {
		t.Fatalf("GET /draft has %d <h1> elements, want exactly 1: %s", got, html)
	}
	if !strings.Contains(html, "Draft room") || !strings.Contains(html, "Round") || !strings.Contains(html, "Pick") {
		t.Fatalf("draft h1 omitted the round/pick summary: %s", html)
	}

	// The mobile bottom tab bar's fifth slot opens the standard site
	// navigation dialog — the same one Layout()'s hamburger button
	// targets (data-gosx-disclosure-target is a plain, delegated
	// attribute selector, so a second trigger needs no new dialog
	// markup) — so a phone-width visitor is never stuck inside the draft
	// room with no way to reach the rest of the league.
	if !strings.Contains(html, `class="draft-tabbar__tab" aria-label="Open league navigation" aria-controls="primary-navigation-dialog" aria-expanded="false" data-gosx-disclosure-target="#primary-navigation-dialog"`) {
		t.Fatalf("draft-tabbar has no correctly wired trigger for the standard navigation dialog: %s", html)
	}
}

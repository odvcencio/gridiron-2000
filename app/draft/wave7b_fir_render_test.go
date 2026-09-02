package draft

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestDraftCommandPillMarkupIsPresentAndSheetControlsAreReachable is wave
// 7b item 1's own shell render evidence: the floating pill's own markup
// (the compact round/pick/status spans, the ▾ toggle, and the sheet
// controls behind it — ready state, sound, autopick, League navigation,
// and — for the commissioner — the force-pick trigger) all render in the
// document, reachable from a plain server render with no JavaScript.
func TestDraftCommandPillMarkupIsPresentAndSheetControlsAreReachable(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftCommandPillMarkupIsPresentAndSheetControlsAreReachableFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"DRAFT_PILL_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
		"COMMISSIONER_EMAILS=fir-pill-commissioner@example.com",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("draft pill fixture process: %v\n%s", err, output)
	}
}

func TestDraftCommandPillMarkupIsPresentAndSheetControlsAreReachableFixtureProcess(t *testing.T) {
	if os.Getenv("DRAFT_PILL_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	service.SetPlayerSource(livePool()) // draftStartReadiness refuses an offline pool (admin.go)
	const commissioner = "fir-pill-commissioner@example.com"
	if _, err := service.AssignManager(commissioner, "Fir Pill Commissioner"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	handler := buildDraftAuthenticatedHandler(t)
	// The pre-draft command bar carries no data-pick-clock at all (the
	// scheduled-window/NOT SET states, DraftCommandBar) — start the draft
	// first, matching TestDraftShellRendersEveryDraftStateFixtureProcess's
	// own "live" state.
	postDraftAction(t, handler, commissioner, "draft-start", url.Values{"confirm": {"START"}})
	html := renderDraftForUser(t, handler, commissioner)

	// The pill row itself: team badge, compact round/pick, compact status,
	// the clock (unmodified, still carrying data-pick-clock — see
	// DraftCommandBar's own doc comment on why this element is never
	// duplicated), and the ▾ toggle — wave-7 re-audit item 2 (yew) gave
	// the toggle a visible "MENU" label instead of a bare glyph (the
	// glyph alone rendered a sub-9px-wide hit target), which also became
	// the toggle's own accessible name in place of the old aria-label.
	for _, want := range []string{
		`class="draft-command__pill-row"`,
		`class="draft-command__pill-meta mono"`,
		`class="draft-command__pill-status mono"`,
		`class="draft-command__pill-toggle"`,
		`class="draft-command__pill-caret" aria-controls="draft-command-sheet"`,
		`class="draft-command__pill-caret-label mono">MENU</span>`,
		`data-pick-clock`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("draft command pill missing %q", want)
		}
	}

	// The sheet, behind the toggle: every control item 1 promises.
	sheetStart := strings.Index(html, `class="draft-command__sheet"`)
	if sheetStart < 0 {
		t.Fatal("draft command pill sheet not found")
	}
	sheetEnd := strings.Index(html[sheetStart:], `</details>`)
	if sheetEnd < 0 {
		t.Fatal("draft command pill sheet has no closing </details>")
	}
	sheet := html[sheetStart : sheetStart+sheetEnd]
	for _, want := range []string{
		"Mark me ready",
		"Turn autopick on",
		`aria-label="Open league navigation" aria-controls="primary-navigation-dialog" aria-expanded="false" data-gosx-disclosure-target="#primary-navigation-dialog"`,
		`data-gosx-disclosure-target="#draft-commissioner"`,
		"draft-command__pill-sound",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("draft command pill sheet missing %q: %s", want, sheet)
		}
	}

	// The League trigger moved OUT of the bottom tab bar (item 2): the
	// tab bar itself must carry exactly 5 real content tabs, never a
	// sixth League slot.
	if strings.Contains(html, `class="draft-tabbar__tab" aria-label="Open league navigation"`) {
		t.Error("the League trigger is still in the bottom tab bar; item 2 moved it into the pill sheet")
	}
	tabbarStart := strings.Index(html, `class="draft-tabbar"`)
	tabbarEnd := strings.Index(html[tabbarStart:], `</nav>`)
	tabbar := html[tabbarStart : tabbarStart+tabbarEnd]
	if n := strings.Count(tabbar, `class="draft-tabbar__tab"`); n != 5 {
		t.Errorf("draft-tabbar has %d tabs, want exactly 5 (Pool, Big Board, Picks, Draft grid, Teams): %s", n, tabbar)
	}
}

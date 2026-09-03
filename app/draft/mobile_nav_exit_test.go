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
//
// Wave 7b item 2 (2026-08-31 audit) moved the League-navigation trigger
// out of the bottom tab bar (a sixth "flex: 1 1 0" slot squeezed all five
// real content tabs, "Big Board" already the tightest label) and into
// DraftCommandBar's own new .draft-command__pill-toggle sheet (wave 7b
// item 1) instead — one tap from the always-visible pill rather than a
// seventh-width tab-bar slot. The exit itself is unchanged: still the
// SAME #primary-navigation-dialog Layout()'s hamburger button targets.
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
	// D15 (spruce audit): pre-draft, the h1 reads "Draft room · opens
	// <date> · <time>" (never a fake "Round 1 · Pick 1" the room has not
	// reached yet) — DraftCommandBar's OWN "PICK 1 / 136" chip still
	// carries the round/pick figures for a started room, so this check
	// accepts either shape rather than assuming the draft has started.
	if !strings.Contains(html, "Draft room") {
		t.Fatalf("draft h1 lost its own title: %s", html)
	}
	if !strings.Contains(html, "opens") && (!strings.Contains(html, "Round") || !strings.Contains(html, "Pick")) {
		t.Fatalf("draft h1 has neither the pre-draft \"opens\" copy nor the round/pick summary: %s", html)
	}

	// DraftCommandBar's pill sheet (wave 7b item 1) opens the standard site
	// navigation dialog — the same one Layout()'s hamburger button
	// targets (data-gosx-disclosure-target is a plain, delegated
	// attribute selector, so a second trigger needs no new dialog
	// markup) — so a phone-width visitor is never stuck inside the draft
	// room with no way to reach the rest of the league. Wave 7b item 2
	// moved this trigger off the bottom tab bar (its old home) into the
	// pill sheet: a plain "btn btn-sm" button now, not a
	// "draft-tabbar__tab".
	if !strings.Contains(html, `class="btn btn-sm" aria-label="Open league navigation" aria-controls="primary-navigation-dialog" aria-expanded="false" data-gosx-disclosure-target="#primary-navigation-dialog"`) {
		t.Fatalf("draft-command pill sheet has no correctly wired trigger for the standard navigation dialog: %s", html)
	}
	if strings.Contains(html, `class="draft-tabbar__tab" aria-label="Open league navigation"`) {
		t.Fatalf("the League trigger is still in the bottom tab bar; wave 7b item 2 moved it into the pill sheet: %s", html)
	}
}

// TestDraftTabVocabularyIsOneWordPerViewEverywhere is gap-audit item 5
// (wave 4 — linden): three separate on-page vocabularies used to name the
// same views differently — the bottom bar's "Players"/"Queue", the
// pick-history pane's "Tape"/"Board", and the desktop rail's "Queue" all
// meant the pool/Big Board views, so a screen reader or a mobile-then-
// desktop manager heard three different names for one thing. Wave 4's own
// fix over-corrected: it renamed the pick-history pane's "Board" segment
// (DraftHistoryHead's BoardHref tab, the LEAGUE-WIDE pick grid — every
// team's picks laid out by round) to "Big Board" too, so the page carried
// two different .segment controls both reading "Big Board" — one the
// manager's own private ranked list (DraftMyTeam's mine-queue tab, the
// actual Big Board), one the shared draft grid. Wave 6 item 2b renames
// the pick-history pane's tab to "Draft grid", leaving "Big Board" for
// the private list alone. This pins the one-word-per-view outcome
// directly in the source: POOL, BIG BOARD, DRAFT GRID, PICKS, TEAMS,
// ROOM in all three navigations (Roster is desktop-rail-only and does
// not collide with anything else, so it is untouched).
func TestDraftTabVocabularyIsOneWordPerViewEverywhere(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		// Bottom bar (DraftMobileTabs): Players -> Pool, Queue -> Big Board.
		`<label class="draft-tabbar__tab" for="tab-players">Pool</label>`,
		`<label class="draft-tabbar__tab" for="tab-queue">Big Board</label>`,
		`>Picks</a>`,
		`>Teams</a>`,
		// In-pane pick-history segment (DraftHistoryHead): Tape -> Picks,
		// Board -> Draft grid — the league-wide grid, never the manager's
		// own private list.
		`aria-current={props.ShowTape}>Picks</a>`,
		`aria-current={props.ShowBoard}>Draft grid</a>`,
		// Desktop rail "my team" segment (DraftMyTeam): Queue -> Big Board —
		// the manager's own private ranked list, the only "Big Board" on
		// the page.
		`<label class="segment__option" for="mine-queue">Big Board</label>`,
		`<label class="segment__option" for="mine-room">Room</label>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("draft page.gsx missing one-word-per-view tab contract %q", want)
		}
	}
	for _, stale := range []string{
		`for="tab-players">Players</label>`,
		`for="tab-queue">Queue</label>`,
		`>Tape</a>`,
		`for="mine-queue">Queue</label>`,
		`aria-current={props.ShowBoard}>Big Board</a>`,
	} {
		if strings.Contains(source, stale) {
			t.Errorf("draft page.gsx retained a stale tab label %q", stale)
		}
	}
	// "Big Board" names the manager's own private ranked list on the two
	// navs that offer it (the mobile bottom bar's queue tab, the desktop
	// rail's mine-queue tab) — never the league-wide draft grid.
	if got := strings.Count(source, ">Big Board<"); got != 2 {
		t.Errorf("draft page.gsx must say \"Big Board\" exactly twice (the private list, on its two navs) — the league-wide grid is a different name: found %d occurrences", got)
	}
}

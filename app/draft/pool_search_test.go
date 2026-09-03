package draft

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestDraftPoolSearchIsReal is item 1's own regression test (comb —
// oleander, 2026-09-02 audit). Before this fix, typing "Hekker" in the
// pool search set the shared live region to "0 of 50 shown" while every
// non-matching row stayed painted: the client-side filter toggled
// gosx-filter-row--hidden on each .avail-row, but styles.css only ever
// hid .pool-row (/players' own row class) with that class, and the
// search input itself sat outside any <form> with no name attribute, so
// a no-JS visitor's Enter did nothing at all. This test pins both
// halves of the fix: the rendered no-JS <form> contract, and the
// server-side ?q= filter (already correct at the data layer,
// service.go's draftData/playerMatchesQuery) actually narrowing the
// rendered rows once the form can reach it.
func TestDraftPoolSearchIsReal(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftPoolSearchIsRealFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"DRAFT_POOL_SEARCH_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("draft pool search fixture process: %v\n%s", err, output)
	}
}

func TestDraftPoolSearchIsRealFixtureProcess(t *testing.T) {
	if os.Getenv("DRAFT_POOL_SEARCH_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	service.SetPlayerSource(func() ([]league.Player, int64, string) {
		return []league.Player{
			{ID: "p-hekker", Name: "Johnny Hekker", Position: "P", NFLTeam: "CAR", ADPRank: 300, Projection: 4},
			{ID: "p-other", Name: "Someone Else", Position: "WR", NFLTeam: "SEA", ADPRank: 10, Projection: 12},
		}, 1, "live"
	})
	const seatedEmail = "search-draft-render@example.com"
	if _, err := service.AssignManager(seatedEmail, "Search Draft Render"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	handler := buildDraftAuthenticatedHandler(t)

	// The pool head's search input sits inside a real GET form: a no-JS
	// visitor's Enter/submit reaches the server, not a dead end.
	page := renderDraftForUser(t, handler, seatedEmail)
	for _, want := range []string{
		`<form method="get" action="/draft" class="draft-search-form">`,
		`id="draft-search"`,
		`name="q"`,
		`type="submit"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("draft page missing no-JS search form contract %q: %s", want, page)
		}
	}

	// The no-JS path itself: a plain GET with ?q= must narrow the
	// rendered rows server-side, with no JavaScript and no reliance on
	// a client-side hidden class.
	searched := renderDraftForUserPath(t, handler, seatedEmail, "/?q=hekker")
	if !strings.Contains(searched, "Johnny Hekker") {
		t.Errorf("?q=hekker must keep the matching player in the rendered rows: %s", searched)
	}
	if strings.Contains(searched, "Someone Else") {
		t.Errorf("?q=hekker must drop the non-matching player from the rendered rows: %s", searched)
	}
	if !strings.Contains(searched, `value="hekker"`) {
		t.Errorf("the search input must echo the active query back after a no-JS reload: %s", searched)
	}
}

// TestAvailRowHiddenClassIsActuallyHidden pins the CSS half of item 1:
// the client-side filter (data-gosx-filter, gosx navigation runtime)
// toggles gosx-filter-row--hidden on a non-matching .avail-row, and the
// stylesheet must actually hide it — the shared rule this repo already
// carried only ever covered .pool-row (/players' own row class), so
// every .avail-row the runtime marked hidden stayed painted while the
// live region announced a lower count than what was on screen.
func TestAvailRowHiddenClassIsActuallyHidden(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	if !strings.Contains(css, ".avail-row.gosx-filter-row--hidden") {
		t.Fatal("stylesheet missing .avail-row.gosx-filter-row--hidden — the client search filter marks rows hidden with no CSS to act on it")
	}
}

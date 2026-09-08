package team

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestTeamNameResetButtonRendersOnlyForACustomName pins wave-6 glue item
// 5: /team's secondary "Reset to configured name" control (posting to the
// team-name-reset action, league.Service.ResetTeamName's explicit
// counterpart to the now-blank-rejecting team-rename) must render only
// for a seat carrying a custom name override (team.has_custom_name) — a
// team still on its configured default name has nothing to reset.
//
// J5 F27 (wave C — birch): the control used to post immediately with no
// confirmation and named no value. It is now a closed-by-default
// action-confirmation disclosure whose summary names the exact value it
// restores ("Reset to \"<configured name>\""), so this test checks for
// that dynamic phrase and the disclosure/confirm shape instead of the
// old static "Reset to configured name" string.
func TestTeamNameResetButtonRendersOnlyForACustomName(t *testing.T) {
	// TestMain (testmain_test.go) sets DEMO_MODE=true once, before this
	// process-wide league.Default() singleton is ever initialized: the
	// anonymous request below acts as team-1 with commissioner authority,
	// the same fixture shape app/activity's own real-render tests use.
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
	get := func() string {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET / = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	service := league.Default()
	teamID := service.Teams()[0].ID
	anonymous := httptest.NewRequest(http.MethodGet, "/", nil)

	// Demo mode's viewer acts as this team already carrying its configured
	// default name (TestMain's fresh, per-package DATA_FILE fixture never
	// renames it before this test runs): the Reset control must stay off.
	configuredName := configuredTeamName(teamID)
	if configuredName == "" {
		t.Fatalf("configuredTeamName(%q) is empty; fixture team not found", teamID)
	}
	// The rendered HTML entity-escapes the literal quote marks in the
	// button text (&#34;); a browser still shows a plain " to the manager.
	resetPhrase := "Reset to &#34;" + configuredName + "&#34;"
	before := get()
	if strings.Contains(before, resetPhrase) {
		t.Fatalf("configured-default team name still renders the Reset control: %s", before)
	}

	if _, err := service.RenameTeam(anonymous, teamID, "Custom Franchise Name"); err != nil {
		t.Fatalf("RenameTeam: %v", err)
	}
	t.Cleanup(func() {
		if _, err := service.ResetTeamName(anonymous, teamID); err != nil {
			t.Errorf("cleanup ResetTeamName: %v", err)
		}
	})

	after := get()
	if !strings.Contains(after, resetPhrase) {
		t.Fatalf("custom team name did not render the Reset control naming %q: %s", configuredName, after)
	}
	if !strings.Contains(after, `class="action-confirmation"`) {
		t.Fatalf("Reset control is missing its action-confirmation disclosure: %s", after)
	}
	if !strings.Contains(after, `class="button button--secondary button--compact"`) {
		t.Fatalf("Reset control is missing its secondary button styling: %s", after)
	}

	if _, err := service.ResetTeamName(anonymous, teamID); err != nil {
		t.Fatalf("ResetTeamName: %v", err)
	}
	restored := get()
	if strings.Contains(restored, resetPhrase) {
		t.Fatalf("Reset control still rendered after the name was reset: %s", restored)
	}
}

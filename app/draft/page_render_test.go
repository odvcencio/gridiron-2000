package draft

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

func renderDraftForUser(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Test-User", email)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /draft for %s = %d, want 200; body: %s", email, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func buildDraftAuthenticatedHandler(t *testing.T) http.Handler {
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
	return authn.Middleware(handler)
}

func TestDraftPageSeatlessOmitsControlsButKeepsOnboarding(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftPageSeatlessOmitsControlsButKeepsOnboardingFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"DRAFT_SEATLESS_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("draft seatless fixture process: %v\n%s", err, output)
	}
}

func TestDraftPageSeatlessOmitsControlsButKeepsOnboardingFixtureProcess(t *testing.T) {
	if os.Getenv("DRAFT_SEATLESS_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	const seatlessEmail = "seatless-draft-render@example.com"
	const seatedEmail = "seated-draft-render@example.com"
	if _, err := service.EnsureMember(seatlessEmail, "Seatless Draft Render"); err != nil {
		t.Fatalf("EnsureMember: %v", err)
	}
	if _, err := service.AssignManager(seatedEmail, "Seated Draft Render"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	handler := buildDraftAuthenticatedHandler(t)
	seatless := renderDraftForUser(t, handler, seatlessEmail)
	for _, want := range []string{"NO TEAM SEAT", "Build your big board", "Claim a franchise", "href=\"/join\""} {
		if !strings.Contains(seatless, want) {
			t.Errorf("seatless draft page missing %q: %s", want, seatless)
		}
	}
	for _, forbidden := range []string{"#ready-toggle", "#autopick-toggle", "toggle-ready", "toggle-autopick", "make-pick", ">Locked<"} {
		if strings.Contains(seatless, forbidden) {
			t.Errorf("seatless draft page rendered forbidden control %q: %s", forbidden, seatless)
		}
	}

	pickemOnly := renderDraftForUser(t, handler, "pickem-only-draft-render@example.com")
	for _, want := range []string{"SIGNED IN · MEMBERSHIP NOT RECORDED", `href="/guide#identity"`, "Open Pick&#39;em HQ"} {
		if !strings.Contains(pickemOnly, want) {
			t.Errorf("pick'em-only draft page missing truthful next link %q: %s", want, pickemOnly)
		}
	}
	for _, forbidden := range []string{"Claim an open franchise", "Claim a franchise", `href="/join"`} {
		if strings.Contains(pickemOnly, forbidden) {
			t.Errorf("pick'em-only draft page offered unavailable action %q: %s", forbidden, pickemOnly)
		}
	}

	// Fill the remaining seats, then admit a persisted but seatless viewer.
	// This distinguishes a full league from the open-seat fixture above and
	// proves the CTA changes to waiting guidance without retaining /join.
	for index := 0; index < len(service.Teams())-1; index++ {
		if _, err := service.AssignManager(fmt.Sprintf("full-draft-render-%d@example.com", index), fmt.Sprintf("Full Draft Render %d", index)); err != nil {
			t.Fatalf("AssignManager %d: %v", index, err)
		}
	}
	if _, err := service.EnsureMember("full-draft-render@example.com", "Full Draft Render"); err != nil {
		t.Fatalf("EnsureMember full viewer: %v", err)
	}
	full := renderDraftForUser(t, handler, "full-draft-render@example.com")
	for _, want := range []string{"ADMITTED · NO FRANCHISE", `href="/pickem"`, "Browse player pool"} {
		if !strings.Contains(full, want) {
			t.Errorf("full-league draft page missing truthful waiting link %q: %s", want, full)
		}
	}
	for _, forbidden := range []string{"Claim an open franchise", "Claim a franchise", `href="/join"`} {
		if strings.Contains(full, forbidden) {
			t.Errorf("full-league draft page offered unavailable action %q: %s", forbidden, full)
		}
	}

	seated := renderDraftForUser(t, handler, seatedEmail)
	for _, want := range []string{
		"#ready-toggle",
		"#autopick-toggle",
		"toggle-ready",
		"toggle-autopick",
		"make-pick",
		">Locked<",
		"Check in once your Big Board is set.",
		"Mark me ready",
		`class="button button--primary button--compact"`,
		`data-ready="false"`,
		`data-on-clock="false"`,
		`aria-pressed="false"`,
		"Check in now ↑",
	} {
		if !strings.Contains(seated, want) {
			t.Errorf("seated draft page missing %q: %s", want, seated)
		}
	}
}

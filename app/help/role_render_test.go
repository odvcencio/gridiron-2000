package help

import (
	"bytes"
	"encoding/json"
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

// TestHelpRoleFixtureProcess is the child side of
// TestHelpIndexRendersActualViewerRoles. A subprocess keeps league.Default's
// process-wide service isolated for each role/mode fixture while still driving
// the actual route, GoSX template, auth middleware, and PublicEntry projection.
func TestHelpRoleFixtureProcess(t *testing.T) {
	fixture := os.Getenv("HELP_ROLE_RENDER_FIXTURE")
	if fixture == "" {
		return
	}
	parts := strings.Split(fixture, "/")
	if len(parts) != 2 {
		t.Fatalf("invalid HELP_ROLE_RENDER_FIXTURE %q", fixture)
	}
	role := parts[1]
	email := map[string]string{
		"primary":               "primary@example.com",
		"co-manager":            "co@example.com",
		"seatless-commissioner": "commissioner@example.com",
	}[role]
	if email == "" {
		t.Fatalf("unknown fixture role %q", role)
	}

	// Calling league.Default after the fixture environment is set makes this
	// route use the temporary config/state files, not the developer checkout.
	_ = league.Default()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			identity := r.Header.Get("X-Test-User")
			return auth.User{ID: identity, Email: identity, Name: "Help Role Fixture"}, identity != ""
		}),
	})
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help role fixture", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Test-User", email)
	authn.Middleware(handler).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	fmt.Print(recorder.Body.String())
}

func writeHelpRoleFixture(t *testing.T, mode, role string) (configPath, statePath string) {
	t.Helper()
	dir := t.TempDir()
	config, err := os.ReadFile(filepath.Join("..", "..", "config", "league.json.example"))
	if err != nil {
		t.Fatal(err)
	}
	config = bytes.Replace(config, []byte(`"mode_label": "DYNASTY"`), []byte(fmt.Sprintf(`"mode_label": "%s"`, strings.ToUpper(mode))), 1)
	configPath = filepath.Join(dir, "league.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}

	email := map[string]string{
		"primary":               "primary@example.com",
		"co-manager":            "co@example.com",
		"seatless-commissioner": "commissioner@example.com",
	}[role]
	state := league.PersistedState{Members: map[string]league.Member{
		email: {TeamID: "", Name: "Help Role Fixture", Email: email},
	}}
	if role == "primary" {
		state.Members[email] = league.Member{TeamID: "team-1", Name: "Help Role Fixture", Email: email}
	}
	if role == "co-manager" {
		state.Members[email] = league.Member{TeamID: "team-1", Name: "Help Role Fixture", Email: email, Role: "co"}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statePath = filepath.Join(dir, "league-state.json")
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, statePath
}

func renderHelpRoleFixture(t *testing.T, mode, role string) string {
	t.Helper()
	configPath, statePath := writeHelpRoleFixture(t, mode, role)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelpRoleFixtureProcess$")
	cmd.Dir = "."
	cmd.Env = append(os.Environ(),
		"HELP_ROLE_RENDER_FIXTURE="+mode+"/"+role,
		"LEAGUE_FILE="+configPath,
		"DATA_FILE="+statePath,
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"GOOGLE_CLIENT_SECRET=",
		"COMMISSIONER_EMAILS=commissioner@example.com",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("role fixture %s/%s: %v\n%s", mode, role, err, output)
	}
	return string(output)
}

func TestHelpIndexRendersActualViewerRoles(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		role        string
		roleLabel   string
		required    string
		showOverlay bool
	}{
		{name: "dynasty primary", mode: "dynasty", role: "primary", roleLabel: "PRIMARY MANAGER", required: "Open the Team terminal"},
		{name: "redraft co-manager", mode: "redraft", role: "co-manager", roleLabel: "CO-MANAGER", required: "private team-seat order"},
		{name: "dynasty seatless commissioner", mode: "dynasty", role: "seatless-commissioner", roleLabel: "SEATLESS MEMBER", required: "Big Board truth", showOverlay: true},
		{name: "redraft seatless commissioner", mode: "redraft", role: "seatless-commissioner", roleLabel: "SEATLESS MEMBER", required: "Big Board truth", showOverlay: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderHelpRoleFixture(t, tt.mode, tt.role)
			start := strings.Index(body, `<section class="guide-section guide-section--accent" id="checklists"`)
			end := strings.Index(body, `<section class="guide-section" id="migration"`)
			if start < 0 || end <= start {
				t.Fatalf("rendered help checklist section is missing: %s", body)
			}
			checklist := body[start:end]
			if !strings.Contains(checklist, tt.roleLabel) {
				t.Errorf("role fixture omitted %q: %s", tt.roleLabel, checklist)
			}
			if !strings.Contains(body, tt.mode) {
				t.Errorf("role fixture omitted mode %q: %s", tt.mode, body)
			}
			if !strings.Contains(body, tt.required) {
				t.Errorf("role fixture omitted %q: %s", tt.required, body)
			}
			if tt.showOverlay {
				if !strings.Contains(checklist, "COMMISSIONER OVERLAY") {
					t.Errorf("commissioner fixture omitted orthogonal overlay: %s", checklist)
				}
			} else if strings.Contains(checklist, "COMMISSIONER OVERLAY") {
				t.Errorf("non-commissioner fixture rendered commissioner overlay: %s", checklist)
			}
		})
	}
}

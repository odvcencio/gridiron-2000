package settings

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func TestSettingsLoadConsumesOneNativeNoticeAndSharedActionFeedback(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	if got := strings.Count(source, "Flashes(\"notice\")"); got != 1 {
		t.Fatalf("settings Load consumes %d notice flash readers, want exactly one", got)
	}
	if !strings.Contains(source, "session.Current(ctx.Request)") {
		t.Fatal("settings Load must read the native notice through the current session")
	}
	if strings.Contains(source, "session.AddFlash") {
		t.Fatal("settings action must use the shared redirect feedback helper, not write a second flash")
	}
	if !strings.Contains(source, "actionui.RedirectWithNotice(ctx, \"/settings\", message)") {
		t.Fatal("settings action must return its success copy through RedirectWithNotice")
	}
}

func TestSettingsTemplateKeepsBothNativeChoicesActionable(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	if got := strings.Count(source, "<form method=\"post\""); got < 2 {
		t.Fatalf("settings template exposes %d native forms, want at least On and Off", got)
	}
	if got := strings.Count(source, "type=\"submit\""); got < 2 {
		t.Fatalf("settings template exposes %d submit controls, want both choices actionable", got)
	}
	if strings.Contains(source, "disabled=\"disabled\"") {
		t.Fatal("live notification choices must remain actionable")
	}
}

func TestSettingsMobileContractKeepsControlsTouchAndFocusSafe(t *testing.T) {
	cssBytes, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	for _, want := range []string{
		".notification-choice[aria-pressed=\"true\"]",
		".notification-choice[data-current=\"true\"]",
		"touch-action: manipulation",
		".notification-choice:focus-visible",
		".notification-choice-group form",
		".notification-choice-group form,",
		"width: 100%",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("settings mobile CSS omitted %q", want)
		}
	}
}

func TestSettingsSelectedChoiceKeepsSolidInteractionContrast(t *testing.T) {
	cssBytes, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	const selectedInteractions = `.notification-choice[aria-pressed="true"]:hover,
.notification-choice[data-current="true"]:hover,
.notification-choice[aria-pressed="true"]:active,
.notification-choice[data-current="true"]:active,
.notification-choice[aria-pressed="true"]:focus-visible,
.notification-choice[data-current="true"]:focus-visible`
	start := strings.Index(css, selectedInteractions)
	if start < 0 {
		t.Fatal("selected notification choices need explicit hover/active/focus interaction styling")
	}
	end := strings.Index(css[start:], "}")
	if end < 0 {
		t.Fatal("selected notification interaction rule is incomplete")
	}
	rule := css[start : start+end]
	if !strings.Contains(rule, "color: var(--color-ink-on-accent)") {
		t.Fatal("selected notification interaction text must keep its high-contrast ink")
	}
	if !strings.Contains(rule, "background: var(--color-accent-cyan)") {
		t.Fatal("selected notification interaction must retain a solid accent background")
	}
	if strings.Contains(rule, "var(--color-cyan-haze)") {
		t.Fatal("selected notification interaction must not combine ink-on-accent with translucent cyan haze")
	}
}

func TestSettingsPageRendersDeliveryTruthAndCurrentMarkers(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

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

	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "settings-manager", Email: "manager@example.com"}, true
	})})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	authn.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (settings page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"EMAIL NOT CONFIGURED",
		"EMAIL ONLY // SMS NOT SUPPORTED",
		"Preferences saved for",
		"aria-pressed=\"true\"",
		"data-current=\"true\"",
		"CURRENT",
		"✓",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings render omitted %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Delivering to") {
		t.Fatal("settings render claims delivery to an account while email transport is not configured")
	}
}

// TestSettingsPageNoTransportRowsAndSectionNoticeMatchDeliveryTruth is item
// 6's own regression test (2026-09-02 audit): with no mail transport
// configured, every row under "01 // LIVE DELIVERY" used to read "Current
// state: ON" beside the masthead's own honest "EMAIL NOT CONFIGURED" —
// each row must now say so is not configured too, and the section itself
// carries a plain-language notice instead of silently promising delivery.
func TestSettingsPageNoTransportRowsAndSectionNoticeMatchDeliveryTruth(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

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

	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "settings-manager-2", Email: "manager2@example.com"}, true
	})})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	authn.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (settings page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Email delivery is not configured on this league; these preferences apply once it is.") {
		t.Errorf("settings render missing the live-delivery section's no-transport notice: %s", body)
	}
	if !strings.Contains(body, "On · sends once email is configured") && !strings.Contains(body, "Off · sends once email is configured") {
		t.Errorf("settings render has no row stating its no-transport state: %s", body)
	}
	if strings.Contains(body, "Current state: ON</span>") || strings.Contains(body, "Current state: OFF</span>") {
		t.Errorf("settings render has a bare ON/OFF row while email transport is not configured: %s", body)
	}
}

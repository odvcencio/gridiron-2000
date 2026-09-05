package settings

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderSettingsPage(t *testing.T) string {
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
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "settings-textflow", Email: "settings-textflow@example.com"}, true
	})})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	authn.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (settings page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestSettingsCategoryLegendAndStateLineUseTextBlock pins the textflow
// wave (2026-09-05): a notification category's own name (<legend>) and
// its ON/OFF state line (.notification-preference__state) render through
// <TextBlock> instead of plain, unbounded elements. NotificationRow
// moved from a strict `component` to a legacy `func` for this wave:
// GoSX v0.55.2 forbids <TextBlock> inside a strict server component, and
// this component is never rendered as a RenderProgramComponent entry
// (only nested, from Page()), so the move carries no render-entry risk
// the way app/wire's SignalCard would have (see that file's own comment).
func TestSettingsCategoryLegendAndStateLineUseTextBlock(t *testing.T) {
	body := renderSettingsPage(t)
	at := strings.Index(body, `data-notification-category="density"`)
	if at < 0 {
		t.Fatal("no density notification-preference in the rendered settings page")
	}
	segment := body[at:]
	legendAt := strings.Index(segment, "<legend")
	if legendAt < 0 {
		t.Fatal("density fieldset has no <legend")
	}
	legendTag := segment[legendAt : legendAt+strings.Index(segment[legendAt:], ">")]
	if !strings.Contains(legendTag, "data-gosx-text-layout") {
		t.Errorf("category legend missing data-gosx-text-layout: %s", legendTag)
	}

	stateAt := strings.Index(body, `class="notification-preference__state"`)
	if stateAt < 0 {
		t.Fatal("no .notification-preference__state in the rendered settings page")
	}
	stateTagStart := strings.LastIndex(body[:stateAt], "<span")
	stateTag := body[stateTagStart : stateAt+strings.Index(body[stateAt:], ">")]
	if !strings.Contains(stateTag, "data-gosx-text-layout") {
		t.Errorf("ON/OFF state line missing data-gosx-text-layout: %s", stateTag)
	}
}

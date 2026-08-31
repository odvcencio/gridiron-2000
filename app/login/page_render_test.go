package login

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderLoginPage(t *testing.T, target string) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")

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

	req := httptest.NewRequest(http.MethodGet, "/?next="+target, nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET / (login page) = %d, want 200; body: %s", res.Code, res.Body.String())
	}
	return res.Body.String()
}

func TestLoginPageRendersSanitizedReturnCTA(t *testing.T) {
	valid := renderLoginPage(t, "%2Fdraft%3Fweek%3D1")
	if !strings.Contains(valid, `href="/auth/google/start?next=%2Fdraft%3Fweek%3D1"`) {
		t.Fatalf("valid login CTA did not preserve encoded target: %s", valid)
	}
	if !strings.Contains(valid, "After sign-in, we&#39;ll return you to the page you requested.") {
		t.Fatalf("valid login page omitted the return note: %s", valid)
	}
	if !strings.Contains(valid, "SIGN IN TO ENTER.") {
		t.Fatalf("login page omitted authentication-first headline: %s", valid)
	}
	if !strings.Contains(valid, "The league checks its admission policy after authentication") {
		t.Fatalf("login page omitted truthful admission guidance: %s", valid)
	}
	if strings.Contains(valid, "Every seat belongs to one manager.") || strings.Contains(valid, "Your league access will be waiting.") {
		t.Fatalf("login page retained unconditional admission/seat promise: %s", valid)
	}
	for _, want := range []string{
		"LEAGUE DRAFT",
		"Wednesday, December 31, 2098",
		"7:00 PM EST",
		"Eastern Time",
		"SCHEDULED WINDOW",
	} {
		if !strings.Contains(valid, want) {
			t.Errorf("login page omitted configured event fact %q: %s", want, valid)
		}
	}

	root := renderLoginPage(t, "%2F")
	if strings.Contains(root, "login-return-note") {
		t.Fatalf("root login target unexpectedly rendered a return note: %s", root)
	}

	hostile := renderLoginPage(t, "https%3A%2F%2Fevil.example%2Fsteal")
	if strings.Contains(hostile, `href="/auth/google/start?next=https`) || strings.Contains(hostile, "login-return-note") {
		t.Fatalf("hostile next leaked into the login CTA: %s", hostile)
	}
}
func TestLoginPageFallsBackFromAuthenticationReturnTargets(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "login page", target: "%2Flogin"},
		{name: "oauth start", target: "%2Fauth%2Fgoogle%2Fstart"},
		{name: "oauth callback", target: "%2Fauth%2Fgoogle%2Fcallback%3Fcode%3Dstale"},
		{name: "login traversal", target: "%2Fdraft%2F..%2Flogin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderLoginPage(t, tt.target)
			if strings.Contains(rendered, "login-return-note") {
				t.Fatalf("authentication endpoint target rendered a return note: %s", rendered)
			}
			if !strings.Contains(rendered, "auth/google/start?next=%2F") {
				t.Fatalf("authentication endpoint target did not fall back to root CTA: %s", rendered)
			}
		})
	}
}

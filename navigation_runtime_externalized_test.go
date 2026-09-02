package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// navigationScriptTagPattern matches the layout's navigation-runtime
// <script> tag once it loads externally: an hashed "/gosx-nav/<hash>.js"
// src, the framework's own "data-gosx-navigation" contract attribute
// (styles.css and navigation_layout_test.go both key CSS/behavior off its
// presence), and a CSP nonce.
var navigationScriptTagPattern = regexp.MustCompile(`<script data-gosx-navigation="true" src="(/gosx-nav/[0-9a-f]+\.js)" nonce="[^"]+"></script>`)

// TestNavigationRuntimeIsExternalizedAndImmutable covers the wave-3
// addendum "externalize the 88KB inline navigation runtime": GoSX's
// App.EnableNavigation default inlines runtimehost.NavigationRuntime
// (88,076 bytes) as literal <script> text on every single page render —
// 71-93% of a typical page's transfer per the coordinator's measurement.
// BuildApp instead supplies its own router.SetNavigationHead builder that
// references a hashed, immutable, externally-served copy. See app_build.go's
// router.SetNavigationHead call for the full mechanism (and why
// App.EnableNavigation is not also called).
func TestNavigationRuntimeIsExternalizedAndImmutable(t *testing.T) {
	handler := buildHarnessApp(t, false)

	loginRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", loginRecorder.Code)
	}
	body := loginRecorder.Body.String()

	match := navigationScriptTagPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatal("GET /login: no externalized, nonced navigation <script src> tag found")
	}
	scriptHref := match[1]

	// The runtime's own JS text (a distinctive identifier from
	// client/runtime/host/navigation.ts's minified output) must not also
	// be inlined anywhere in the document — that would mean both the old
	// and new mechanisms fired.
	if strings.Contains(body, "gosxHost") {
		t.Error("GET /login still inlines the navigation runtime's own JS text alongside the externalized <script src> tag")
	}

	// data-gosx-navigation-state proves RouteContext.NavigationEnabled()
	// still reports true (see route.go's newRouteContext and
	// documentNavigationAttrValues): every soft-navigation, managed-form,
	// and revalidation behavior in the app depends on this attribute
	// being present, and it comes from the router's own navigationHead
	// field being non-nil, not from App.EnableNavigation directly.
	if !strings.Contains(body, `data-gosx-navigation-state="idle"`) {
		t.Error(`GET /login: no data-gosx-navigation-state="idle" on <html> -- NavigationEnabled() looks false`)
	}

	scriptRequest := httptest.NewRequest(http.MethodGet, scriptHref, nil)
	scriptRecorder := httptest.NewRecorder()
	handler.ServeHTTP(scriptRecorder, scriptRequest)
	if scriptRecorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", scriptHref, scriptRecorder.Code)
	}
	if got := scriptRecorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("GET %s Cache-Control = %q, want the immutable policy", scriptHref, got)
	}
	if got := scriptRecorder.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("GET %s Content-Type = %q, want text/javascript", scriptHref, got)
	}
	runtimeBody := scriptRecorder.Body.String()
	if len(runtimeBody) < 50000 {
		t.Errorf("GET %s body is %d bytes, want the full ~88KB navigation runtime", scriptHref, len(runtimeBody))
	}
	if !strings.Contains(runtimeBody, "gosxHost") {
		t.Errorf("GET %s body does not look like the navigation runtime (no \"gosxHost\")", scriptHref)
	}
}

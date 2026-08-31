package locker

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

// TestLockerPageRendersWithRealData is the one end-to-end smoke test in
// this package permitted to touch league.Default() (activity's
// TestActivityPageRendersWithRealData precedent): league.Default() is a
// process-wide sync.Once singleton, so only the FIRST call in this test
// binary controls its DATA_FILE — a second, differently-configured call
// from a sibling test in this same package would silently reuse the first
// one's store instead. Every other locker test in this package (the
// render-contract assertions in fragment_test.go, the hub tests in
// live_test.go) is deliberately hermetic and never calls it, exactly as
// instructed. It drives a real HTTP GET through the same route.AddDir
// mechanism main.go uses, against this package's own page.gsx/
// page.server.go exactly as they sit on disk.
func TestLockerPageRendersWithRealData(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/locker): AddDir treats it
	// as the route tree's root, so page.gsx here answers "/" — enough to
	// drive one real render without pulling every other page's file
	// modules (and their own env/store needs) into this test.
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (locker page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("locker page rendered the error page instead of the board: %s", body)
	}
	// A fresh league has no posts yet: the honest empty-board state.
	if !strings.Contains(body, "NO POSTS YET") {
		t.Fatalf("expected the honest empty-board state on a fresh league, got: %s", body)
	}
	// Demo mode is read-only (GC-4): the truthful notice renders instead
	// of a live compose form, and posting is never offered to a rehearsal
	// visitor.
	if strings.Contains(body, `name="body"`) {
		t.Fatalf("demo mode rendered a live compose form: %s", body)
	}
	if !strings.Contains(body, "Demo mode is read-only") {
		t.Fatalf("expected the demo read-only notice, got: %s", body)
	}
	if !strings.Contains(body, "LOCKER ROOM") {
		t.Fatalf("expected the retro Locker Room label, got: %s", body)
	}
}

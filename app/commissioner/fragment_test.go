package commissioner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"

	"m31labs.dev/gosx/route"
)

func TestCommissionerFragmentRejectsMethodAndUnauthorizedReaders(t *testing.T) {
	tests := []struct {
		name   string
		method string
		access func(*http.Request) (int, bool)
		want   int
	}{
		{name: "method", method: http.MethodPost, access: func(*http.Request) (int, bool) { return 0, true }, want: http.StatusMethodNotAllowed},
		{name: "anonymous", method: http.MethodGet, access: func(*http.Request) (int, bool) { return http.StatusUnauthorized, false }, want: http.StatusUnauthorized},
		{name: "noncommissioner", method: http.MethodGet, access: func(*http.Request) (int, bool) { return http.StatusForbidden, false }, want: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetches := 0
			handler := fragmentHandler(test.access, func(context.Context) []commissionerhq.FleetEntry {
				fetches++
				return nil
			}, true)
			request := httptest.NewRequest(test.method, "/commissioner/fragment", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if fetches != 0 {
				t.Fatalf("unauthorized request performed %d peer reads", fetches)
			}
			if test.method != http.MethodGet && response.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("Allow = %q", response.Header().Get("Allow"))
			}
		})
	}
}

func TestCommissionerFragmentUsesSharedReadoutAndDegradesWholeFragment(t *testing.T) {
	fixed := time.Date(2026, time.August, 22, 20, 30, 0, 0, time.UTC)
	previousNow := timeNow
	timeNow = func() time.Time { return fixed }
	t.Cleanup(func() { timeNow = previousNow })

	entries := []commissionerhq.FleetEntry{{
		PeerID: "skl", PublicURL: "https://sk.example",
		Error: "https://service.internal Bearer bearer-token operator@example.com",
	}}
	fetches := 0
	handler := fragmentHandler(
		func(*http.Request) (int, bool) { return 0, true },
		func(context.Context) []commissionerhq.FleetEntry {
			fetches++
			return entries
		},
		true,
	)
	request := httptest.NewRequest(http.MethodGet, "/commissioner/fragment", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if fetches != 1 {
		t.Fatalf("peer reads = %d, want 1", fetches)
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("Vary"); got != "Cookie" {
		t.Fatalf("Vary = %q", got)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	etag := response.Header().Get("ETag")
	if etag == "" {
		t.Fatal("fragment omitted ETag")
	}
	unchanged := httptest.NewRecorder()
	unchangedRequest := httptest.NewRequest(http.MethodGet, "/commissioner/fragment", nil)
	unchangedRequest.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, unchangedRequest)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged fragment = %d body %q", unchanged.Code, unchanged.Body.String())
	}

	props := readoutFromView(buildFleetView(entries, fixed), true, true)
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": props},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.String() != expected {
		t.Fatalf("fragment diverged from shared FleetReadout component\nwant: %s\n got: %s", expected, response.Body.String())
	}
	fullPage, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{
		Values: map[string]any{"data": map[string]any{"is_commissioner": true, "fleet": props}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fullPage, expected) {
		t.Fatal("full SSR page does not contain the same FleetReadout markup as the fragment")
	}
	for _, want := range []string{"League unavailable", "UNAVAILABLE", "LEAGUE REPORT"} {
		if !strings.Contains(expected, want) {
			t.Errorf("degraded fragment missing %q: %s", want, expected)
		}
	}
	for _, forbidden := range []string{"service.internal", "bearer-token", "operator@example.com", "<form", "method=\"post\""} {
		if strings.Contains(strings.ToLower(expected), strings.ToLower(forbidden)) {
			t.Errorf("fragment leaked or gained write surface %q: %s", forbidden, expected)
		}
	}
}

func TestCommissionerRegionContractIsSameOriginReadOnlyAndScoped(t *testing.T) {
	pageSource, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageSource)
	for _, want := range []string{
		"data-gosx-region-url=\"/commissioner/fragment\"",
		"data-gosx-region-interval=\"15s\"",
		"data-gosx-region-signal=\"$commissioner.hq.refresh\"",
		"<FleetReadout {...data.fleet}></FleetReadout>",
		"aria-live=\"polite\"",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("commissioner region contract missing %q", want)
		}
	}
	if strings.Count(page, "aria-live=\"polite\"") != 1 {
		t.Fatalf("aria-live region count = %d, want 1", strings.Count(page, "aria-live=\"polite\""))
	}
	for _, forbidden := range []string{"service_url", "service.internal", "bearer", "token", "<form"} {
		if strings.Contains(strings.ToLower(page), forbidden) {
			t.Errorf("commissioner page exposes forbidden material %q", forbidden)
		}
	}

	if !strings.Contains(rootPackageSource(t), "app.Mount(\"GET /commissioner/fragment\", commissionerpage.FragmentHandler(hqService))") {
		t.Fatal("same-origin commissioner fragment GET route is not mounted")
	}
}

// rootPackageSource concatenates every non-test Go file of the root package.
// The mount contract asks where a route is registered, not which file holds
// it, so a later move inside the root package cannot silently pass.
func rootPackageSource(t *testing.T) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var sources strings.Builder
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources.Write(body)
		sources.WriteByte('\n')
	}
	if sources.Len() == 0 {
		t.Fatal("root package sources not found")
	}
	return sources.String()
}

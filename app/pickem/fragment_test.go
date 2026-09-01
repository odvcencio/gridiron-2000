package pickem

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gridiron-2000/internal/league"
)

func TestPickemFragmentURLPreservesWeekState(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/pickem?week=4&ignored=private", want: "/pickem/fragment?week=4"},
		{path: "/pickem?week=04", want: "/pickem/fragment?week=4"},
		{path: "/pickem?week=not-a-week", want: "/pickem/fragment"},
		{path: "/pickem", want: "/pickem/fragment"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if got := pickemFragmentURL(request); got != tt.want {
				t.Fatalf("Pick'em fragment URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPickemFragmentIntervalSlowsOnlyWhenDisplayedSlateIsFinal(t *testing.T) {
	if got := pickemFragmentInterval(map[string]any{"games": []league.PickemGameRow{{Final: false}}}); got != pickemRegionFastInterval {
		t.Fatalf("live Pick'em interval = %q, want %q", got, pickemRegionFastInterval)
	}
	if got := pickemFragmentInterval(map[string]any{"games": []league.PickemGameRow{{Final: true}, {Final: true}}}); got != pickemRegionFinalInterval {
		t.Fatalf("final Pick'em interval = %q, want %q", got, pickemRegionFinalInterval)
	}
	if got := pickemFragmentInterval(map[string]any{"games": []league.PickemGameRow{}}); got != pickemRegionFinalInterval {
		t.Fatalf("empty Pick'em interval = %q, want %q", got, pickemRegionFinalInterval)
	}
}

func TestPickemFragmentContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	pageSource := string(page)
	for _, want := range []string{
		`data-gosx-region-url={data.pickem_fragment_url}`,
		`data-gosx-region-interval={data.pickem_fragment_interval}`,
		`data-gosx-region-signal="$pickem.state.refresh"`,
		`data-gosx-set="$pickem.state.refresh"`,
		`<PickemLiveRegion></PickemLiveRegion>`,
		`data-gosx-action-signal="$pickem.state.refresh"`,
		`func PickemLiveRegion() Node`,
	} {
		if !strings.Contains(pageSource, want) {
			t.Errorf("Pick'em page missing synchronization contract %q", want)
		}
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"EnableBootstrap",
		"PickemDataReadOnly",
		"pickemFragmentURL",
		"pickemRegionFinalInterval",
	} {
		if !strings.Contains(string(server), want) {
			t.Errorf("Pick'em server missing synchronization contract %q", want)
		}
	}
	// Wave-1 stale-state fix: a successful pick must redirect like every
	// other GoSX-managed action (team-rename, notification-set), not answer
	// a bare {ok:true, data:{value:"refresh"}} the managed-form runtime
	// never reads. See pickemSetAction.
	if strings.Contains(string(server), `map[string]any{"value": "refresh"}`) {
		t.Error("Pick'em server still answers a managed pick with a dead refresh signal instead of a redirect")
	}
	fragment, err := os.ReadFile("fragment.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`Cache-Control", "private, no-store"`, `Vary", "Cookie"`, `If-None-Match`, `PickemDataReadOnly`} {
		if !strings.Contains(string(fragment), want) {
			t.Errorf("Pick'em fragment missing %q", want)
		}
	}
}

func TestPickemFragmentETagAndConvergence(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	handler := pickemFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(request *http.Request) map[string]any {
			return map[string]any{"version": version.Load(), "query": request.URL.RawQuery}
		},
		func(data map[string]any, _ *http.Request) (string, error) {
			return fmt.Sprintf(`<section data-pickem-version="%v" data-query="%v">slate</section>`, data["version"], data["query"]), nil
		},
	)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/pickem/fragment?week=4", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `data-pickem-version="1"`) || !strings.Contains(first.Body.String(), "week=4") {
		t.Fatalf("first Pick'em fragment = %d %q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-store" || first.Header().Get("Vary") != "Cookie" {
		t.Fatalf("privacy/etag headers = %#v", first.Header())
	}
	unchanged := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/pickem/fragment?week=4", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, request)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged Pick'em fragment = %d body %q", unchanged.Code, unchanged.Body.String())
	}
	version.Store(2)
	converged := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/pickem/fragment?week=4", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(converged, request)
	if converged.Code != http.StatusOK || !strings.Contains(converged.Body.String(), `data-pickem-version="2"`) {
		t.Fatalf("converged Pick'em fragment = %d %q", converged.Code, converged.Body.String())
	}
}

func TestPickemFragmentRejectsMutationAndUnauthorized(t *testing.T) {
	var rendered atomic.Int64
	handler := pickemFragmentHandlerWithRenderer(
		func(*http.Request) bool { return false },
		func(*http.Request) map[string]any { return map[string]any{} },
		func(map[string]any, *http.Request) (string, error) {
			rendered.Add(1)
			return "<section>pick'em</section>", nil
		},
	)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/pickem/fragment", nil))
	if unauthorized.Code != http.StatusUnauthorized || rendered.Load() != 0 {
		t.Fatalf("unauthorized Pick'em fragment = %d renders=%d", unauthorized.Code, rendered.Load())
	}
	post := httptest.NewRecorder()
	allowed := pickemFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{} },
		func(map[string]any, *http.Request) (string, error) { return "<section>pick'em</section>", nil },
	)
	allowed.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/pickem/fragment", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST Pick'em fragment = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}
}

func TestPickemFragmentConcurrentReads(t *testing.T) {
	handler := pickemFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{} },
		func(map[string]any, *http.Request) (string, error) { return "<section>stable Pick'em</section>", nil },
	)
	const readers = 24
	var wait sync.WaitGroup
	wait.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/pickem/fragment?week=1", nil))
			if response.Code != http.StatusOK || response.Body.String() != "<section>stable Pick'em</section>" {
				t.Errorf("concurrent Pick'em fragment = %d %q", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()
}

package activity

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

func TestActivityFragmentURLPreservesBrowseState(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/activity?team=AAA&q=drop+result&page=4&ignored=private", nil)
	if got, want := activityFragmentURL(request), "/activity/fragment?page=4&q=drop+result&team=AAA"; got != want {
		t.Fatalf("Activity fragment URL = %q, want %q", got, want)
	}
}

func TestActivityFragmentContract(t *testing.T) {
	if activityRegionInterval != league.PollPeriod.String() {
		t.Fatalf("Activity region interval = %q, league poll period = %q", activityRegionInterval, league.PollPeriod)
	}
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	pageSource := string(page)
	for _, want := range []string{
		`data-gosx-region-url={data.activity_fragment_url}`,
		`data-gosx-region-interval={data.activity_fragment_interval}`,
		`data-gosx-region-signal="$players.state.refresh"`,
		`func ActivityRegion() Node`,
		`name="team"`, `name="q"`, `name="page"`,
	} {
		if !strings.Contains(pageSource, want) {
			t.Errorf("Activity page missing synchronization contract %q", want)
		}
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EnableBootstrap", "ActivityDataReadOnly", "activityFragmentURL"} {
		if !strings.Contains(string(server), want) {
			t.Errorf("Activity server missing synchronization contract %q", want)
		}
	}
}

func TestActivityFragmentETagAndConvergence(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	handler := activityFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(request *http.Request) map[string]any {
			return map[string]any{"version": version.Load(), "query": request.URL.RawQuery, "transactions": []map[string]any{}}
		},
		func(data map[string]any) (string, error) {
			return fmt.Sprintf(`<section data-activity-version="%v" data-query="%v">feed</section>`, data["version"], data["query"]), nil
		},
	)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/activity/fragment?team=AAA&q=move&page=3", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `data-activity-version="1"`) || !strings.Contains(first.Body.String(), `team=AAA&q=move&page=3`) {
		t.Fatalf("first Activity fragment = %d %q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-store" || first.Header().Get("Vary") != "Cookie" {
		t.Fatalf("privacy/etag headers = %#v", first.Header())
	}
	unchanged := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/activity/fragment?team=AAA&q=move&page=3", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, request)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged Activity fragment = %d %q", unchanged.Code, unchanged.Body.String())
	}
	version.Store(2)
	converged := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/activity/fragment?team=AAA&q=move&page=3", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(converged, request)
	if converged.Code != http.StatusOK || !strings.Contains(converged.Body.String(), `data-activity-version="2"`) {
		t.Fatalf("converged Activity fragment = %d %q", converged.Code, converged.Body.String())
	}
}

func TestActivityFragmentConcurrentReads(t *testing.T) {
	handler := activityFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"transactions": []map[string]any{}} },
		func(map[string]any) (string, error) { return "<section>stable Activity</section>", nil },
	)
	const readers = 24
	var wait sync.WaitGroup
	wait.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/activity/fragment", nil))
			if response.Code != http.StatusOK || response.Body.String() != "<section>stable Activity</section>" {
				t.Errorf("concurrent Activity fragment = %d %q", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()
}

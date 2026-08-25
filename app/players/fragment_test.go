package players

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gridiron-2000/internal/league"
)

func TestPlayersFragmentURLPreservesBrowseState(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/players?pos=RB&q=wide+receiver&page=3&ignored=drop", nil)
	if got, want := playersFragmentURL(request, "pool"), "/players/fragment/pool?page=3&pos=RB&q=wide+receiver"; got != want {
		t.Fatalf("pool fragment URL = %q, want %q", got, want)
	}
	if got, want := playersFragmentURL(&http.Request{URL: &url.URL{}}, "waivers"), "/players/fragment/waivers"; got != want {
		t.Fatalf("empty waiver fragment URL = %q, want %q", got, want)
	}
}

func TestPlayersFragmentContract(t *testing.T) {
	if playersRegionInterval != league.PollPeriod.String() {
		t.Fatalf("players region interval = %q, league poll period = %q", playersRegionInterval, league.PollPeriod)
	}
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	pageSource := string(page)
	for _, want := range []string{
		`data-gosx-region-url={data.pool_fragment_url}`,
		`data-gosx-region-url={data.waiver_fragment_url}`,
		`data-gosx-region-interval={data.pool_fragment_interval}`,
		`data-gosx-region-signal="$players.state.refresh"`,
		`func PlayerPoolRegion() Node`,
		`func WaiverDeskRegion() Node`,
		`data-gosx-action-signal="$players.state.refresh"`,
		`name="pos"`, `name="q"`, `name="page"`,
	} {
		if !strings.Contains(pageSource, want) {
			t.Errorf("players page missing synchronization contract %q", want)
		}
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EnableBootstrap", "action.WantsJSON", `map[string]any{"value": "refresh"}`} {
		if !strings.Contains(string(server), want) {
			t.Errorf("players server missing synchronization contract %q", want)
		}
	}
	fragment, err := os.ReadFile("fragment.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fragment), "PlayersDataReadOnly") {
		t.Fatal("players fragment does not use the read-only service projection")
	}
}

func TestPlayersFragmentETagPrivacyAndConvergence(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	handler := playersFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(request *http.Request) map[string]any {
			return map[string]any{"version": version.Load(), "query": request.URL.RawQuery}
		},
		func(data map[string]any, _ *http.Request) (string, error) {
			return fmt.Sprintf(`<section data-pool-version="%v" data-query="%v">pool</section>`, data["version"], data["query"]), nil
		},
	)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/players/fragment/pool?pos=RB&q=wide&page=2", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `data-pool-version="1"`) || !strings.Contains(first.Body.String(), `pos=RB&q=wide&page=2`) {
		t.Fatalf("first fragment = %d %q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-store" || first.Header().Get("Vary") != "Cookie" {
		t.Fatalf("privacy/etag headers = %#v", first.Header())
	}
	unchanged := httptest.NewRecorder()
	unchangedRequest := httptest.NewRequest(http.MethodGet, "/players/fragment/pool?pos=RB&q=wide&page=2", nil)
	unchangedRequest.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, unchangedRequest)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged fragment = %d %q", unchanged.Code, unchanged.Body.String())
	}
	version.Store(2)
	converged := httptest.NewRecorder()
	convergedRequest := httptest.NewRequest(http.MethodGet, "/players/fragment/pool?pos=RB&q=wide&page=2", nil)
	convergedRequest.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(converged, convergedRequest)
	if converged.Code != http.StatusOK || !strings.Contains(converged.Body.String(), `data-pool-version="2"`) || converged.Header().Get("ETag") == etag {
		t.Fatalf("converged fragment = %d etag=%q body=%q", converged.Code, converged.Header().Get("ETag"), converged.Body.String())
	}
}

func TestPlayersFragmentRejectsMutationAndUnauthorized(t *testing.T) {
	var rendered atomic.Int64
	handler := playersFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"ok": true} },
		func(map[string]any, *http.Request) (string, error) {
			rendered.Add(1)
			return "<section>pool</section>", nil
		},
	)
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/players/fragment/pool", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet || rendered.Load() != 0 {
		t.Fatalf("POST = %d allow=%q rendered=%d", post.Code, post.Header().Get("Allow"), rendered.Load())
	}
	unauthorized := playersFragmentHandlerWithRenderer(
		func(*http.Request) bool { return false },
		func(*http.Request) map[string]any { t.Fatal("unauthorized request reached loader"); return nil },
		func(map[string]any, *http.Request) (string, error) {
			t.Fatal("unauthorized request reached renderer")
			return "", nil
		},
	)
	response := httptest.NewRecorder()
	unauthorized.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d, want 401", response.Code)
	}
}

func TestPlayersFragmentConcurrentReads(t *testing.T) {
	handler := playersFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"ok": true} },
		func(map[string]any, *http.Request) (string, error) { return "<section>stable pool</section>", nil },
	)
	const readers = 24
	var wait sync.WaitGroup
	wait.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/players/fragment/pool", nil))
			if response.Code != http.StatusOK || response.Body.String() != "<section>stable pool</section>" {
				t.Errorf("concurrent fragment = %d %q", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()
}

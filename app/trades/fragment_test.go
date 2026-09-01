package trades

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

func TestTradeFragmentURLPreservesCounterparty(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/trades?counterparty=team-2&ignored=private", nil)
	if got, want := tradeFragmentURL(request), "/trades/fragment?counterparty=team-2"; got != want {
		t.Fatalf("Trade fragment URL = %q, want %q", got, want)
	}
	if got, want := tradeRedirectTarget("team-2"), "/trades?counterparty=team-2"; got != want {
		t.Fatalf("Trade redirect target = %q, want %q", got, want)
	}
	if got, want := tradeRedirectTarget(""), "/trades"; got != want {
		t.Fatalf("empty Trade redirect target = %q, want %q", got, want)
	}
}

func TestTradeFragmentContract(t *testing.T) {
	if tradesRegionInterval != league.PollPeriod.String() {
		t.Fatalf("Trade region interval = %q, league poll period = %q", tradesRegionInterval, league.PollPeriod)
	}
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	pageSource := string(page)
	for _, want := range []string{
		`data-gosx-region-url={data.trades_fragment_url}`,
		`data-gosx-region-interval={data.trades_fragment_interval}`,
		`data-gosx-region-signal="$trades.state.refresh"`,
		`data-gosx-set="$trades.state.refresh"`,
		`func TradeDeskRegion() Node`,
		`data-gosx-action-signal="$trades.state.refresh"`,
		`actionPath("trade-propose")`,
		`actionPath("trade-counter")`,
		`id="inbox"`, `id="outbox"`, `id="review"`, `id="history"`,
	} {
		if !strings.Contains(pageSource, want) {
			t.Errorf("Trade page missing synchronization contract %q", want)
		}
	}
	if strings.Contains(pageSource, "data-gosx-revalidate-src") || strings.Contains(pageSource, "data-gosx-revalidate-interval") {
		t.Fatal("Trade page still has document-wide revalidation; state belongs to the Trade Desk region")
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	// Wave-1 stale-state fix: a managed mutation must redirect like every
	// other GoSX-managed action (team-rename, notification-set), not answer
	// a bare {ok:true, data:{value:"refresh"}} the managed-form runtime
	// never reads. See tradeMutationSuccess.
	for _, want := range []string{"EnableBootstrap", "tradeFragmentURL", "tradeMutationSuccess(ctx,"} {
		if !strings.Contains(string(server), want) {
			t.Errorf("Trade server missing synchronization contract %q", want)
		}
	}
	if strings.Contains(string(server), `map[string]any{"value": "refresh"}`) {
		t.Error("Trade server still answers a managed mutation with a dead refresh signal instead of a redirect")
	}
	fragment, err := os.ReadFile("fragment.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`Cache-Control", "private, no-store"`, `Vary", "Cookie"`, "If-None-Match", "TradesDataReadOnly"} {
		if !strings.Contains(string(fragment), want) {
			t.Errorf("Trade fragment handler missing %q", want)
		}
	}
}

func TestTradeFragmentETagPrivacyAndConvergence(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	handler := tradesFragmentHandler(
		func(*http.Request) bool { return true },
		func(request *http.Request) map[string]any {
			return map[string]any{"version": version.Load(), "query": request.URL.RawQuery}
		},
		func(data map[string]any, _ *http.Request) (string, error) {
			return fmt.Sprintf(`<div data-trade-version="%v" data-query="%v">desk</div>`, data["version"], data["query"]), nil
		},
	)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/trades/fragment?counterparty=team-2", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `data-trade-version="1"`) || !strings.Contains(first.Body.String(), "counterparty=team-2") {
		t.Fatalf("first Trade fragment = %d %q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-store" || first.Header().Get("Vary") != "Cookie" {
		t.Fatalf("Trade privacy/etag headers = %#v", first.Header())
	}
	unchanged := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/trades/fragment?counterparty=team-2", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, request)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged Trade fragment = %d %q", unchanged.Code, unchanged.Body.String())
	}
	version.Store(2)
	converged := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/trades/fragment?counterparty=team-2", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(converged, request)
	if converged.Code != http.StatusOK || !strings.Contains(converged.Body.String(), `data-trade-version="2"`) || converged.Header().Get("ETag") == etag {
		t.Fatalf("converged Trade fragment = %d etag=%q body=%q", converged.Code, converged.Header().Get("ETag"), converged.Body.String())
	}
}

func TestTradeFragmentRejectsMutationAndUnauthorized(t *testing.T) {
	var rendered atomic.Int64
	handler := tradesFragmentHandler(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"ok": true} },
		func(map[string]any, *http.Request) (string, error) {
			rendered.Add(1)
			return "<div>desk</div>", nil
		},
	)
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/trades/fragment", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet || rendered.Load() != 0 {
		t.Fatalf("POST = %d allow=%q rendered=%d", post.Code, post.Header().Get("Allow"), rendered.Load())
	}
	unauthorized := tradesFragmentHandler(
		func(*http.Request) bool { return false },
		func(*http.Request) map[string]any { t.Fatal("unauthorized request reached loader"); return nil },
		func(map[string]any, *http.Request) (string, error) {
			t.Fatal("unauthorized request reached renderer")
			return "", nil
		},
	)
	response := httptest.NewRecorder()
	unauthorized.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/trades/fragment", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d, want 401", response.Code)
	}
}

func TestTradeFragmentConcurrentReads(t *testing.T) {
	handler := tradesFragmentHandler(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"ok": true} },
		func(map[string]any, *http.Request) (string, error) { return "<div>stable desk</div>", nil },
	)
	const readers = 24
	var wait sync.WaitGroup
	wait.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/trades/fragment", nil))
			if response.Code != http.StatusOK || response.Body.String() != "<div>stable desk</div>" {
				t.Errorf("concurrent Trade fragment = %d %q", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()
}

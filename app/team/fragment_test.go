package team

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

func TestTeamLineupFragmentContract(t *testing.T) {
	if teamLineupFragmentInterval != league.PollPeriod.String() {
		t.Fatalf("Team region interval = %q, league poll period = %q", teamLineupFragmentInterval, league.PollPeriod)
	}
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	pageSource := string(page)
	for _, want := range []string{
		`data-gosx-region-url={data.lineup_fragment_url}`,
		`data-gosx-region-interval={data.lineup_fragment_interval}`,
		`data-gosx-region-signal="$team.lineup.refresh"`,
		`data-gosx-set="$team.lineup.refresh"`,
		`<TeamLineupRegion></TeamLineupRegion>`,
		`method="post" action={actionPath("lineup-set")} data-gosx-managed="true"`,
	} {
		if !strings.Contains(pageSource, want) {
			t.Errorf("Team lineup region missing %q", want)
		}
	}

	fragment, err := os.ReadFile("fragment.go")
	if err != nil {
		t.Fatal(err)
	}
	fragmentSource := string(fragment)
	for _, want := range []string{
		`Cache-Control", "private, no-store"`,
		`Vary", "Cookie"`,
		`If-None-Match`,
		`TeamDataReadOnly`,
	} {
		if !strings.Contains(fragmentSource, want) {
			t.Errorf("fragment handler missing %q", want)
		}
	}
}

func TestTeamLineupFragmentETagAndReadOnlyRecovery(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	load := func(*http.Request) map[string]any {
		return map[string]any{
			"has_seat": true,
			"version":  version.Load(),
		}
	}
	render := func(data map[string]any, _ *http.Request) (string, error) {
		return fmt.Sprintf(`<div data-lineup-version="%d">authoritative lineup</div>`, data["version"]), nil
	}
	handler := teamLineupFragmentHandler(func(*http.Request) bool { return true }, load, render)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/team/fragment?week=1", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `data-lineup-version="1"`) {
		t.Fatalf("first fragment = %d %q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-store" || first.Header().Get("Vary") != "Cookie" {
		t.Fatalf("privacy/etag headers = %#v", first.Header())
	}

	unchanged := httptest.NewRecorder()
	unchangedRequest := httptest.NewRequest(http.MethodGet, "/team/fragment?week=1", nil)
	unchangedRequest.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, unchangedRequest)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged fragment = %d body %q", unchanged.Code, unchanged.Body.String())
	}

	version.Store(2)
	converged := httptest.NewRecorder()
	convergedRequest := httptest.NewRequest(http.MethodGet, "/team/fragment?week=1", nil)
	convergedRequest.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(converged, convergedRequest)
	if converged.Code != http.StatusOK || !strings.Contains(converged.Body.String(), `data-lineup-version="2"`) {
		t.Fatalf("converged fragment = %d %q", converged.Code, converged.Body.String())
	}
	if converged.Header().Get("ETag") == etag {
		t.Fatal("authoritative mutation retained the old ETag")
	}
}

func TestTeamLineupFragmentRejectsMutationAndSeatlessRender(t *testing.T) {
	var rendered atomic.Int64
	render := func(map[string]any, *http.Request) (string, error) {
		rendered.Add(1)
		return "<div>lineup</div>", nil
	}
	load := func(*http.Request) map[string]any { return map[string]any{"has_seat": false} }
	handler := teamLineupFragmentHandler(func(*http.Request) bool { return true }, load, render)
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/team/fragment", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}
	seatless := httptest.NewRecorder()
	handler.ServeHTTP(seatless, httptest.NewRequest(http.MethodGet, "/team/fragment", nil))
	if seatless.Code != http.StatusForbidden || rendered.Load() != 0 {
		t.Fatalf("seatless fragment = %d renders=%d", seatless.Code, rendered.Load())
	}
}

func TestTeamLineupFragmentConcurrentReads(t *testing.T) {
	handler := teamLineupFragmentHandler(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"has_seat": true} },
		func(map[string]any, *http.Request) (string, error) { return "<div>stable lineup</div>", nil },
	)
	const readers = 24
	var wait sync.WaitGroup
	wait.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/team/fragment", nil))
			if response.Code != http.StatusOK || response.Body.String() != "<div>stable lineup</div>" {
				t.Errorf("concurrent fragment = %d %q", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()
}

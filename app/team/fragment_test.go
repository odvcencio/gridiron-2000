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

// TestTeamLineupFragmentURLPreservesRequestedWeekForNotice covers wave-6
// audit item 5 (second pass): data["week"] is already clamped by the
// service (teamWeekOptions), so a naive fragment URL keyed off it alone
// silently drops the raw, out-of-range week a manager actually asked
// for — and with it, the notice explaining the clamp. The fragment URL
// must instead carry the raw requested week so a later poll re-derives
// the same notice.
func TestTeamLineupFragmentURLPreservesRequestedWeekForNotice(t *testing.T) {
	data := map[string]any{"week": "1"}
	request := httptest.NewRequest(http.MethodGet, "/team?week=99", nil)
	if got, want := teamLineupFragmentURL(data, request), "/team/fragment?week=99"; got != want {
		t.Errorf("teamLineupFragmentURL(week=99 request, clamped data) = %q, want %q", got, want)
	}

	// A request with no week param at all still falls back to data["week"]
	// (an ordinary /team load, current week, no clamp, no notice to carry).
	bare := httptest.NewRequest(http.MethodGet, "/team", nil)
	if got, want := teamLineupFragmentURL(data, bare), "/team/fragment?week=1"; got != want {
		t.Errorf("teamLineupFragmentURL(no week param) = %q, want %q", got, want)
	}
}

// TestTeamLineupFragmentEndpointRendersOutOfRangeWeekNotice is item 5's
// own fragment-endpoint test: /team/fragment?week=99 (the URL
// teamLineupFragmentURL now produces for a /team?week=99 load) must
// render the same out-of-range notice the initial page shows, on every
// poll — not just the first render — since the region's own 4s
// revalidation re-fetches this exact URL indefinitely.
func TestTeamLineupFragmentEndpointRendersOutOfRangeWeekNotice(t *testing.T) {
	service := league.Default()
	handler := TeamLineupFragmentHandler(service)

	initial := service.TeamData(httptest.NewRequest(http.MethodGet, "/team?week=99", nil))
	initialNotice, _ := initial["week_notice"].(string)
	if initialNotice == "" || !strings.Contains(initialNotice, "is not on the published schedule") {
		t.Fatalf("initial /team?week=99 load carried no out-of-range notice: %#v", initial["week_notice"])
	}
	fragmentURL, _ := prepareTeamData(initial, httptest.NewRequest(http.MethodGet, "/team?week=99", nil))["lineup_fragment_url"].(string)
	if fragmentURL != "/team/fragment?week=99" {
		t.Fatalf("initial page's own lineup_fragment_url = %q, want /team/fragment?week=99", fragmentURL)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, fragmentURL, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("fragment %s = %d %q", fragmentURL, response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), initialNotice) {
		t.Errorf("fragment %s dropped the out-of-range notice %q the initial page showed: %s", fragmentURL, initialNotice, response.Body.String())
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

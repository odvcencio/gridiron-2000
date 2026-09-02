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
	"m31labs.dev/gosx/route"
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

// TestActivityRowsCarryCommissionerActorClass checks activityRows' map-to-
// struct conversion preserves the wave-2 commissioner-console attribution
// fields (time_relative, actor_class) that ActivityData's merged feed now
// emits for a CommissionerEvent row (internal/league's activityMaps).
func TestActivityRowsCarryCommissionerActorClass(t *testing.T) {
	rows := activityRows([]map[string]any{
		{"time": "Sep 1, 12:00 PM EDT", "time_iso": "2026-09-01T16:00:00Z", "time_relative": "1 hour ago", "team": "Alex", "action": "posted an announcement", "player": "", "actor_class": "COMMISSIONER"},
		{"time": "Sep 1, 11:00 AM EDT", "time_iso": "2026-09-01T15:00:00Z", "time_relative": "2 hours ago", "team": "Eastside Elite (E1)", "action": "signs", "player": "Tre Harris (WR)", "actor_class": ""},
	})
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].ActorClass != "COMMISSIONER" || rows[0].Team != "Alex" || rows[0].TimeRelative != "1 hour ago" || rows[0].TimeISO != "2026-09-01T16:00:00Z" {
		t.Fatalf("commissioner row = %+v", rows[0])
	}
	if rows[1].ActorClass != "" || rows[1].Team != "Eastside Elite (E1)" || rows[1].TimeISO != "2026-09-01T15:00:00Z" {
		t.Fatalf("team-move row = %+v", rows[1])
	}
}

// TestActivityRegionRendersDatetimeAttributeForEveryRow is the wave-2
// audit fix (finding 1): every /activity row's <time> element must carry
// a real datetime attribute (never an empty one) plus the relative label,
// for both an ordinary team move and a commissioner-actor-class row.
func TestActivityRegionRendersDatetimeAttributeForEveryRow(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"teams": []string{}, "team": "", "query": "", "has_filters": false,
		"filtered_count": 2, "transactions_count": 2, "page": 1, "pages": 1,
		"page_start": 1, "page_end": 2, "has_previous": false, "has_next": false,
		"has_transactions": true, "transactions_empty": false,
		"transactions": activityRows([]map[string]any{
			{"time": "Sep 1, 12:00 PM EDT", "time_iso": "2026-09-01T16:00:00Z", "time_relative": "1 hour ago", "team": "Alex", "action": "posted an announcement", "player": "", "actor_class": "COMMISSIONER"},
			{"time": "Sep 1, 11:00 AM EDT", "time_iso": "2026-09-01T15:00:00Z", "time_relative": "2 hours ago", "team": "Eastside Elite (E1)", "action": "signs", "player": "Tre Harris (WR)", "actor_class": ""},
		}),
	}
	html, err := route.RenderProgramComponent(program, "ActivityRegion", route.ProgramRenderEnv{
		Values: map[string]any{"data": data},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `datetime="2026-09-01T16:00:00Z"`) || !strings.Contains(html, `datetime="2026-09-01T15:00:00Z"`) {
		t.Fatalf("rendered activity feed is missing a real datetime attribute: %s", html)
	}
	if strings.Contains(html, `datetime="">`) {
		t.Fatalf("rendered activity feed emitted an empty datetime attribute: %s", html)
	}
	if !strings.Contains(html, "1 hour ago") || !strings.Contains(html, "2 hours ago") {
		t.Fatalf("rendered activity feed is missing a relative-time label: %s", html)
	}
	if !strings.Contains(html, "COMMISSIONER") {
		t.Fatalf("rendered activity feed is missing the commissioner actor class: %s", html)
	}
}

// TestActivityRegionTokensCarryGapClassAcrossWhitespaceFreeJunctions pins
// wave-2-verification item 9: ActivityRegion() — the component that
// replaces the live DOM on every poll via data-gosx-region-url, unlike
// Page()'s own once-per-load SSR markup — is hand-written with zero
// whitespace between </strong>{move.Action} and {move.Action}<b>, so a
// commissioner-actor-class row rendered "COMMISSIONER ·
// Commissionerdeleted an announcement" and a team-move row rendered
// "Hot Path (W4)draftsBills D/ST (DST)" on every single re-render, not
// only after a morph strips an already-present space. This first proves
// those exact junctions are still bare of any literal whitespace
// character in ActivityRegion()'s output (so the test would fail if the
// CSS fix were the only thing holding the row together and someone
// reverted it), then asserts the token elements bordering each junction
// carry activity-token-gap, the class page_ui_contract's styles.css
// check pins to margin-inline: 0.3ch.
func TestActivityRegionTokensCarryGapClassAcrossWhitespaceFreeJunctions(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"teams": []string{}, "team": "", "query": "", "has_filters": false,
		"filtered_count": 2, "transactions_count": 2, "page": 1, "pages": 1,
		"page_start": 1, "page_end": 2, "has_previous": false, "has_next": false,
		"has_transactions": true, "transactions_empty": false,
		"transactions": activityRows([]map[string]any{
			{"time": "Sep 1, 8:08 PM EDT", "time_iso": "2026-09-01T00:08:00Z", "time_relative": "3 minutes ago", "team": "Commissioner", "action": "deleted an announcement", "player": "", "actor_class": "COMMISSIONER"},
			{"time": "Sep 1, 7:00 PM EDT", "time_iso": "2026-08-31T23:00:00Z", "time_relative": "1 hour ago", "team": "Hot Path (W4)", "action": "drafts", "player": "Bills D/ST (DST)", "actor_class": ""},
		}),
	}
	html, err := route.RenderProgramComponent(program, "ActivityRegion", route.ProgramRenderEnv{
		Values: map[string]any{"data": data},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The junctions this fix targets carry no literal space character at
	// all in this component's own markup (it is hand-minified, unlike
	// Page()'s SSR version) — reproducing "Commissionerdeleted an
	// announcement" and "Hot Path (W4)draftsBills D/ST (DST)" exactly.
	for _, junction := range []string{
		"</strong>deleted an announcement",
		"</strong>drafts<b",
	} {
		if !strings.Contains(html, junction) {
			t.Fatalf("fixture no longer reproduces the whitespace-free junction this fix targets: want %q in %s", junction, html)
		}
	}

	for _, want := range []string{
		`<span class="activity-actor-class mono activity-token-gap">COMMISSIONER</span>`,
		`<strong class="activity-token-gap">Commissioner</strong>deleted an announcement`,
		`<strong class="activity-token-gap">Hot Path (W4)</strong>drafts<b class="activity-token-gap">Bills D/ST (DST)</b>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered activity row token is missing its CSS-supplied gap class: want %q in %s", want, html)
		}
	}
}

// TestActivityPageRendersCommissionerActorClassMarkup checks page.gsx
// carries the distinct actor-class markup ahead of an actor's name — the
// literal "COMMISSIONER · <name> <summary>" shape a commissioner event
// row must render as (wave-2 commissioner-console audit).
func TestActivityPageRendersCommissionerActorClassMarkup(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`data-actor-class={move.ActorClass}`,
		`<If cond={move.ActorClass != ""}>`,
		`class="activity-actor-class mono activity-token-gap"`,
		`{move.ActorClass}</span> ·`,
		`move.TimeRelative`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Activity page missing commissioner actor-class markup %q", want)
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

package activity

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

// activityTextContent approximates a DOM node's textContent from rendered
// HTML: it drops every tag, the same way a browser, a screen reader, or
// find-in-page collapses markup away and reads only what is left. A real
// space character survives this exactly when it survives the browser's
// own textContent — a CSS-only gap (margin, padding) does not.
var activityTagPattern = regexp.MustCompile(`<[^>]*>`)

func activityTextContent(html string) string {
	return activityTagPattern.ReplaceAllString(html, "")
}

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

// TestActivityRegionVerbJunctionsCarryARealSpace pins wave-6 item 1:
// ActivityRegion() — the component that replaces the live DOM on every
// poll via data-gosx-region-url — used to butt </strong> straight against
// {move.Action} and {move.Action} straight against <b> with zero
// whitespace, so a commissioner-actor-class row's textContent read
// "Commissionerdeleted an announcement" and a team-move row's read "Hot
// Path (W4)draftsBills D/ST (DST)". The previous fix (wave-2-verification
// item 9) left those junctions bare and relied on .activity-token-gap's
// margin-inline for the visible gap alone, which does not help
// textContent, a screen reader, find-in-page, or copy-paste. The fix
// wraps move.Action in a <span class="activity-verb"> that carries the
// space itself as a real character, so the gap survives every render
// path, not only the CSS-painted one. This asserts the space is a literal
// character in the rendered HTML, not just a class name.
func TestActivityRegionVerbJunctionsCarryARealSpace(t *testing.T) {
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

	for _, junction := range []string{
		"</strong>deleted an announcement",
		"</strong>drafts<b",
	} {
		if strings.Contains(html, junction) {
			t.Fatalf("rendered activity row still concatenates Team and Action with no space: found %q in %s", junction, html)
		}
	}

	for _, want := range []string{
		`<strong class="activity-token-gap">Commissioner</strong><span class="activity-verb"> deleted an announcement</span>`,
		`<strong class="activity-token-gap">Hot Path (W4)</strong><span class="activity-verb"> drafts </span><b class="activity-token-gap">Bills D/ST (DST)</b>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered activity row is missing the real-space verb wrapper: want %q in %s", want, html)
		}
	}

	if text := activityTextContent(html); !strings.Contains(text, "Commissioner deleted an announcement") {
		t.Errorf("rendered activity row's own textContent does not read as a real sentence: %q", text)
	}
}

// TestActivityFeedTextReadsAsARealSentence pins wave-6 item 1's literal
// acceptance test: both Page() (once-per-load SSR) and ActivityRegion()
// (the 4s poll fragment that replaces the live DOM) must render an actor
// row so its own rendered text contains "Commissioner posted" — a real
// space character between the actor and the verb — not the concatenated
// "Commissionerposted" a screen reader, find-in-page, and copy-paste all
// read before this fix.
func TestActivityFeedTextReadsAsARealSentence(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	row := activityRows([]map[string]any{
		{"time": "Sep 1, 4:41 PM EDT", "time_iso": "2026-09-01T20:41:00Z", "time_relative": "just now", "team": "Commissioner", "action": "posted an announcement", "player": "", "actor_class": "COMMISSIONER"},
	})
	fragmentData := map[string]any{
		"teams": []string{}, "team": "", "query": "", "has_filters": false,
		"filtered_count": 1, "transactions_count": 1, "page": 1, "pages": 1,
		"page_start": 1, "page_end": 1, "has_previous": false, "has_next": false,
		"has_transactions": true, "transactions_empty": false,
		"transactions": row,
	}
	pageData := map[string]any{
		"timezone": "EDT", "activity_fragment_url": "/activity/fragment", "activity_fragment_interval": "4s",
		"playoff_truth": map[string]any{"headline": "", "status_label": "", "detail": "", "recovery": ""},
	}
	for key, value := range fragmentData {
		pageData[key] = value
	}
	for component, data := range map[string]map[string]any{"Page": pageData, "ActivityRegion": fragmentData} {
		html, err := route.RenderProgramComponent(program, component, route.ProgramRenderEnv{
			Values: map[string]any{"data": data},
		})
		if err != nil {
			t.Fatalf("%s: %v", component, err)
		}
		text := activityTextContent(html)
		if !strings.Contains(text, "Commissioner posted") {
			t.Errorf("%s rendered textContent does not contain \"Commissioner posted\": %q", component, text)
		}
		if strings.Contains(text, "Commissionerposted") {
			t.Errorf("%s rendered textContent concatenates the actor and verb with no space: %q", component, text)
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

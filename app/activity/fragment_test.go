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

// TestActivityRowsCarryTeamNameAndCode is F21's failing-test-first
// reproduction (gap-audit J6): activityRows must carry the new split
// team_name/team_code/has_team_code fields internal/league's activityMaps
// now emits, so the template can lead with the name and demote the code
// to a secondary chip instead of repeating "(CODE)" inline on every row.
func TestActivityRowsCarryTeamNameAndCode(t *testing.T) {
	rows := activityRows([]map[string]any{
		{"time": "Sep 1, 11:00 AM EDT", "team": "Eastside Elite (E1)", "team_name": "Eastside Elite", "team_code": "E1", "has_team_code": true, "action": "signs", "player": "Tre Harris (WR)", "actor_class": ""},
		{"time": "Sep 1, 12:00 PM EDT", "team": "Alex", "team_name": "Alex", "team_code": "", "has_team_code": false, "action": "posted an announcement", "player": "", "actor_class": "COMMISSIONER"},
	})
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if !rows[0].HasTeamCode || rows[0].TeamName != "Eastside Elite" || rows[0].TeamCode != "E1" {
		t.Fatalf("team-move row = %+v, want split name/code", rows[0])
	}
	if rows[1].HasTeamCode || rows[1].TeamName != "Alex" || rows[1].TeamCode != "" {
		t.Fatalf("commissioner row = %+v, want no code chip", rows[1])
	}
}

// TestActivityRegionLeadsWithTeamNameAndRendersCodeAsAChip is F21's render
// contract: an ordinary row with a team code renders the name first, then
// a distinct ".activity-team-code" chip for the code — never the old bare
// parenthetical repeated inline with the name. A row with no code (a
// commissioner event, or a draft pick's own provenance label) renders the
// combined string exactly as before, with no chip at all.
func TestActivityRegionLeadsWithTeamNameAndRendersCodeAsAChip(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	rows := activityRows([]map[string]any{
		{"time": "Sep 1, 11:00 AM EDT", "time_iso": "2026-09-01T15:00:00Z", "time_relative": "2 hours ago", "team": "Eastside Elite (E1)", "team_name": "Eastside Elite", "team_code": "E1", "has_team_code": true, "action": "signs", "player": "Tre Harris (WR)", "actor_class": ""},
		{"time": "Sep 1, 10:00 AM EDT", "time_iso": "2026-09-01T14:00:00Z", "time_relative": "3 hours ago", "team": "Autopick for Eastside Elite (E1)", "team_name": "Autopick for Eastside Elite (E1)", "team_code": "", "has_team_code": false, "action": "selects", "player": "Bucky Irving (RB)", "actor_class": ""},
	})
	data := map[string]any{
		"teams": []string{}, "team": "", "query": "", "has_filters": false,
		"filtered_count": 2, "transactions_count": 2, "page": 1, "pages": 1,
		"page_start": 1, "page_end": 2, "has_previous": false, "has_next": false,
		"has_transactions": true, "transactions_empty": false,
		"transactions": rows,
	}
	html, err := route.RenderProgramComponent(program, "ActivityRegion", route.ProgramRenderEnv{
		Values: map[string]any{"data": data},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `data-gosx-text-layout-source="Eastside Elite"`) {
		t.Errorf("team-move row should lead with the plain team name: %s", html)
	}
	if !strings.Contains(html, `<span class="activity-team-code mono activity-token-gap">E1</span>`) {
		t.Errorf("team-move row missing its secondary code chip: %s", html)
	}
	if strings.Contains(html, `data-gosx-text-layout-source="Eastside Elite (E1)"`) {
		t.Errorf("team-move row should not render the old combined name-plus-code string as its own name source: %s", html)
	}
	if !strings.Contains(html, "Autopick for Eastside Elite (E1)") {
		t.Errorf("draft-pick provenance row should keep its combined string unchanged: %s", html)
	}
	if strings.Count(html, "activity-team-code") != 1 {
		t.Errorf("only the team-move row should render a code chip: %s", html)
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

	// Team/Player now render through TextBlock (text-flow wave,
	// 2026-09-05), so the literal <strong class="activity-token-gap">
	// opening tag carries extra data-gosx-text-layout-* attributes ahead
	// of its class; this checks the same element order, class, and text,
	// and that each still immediately precedes its own real-space verb
	// wrapper — the load-bearing part of this fix.
	for _, want := range []string{
		`data-gosx-text-layout-source="Commissioner" class="activity-token-gap">Commissioner</strong><span class="activity-verb"> deleted an announcement</span>`,
		`data-gosx-text-layout-source="Hot Path (W4)" class="activity-token-gap">Hot Path (W4)</strong><span class="activity-verb"> drafts </span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered activity row is missing the real-space verb wrapper: want %q in %s", want, html)
		}
	}
	if !strings.Contains(html, `data-gosx-text-layout-source="Bills D/ST (DST)" class="activity-token-gap">Bills D/ST (DST)</b>`) {
		t.Errorf("rendered activity row is missing the Player TextBlock: %s", html)
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

// TestActivityPlayoffCardDemotesUntilPlayoffsAreTheLivePhase is F22's
// failing-test-first reproduction (gap-audit J6): the transaction feed's
// loudest card used to be a full-size playoff-context panel regardless
// of season phase. While the postseason is not yet the live phase, the
// page renders one quiet plain-language line instead; the full card
// (playoffTruthMap's own headline/detail — internal/league/
// postseason_view.go, shared with /matchups and /team, left untouched)
// returns once the season phase actually is playoffs.
func TestActivityPlayoffCardDemotesUntilPlayoffsAreTheLivePhase(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{
		"teams": []string{}, "team": "", "query": "", "has_filters": false,
		"filtered_count": 0, "transactions_count": 0, "page": 1, "pages": 1,
		"page_start": 0, "page_end": 0, "has_previous": false, "has_next": false,
		"has_transactions": false, "transactions_empty": false,
		"transactions": activityRows(nil),
		"timezone": "EDT", "activity_fragment_url": "/activity/fragment", "activity_fragment_interval": "4s",
	}
	tests := []struct {
		name        string
		seasonPhase string
		wantQuiet   bool
	}{
		{name: "preseason demotes to one line", seasonPhase: "", wantQuiet: true},
		{name: "regular season demotes to one line", seasonPhase: "regular-season", wantQuiet: true},
		{name: "playoffs shows the full card", seasonPhase: "playoffs", wantQuiet: false},
		{name: "season complete shows the full card", seasonPhase: "season-complete", wantQuiet: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := map[string]any{}
			for k, v := range base {
				data[k] = v
			}
			data["playoff_truth"] = map[string]any{
				"headline": "PLAYOFFS NOT ACTIVE", "status_label": "WAITING",
				"detail": "The published season phase is Preseason; playoff truth will appear after the regular season is final.",
				"recovery": "", "season_phase": test.seasonPhase,
			}
			html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{
				Values: map[string]any{"data": data},
			})
			if err != nil {
				t.Fatal(err)
			}
			hasQuietLine := strings.Contains(html, "Playoff bracket: not seeded yet.")
			hasFullCard := strings.Contains(html, `class="score-command playoff-truth-card"`)
			if hasQuietLine != test.wantQuiet {
				t.Errorf("quiet line present = %v, want %v: %s", hasQuietLine, test.wantQuiet, html)
			}
			if hasFullCard == test.wantQuiet {
				t.Errorf("full card present = %v, want %v: %s", hasFullCard, !test.wantQuiet, html)
			}
		})
	}
}

// TestActivityRefreshNoteIsCalmAndInsideThePolledRegion is F27's
// failing-test-first reproduction (gap-audit J6): "If a refresh fails,
// use [Refresh Activity now]" told a manager to expect failure, and the
// "within 4 seconds" phrasing read as implementation detail. The note
// now states the cadence plainly and names the last real update, and it
// lives inside ActivityRegion (the same component the 4s poll re-renders)
// so "last update" stays current instead of freezing at first page load.
func TestActivityRefreshNoteIsCalmAndInsideThePolledRegion(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"teams": []string{}, "team": "", "query": "", "has_filters": false,
		"filtered_count": 1, "transactions_count": 1, "page": 1, "pages": 1,
		"page_start": 1, "page_end": 1, "has_previous": false, "has_next": false,
		"has_transactions": true, "transactions_empty": false,
		"last_update": "Sep 4, 5:31 AM EDT", "has_last_update": true,
		"transactions": activityRows([]map[string]any{
			{"time": "Sep 4, 5:31 AM EDT", "time_iso": "2026-09-04T09:31:00Z", "time_relative": "just now", "team": "Team A", "team_name": "Team A", "team_code": "", "has_team_code": false, "action": "drafts", "player": "Player X", "actor_class": ""},
		}),
	}
	html, err := route.RenderProgramComponent(program, "ActivityRegion", route.ProgramRenderEnv{
		Values: map[string]any{"data": data},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Updates every 4 seconds; last update Sep 4, 5:31 AM EDT.") {
		t.Errorf("ActivityRegion missing the calm refresh note: %s", html)
	}
	for _, unwanted := range []string{"If a refresh fails", "Activity refreshes automatically within"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("ActivityRegion still carries the retired anxious refresh copy %q: %s", unwanted, html)
		}
	}
}

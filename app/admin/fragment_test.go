package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAdminAttentionRegionContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`data-gosx-region-url="/admin/fragment"`,
		`data-gosx-region-interval="4s"`,
		`data-gosx-region-signal="$admin.attention.refresh"`,
		`data-gosx-set="$admin.attention.refresh"`,
		`<AdminAttentionReadout {...data.admin_attention}></AdminAttentionReadout>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("admin attention region missing %q", want)
		}
	}
	if !strings.Contains(rootPackageSource(t), `app.Mount("GET /admin/fragment", adminpage.AdminAttentionFragmentHandler(league.Default()))`) {
		t.Fatal("admin attention route is not mounted")
	}
}

// TestAdminAttentionReadAtRendersValidDateTimeWithRelativeText pins
// wave-2-verification item 6: the "READ AT" row printed a bare
// now.UTC().Format(time.RFC3339) stamp straight into <time class="mono">
// with no datetime attribute — an invalid <time> element and a raw ISO
// stamp in visible text, unlike every other timestamp on the page. The
// markup now mirrors the fleet card's own GeneratedAt/GeneratedAtISO/
// GeneratedAtRelative split (app/commissioner/page.gsx), falling back to
// a plain span when no instant is available rather than an empty
// datetime attribute.
func TestAdminAttentionReadAtRendersValidDateTimeWithRelativeText(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<If cond={props.GeneratedAtISO != ""}>`,
		`<time class="mono" datetime={props.GeneratedAtISO}>{props.GeneratedAt}<If cond={props.GeneratedAtRelative != ""}> · {props.GeneratedAtRelative}</If></time>`,
		`<If cond={props.GeneratedAtISO == ""}>`,
		`<span class="mono">{props.GeneratedAt}</span>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("READ AT markup missing %q", want)
		}
	}
	if strings.Contains(source, `<time class="mono">{props.GeneratedAt}</time>`) {
		t.Error("READ AT still renders an unconditional <time> with no datetime attribute")
	}
}

func sampleAdminAttention() adminAttentionReadoutProps {
	return adminAttentionReadoutProps{
		Phase: "preseason", DraftStatus: "AWAITING COMMISSIONER", DraftDate: "SAT · AUG 29", DraftTime: "1:00 PM EDT", DraftPublished: true,
		ScheduleStatus: "GENERATED", ScheduleWeek: 1, ScheduleReady: false, ScheduleReason: "waiting for final games",
		SeatCount: 8, ClaimedCount: 7, ReadyCount: 6, InviteCount: 2, BoardGapCount: 1,
		PresenceHere: 2, PresenceIdle: 1, PresenceAway: 2, PresenceNotSeen: 2, PresenceUnclaimed: 1,
		GeneratedAt: "2026-08-25T15:00:00Z",
		Seats:       []adminAttentionSeatView{{Name: "Alpha", Abbreviation: "ALP", Claimed: true, Ready: false, Presence: "here", PresenceLabel: "In the room", PresenceDetail: "At the room now.", BoardCount: 0, BoardGap: true}},
	}
}

// TestAdminAttentionReadoutRendersFriendlyDraftDateAndPresenceWords is the
// item 5 decisive proof (2026-09-02 audit): the live readout used to
// print a raw RFC3339 draft instant ("2026-09-06T16:05:00-04:00") where
// every other draft-date fact on the page already reads through the
// league-local formatter, and each seat's own presence enum
// ("not_seen") straight into the page as if it were prose, rather than
// plain words.
func TestAdminAttentionReadoutRendersFriendlyDraftDateAndPresenceWords(t *testing.T) {
	rendered, err := adminAttentionFragmentRender(sampleAdminAttention())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SAT · AUG 29", "1:00 PM EDT", "In the room"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("attention readout missing %q: %s", want, rendered)
		}
	}
	for _, notWant := range []string{"T17:00:00Z", "T16:05:00", "-04:00", ">here<", ">not_seen<"} {
		if strings.Contains(rendered, notWant) {
			t.Errorf("attention readout must not print the raw %q: %s", notWant, rendered)
		}
	}
}

// TestAdminAttentionReadoutFromDataNamesManagersAndSummarizesNotCheckedIn
// pins F6 + F19 (gap-audit J2): a not-ready row named only its seat code
// and team name, and the console reported "READY 4 / 8" with nowhere to
// see who that meant or act on it. adminAttentionReadoutFromData now
// carries each seat's own manager name (plain, not a code) and a
// precomputed "Not checked in: <first name> (<team>), ..." summary of
// every claimed-but-not-ready seat.
func TestAdminAttentionReadoutFromDataNamesManagersAndSummarizesNotCheckedIn(t *testing.T) {
	data := map[string]any{
		"seats": []map[string]any{
			{"name": "In Shedeur Time", "abbreviation": "AQ1", "manager": "Oscar Villavicencio", "claimed": true, "ready": true, "presence": "here"},
			{"name": "Los Delfines del Norte", "abbreviation": "AQ2", "manager": "Jorge V", "claimed": true, "ready": false, "presence": "not_seen", "presence_detail": "No room heartbeat since this server started."},
			{"name": "Placeholder go here", "abbreviation": "AQ4", "manager": "", "claimed": false, "ready": false, "presence": "unclaimed"},
		},
	}
	view := adminAttentionReadoutFromData(data)
	if len(view.Seats) != 3 {
		t.Fatalf("len(Seats) = %d, want 3", len(view.Seats))
	}

	ready, notReady, unclaimed := view.Seats[0], view.Seats[1], view.Seats[2]
	if ready.Manager != "Oscar Villavicencio" {
		t.Errorf("ready seat Manager = %q, want %q", ready.Manager, "Oscar Villavicencio")
	}
	if notReady.Manager != "Jorge V" || notReady.ManagerFirstName != "Jorge" {
		t.Errorf("not-ready seat Manager/ManagerFirstName = %q/%q, want %q/%q", notReady.Manager, notReady.ManagerFirstName, "Jorge V", "Jorge")
	}
	// FriendlyPresenceDetail (F19): "No room heartbeat since this server
	// started." is developer/server language; the drawer's own plain
	// rewrite reads "No manager has opened the room yet."
	if notReady.PresenceDetail != "No manager has opened the room yet." {
		t.Errorf("not-ready seat PresenceDetail = %q, want the plain-language rewrite", notReady.PresenceDetail)
	}
	if unclaimed.Manager != "" || unclaimed.ManagerFirstName != "" {
		t.Errorf("unclaimed seat Manager/ManagerFirstName = %q/%q, want both empty", unclaimed.Manager, unclaimed.ManagerFirstName)
	}

	if !view.HasNotCheckedIn {
		t.Fatal("HasNotCheckedIn = false, want true (one claimed-but-not-ready seat)")
	}
	if want := "Jorge (Los Delfines del Norte)"; view.NotCheckedInSummary != want {
		t.Errorf("NotCheckedInSummary = %q, want %q", view.NotCheckedInSummary, want)
	}
}

// TestAdminAttentionReadoutFromDataHasNoNotCheckedInSummaryWhenEveryoneIsReady
// proves the summary line's own gate: it must not render (or claim a
// non-empty summary) once every claimed seat is ready.
func TestAdminAttentionReadoutFromDataHasNoNotCheckedInSummaryWhenEveryoneIsReady(t *testing.T) {
	data := map[string]any{
		"seats": []map[string]any{
			{"name": "In Shedeur Time", "abbreviation": "AQ1", "manager": "Oscar Villavicencio", "claimed": true, "ready": true},
		},
	}
	view := adminAttentionReadoutFromData(data)
	if view.HasNotCheckedIn {
		t.Fatalf("HasNotCheckedIn = true, want false (every claimed seat is ready); summary = %q", view.NotCheckedInSummary)
	}
	if view.NotCheckedInSummary != "" {
		t.Errorf("NotCheckedInSummary = %q, want empty", view.NotCheckedInSummary)
	}
}

// TestAdminAttentionReadoutRendersNotCheckedInSummaryAndRoomLink is the
// template-level half of F6: the render must show the summary sentence
// and a link into the draft room only when there is something to check
// in for.
func TestAdminAttentionReadoutRendersNotCheckedInSummaryAndRoomLink(t *testing.T) {
	props := sampleAdminAttention()
	props.HasNotCheckedIn = true
	props.NotCheckedInSummary = "Jorge (Los Delfines del Norte), Kathleen (DeBÍ TiRAR MáS TOUCHDOWNS)"
	rendered, err := adminAttentionFragmentRender(props)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Not checked in:", "Jorge (Los Delfines del Norte)", `href="/draft"`, "Open the draft room"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("attention readout missing %q: %s", want, rendered)
		}
	}

	everyoneReady := sampleAdminAttention()
	everyoneReady.HasNotCheckedIn = false
	renderedReady, err := adminAttentionFragmentRender(everyoneReady)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(renderedReady, "Not checked in:") {
		t.Error("attention readout renders \"Not checked in:\" even though HasNotCheckedIn is false")
	}
}

func TestAdminAttentionFragmentETagPrivacyAndMethodBoundary(t *testing.T) {
	loaded := 0
	handler := adminAttentionFragmentHandler(
		func(*http.Request) (int, bool) { return 0, true },
		func(*http.Request) adminAttentionReadoutProps {
			loaded++
			return sampleAdminAttention()
		},
		func(props adminAttentionReadoutProps) (string, error) {
			return "<section data-phase=\"" + props.Phase + "\">attention</section>", nil
		},
	)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/admin/fragment?section=seats", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "preseason") {
		t.Fatalf("first fragment = %d %q", first.Code, first.Body.String())
	}
	if first.Header().Get("Cache-Control") != "private, no-store" || first.Header().Get("Vary") != "Cookie" || first.Header().Get("ETag") == "" {
		t.Fatalf("privacy/etag headers = %#v", first.Header())
	}
	etag := first.Header().Get("ETag")
	unchanged := httptest.NewRecorder()
	unchangedRequest := httptest.NewRequest(http.MethodGet, "/admin/fragment?section=seats", nil)
	unchangedRequest.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, unchangedRequest)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 || loaded != 2 {
		t.Fatalf("unchanged fragment = %d body=%q loads=%d", unchanged.Code, unchanged.Body.String(), loaded)
	}
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/admin/fragment", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet || loaded != 2 {
		t.Fatalf("POST = %d allow=%q loads=%d", post.Code, post.Header().Get("Allow"), loaded)
	}
}

func TestAdminAttentionFragmentRejectsUnauthorizedBeforeRead(t *testing.T) {
	loaded := 0
	handler := adminAttentionFragmentHandler(
		func(*http.Request) (int, bool) { return http.StatusForbidden, false },
		func(*http.Request) adminAttentionReadoutProps { loaded++; return sampleAdminAttention() },
		func(adminAttentionReadoutProps) (string, error) { return "unreachable", nil },
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/fragment", nil))
	if response.Code != http.StatusForbidden || loaded != 0 {
		t.Fatalf("unauthorized fragment = %d loads=%d", response.Code, loaded)
	}
}

func TestAdminAttentionFragmentConcurrentReads(t *testing.T) {
	handler := adminAttentionFragmentHandler(
		func(*http.Request) (int, bool) { return 0, true },
		func(*http.Request) adminAttentionReadoutProps { return sampleAdminAttention() },
		func(props adminAttentionReadoutProps) (string, error) { return props.Phase, nil },
	)
	const readers = 20
	var wait sync.WaitGroup
	wait.Add(readers)
	for range readers {
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/fragment", nil))
			if response.Code != http.StatusOK || response.Body.String() != "preseason" {
				t.Errorf("concurrent fragment = %d %q", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()
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

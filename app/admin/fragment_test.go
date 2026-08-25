package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
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
	mainSource, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainSource), `app.Mount("GET /admin/fragment", adminpage.AdminAttentionFragmentHandler(league.Default()))`) {
		t.Fatal("admin attention route is not mounted")
	}
}

func sampleAdminAttention() adminAttentionReadoutProps {
	return adminAttentionReadoutProps{
		Phase: "preseason", DraftStatus: "AWAITING COMMISSIONER", DraftAt: "2026-08-29T17:00:00Z",
		ScheduleStatus: "GENERATED", ScheduleWeek: 1, ScheduleReady: false, ScheduleReason: "waiting for final games",
		SeatCount: 8, ClaimedCount: 7, ReadyCount: 6, InviteCount: 2, BoardGapCount: 1,
		PresenceHere: 2, PresenceIdle: 1, PresenceAway: 2, PresenceNotSeen: 2, PresenceUnclaimed: 1,
		GeneratedAt: "2026-08-25T15:00:00Z",
		Seats:       []adminAttentionSeatView{{Name: "Alpha", Abbreviation: "ALP", Claimed: true, Ready: false, Presence: "here", PresenceDetail: "At the room now.", BoardCount: 0, BoardGap: true}},
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

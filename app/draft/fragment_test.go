package draft

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

func TestDraftFragmentRejectsMethodAndUnauthorizedBeforeLoading(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		allowed bool
		want    int
	}{
		{name: "method", method: http.MethodPost, allowed: true, want: http.StatusMethodNotAllowed},
		{name: "anonymous", method: http.MethodGet, allowed: false, want: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loads := 0
			handler := draftFragmentHandler(draftRoomRegion, func(*http.Request) bool {
				return test.allowed
			}, func(*http.Request) map[string]any {
				loads++
				return nil
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, "/draft/fragment/room", nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if loads != 0 {
				t.Fatalf("rejected request performed %d draft reads", loads)
			}
			if test.method != http.MethodGet && response.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("Allow = %q, want GET", response.Header().Get("Allow"))
			}
		})
	}
}

func TestDraftFragmentsRenderScopedHTMLAndReturnBodyless304(t *testing.T) {
	load := func(request *http.Request) map[string]any {
		return league.Default().DraftDataReadOnly(request)
	}
	tests := []struct {
		region string
		path   string
		class  string
	}{
		{region: draftRoomRegion, path: "/draft/fragment/room", class: "draft-live-room"},
		{region: draftWorkspaceRegion, path: "/draft/fragment/workspace?pos=RB&q=run&page=2", class: "draft-live-workspace"},
	}
	for _, test := range tests {
		t.Run(test.region, func(t *testing.T) {
			handler := draftFragmentHandler(test.region, func(*http.Request) bool { return true }, load)
			first := httptest.NewRecorder()
			handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, test.path, nil))
			if first.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
			}
			if !strings.Contains(first.Body.String(), `class="`+test.class+`"`) || strings.Contains(first.Body.String(), "<html") {
				t.Fatalf("fragment is not scoped %s markup: %s", test.region, first.Body.String())
			}
			for name, want := range map[string]string{
				"Cache-Control": "private, no-store",
				"Content-Type":  "text/html; charset=utf-8",
				"Vary":          "Cookie",
			} {
				if got := first.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			etag := first.Header().Get("ETag")
			if etag == "" {
				t.Fatal("missing ETag")
			}

			secondRequest := httptest.NewRequest(http.MethodGet, test.path, nil)
			secondRequest.Header.Set("If-None-Match", etag)
			second := httptest.NewRecorder()
			handler.ServeHTTP(second, secondRequest)
			if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
				t.Fatalf("conditional response = %d with %d bytes, want bodyless 304", second.Code, second.Body.Len())
			}
		})
	}
}

func TestDraftRegionETagIgnoresWallClockTextButTracksLeagueState(t *testing.T) {
	view := draftRoomView{Data: map[string]any{
		"pick_number": 4,
		"clock":       map[string]any{"armed": true, "effective_deadline": "2026-08-23T00:00:00Z", "server_now": "first", "remaining_seconds": 40, "remaining_label": "00:40"},
		"draft":       map[string]any{"started": true, "countdown_label": "TODAY", "days_until": 0},
	}}
	first, err := draftRegionETag(draftRoomRegion, view)
	if err != nil {
		t.Fatal(err)
	}
	view.Data["clock"].(map[string]any)["server_now"] = "second"
	view.Data["clock"].(map[string]any)["remaining_seconds"] = 39
	view.Data["clock"].(map[string]any)["remaining_label"] = "00:39"
	second, err := draftRegionETag(draftRoomRegion, view)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("wall-clock-only update changed ETag: %s != %s", first, second)
	}
	view.Data["pick_number"] = 5
	third, err := draftRegionETag(draftRoomRegion, view)
	if err != nil {
		t.Fatal(err)
	}
	if third == second {
		t.Fatal("authoritative pick change did not change ETag")
	}
}

func TestDraftRegionContractIsScopedAndMounted(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`data-gosx-region-url="/draft/fragment/room"`,
		`data-gosx-region-interval="4s"`,
		`<DraftRoom {...data.room}></DraftRoom>`,
		`<DraftWorkspace {...data.workspace}></DraftWorkspace>`,
		`aria-live="polite"`,
		`Reload the room`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("draft region contract missing %q", want)
		}
	}
	if strings.Contains(source, "data-gosx-revalidate") {
		t.Fatal("draft page still performs whole-page revalidation")
	}

	mainSource, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`app.Mount("GET /draft/fragment/room", draftpage.RoomFragmentHandler(league.Default()))`,
		`app.Mount("GET /draft/fragment/workspace", draftpage.WorkspaceFragmentHandler(league.Default()))`,
	} {
		if !strings.Contains(string(mainSource), want) {
			t.Errorf("draft fragment route missing mount %q", want)
		}
	}
}

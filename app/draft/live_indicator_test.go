package draft

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDraftPickTapeLabelFollowsLifecycleState(t *testing.T) {
	cases := []struct {
		name        string
		started     bool
		complete    bool
		want        string
		wantLiveDot bool
	}{
		{name: "scheduled", started: false, want: "DRAFT LOG"},
		{name: "started", started: true, want: "LIVE LOG", wantLiveDot: true},
		{name: "complete", started: true, complete: true, want: "FINAL LEDGER"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := draftFragmentFixture()
			draft := fixture["draft"].(map[string]any)
			draft["started"] = test.started
			draft["complete"] = test.complete
			fixture["draft_complete"] = test.complete
			handler := draftFragmentHandler(draftWorkspaceRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any {
				return fixture
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/workspace", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
			}
			body := response.Body.String()
			if !strings.Contains(body, test.want) {
				t.Fatalf("pick tape omitted %q: %s", test.want, body)
			}
			hasLiveDot := strings.Contains(body, `class="live-dot live-dot--bound"`)
			if hasLiveDot != test.wantLiveDot {
				t.Fatalf("live dot = %v, want %v: %s", hasLiveDot, test.wantLiveDot, body)
			}
			if !test.started && strings.Contains(body, "LIVE LOG") {
				t.Fatal("scheduled draft mislabeled as live log")
			}
			if test.complete && strings.Contains(body, "LIVE LOG") {
				t.Fatal("completed draft retained live log label")
			}
		})
	}
}

func TestDraftPickClockRendersBrowserOwnedCountdown(t *testing.T) {
	fixture := draftFragmentFixture()
	clock := fixture["clock"].(map[string]any)
	clock["armed"] = true
	clock["effective_deadline"] = "2026-08-29T01:02:03Z"
	clock["remaining_label"] = "01:23"
	handler := draftFragmentHandler(draftRoomRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any {
		return fixture
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/room", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{
		`data-gosx-countdown="2026-08-29T01:02:03Z"`,
		`data-gosx-countdown-format="mm:ss"`,
		`data-gosx-countdown-warn="30s:pick-clock--warn"`,
		`data-gosx-countdown-cue="10s:beep"`,
		`>01:23</strong>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("live pick clock missing %q: %s", want, body)
		}
	}
}

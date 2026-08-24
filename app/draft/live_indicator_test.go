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

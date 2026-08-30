package draft

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildFragmentBody renders region against a fresh fixture with started/
// complete overridden, and returns the response body. t.Helper keeps
// failures pointing at the caller.
func buildFragmentBody(t *testing.T, region string, started, complete bool) string {
	t.Helper()
	fixture := draftFragmentFixture()
	draft := fixture["draft"].(map[string]any)
	draft["started"] = started
	draft["complete"] = complete
	fixture["draft_complete"] = complete
	handler := draftFragmentHandler(region, func(*http.Request) bool { return true }, func(*http.Request) map[string]any {
		return fixture
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/"+region, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("%s status = %d; body: %s", region, response.Code, response.Body.String())
	}
	return response.Body.String()
}

// TestDraftPickTapeLabelFollowsLifecycleState covers two regions the old
// combined workspace fragment used to carry together: the tape pane's own
// FINAL LEDGER label (draftTapeRegion, D4's DraftHistory — the DRAFT LOG/
// LIVE LOG state copy retired with Task 7's round-grouped tape, which
// carries its own "N of M made" progress per round instead) and the
// command bar's live dot (draftCommandRegion, DraftCommandBar) — the dot
// is the room's one live-dot site (D2), bound only while the draft is
// started and not complete.
func TestDraftPickTapeLabelFollowsLifecycleState(t *testing.T) {
	cases := []struct {
		name        string
		started     bool
		complete    bool
		wantLedger  bool
		wantLiveDot bool
	}{
		{name: "scheduled", started: false},
		{name: "started", started: true, wantLiveDot: true},
		{name: "complete", started: true, complete: true, wantLedger: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tapeBody := buildFragmentBody(t, draftTapeRegion, test.started, test.complete)
			hasLedger := strings.Contains(tapeBody, "FINAL LEDGER")
			if hasLedger != test.wantLedger {
				t.Fatalf("pick tape FINAL LEDGER = %v, want %v: %s", hasLedger, test.wantLedger, tapeBody)
			}

			commandBody := buildFragmentBody(t, draftCommandRegion, test.started, test.complete)
			hasLiveDot := strings.Contains(commandBody, `class="live-dot live-dot--bound" aria-hidden="true">LIVE<`)
			if hasLiveDot != test.wantLiveDot {
				t.Fatalf("live dot = %v, want %v: %s", hasLiveDot, test.wantLiveDot, commandBody)
			}
		})
	}
}

func TestDraftPickClockRendersBrowserOwnedCountdown(t *testing.T) {
	fixture := draftFragmentFixture()
	clock := fixture["clock"].(map[string]any)
	clock["state"] = "RUNNING"
	clock["armed"] = true
	clock["effective_deadline"] = "2026-08-29T01:02:03Z"
	clock["remaining_label"] = "01:23"
	handler := draftFragmentHandler(draftCommandRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any {
		return fixture
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/command", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{
		`data-pick-clock`,
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

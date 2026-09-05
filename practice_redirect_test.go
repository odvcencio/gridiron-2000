package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gridiron-2000/internal/league"
)

// TestPracticeRedirectsToTheRealRoomOnceTheDraftHasStarted is the owner's
// rule (2026-09-04): once the real draft has started, live or complete,
// the practice draft is gone — its page and its actions answer a 303 to
// the real room. Before the start every request passes through untouched,
// and no other route is ever affected.
func TestPracticeRedirectsToTheRealRoomOnceTheDraftHasStarted(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cases := []struct {
		name    string
		started bool
		method  string
		target  string
		want    int
	}{
		{name: "page before the draft", started: false, method: http.MethodGet, target: league.PracticeRoomPath, want: http.StatusOK},
		{name: "action before the draft", started: false, method: http.MethodPost, target: league.PracticeRoomPath + "/__actions/practice-start", want: http.StatusOK},
		{name: "page once the draft is live", started: true, method: http.MethodGet, target: league.PracticeRoomPath, want: http.StatusSeeOther},
		{name: "page with a trailing slash", started: true, method: http.MethodGet, target: league.PracticeRoomPath + "/", want: http.StatusSeeOther},
		{name: "page with a view query", started: true, method: http.MethodGet, target: league.PracticeRoomPath + "?view=board", want: http.StatusSeeOther},
		{name: "start action once the draft is live", started: true, method: http.MethodPost, target: league.PracticeRoomPath + "/__actions/practice-start", want: http.StatusSeeOther},
		{name: "pick action once the draft is live", started: true, method: http.MethodPost, target: league.PracticeRoomPath + "/__actions/make-pick", want: http.StatusSeeOther},
		{name: "the real room is untouched", started: true, method: http.MethodGet, target: "/draft", want: http.StatusOK},
		{name: "the real room's actions are untouched", started: true, method: http.MethodPost, target: "/draft/__actions/make-pick", want: http.StatusOK},
		{name: "draft results are untouched", started: true, method: http.MethodGet, target: "/draft/results", want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := redirectPracticeAfterDraftStartWith(next, func() bool { return tc.started })
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.target, nil))
			if recorder.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.target, recorder.Code, tc.want)
			}
			if tc.want == http.StatusSeeOther {
				if location := recorder.Header().Get("Location"); location != "/draft" {
					t.Fatalf("redirect target = %q, want /draft", location)
				}
			}
		})
	}
}

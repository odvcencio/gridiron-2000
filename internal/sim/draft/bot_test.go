package draft

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFTokenParsesHiddenField(t *testing.T) {
	if got := csrfTokenFrom(`<form><input type="hidden" name="csrf_token" value="abc123"></form>`); got != "abc123" {
		t.Fatalf("csrf = %q", got)
	}
}

func TestMotifParsesFirstOption(t *testing.T) {
	if got := firstMotifFrom(`<input type="radio" name="motif" value="astronaut"><input type="radio" name="motif" value="rocket">`); got != "astronaut" {
		t.Fatalf("motif = %q", got)
	}
}

func TestBotSendsIdentityHeaderAndCSRF(t *testing.T) {
	headers := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	bot := New(srv.URL, "east1@sim.test", "East One")
	bot.csrf = "tok"
	if _, err := bot.postAction("/draft/__actions/toggle-ready", map[string]string{"team_id": "east-1"}); err != nil {
		t.Fatal(err)
	}
	seen := <-headers
	if seen.Get("X-Test-User") != "east1@sim.test|East One" || seen.Get("X-CSRF-Token") != "tok" || seen.Get("Accept") != "application/json" {
		t.Fatalf("headers = %v", seen)
	}
}

func TestEligiblePickSkipsIneligibleRows(t *testing.T) {
	state := DraftState{Available: []map[string]any{
		{"id": "a", "draft_eligible": false},
		{"id": "b", "draft_eligible": true},
	}}
	if got := state.EligiblePick(); got != "b" {
		t.Fatalf("eligible = %q", got)
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSplitMembers guards the --members parser: whitespace around an
// email trims away, and an empty entry ("a@x,,b@y" or a trailing comma)
// never turns into a blank seat request.
func TestSplitMembers(t *testing.T) {
	got := splitMembers(" a@x , , b@y ,")
	want := []string{"a@x", "b@y"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("splitMembers = %q, want %q", got, want)
	}
	if got := splitMembers(""); got != nil {
		t.Fatalf("splitMembers(\"\") = %q, want nil", got)
	}
}

// existingSeatsFixture is a fake harness that answers GET /join (with a
// CSRF token, the way an already-seated viewer's redirect to /team also
// would — Prime's doc comment) and GET /test/draft with a fixed team
// grid: two claimed seats carrying member_email, one unclaimed seat with
// none. It resolves viewer_team_id from the caller's X-Test-User email so
// State (called inside seatExisting) adopts the right TeamID for whoever
// signed in — the same contract the real harness route honors.
func existingSeatsFixture(t *testing.T) *httptest.Server {
	t.Helper()
	seatByEmail := map[string]string{
		"alice@x.example": "team-1",
		"bob@y.example":   "team-2",
	}
	teams := []map[string]any{
		{"id": "team-1", "name": "Alpha", "manager": "Alice", "member_email": "alice@x.example"},
		{"id": "team-2", "name": "Bravo", "manager": "Bob", "member_email": "bob@y.example"},
		{"id": "team-3", "name": "Charlie", "manager": "", "member_email": ""},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/join", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<form><input type="hidden" name="csrf_token" value="tok"></form>`))
	})
	mux.HandleFunc("/test/draft", func(w http.ResponseWriter, r *http.Request) {
		identity := r.Header.Get("X-Test-User")
		email, _, _ := strings.Cut(identity, "|")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"started": false, "complete": false, "pick_number": 1,
			"on_clock_id": "", "viewer_team_id": seatByEmail[email], "rounds": 17,
			"teams": teams, "picks": []any{}, "available": []any{},
		})
	})
	return httptest.NewServer(mux)
}

// TestSeatExistingSignsInAsCurrentHolders proves the default (no
// --members) case: every claimed seat that carries a member_email is
// signed in as that holder, and the one unclaimed seat (no member_email)
// is skipped rather than treated as a seat to claim.
func TestSeatExistingSignsInAsCurrentHolders(t *testing.T) {
	srv := existingSeatsFixture(t)
	defer srv.Close()
	_, managers, err := seatExisting(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(managers) != 2 {
		t.Fatalf("len(managers) = %d, want 2 (team-3 has no member_email)", len(managers))
	}
	byEmail := map[string]*manager{}
	for _, m := range managers {
		byEmail[m.bot.Email] = m
	}
	alice, ok := byEmail["alice@x.example"]
	if !ok {
		t.Fatal("alice@x.example was not signed in")
	}
	if alice.bot.TeamID != "team-1" || alice.team != "Alpha" {
		t.Fatalf("alice seat = %q team %q, want team-1 Alpha", alice.bot.TeamID, alice.team)
	}
	bob, ok := byEmail["bob@y.example"]
	if !ok {
		t.Fatal("bob@y.example was not signed in")
	}
	if bob.bot.TeamID != "team-2" || bob.team != "Bravo" {
		t.Fatalf("bob seat = %q team %q, want team-2 Bravo", bob.bot.TeamID, bob.team)
	}
}

// TestSeatExistingFiltersToRequestedMembers proves --members drives only
// the named subset of the league's real seat holders, leaving every other
// claimed seat untouched for a human or the server's own autopick.
func TestSeatExistingFiltersToRequestedMembers(t *testing.T) {
	srv := existingSeatsFixture(t)
	defer srv.Close()
	_, managers, err := seatExisting(srv.URL, []string{"bob@y.example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(managers) != 1 {
		t.Fatalf("len(managers) = %d, want 1", len(managers))
	}
	if managers[0].bot.Email != "bob@y.example" || managers[0].team != "Bravo" {
		t.Fatalf("managers[0] = %+v, want bob@y.example / Bravo", managers[0])
	}
}

// TestSeatExistingUnknownMemberYieldsNoManagers proves an email that
// matches no claimed seat's member_email drives nothing (rather than
// falling back to some other seat), so a typo in --members is loud —
// zero managers, not a silently wrong one.
func TestSeatExistingUnknownMemberYieldsNoManagers(t *testing.T) {
	srv := existingSeatsFixture(t)
	defer srv.Close()
	_, managers, err := seatExisting(srv.URL, []string{"nobody@z.example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(managers) != 0 {
		t.Fatalf("len(managers) = %d, want 0", len(managers))
	}
}

// TestRunDraftRejectsUnknownSeatSource proves an unrecognized --seats
// value fails fast, before any network call: the switch that validates it
// runs ahead of preflight in runDraft, so a typo never even reaches a
// server (production or otherwise).
func TestRunDraftRejectsUnknownSeatSource(t *testing.T) {
	err := runDraft([]string{"--url", "http://127.0.0.1:1", "--seats", "bogus"})
	if err == nil {
		t.Fatal("want an error for --seats bogus")
	}
	if !strings.Contains(err.Error(), "--seats") {
		t.Fatalf("error = %v, want it to name --seats", err)
	}
}

// TestRunDraftGuardsExistingSeatsAgainstNonHarnessTarget proves the
// never-production preflight guard still runs ahead of --seats existing:
// a target with no harness routes mounted (a 404 on /test/draft, exactly
// what a production build answers) stops the run before it signs in as
// anyone, the same as it already does for --seats claim.
func TestRunDraftGuardsExistingSeatsAgainstNonHarnessTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	err := runDraft([]string{"--url", srv.URL, "--seats", "existing"})
	if err == nil {
		t.Fatal("want an error: the target mounts no harness routes")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error = %v, want it to mention the 404 preflight answered", err)
	}
}

// TestMembersFlagImpliesExistingSeatSource proves --members alone (with
// --seats left at its "claim" default) still selects the existing-seats
// path: naming specific seat holders only makes sense against seats a
// real league already has.
func TestMembersFlagImpliesExistingSeatSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	err := runDraft([]string{"--url", srv.URL, "--members", "a@x.example"})
	if err == nil {
		t.Fatal("want an error: the target mounts no harness routes")
	}
	// The failure must be the shared preflight 404, not a --managers range
	// error: --seats claim's manager-count validation must not run once
	// --members has selected the existing-seats path.
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error = %v, want the preflight 404, not a --managers error", err)
	}
}

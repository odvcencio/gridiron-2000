package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// fetchAsTestUser GETs path against child using the harness's own
// X-Test-User header auth (the same mechanism draft.Bot's internal get()
// uses), independent of both cookies and any browser JS runtime.
func fetchAsTestUser(t *testing.T, child *simChild, identity, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, child.URL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}
	req.Header.Set("X-Test-User", identity)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body for %s: %v", path, err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, res.StatusCode, body)
	}
	return string(body)
}

// drawerMarkup returns the #draft-commissioner element's own opening tag
// (through its first ">"), or fails if the id is missing.
func drawerMarkup(t *testing.T, body string) string {
	t.Helper()
	idx := strings.Index(body, `id="draft-commissioner"`)
	if idx == -1 {
		t.Fatalf("no #draft-commissioner in body")
	}
	// Walk back to the element's own opening "<aside" so a stray
	// "hidden" earlier in the document can never leak into the match.
	start := strings.LastIndex(body[:idx], "<aside")
	end := strings.Index(body[idx:], ">")
	if start == -1 || end == -1 {
		t.Fatalf("could not isolate the #draft-commissioner opening tag")
	}
	return body[start : idx+end+1]
}

// TestCommissionerDrawerOpenQueryFlagRendersWithoutHidden pins F24's
// server-side half (gap-audit J2): the commissioner drawer used to close
// on every one of its own actions (pause, extend, resume, force-autopick,
// seat toggles, undo) because the server always rendered <aside ...
// hidden>, with no memory of "the commissioner had this open."
// prepareDraftData now reads a "commissioner=open" query flag back
// (app/draft/page.server.go) and every one of those actions' own redirect
// target (draftCommissionerDrawerTarget, and draft-undo's own
// origin-aware target in app/admin/page.server.go) carries it. A plain
// GET with no flag renders hidden, exactly as before; with the flag, the
// drawer renders open with no further click required — the exact
// response a real no-JS POST-redirect-GET lands on.
func TestCommissionerDrawerOpenQueryFlagRendersWithoutHidden(t *testing.T) {
	root := browserAppRoot(t)
	child := startSimChild(t, "", "GOSX_APP_ROOT="+root)
	league := seatLeagueWith(t, child, true)
	if err := league.commish.StartDraft(); err != nil {
		t.Fatalf("start draft: %v", err)
	}
	commissioner := league.commish.Email + "|" + league.commish.Name

	plain := drawerMarkup(t, fetchAsTestUser(t, child, commissioner, "/draft"))
	if !strings.Contains(plain, " hidden") {
		t.Fatalf("plain /draft drawer = %q, want the hidden attribute present", plain)
	}

	reopened := drawerMarkup(t, fetchAsTestUser(t, child, commissioner, "/draft?commissioner=open"))
	if strings.Contains(reopened, " hidden") {
		t.Fatalf("/draft?commissioner=open drawer = %q, want no hidden attribute", reopened)
	}

	// A non-commissioner viewer never renders the drawer at all, flag or
	// not — this must never become a way to reveal it to the wrong seat.
	manager := league.bots[0].Email + "|" + league.bots[0].Name
	managerBody := fetchAsTestUser(t, child, manager, "/draft?commissioner=open")
	if strings.Contains(managerBody, `id="draft-commissioner"`) {
		t.Fatal("a manager (non-commissioner) must never render the commissioner drawer")
	}
}

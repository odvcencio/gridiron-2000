package draft

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
)

const shellCommissioner = "commish@example.com"

// postDraftAction posts one page action as email; the native form path
// answers 303 (draftActionSuccess, page.server.go:129-135).
func postDraftAction(t *testing.T, handler http.Handler, email, name string, fields url.Values) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/__actions/"+name, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", email)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s as %s = %d: %s", name, email, rec.Code, rec.Body.String())
	}
}

// completeDraftByForcedAutopicks finishes the draft with one forced autopick
// per remaining pick. The action path answers 303 without rendering a page
// (page.server.go:129-135), so each pick costs one store write; a full
// 8 x 15 draft finishes in well under a second. The loop reads `complete`
// from the read-only view, so it stops on its own if the draft ends early.
func completeDraftByForcedAutopicks(t *testing.T, handler http.Handler, service *league.Service) {
	t.Helper()
	limit := len(service.Teams())*league.CurrentDraftRounds() + 1
	for n := 0; n < limit; n++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		view := service.DraftDataReadOnly(req)
		if complete, _ := view["draft_complete"].(bool); complete {
			return
		}
		token, _ := view["current_pick_token"].(string)
		postDraftAction(t, handler, shellCommissioner, "clock-force-autopick", url.Values{"confirm": {"FORCE CURRENT PICK"}, "current_pick_token": {token}})
	}
	t.Fatalf("draft did not complete after %d forced autopicks", limit)
}

func livePool() league.PlayerSource {
	rows := fantasy.OfflinePool()
	out := make([]league.Player, 0, len(rows))
	for _, p := range rows {
		out = append(out, league.Player{ID: p.ID, Name: p.Name, Position: p.Position, NFLTeam: p.NFLTeam, ADP: p.ADP, ADPRank: p.ADPRank, ByeWeek: p.ByeWeek, Projection: p.Projection, Status: "Available"})
	}
	return func() ([]league.Player, int64, string) { return out, 1, "live" }
}

func TestDraftShellRendersEveryDraftState(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftShellRendersEveryDraftStateFixtureProcess$")
	cmd.Env = append(os.Environ(), "DRAFT_SHELL_FIXTURE=1", "DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=false", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=", "COMMISSIONER_EMAILS="+shellCommissioner)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("draft shell fixture process: %v\n%s", err, output)
	}
}

func TestDraftShellRendersEveryDraftStateFixtureProcess(t *testing.T) {
	if os.Getenv("DRAFT_SHELL_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", os.Getenv("DATA_FILE"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	service := league.Default()
	service.SetPlayerSource(livePool()) // draftStartReadiness refuses an offline label (admin.go:1182-1201)
	const seated = "seated-shell@example.com"
	if _, err := service.AssignManager(seated, "Seated Shell"); err != nil {
		t.Fatal(err)
	}
	handler := buildDraftAuthenticatedHandler(t)
	common := []string{
		`class="draft-shell`, `data-draft-live-mode="target"`, `class="draft-command"`,
		// Task 8 (target mode, gosx@v0.53.10): the command bar carries no
		// data-gosx-region of its own any more — a fetchless
		// data-gosx-live-mode="event" root applies hub payloads directly
		// (S6's zero-fetch-per-pick budget).
		`data-gosx-live-mode="event"`, `data-gosx-live-on="draft:pick draft:undo draft:clock draft:state"`,
		`draft-pane--history`, `data-gosx-region-url="/draft/fragment/tape?view=tape"`,
		`id="tab-players"`, `id="tab-picks"`, `class="draft-tabbar"`,
		`aria-live="polite"`, `aria-live="off"`, `<nav class="pool-pagination" aria-label="Draft pool pages">`,
		`data-gosx-cue-toggle`, `data-gosx-cue-label-off="Sound off"`, // live on v0.53.10
		`class="live-dot live-dot--bound" aria-hidden="true"`,
	}
	forbidden := []string{"draft-masthead", "THE FUTURE", "REHEARSAL MODE:", "Presence is observational. AUTO is authority.", `class="page draft-page"`, `id="draft-commissioner"`}
	check := func(state, body string) {
		t.Helper()
		for _, want := range common {
			if !strings.Contains(body, want) {
				t.Errorf("%s shell missing %q", state, want)
			}
		}
		for _, bad := range forbidden {
			if strings.Contains(body, bad) {
				t.Errorf("%s shell (manager) still renders %q", state, bad)
			}
		}
		if strings.Count(body, `class="live-dot`) != 1 {
			t.Errorf("%s shell renders %d live dots, want 1", state, strings.Count(body, `class="live-dot`))
		}
		start, end := strings.Index(body, `class="draft-command"`), strings.Index(body, `class="draft-panes"`)
		if start < 0 || end < 0 || end < start {
			t.Fatalf("%s shell: command region (%d) must precede the panes (%d)", state, start, end)
		}
		command := body[start:end]
		if n := strings.Count(body, `data-gosx-countdown-format=`); n != 1 || strings.Count(command, `data-gosx-countdown-format=`) != 1 {
			t.Errorf("%s shell: %d countdowns, want exactly one inside the command region", state, n)
		}
	}
	// Available and My team panes exist before and during the draft only; the
	// post-draft surface collapses them into the roster and a free-agent link.
	panes := []string{`draft-pane--available`, `draft-pane--mine`, `data-gosx-region-url="/draft/fragment/available?pos={value}"`, `data-gosx-region-url="/draft/fragment/queue"`, `data-gosx-set="$draft.available.pos"`}
	pre := renderDraftForUser(t, handler, seated)
	check("pre", pre)
	for _, want := range append([]string{`data-gosx-countdown-format="dhms"`, "Build your big board", "Check in now ↑", `id="ready-toggle"`, `id="autopick-toggle"`}, panes...) {
		if !strings.Contains(pre, want) {
			t.Errorf("pre-draft shell missing %q", want)
		}
	}
	// V1: the seat controls (ready check-in, autopick toggle) render inside
	// pane 3's Room tab container (draft-pane--mine, itself inside
	// draft-panes), never as a block between the command bar and the panes.
	if panesAt, mineAt, readyAt := strings.Index(pre, `class="draft-panes"`), strings.Index(pre, `draft-pane--mine`), strings.Index(pre, `id="ready-toggle"`); panesAt < 0 || mineAt < 0 || readyAt < 0 || readyAt < panesAt || readyAt < mineAt {
		t.Errorf("pre-draft shell: ready-toggle (%d) must render inside the Room tab (draft-panes at %d, draft-pane--mine at %d)", readyAt, panesAt, mineAt)
	}
	postDraftAction(t, handler, shellCommissioner, "draft-start", url.Values{"confirm": {"START"}})
	live := renderDraftForUser(t, handler, seated)
	check("live", live)
	// "ROUND 1" (Task 8): the command bar's round number sits inside its
	// own data-gosx-live-bind="pick.round" span now, so the literal text
	// no longer runs contiguous with "ROUND " — this checks the bound
	// span's own value instead.
	for _, want := range append([]string{`data-gosx-countdown-format="mm:ss"`, `data-pick-clock`, `data-pick-label`, `data-clock-state="RUNNING"`, "make-pick", `data-gosx-live-bind="pick.round">1<`}, panes...) {
		if !strings.Contains(live, want) {
			t.Errorf("live shell missing %q", want)
		}
	}
	// Item 9 (2026-08-30 review): the six tape filter chips render on the
	// Tape sub-view only — DraftHistoryHead sits outside the swapped
	// pane body, so it renders once per navigation, in step with
	// whichever sub-view the SAME response's history pane carries.
	if n := strings.Count(live, `class="chip" for="tape-filter-`); n != 6 {
		t.Errorf("tape (default) view: %d filter chips, want 6: %s", n, live)
	}
	board := renderDraftForUserPath(t, handler, seated, "/?view=board")
	if strings.Contains(board, `class="draft-history-filters"`) {
		t.Error("Board view must not render the tape's own filter chips (item 9)")
	}
	teamsView := renderDraftForUserPath(t, handler, seated, "/?view=teams")
	if strings.Contains(teamsView, `class="draft-history-filters"`) {
		t.Error("Teams view must not render the tape's own filter chips (item 9)")
	}
	drawer := renderDraftForUser(t, handler, shellCommissioner)
	for _, want := range []string{`id="draft-commissioner"`, `data-gosx-disclosure-modal`, `role="dialog"`, `aria-modal="true"`, `data-gosx-disclosure-target="#draft-commissioner"`, `data-gosx-disclosure-close="#draft-commissioner"`, `data-gosx-disclosure-initial-focus`, `value="60"`, `value="90"`, `value="120"`, `value="180"`, `value="300"`, `max="600"`, "Draft is running", "FORCE CURRENT PICK", "draft-undo", "previous_pick_token", "NOT SEEN may receive the short safety clock only after the two-minute boot grace"} {
		if !strings.Contains(drawer, want) {
			t.Errorf("commissioner drawer missing %q", want)
		}
	}
	// Finish the draft with forced autopicks (303 responses, no page renders).
	completeDraftByForcedAutopicks(t, handler, service)
	post := renderDraftForUser(t, handler, seated)
	check("post", post)
	for _, want := range []string{`class="draft-shell draft-shell--final"`, "FINAL LEDGER", ">FINAL<", `href="/draft/ledger.csv"`} {
		if !strings.Contains(post, want) {
			t.Errorf("post-draft shell missing %q", want)
		}
	}
}

// TestDraftShellFallbackModeRestoresRegionRefetch is review item 8's own
// render evidence (2026-08-30): DRAFT_LIVE_MODE=fallback restores the
// pre-Task-8 data-gosx-region*-driven refetch-and-swap wiring in the SAME
// page.gsx target mode otherwise uses — the command bar's own region
// (with its "-on" trigger) is the one piece every earlier fallback-mode
// contract test (TestDraftRegionContractIsPushDrivenAndMounted,
// fragment_test.go) already pins as present in the SOURCE; this proves
// it is what actually RENDERS once the env selects fallback.
func TestDraftShellFallbackModeRestoresRegionRefetch(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftShellFallbackModeRestoresRegionRefetchFixtureProcess$")
	cmd.Env = append(os.Environ(), "DRAFT_LIVE_MODE_FIXTURE=1", "DRAFT_LIVE_MODE=fallback", "DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=false", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=", "COMMISSIONER_EMAILS="+shellCommissioner)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fallback live-mode fixture process: %v\n%s", err, output)
	}
}

func TestDraftShellFallbackModeRestoresRegionRefetchFixtureProcess(t *testing.T) {
	if os.Getenv("DRAFT_LIVE_MODE_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", os.Getenv("DATA_FILE"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	service := league.Default()
	service.SetPlayerSource(livePool())
	const seated = "seated-fallback@example.com"
	if _, err := service.AssignManager(seated, "Seated Fallback"); err != nil {
		t.Fatal(err)
	}
	handler := buildDraftAuthenticatedHandler(t)
	body := renderDraftForUser(t, handler, seated)
	for _, want := range []string{
		`data-draft-live-mode="fallback"`,
		`data-gosx-region-url="/draft/fragment/command"`,
		`data-gosx-region-signal="$draft.state.refresh"`,
		`data-gosx-region-on="draft:pick draft:undo draft:clock draft:state"`,
		`data-gosx-region-url="/draft/fragment/queue"`,
		`data-gosx-region-on="draft:pick draft:undo draft:state draft:seat"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fallback shell missing %q", want)
		}
	}
	for _, forbidden := range []string{`data-gosx-live-mode="event"`, `data-gosx-live-src=`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("fallback shell must carry no live-mode root, found %q", forbidden)
		}
	}
}

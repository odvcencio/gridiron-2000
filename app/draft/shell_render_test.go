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
		`class="draft-shell`, `data-draft-live-mode="fallback"`, `class="draft-command"`,
		`data-gosx-region-url="/draft/fragment/command"`, `data-gosx-region-on="draft:pick draft:undo draft:clock draft:state"`,
		`draft-pane--history`, `data-gosx-region-url="/draft/fragment/tape"`,
		`id="tab-players"`, `id="tab-picks"`, `class="draft-tabbar"`,
		`aria-live="polite"`, `aria-live="off"`, `<nav class="pool-pagination" aria-label="Draft pool pages">`,
		`data-gosx-cue-toggle`, `data-gosx-cue-label-off="Sound off"`, // inert on v0.53.9: assert the attributes only
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
	postDraftAction(t, handler, shellCommissioner, "draft-start", url.Values{"confirm": {"START"}})
	live := renderDraftForUser(t, handler, seated)
	check("live", live)
	for _, want := range append([]string{`data-gosx-countdown-format="mm:ss"`, `data-pick-clock`, `data-pick-label`, `data-clock-state="RUNNING"`, "make-pick", "ROUND 1"}, panes...) {
		if !strings.Contains(live, want) {
			t.Errorf("live shell missing %q", want)
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

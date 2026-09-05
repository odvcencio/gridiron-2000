package draft

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestDraftPickBarNamesTheRealStateAtEveryDraftPhase is J1 F2's own render
// evidence. DraftPickBar's four branches used to share one shared "on
// clock with a queued pick" negation
// ((started && on_clock && queued) == false) for check-in AND both ready/
// autopick branches alike, so it never tested draft.started on its own —
// a live draft with the viewer on the clock but no queued player (or
// simply not their own turn) still fell into the FIRST false branch,
// "Before the room opens · Check in for draft night," the pre-draft-only
// copy a live manager saw mid-draft. This pins all three top-level draft
// states the bar must now tell apart: not started (check-in), live (the
// on-clock prompt, with or without a queued pick), and complete (a
// results link — the bar used to render nothing at all once complete).
func TestDraftPickBarNamesTheRealStateAtEveryDraftPhase(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftPickBarNamesTheRealStateAtEveryDraftPhaseFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"LARCH_PICKBAR_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
		"COMMISSIONER_EMAILS="+shellCommissioner,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("draft pick bar fixture process: %v\n%s", err, output)
	}
}

func TestDraftPickBarNamesTheRealStateAtEveryDraftPhaseFixtureProcess(t *testing.T) {
	if os.Getenv("LARCH_PICKBAR_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", os.Getenv("DATA_FILE"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	service := league.Default()
	service.SetPlayerSource(livePool())
	const seated = "larch-pickbar-seat@example.com"
	if _, err := service.AssignManager(seated, "Larch Pickbar Seat"); err != nil {
		t.Fatal(err)
	}
	handler := buildDraftAuthenticatedHandler(t)

	// State 1: not started, never checked in — the ONLY state that may
	// still show "Before the room opens · Check in for draft night".
	pre := renderDraftForUser(t, handler, seated)
	preBar := draftPickBarSlice(t, pre)
	if !strings.Contains(preBar, "Before the room opens") || !strings.Contains(preBar, "Check in for draft night") {
		t.Errorf("pre-draft pick bar must show the check-in prompt: %s", preBar)
	}

	// State 2: live, the seated manager on the clock (round 1 pick 1,
	// assuming AssignManager seats the first manager into the first
	// draft slot), no queued player. Before the fix this fell into the
	// SAME "Before the room opens" branch as state 1.
	postDraftAction(t, handler, shellCommissioner, "draft-start", url.Values{"confirm": {"START"}})
	live := renderDraftForUser(t, handler, seated)
	liveBar := draftPickBarSlice(t, live)
	if strings.Contains(liveBar, "Before the room opens") {
		t.Errorf("live pick bar must never show the pre-draft check-in prompt: %s", liveBar)
	}
	if !strings.Contains(liveBar, "Pick from the pool") && !strings.Contains(liveBar, "queue #1") {
		t.Errorf("live, on-the-clock pick bar must show either the no-queue or the queued-pick prompt: %s", liveBar)
	}

	// State 3: complete — the bar used to render nothing (viewer.has_seat
	// && draft.complete == false gated the WHOLE bar); it must now offer
	// a way back to the results.
	completeDraftByForcedAutopicks(t, handler, service)
	post := renderDraftForUser(t, handler, seated)
	postBar := draftPickBarSlice(t, post)
	if !strings.Contains(postBar, "/draft/results") {
		t.Errorf("post-draft pick bar must link to /draft/results: %s", postBar)
	}
	if strings.Contains(postBar, "Before the room opens") {
		t.Errorf("post-draft pick bar must not show the pre-draft check-in prompt: %s", postBar)
	}
}

// draftPickBarSlice returns just the .draft-pickbar element's own markup,
// so an assertion here cannot pass by matching text that actually lives
// in the command bar, the pool, or the tape instead.
func draftPickBarSlice(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `class="draft-pickbar"`)
	if start < 0 {
		t.Fatalf("no .draft-pickbar in body: %s", body)
	}
	end := strings.Index(body[start:], `</div>`)
	if end < 0 {
		t.Fatalf("unterminated .draft-pickbar in body: %s", body)
	}
	// Walk forward far enough to capture the bar's own nested forms
	// (the outer </div> closing the bar sits after them); a generous
	// fixed slice avoids a full HTML parse for a render-fixture test.
	sliceEnd := start + 2000
	if sliceEnd > len(body) {
		sliceEnd = len(body)
	}
	return body[start:sliceEnd]
}

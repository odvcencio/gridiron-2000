package draft

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestDraftCommandBarOffersPracticeBeforeTheDraft pins the visible practice
// entry (comb — sequoia, 2026-09-04 UX pass): before the draft starts, a
// seated manager sees "Practice the draft room →" in the command bar's own
// pre-draft block and in the phone sheet, not only inside the collapsed
// checklist. A viewer without a seat never sees it. Runs as a fixture
// process because league.Default() is process-wide.
func TestDraftCommandBarOffersPracticeBeforeTheDraft(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftCommandBarOffersPracticeBeforeTheDraftFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"DRAFT_PRACTICE_ENTRY_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("practice entry fixture process: %v\n%s", err, output)
	}
}

func TestDraftCommandBarOffersPracticeBeforeTheDraftFixtureProcess(t *testing.T) {
	if os.Getenv("DRAFT_PRACTICE_ENTRY_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	const seatedEmail = "seated-practice-entry@example.com"
	const seatlessEmail = "seatless-practice-entry@example.com"
	if _, err := service.AssignManager(seatedEmail, "Seated Practice Entry"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}
	if _, err := service.EnsureMember(seatlessEmail, "Seatless Practice Entry"); err != nil {
		t.Fatalf("EnsureMember: %v", err)
	}
	handler := buildDraftAuthenticatedHandler(t)

	seated := renderDraftForUser(t, handler, seatedEmail)
	if !strings.Contains(seated, "Practice now →") {
		t.Fatalf("fixture does not offer practice to a seated manager (checklist item missing); cannot pin the command-bar entry")
	}
	if got := strings.Count(seated, "draft-command__practice"); got < 2 {
		t.Errorf("seated pre-draft room renders %d command-bar practice links, want 2 (desktop block and phone sheet)", got)
	}
	if !strings.Contains(seated, `href="/draft/practice"`) {
		t.Errorf("practice links do not point at /draft/practice")
	}

	seatless := renderDraftForUser(t, handler, seatlessEmail)
	if strings.Contains(seatless, "draft-command__practice") {
		t.Errorf("seatless viewer sees a practice entry in the command bar")
	}
}

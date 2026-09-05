package admin

import (
	"os"
	"strings"
	"testing"
)

// TestAdminTextflowSeatLedgerUsesTextBlock pins the textflow wave
// (2026-09-05): the seat ledger's team/manager identity line
// (.commissioner-hq__attention-copy .section-index) renders through
// <TextBlock> instead of a plain <span> that CSS could only clip, not
// flow. maxLines=2 lets a long combined "abbreviation · team · manager"
// line wrap once instead of losing the manager's own name to an
// ellipsis at one line.
func TestAdminTextflowSeatLedgerUsesTextBlock(t *testing.T) {
	body := renderAdminPage(t)
	at := strings.Index(body, "commissioner-hq__attention-copy")
	if at < 0 {
		t.Fatal("no .commissioner-hq__attention-copy in the rendered admin page")
	}
	segment := body[at:]
	if end := strings.Index(segment, "</span>"); end > 0 {
		segment = segment[:end]
	}
	if !strings.Contains(segment, "data-gosx-text-layout") {
		t.Errorf("seat ledger identity line missing data-gosx-text-layout: %s", segment)
	}
	if !strings.Contains(segment, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("seat ledger identity line missing max-lines=2: %s", segment)
	}
}

// TestAdminTextflowChecklistTitleUsesTextBlock pins the pre-draft-style
// runbook checklist's own item title (span.checklist-item__text > strong)
// rendering through <TextBlock maxLines={2}> instead of an unbounded
// plain <strong> (there was no CSS truncation to retire here — the
// convert is for consistency with the flow/clamp contract every other
// checklist title on the page now follows).
func TestAdminTextflowRunbookStepTitleUsesTextBlock(t *testing.T) {
	body := renderAdminPage(t)
	at := strings.Index(body, "About an hour early, drop the seats nobody claimed")
	if at < 0 {
		t.Fatal("runbook step 1 title text not found in the rendered admin page")
	}
	segment := body[:at]
	openAt := strings.LastIndex(segment, "<strong")
	if openAt < 0 {
		t.Fatal("runbook step 1 title has no enclosing <strong")
	}
	tag := body[openAt:at]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("runbook step title missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("runbook step title missing max-lines=2: %s", tag)
	}
}

// TestAdminTextflowResetHeadingUsesTextBlock pins the danger-zone reset
// card headings (both "Reset <league>'s draft" and "Reset <league> to a
// blank league") rendering through <TextBlock maxLines={2}>.
func TestAdminTextflowResetHeadingUsesTextBlock(t *testing.T) {
	body := renderAdminPage(t)
	for _, want := range []string{"Reset THE LEAGUE&#39;s draft", "Reset THE LEAGUE to a blank league"} {
		at := strings.Index(body, want)
		if at < 0 {
			t.Fatalf("reset heading %q not found", want)
		}
		segment := body[:at]
		openAt := strings.LastIndex(segment, "<strong")
		if openAt < 0 {
			t.Fatalf("reset heading %q has no enclosing <strong", want)
		}
		tag := body[openAt:at]
		if !strings.Contains(tag, "data-gosx-text-layout") || !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
			t.Errorf("reset heading %q missing TextBlock max-lines=2 attrs: %s", want, tag)
		}
	}
}

// TestAdminTextflowInviteAndAnnouncementRowsSourceUsesTextBlock covers
// invite rows and announcement rows, which the built-in demo league
// fixture renders empty (no invites, no announcements posted) — the
// render assertions above already prove the runtime attribute shape for
// a populated <TextBlock>, so this checks the source template directly
// for the two row shapes gap-audit's brief named explicitly, the same
// source-level pinning style TestSignedInConsoleBranchesOnFantasySeat
// (app/login) already uses for a branch the default fixture cannot
// reach either.
func TestAdminTextflowInviteAndAnnouncementRowsSourceUsesTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`TextBlock as="b" class="mono" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={2} overflow="ellipsis" text={invite.email}`,
		`TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={2} overflow="ellipsis" text={invite.status_detail}`,
		`TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={note.body}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("admin page.gsx missing textflow conversion %q", want)
		}
	}
}

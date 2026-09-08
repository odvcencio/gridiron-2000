package draft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestDraftTextflowPreflightChecklistUsesTextBlock pins the textflow wave
// (2026-09-05): DraftPreflight's own checklist item titles/sentences
// (.checklist-item__text > strong/small) render through <TextBlock>
// instead of a plain, unbounded pair — "Build your big board" clamps at
// two lines and its detail sentence flows.
func TestDraftTextflowPreflightChecklistUsesTextBlock(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	const seatedEmail = "textflow-preflight@example.com"
	if _, err := service.AssignManager(seatedEmail, "Textflow Preflight"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	handler := buildDraftAuthenticatedHandler(t)
	page := renderDraftForUser(t, handler, seatedEmail)

	at := strings.Index(page, "Build your big board")
	if at < 0 {
		t.Fatalf("pre-draft checklist title not found: %s", page)
	}
	segment := page[:at]
	openAt := strings.LastIndex(segment, "<strong")
	if openAt < 0 {
		t.Fatal("checklist title has no enclosing <strong")
	}
	tag := page[openAt:at]
	if !strings.Contains(tag, "data-gosx-text-layout") {
		t.Errorf("checklist title missing data-gosx-text-layout: %s", tag)
	}
	if !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("checklist title missing max-lines=2: %s", tag)
	}

	detailAt := strings.Index(page, "Rank your targets now.")
	if detailAt < 0 {
		t.Fatal("checklist detail sentence not found")
	}
	detailSeg := page[:detailAt]
	detailOpenAt := strings.LastIndex(detailSeg, "<small")
	if detailOpenAt < 0 {
		t.Fatal("checklist detail has no enclosing <small")
	}
	detailTag := page[detailOpenAt:detailAt]
	if !strings.Contains(detailTag, "data-gosx-text-layout") {
		t.Errorf("checklist detail sentence missing data-gosx-text-layout: %s", detailTag)
	}
}

// TestDraftTextflowPoolStatusBannerUsesTextBlock pins the pool-status
// banner (the "CACHED SNAPSHOT: ..." line, DraftCommandBar) rendering
// through <TextBlock maxLines={1}> instead of the old white-space:
// nowrap + text-overflow: ellipsis CSS clip (public/styles.css
// .draft-command__banner-line, now retired under the textflow comb).
func TestDraftTextflowPoolStatusBannerSourceUsesTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	if !strings.Contains(page, `TextBlock as="p" class="draft-command__banner-line" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis"`) {
		t.Error("draft page.gsx pool-status banner line is not a maxLines=1 TextBlock")
	}
}

// TestDraftTextflowPracticeStripSourceUsesTextBlock pins DraftPracticeStrip's
// full sentence and phone short line (app/draft/page.gsx) rendering through
// <TextBlock>: the phone short line retires .draft-practice-strip__line's
// own -webkit-line-clamp: 2 CSS (public/styles.css, now overridden under
// the textflow comb) in favor of TextBlock maxLines={2}.
func TestDraftTextflowPracticeStripSourceUsesTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`class="draft-practice-strip__text"`,
		`class="draft-practice-strip__line mono"`,
		`maxLines={2}`,
		`text={props.Data.practice.summary_full}`,
		`text={props.Data.practice.summary_short}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("draft practice strip missing textflow conversion %q", want)
		}
	}
	if !strings.Contains(page, "<TextBlock") {
		t.Error("draft practice strip has no <TextBlock element at all")
	}
}

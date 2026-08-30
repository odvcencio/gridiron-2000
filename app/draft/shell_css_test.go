package draft

import (
	"os"
	"strings"
	"testing"
)

func TestDraftShellStylesheetSection(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, want := range []string{
		"/* Draft war room */", "body:has(.draft-shell) { overflow: hidden", "body:has(.draft-shell) .site-rail { width: 4rem",
		".draft-shell {", "height: 100dvh", ".draft-command {", ".draft-command__clock {", "@keyframes clock-pulse",
		".draft-panes {", "grid-template-columns: 360px minmax(0, 1fr) 300px", ".draft-pane__body {", "overflow-y: auto",
		".draft-shell:has(#tab-picks:checked) .draft-pane--history", ".draft-tabbar__tab {", ".draft-drawer {",
		".pos-QB {", ".pos-RB {", ".pos-WR {", ".pos-TE {", ".pos-K {", ".pos-DST {",
		`.avail-row[data-taken="true"]`, `.q-row[data-taken="true"]`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
	last := strings.LastIndex(css, "@media (max-width: 38rem)")
	if !strings.Contains(css[last:], ".site-frame .draft-tabbar__tab") || !strings.Contains(css[last:], ".site-frame .draft-command__sound") {
		t.Error("the last 38rem block lacks the draft touch-target rules")
	}
	if strings.Count(css[strings.Index(css, "/* Draft war room */"):], "@media (max-width: 38rem)") != 0 {
		t.Error("the draft section must not add a 38rem block after the touch-baseline block")
	}
}

// TestDraftPaneInnerSegmentsSwitchWithCSS pins the Task 6 review's R3 fix:
// the history pane's Tape/Board/Teams segment and the my-team pane's
// Queue/Roster/Room segment each need their own three panel-switching
// rules (both radio groups previously rendered every panel at once, with
// no :has() pairing at all).
func TestDraftPaneInnerSegmentsSwitchWithCSS(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, want := range []string{
		".draft-pane--history:has(#history-tape:checked) .draft-history__view--tape",
		".draft-pane--history:has(#history-board:checked) .draft-history__view--board",
		".draft-pane--history:has(#history-teams:checked) .draft-history__view--teams",
		".draft-mine:has(#mine-queue:checked) .draft-mine__view--queue",
		".draft-mine:has(#mine-roster:checked) .draft-mine__view--roster",
		".draft-mine:has(#mine-room:checked) .draft-mine__view--room",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing panel-switching rule %q", want)
		}
	}
}

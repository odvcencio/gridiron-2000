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

// TestDraftPaneInnerSegmentsSwitchWithCSS pins the Task 6 review's R3 fix
// for the surviving radio-group segment (My Team's Queue/Roster/Room) and
// item 1a's own replacement for the history pane's Tape/Board/Teams
// segment: since the server now renders exactly one .draft-history__view
// at a time (DraftHistory's ShowTape/ShowBoard/ShowTeams), and the
// segment itself is three plain data-gosx-link navigations rather than a
// radio group (DraftHistoryHead's own doc comment), there is no
// panel-switching CSS rule to pin for it — its absence is the fix.
func TestDraftPaneInnerSegmentsSwitchWithCSS(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, want := range []string{
		".draft-mine:has(#mine-queue:checked) .draft-mine__view--queue",
		".draft-mine:has(#mine-roster:checked) .draft-mine__view--roster",
		".draft-mine:has(#mine-room:checked) .draft-mine__view--room",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing panel-switching rule %q", want)
		}
	}
	for _, obsolete := range []string{"#history-tape:checked", "#history-board:checked", "#history-teams:checked"} {
		if strings.Contains(css, obsolete) {
			t.Errorf("stylesheet still references %q — item 1a removed the history segment's native radios (DraftHistoryHead is three plain data-gosx-link navigations now)", obsolete)
		}
	}
}

// TestDraftBoardTabExpandsTheHistoryPane is T2 (2026-08-30 polish pass),
// updated for item 1a (2026-08-30 review): while the Board view is
// active, the history pane spans the workspace (the available pane
// hides) so every team column shows at 1440px, matching
// PickBoard.dc.html. The trigger is now data-history-board="true" on
// .draft-panes (Page(), page.gsx), a plain attribute Go sets from the
// same ShowBoard flag DraftHistory branches on, not a :has(#history-
// board:checked) selector reaching for a radio that no longer exists.
func TestDraftBoardTabExpandsTheHistoryPane(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, want := range []string{
		`.draft-panes[data-history-board="true"] {`,
		`.draft-panes[data-history-board="true"] .draft-pane--available {`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing T2 board-expand rule %q", want)
		}
	}
}

// TestDraftTapeRoundHeaderIsOneLine is T1 (2026-08-30 polish pass): the
// sticky round header's own text never wraps — the pre-polish three-span
// layout could split "ROUND" from its number across two lines at the
// pane's fixed 360px width.
func TestDraftTapeRoundHeaderIsOneLine(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	ruleStart := strings.Index(css, ".tape-round {")
	if ruleStart < 0 {
		t.Fatal("stylesheet missing .tape-round rule")
	}
	ruleEnd := strings.Index(css[ruleStart:], "}")
	rule := css[ruleStart : ruleStart+ruleEnd]
	if !strings.Contains(rule, "white-space: nowrap") {
		t.Errorf(".tape-round must set white-space: nowrap (T1, one line, never a wrap): %s", rule)
	}
}

// TestDraftTapeRowTogglePassesTheTouchBaseline is T3 (2026-08-30 polish
// pass): the row's own <summary> — its DETAIL toggle, T3 merges them —
// meets the 44px (2.75rem) touch target in both dimensions, and the
// decorative chevron marker never itself claims a competing hit target
// (min-width/height unset, so only the summary's own box counts).
func TestDraftTapeRowTogglePassesTheTouchBaseline(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	ruleStart := strings.Index(css, ".tape-row__summary {")
	if ruleStart < 0 {
		t.Fatal("stylesheet missing .tape-row__summary rule")
	}
	ruleEnd := strings.Index(css[ruleStart:], "}")
	rule := css[ruleStart : ruleStart+ruleEnd]
	if !strings.Contains(rule, "min-height: 2.75rem") {
		t.Errorf(".tape-row__summary must set min-height: 2.75rem (T3, the 44px touch target): %s", rule)
	}
	if !strings.Contains(css, ".tape-row__summary::-webkit-details-marker") && !strings.Contains(css, ".tape-row__summary::marker") {
		t.Error("stylesheet must hide the native <summary> disclosure triangle on .tape-row__summary")
	}
}

// TestDraftPhoneTeamsTabRevealsTheHistoryPane is P8 (2026-08-30 review),
// updated for item 1a: the phone bottom tab bar's Teams tab still reveals
// the history pane the same way the Picks tab does — #tab-teams:checked
// is a real radio in the "draft-tab" group either way (DraftMobileTabs,
// page.gsx). It no longer needs a CSS rule to also force a sub-view
// inside that pane: #tab-teams's own checked state is now driven by the
// same server-computed ShowTeams flag that decided which ONE
// .draft-history__view--X the server rendered in the first place, so the
// two can never disagree, and the old force-display override
// (page.gsx's DraftMobileTabs doc comment has the detail) is gone.
func TestDraftPhoneTeamsTabRevealsTheHistoryPane(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	if !strings.Contains(css, "#tab-teams:checked) .draft-pane--history") {
		t.Error("stylesheet missing the #tab-teams:checked rule that reveals .draft-pane--history")
	}
	for _, obsolete := range []string{
		"#tab-teams:checked) .draft-history__view--tape",
		"#tab-teams:checked) .draft-history__view--board",
	} {
		if strings.Contains(css, obsolete) {
			t.Errorf("stylesheet still references %q — item 1a made this force-display override unnecessary (DraftMobileTabs' own doc comment, page.gsx)", obsolete)
		}
	}
}

// TestDraftByTeamPlayerNameStacksOverPosAndNFL is item 7 (2026-08-30
// review): the By Team tab's own compact rows must restore the stacked
// player-name-over-"POS · NFL" layout .tape-row__player lost when it
// became a flex row for the main tape row's own T4 layout (name,
// position chip, and AUTO/COMM tag sharing one line there).
func TestDraftByTeamPlayerNameStacksOverPosAndNFL(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	ruleStart := strings.Index(css, ".team-column__picks .tape-row__player {")
	if ruleStart < 0 {
		t.Fatal("stylesheet missing .team-column__picks .tape-row__player rule")
	}
	ruleEnd := strings.Index(css[ruleStart:], "}")
	rule := css[ruleStart : ruleStart+ruleEnd]
	if !strings.Contains(rule, "display: block") {
		t.Errorf(".team-column__picks .tape-row__player must set display: block (item 7, stacked over POS · NFL): %s", rule)
	}
}

// TestDraftPhoneNoticeStaysOneLine is P6 (2026-08-30 review, superseding
// this task's own T5 draft): the offline-pool/paused/rehearsal banner
// stays a single nowrap/ellipsis line at every width, phone included — no
// phone-width override reintroduces wrapping or a multi-line clamp (a
// two-line -webkit-line-clamp attempt earlier in this same pass did not
// reliably clip on a grid item and let its overflow bleed into the tab
// segment below it; P6's one-line shape sidesteps that engine quirk
// entirely). The base (non-media) rule is the one and only source of
// truth for this now.
func TestDraftPhoneNoticeStaysOneLine(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	ruleStart := strings.Index(css, ".draft-command__banner {")
	if ruleStart < 0 {
		t.Fatal("stylesheet missing .draft-command__banner rule")
	}
	ruleEnd := strings.Index(css[ruleStart:], "}")
	rule := css[ruleStart : ruleStart+ruleEnd]
	for _, want := range []string{"white-space: nowrap", "overflow: hidden", "text-overflow: ellipsis"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".draft-command__banner must set %q (P6, one line, never clipped mid-glyph): %s", want, rule)
		}
	}
	last := strings.LastIndex(css, "@media (max-width: 56.1875rem)")
	if last < 0 {
		t.Fatal("stylesheet missing the phone/tablet @media (max-width: 56.1875rem) block")
	}
	// Bounded by the next top-level "@media" (or EOF): good enough to prove
	// no SECOND .draft-command__banner rule reappears inside this block,
	// the same imprecise-but-sufficient bound TestDraftPhoneTeamsTabSelects
	// TheTeamsSubView above already relies on.
	block := css[last:]
	if next := strings.Index(block[len("@media (max-width: 56.1875rem)"):], "\n@media"); next >= 0 {
		block = block[:next+len("@media (max-width: 56.1875rem)")]
	}
	if strings.Contains(block, ".draft-command__banner {") {
		t.Error("P6: no phone-width override of .draft-command__banner may remain (it must inherit the base one-line rule unchanged)")
	}
}

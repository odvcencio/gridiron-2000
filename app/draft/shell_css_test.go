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
		"/* Draft war room */", "body:has(.draft-shell) { overflow: hidden", "body:has(.draft-shell) .site-rail:not(:hover, :focus-within) { width: 4rem",
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
	// Item 1 (wave 7b mobile-foundation audit — larch): the mobile
	// interaction baseline (public/styles.css) used to be one single
	// "@media (max-width: 38rem)" block covering both the 44px touch-target
	// rules AND every phone-width content-layout rule that came after them
	// in the same file (matchups summary-first stacking, etc.) — so "the
	// LAST literal '@media (max-width: 38rem)' in the file" and "the touch-
	// target block" were the same block, and this test's own literal-text
	// search found it correctly either way. Item 1 split the touch-target
	// rules into their own "@media (pointer: coarse), (hover: none), (max-
	// width: 38rem)" query (so a landscape phone — still a coarse pointer
	// past 38rem — keeps its 44px floor), leaving every unrelated phone-
	// width content rule (this repo's own, plus this wave's new .page-
	// action-bar phone-display rule) in plain "@media (max-width: 38rem)"
	// blocks that now legitimately follow the touch-target query in file
	// order. The touch-target query is what this test actually needs to
	// find; searching for its own literal string (rather than the bare
	// "@media (max-width: 38rem)" substring, which the touch-target
	// query's text no longer contains as an exact match) is the fix.
	const touchFloorQuery = "@media (pointer: coarse), (hover: none), (max-width: 38rem)"
	last := strings.LastIndex(css, touchFloorQuery)
	if last < 0 {
		t.Fatalf("no %q block found", touchFloorQuery)
	}
	touchBlockEnd := strings.Index(css[last:], "\n}\n")
	if touchBlockEnd < 0 {
		t.Fatal("could not find the end of the touch-target query block")
	}
	touchBlock := css[last : last+touchBlockEnd]
	if !strings.Contains(touchBlock, ".site-frame .draft-tabbar__tab") || !strings.Contains(touchBlock, ".site-frame .draft-command__sound") {
		t.Error("the touch-target query lacks the draft touch-target rules")
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
// pass), updated for item 1 (2026-08-30 review): the row's own
// <a class="tape-row__summary" data-gosx-link> — its DETAIL toggle —
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
	for _, obsolete := range []string{".tape-row__summary::-webkit-details-marker", "details[open] > .tape-row__summary"} {
		if strings.Contains(css, obsolete) {
			t.Errorf("stylesheet still references %q — item 1 replaced <details><summary> with a plain <article><a> (no native disclosure marker to hide)", obsolete)
		}
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

// TestDraftPhoneNoticeAllowsTwoLinesAtPhoneWidth is P1-10 (UI pass
// 2026-08-30 review), superseding P6 above: a phone has no hover, so P6's
// one-line nowrap/ellipsis banner left its title= attribute (the only
// place the rest of a truncated notice was readable) unreachable at
// phone width. A phone-width override allows a second line, via
// max-height + overflow: hidden — the same two-line fallback
// .board-grid__name already uses, not -webkit-line-clamp, since P6's own
// comment already found line-clamp unreliable on a CSS grid item in the
// engine the screenshot pass measured against.
//
// D16 (2026-09-02 draft-week audit) superseded P1-10's own "one line at
// desktop" half in turn: a genuinely long banner message (a paused/
// rehearsal/offline-pool notice, service.go's own priority chain) still
// silently dropped whatever text ran past the command bar's width at
// 1280px, exactly the truncation problem P1-10 already fixed for phone
// width — this test's own desktop assertion now checks for the same
// wrap the phone override already used, not nowrap/ellipsis.
func TestDraftPhoneNoticeAllowsTwoLinesAtPhoneWidth(t *testing.T) {
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
	for _, want := range []string{"white-space: normal", "overflow: hidden"} {
		if !strings.Contains(rule, want) {
			t.Errorf("the base .draft-command__banner rule must set %q (D16: wraps, no longer truncates, at desktop): %s", want, rule)
		}
	}
	for _, unwanted := range []string{"white-space: nowrap", "text-overflow: ellipsis"} {
		if strings.Contains(rule, unwanted) {
			t.Errorf("the base .draft-command__banner rule must not set %q (D16: no ellipsis truncation at 1280px): %s", unwanted, rule)
		}
	}
	// Wave 7 (multiple workers, each appending their own page-scoped
	// rules in a file-end block per the shared append convention) added
	// several more "@media (max-width: 56.1875rem)" blocks after this
	// one, so the single LAST such block is no longer reliably the one
	// carrying this override (round-2 fix, 2026-09-02: the ash/wave-7b
	// worktree's own merge surfaced this false failure). Scan every
	// occurrence of that media query and use whichever one actually
	// contains .draft-command__banner, rather than assuming it is last.
	mediaQuery := "@media (max-width: 56.1875rem)"
	var block string
	for search := css; ; {
		next := strings.Index(search, mediaQuery)
		if next < 0 {
			t.Fatal("stylesheet missing a phone/tablet @media (max-width: 56.1875rem) block that overrides .draft-command__banner")
		}
		candidate := search[next:]
		if end := strings.Index(candidate[len(mediaQuery):], "\n@media"); end >= 0 {
			candidate = candidate[:end+len(mediaQuery)]
		}
		if strings.Contains(candidate, ".draft-command__banner {") {
			block = candidate
			break
		}
		search = search[next+len(mediaQuery):]
	}
	overrideStart := strings.Index(block, ".draft-command__banner {")
	overrideEnd := strings.Index(block[overrideStart:], "}")
	override := block[overrideStart : overrideStart+overrideEnd]
	for _, want := range []string{"white-space: normal", "max-height"} {
		if !strings.Contains(override, want) {
			t.Errorf("the phone-width .draft-command__banner override must set %q: %s", want, override)
		}
	}
}

// TestDraftTapeFilterChipsStylesheetRules is item 9's own CSS test
// (2026-08-30 review): the six position/mine filter chips and the CSS
// rules that hide a non-matching .tape-row when one is checked, scoped
// to the tape sub-view only.
func TestDraftTapeFilterChipsStylesheetRules(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, want := range []string{
		".draft-history-filters {",
		".draft-shell .draft-history-filters input:checked + .chip {",
		`.draft-pane--history:has(#tape-filter-qb:checked) .draft-history__view--tape .tape-row:not([data-position="QB"])`,
		`.draft-pane--history:has(#tape-filter-rb:checked) .draft-history__view--tape .tape-row:not([data-position="RB"])`,
		`.draft-pane--history:has(#tape-filter-wr:checked) .draft-history__view--tape .tape-row:not([data-position="WR"])`,
		`.draft-pane--history:has(#tape-filter-te:checked) .draft-history__view--tape .tape-row:not([data-position="TE"])`,
		`.draft-pane--history:has(#tape-filter-mine:checked) .draft-history__view--tape .tape-row:not([data-mine="true"])`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing tape-filter rule %q", want)
		}
	}
}

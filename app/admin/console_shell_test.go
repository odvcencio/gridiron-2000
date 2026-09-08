package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file pins wave-C's console-shell fixes (2026-09-08): the section
// strip's sticky behavior and chip/heading match (J4 F5, F18, F19), the
// task board's missing jobs and boxed lineup row (J4 F17), status-chip
// sizing (J4 F20), the invite count's own label (J2 F21), the clock and
// playoff cards' empty cells (J2 F17), the clock's mm:ss labels (J2 F32),
// and the schedule seed's disclosure (J2 F33).

// stripAnchor finds the strip's <a href="#admin-ID" ...>Label</a> markup
// for one jump target inside the section-strip nav's own source slice.
func stripAnchorLabel(t *testing.T, strip, id string) string {
	t.Helper()
	re := regexp.MustCompile(`href="#admin-` + regexp.QuoteMeta(id) + `"[^>]*>([^<]+)</a>`)
	m := re.FindStringSubmatch(strip)
	if m == nil {
		t.Fatalf("admin-section-strip has no anchor for %q", id)
	}
	return strings.TrimSpace(m[1])
}

func adminSectionStripSource(t *testing.T) string {
	t.Helper()
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	start := strings.Index(source, `<nav class="admin-section-strip" aria-label="Jump to a console section">`)
	if start < 0 {
		t.Fatal("page.gsx is missing the admin-section-strip nav")
	}
	end := strings.Index(source[start:], "</nav>")
	if end < 0 {
		t.Fatal("admin-section-strip nav never closes")
	}
	return source[start : start+end]
}

// TestAdminSectionStripChipsMatchLandedHeadings pins J4 F18: nine of
// thirteen chips used to land on a heading that read nothing like the
// chip's own word. Every chip's label must now equal (or, for the
// draft/runbook section whose heading changes by phase, be contained in)
// the <h2> text of the section its href targets.
func TestAdminSectionStripChipsMatchLandedHeadings(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	strip := adminSectionStripSource(t)

	headingFor := func(id string) string {
		re := regexp.MustCompile(`<h2 id="admin-` + regexp.QuoteMeta(id) + `-heading"[^>]*>([\s\S]*?)</h2>`)
		m := re.FindStringSubmatch(source)
		if m == nil {
			t.Fatalf("no <h2 id=%q-heading> found", id)
		}
		return m[1]
	}

	cases := []struct {
		id    string
		label string
		// wantWithin, when set, replaces an exact-match check: the chip's
		// word must appear inside the (phase-dependent) heading instead of
		// equaling it outright.
		wantWithin string
	}{
		{id: "draft-control", label: "Runbook", wantWithin: "runbook"},
		{id: "schedule", label: "Schedule"},
		{id: "week-close", label: "Week close"},
		{id: "playoffs", label: "Playoffs"},
		{id: "seats", label: "Seats"},
		{id: "invites", label: "Invites"},
		{id: "draft-order", label: "Draft order"},
		{id: "data", label: "Data"},
		{id: "clock", label: "Clock"},
		{id: "roster", label: "Roster"},
		{id: "announcements", label: "Notes"},
		{id: "backup", label: "Backup"},
		{id: "danger", label: "Danger zone"},
	}
	for _, c := range cases {
		chipLabel := stripAnchorLabel(t, strip, c.id)
		if chipLabel != c.label {
			t.Errorf("strip chip for %q reads %q, want %q", c.id, chipLabel, c.label)
		}
		heading := headingFor(c.id)
		if c.wantWithin != "" {
			if !strings.Contains(strings.ToLower(heading), c.wantWithin) {
				t.Errorf("section %q heading %q does not contain %q", c.id, heading, c.wantWithin)
			}
			continue
		}
		if !strings.Contains(heading, c.label) {
			t.Errorf("section %q heading %q does not contain chip label %q", c.id, heading, c.label)
		}
	}
}

// TestAdminSectionStripUsesPlainAnchors pins J4 F25: every strip href is a
// same-document #anchor, not a full "/admin?section=..." navigation — the
// slow path the finding measured a ~1,600px, several-second-long landing
// lurch on.
func TestAdminSectionStripUsesPlainAnchors(t *testing.T) {
	strip := adminSectionStripSource(t)
	if strings.Contains(strip, "?section=") {
		t.Error("admin-section-strip still carries a ?section= navigation; F25 requires plain #anchor hrefs")
	}
	if count := strings.Count(strip, `aria-current={data.admin_section ==`); count != 13 {
		t.Errorf("admin-section-strip has %d aria-current bindings, want 13 (one per chip, F18's current-section marker)", count)
	}
}

// TestAdminSectionEyebrowsShareOneNumberingScheme pins J4 F19: the
// console used to carry two numbering schemes — real "NN // NAME" numbers
// for some sections, and the literal word "SEASON" standing in for a
// number on others. Every routine section's eyebrow is now a real number
// in one reading-order sequence; only the Danger Zone keeps its
// deliberately out-of-sequence 99.
func TestAdminSectionEyebrowsShareOneNumberingScheme(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if strings.Contains(source, "SEASON //") {
		t.Error("page.gsx still carries a \"SEASON //\" eyebrow; F19 requires one numbered scheme")
	}
	want := []string{
		`<span class="section-index">00 // SEASON OPERATIONS</span>`,
		`<span class="section-index">00 // DRAFT NIGHT</span>`,
		`<span class="section-index">01 // SCHEDULE</span>`,
		`<span class="section-index">02 // WEEK CLOSE</span>`,
		`<span class="section-index">03 // WAIVER RUN</span>`,
		`<span class="section-index">04 // PLAYOFFS</span>`,
		`<span class="section-index">05 // SEATS</span>`,
		`<span class="section-index">06 // INVITES</span>`,
		`<span class="section-index">07 // DRAFT ORDER</span>`,
		`<span class="section-index">08 // PLAYER DATA</span>`,
		`<span class="section-index">09 // DRAFT CLOCK</span>`,
		`<span class="section-index">10 // ROSTER SHAPE</span>`,
		`<span class="section-index">11 // ANNOUNCEMENTS</span>`,
		`<span class="section-index">12 // BACKUP</span>`,
		`<span class="section-index">99 // DANGER ZONE</span>`,
	}
	for _, snippet := range want {
		if !strings.Contains(source, snippet) {
			t.Errorf("page.gsx missing eyebrow %q", snippet)
		}
	}
}

// TestAdminTaskBoardListsWaiversTradesScoringAndLog pins J4 F17: the
// board used to omit four Sunday jobs the console holds (waivers, trade
// review, scoring, the league log), and rendered the lineup-intervention
// job as a bare disclosure triangle instead of a boxed row like its
// siblings.
func TestAdminTaskBoardListsWaiversTradesScoringAndLog(t *testing.T) {
	body := renderAdminPage(t)
	for _, want := range []string{
		"Run waivers",
		"Review a trade",
		"Change scoring",
		"Read the league log",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task board missing job %q", want)
		}
	}
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	liStart := strings.Index(source, `id="admin-task-nav-lineup"`)
	if liStart < 0 {
		t.Fatal("page.gsx is missing #admin-task-nav-lineup")
	}
	// The disclosure's own <li> wrapper must sit inside the "People and
	// access" group's <ul>, and its <summary> must wear the same
	// .admin-task-nav__link box every other row uses.
	window := source[max(0, liStart-400) : liStart+400]
	if !strings.Contains(window, `<li class="admin-task-nav__item">`) {
		t.Error("lineup-intervention row is not wrapped in .admin-task-nav__item like the other rows")
	}
	if !strings.Contains(window, `class="admin-task-nav__link admin-task-nav__lineup-summary"`) {
		t.Error("lineup-intervention summary does not carry .admin-task-nav__link")
	}
}

// TestAdminClockAndPlayoffEmptyCellsHaveFallbackText pins J2 F17: three
// console cells rendered as a label with nothing beside it — an unarmed
// clock's Deadline, and the Playoff Truth card's Source before any
// bracket exists. Both now print a stated reason instead of nothing.
func TestAdminClockAndPlayoffEmptyCellsHaveFallbackText(t *testing.T) {
	body := renderAdminPage(t)
	if !strings.Contains(body, "no pick is armed") {
		t.Error("unarmed Deadline cell has no fallback text")
	}
	if !strings.Contains(body, "no bracket yet") {
		t.Error("empty playoff-truth Source cell has no fallback text")
	}
	if strings.Contains(body, "<span>Deadline</span>\n\t\t\t\t\t\t\t\t<b class=\"mono\"></b>") {
		t.Error("Deadline cell can still render fully empty")
	}
}

// TestAdminClockDurationAndRemainingUseMMSSLabels pins J2 F32: the
// console printed "120 S" / raw seconds for the same quantity the room
// itself shows as "2:00" — this now reads the existing duration_label /
// remaining_label helpers (service.go) instead of formatting seconds
// itself a second way.
func TestAdminClockDurationAndRemainingUseMMSSLabels(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if strings.Contains(source, "{data.clock.duration_seconds}") {
		t.Error("page.gsx still prints the raw duration_seconds; use duration_label")
	}
	if strings.Contains(source, "{data.clock.remaining_seconds}") {
		t.Error("page.gsx still prints the raw remaining_seconds; use remaining_label")
	}
	if !strings.Contains(source, "{data.clock.duration_label}") || !strings.Contains(source, "{data.clock.remaining_label}") {
		t.Error("page.gsx is missing the mm:ss duration_label/remaining_label bindings")
	}
}

// TestAdminInviteCountNamesWhatItCounts pins J2 F21: the invites card's
// own count and the league-status badge above it counted two different
// things (admitted addresses vs. claimed seats) with no label saying so.
func TestAdminInviteCountNamesWhatItCounts(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "admitted addresses hold a seat") {
		t.Error("invites card does not name what its count covers")
	}
}

// TestAdminScheduleSeedBehindDisclosure pins J2 F33: a nineteen-digit
// machine seed used to sit in the main schedule stat grid; it now lives
// behind its own closed "Redraw trail" disclosure.
func TestAdminScheduleSeedBehindDisclosure(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	detailsStart := strings.Index(source, `<details class="admin-redraw-trail">`)
	if detailsStart < 0 {
		t.Fatal("page.gsx is missing the admin-redraw-trail disclosure")
	}
	detailsEnd := strings.Index(source[detailsStart:], "</details>")
	if detailsEnd < 0 {
		t.Fatal("admin-redraw-trail disclosure never closes")
	}
	block := source[detailsStart : detailsStart+detailsEnd]
	if !strings.Contains(block, "Redraw trail") {
		t.Error("admin-redraw-trail disclosure is missing its own summary label")
	}
	if !strings.Contains(block, "{data.schedule.seed}") {
		t.Error("admin-redraw-trail disclosure is missing the seed value")
	}
	// The main stat grid (above the disclosure) must no longer show the
	// seed directly.
	gridStart := strings.Index(source, `<div class="pool-stats">`)
	if gridStart < 0 || gridStart > detailsStart {
		t.Fatal("could not locate the schedule pool-stats grid ahead of the disclosure")
	}
	grid := source[gridStart:detailsStart]
	if strings.Contains(grid, "{data.schedule.seed}") {
		t.Error("schedule pool-stats grid still prints the seed directly, outside the disclosure")
	}
}

// TestAdminSectionStripAndPositionChipStylesExist pins the CSS half of
// J4 F5 (sticky at every width) and J4 F20 (status chips size to
// content and never clip).
func TestAdminSectionStripAndPositionChipStylesExist(t *testing.T) {
	styles, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	blockStart := strings.Index(css, "/* comb — cedar (2026-09-08 wave C)")
	if blockStart < 0 {
		t.Fatal("styles.css is missing the wave C — cedar comb block")
	}
	block := css[blockStart:]
	for _, want := range []string{
		".admin-section-strip-wrap {",
		"display: contents;",
		".admin-section-strip {",
		"position: sticky;",
		".position-chip {",
		"flex-shrink: 0;",
		"#main-content .admin-task-nav__link {",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("wave C — cedar comb block missing %q", want)
		}
	}
}

// TestAdminTaskBoardLinkBeatsInlineLinkTouchRuleSpecificity pins J2 F26's
// real root cause: #main-content li a (public/styles.css, ~522, written
// for a prose link inside a <p> or an <li>) outranks .admin-task-nav__
// link's own display: flex on specificity alone — one #id plus two
// elements beats one class, regardless of source order — because
// AdminTaskLink's own <a> happens to sit inside an <li>. The mis-applied
// inline-block plus its zero-sum padding-block/margin-block trick under-
// measures each row once its status text wraps at phone width, and the
// next grid row overlaps it. This asserts the fix's own selector
// specificity is real: an #id + one class (#main-content
// .admin-task-nav__link) has one more class than the #id + two elements
// it must beat, which wins the tie ahead of it in the cascade.
func TestAdminTaskBoardLinkBeatsInlineLinkTouchRuleSpecificity(t *testing.T) {
	styles, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	fixStart := strings.Index(css, "#main-content .admin-task-nav__link {")
	if fixStart < 0 {
		t.Fatal("styles.css is missing the #main-content .admin-task-nav__link override")
	}
	fixEnd := strings.Index(css[fixStart:], "}")
	if fixEnd < 0 {
		t.Fatal("#main-content .admin-task-nav__link rule never closes")
	}
	rule := css[fixStart : fixStart+fixEnd]
	for _, want := range []string{"display: flex", "margin-block: 0"} {
		if !strings.Contains(rule, want) {
			t.Errorf("#main-content .admin-task-nav__link rule missing %q: %s", want, rule)
		}
	}
	conflictStart := strings.Index(css, "#main-content li a {")
	if conflictStart < 0 {
		t.Fatal("could not find #main-content li a (the rule this fix must outrank)")
	}
	if fixStart < conflictStart {
		t.Error("the admin-task-nav__link fix must come from a selector that wins on specificity, not merely on source order")
	}
}

// TestAdminBackupSpeaksToACommissioner pins J4 F30: the backup section
// used to lead with schema versions and a database SHA-256; it now leads
// with what is backed up, when the last one ran, and what to do if a
// backup fails to open. The Danger Zone's own consequence sentences now
// link back to it.
func TestAdminBackupSpeaksToACommissioner(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, unwanted := range []string{
		"consistent database snapshot",
		"schema versions",
		"SHA-256",
	} {
		if strings.Contains(source, unwanted) {
			t.Errorf("backup section still speaks in operator language: %q", unwanted)
		}
	}
	for _, want := range []string{
		"One file with your whole league. Keep it somewhere safe.",
		"If a backup ever fails to open",
		`data.admin_backup.has_last_run`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("backup section missing %q", want)
		}
	}
	if count := strings.Count(source, `<a href="#admin-backup" class="board-button">Back up first`); count != 2 {
		t.Errorf("danger-zone consequence sentences link to backup %d times, want 2", count)
	}
}

// TestAdminThisWeekCardAnswersTheJobInOneScreen pins J4 F35: a "This
// week" card leads the in-season console — week, close readiness, open
// waivers, trades in review, and one line naming what needs the
// commissioner today, each line linking to its own section.
func TestAdminThisWeekCardAnswersTheJobInOneScreen(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	start := strings.Index(source, `class="admin-this-week"`)
	if start < 0 {
		t.Fatal("page.gsx is missing the admin-this-week card")
	}
	end := strings.Index(source[start:], "</div>\n\t\t\t<details")
	if end < 0 {
		t.Fatal("admin-this-week card never closes ahead of the draft-night disclosure")
	}
	card := source[start : start+end]
	for _, want := range []string{
		`href="#admin-week-close"`,
		`href="/trades"`,
		`href="#admin-invites"`,
		"props.ScheduleWeek",
		"props.OpenClaimCount",
		"props.TradesInReviewCount",
		"Needs you today:",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("admin-this-week card missing %q", want)
		}
	}
}

// TestAdminAttentionSeatRowsLeadWithTeamNotCode pins J4 F28 + J2 F18: the
// readiness ledger used to lead each row with an unexplained code
// ("AQ1 · In Shedeur Time"); the team name leads now, the code is a
// secondary chip, and one legend line defines it where codes first
// appear.
func TestAdminAttentionSeatRowsLeadWithTeamNotCode(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if strings.Contains(source, "{seat.Abbreviation} · {seat.Name}") {
		t.Error("attention ledger row still leads with the seat code ahead of the team name")
	}
	if !strings.Contains(source, `<span class="position-chip mono">{seat.Abbreviation}</span>`) {
		t.Error("attention ledger row is missing the seat code as a secondary chip")
	}
	if !strings.Contains(source, "are each team's short seat code") {
		t.Error("attention ledger is missing its one legend line for seat codes")
	}
}

// TestAdminMastheadLeadsWithWeekOnceDraftIsComplete pins a coordinator
// follow-up on wave C (2026-09-08): the league-status hero card at the
// top of /admin kept showing draft-night facts ("8 / 8 SEATS · 136
// PICKS", "Draft MON · SEP 7 · 4:30 PM EDT", "4 / 8 READY") after the
// draft finished, the same condition the console already uses to
// collapse the draft-night readiness panels elsewhere on this page.
// Once the draft is complete, the card leads with the week: the week
// number and its close-readiness state in words, the first kickoff in
// league-local time with zone (when the schedule carries one), and the
// seat count; the draft-night READY fraction and the draft date drop
// out of the card (they still live inside the "Draft night (complete)"
// disclosure below it). The task board's own "Manage seats and
// managers" row (AdminTaskLink, page.gsx) shows the week's own status
// in place of the same stale READY fraction.
func TestAdminMastheadLeadsWithWeekOnceDraftIsComplete(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdminTaskBoardDraftPhaseFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"ADMIN_TASK_DRAFT_PHASE=complete",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("complete-draft fixture: %v\n%s", err, output)
	}
	body := string(output)

	mastheadStart := strings.Index(body, `<div class="draft-clock-panel">`)
	if mastheadStart < 0 {
		t.Fatal("rendered page is missing the draft-clock-panel masthead")
	}
	mastheadEnd := strings.Index(body[mastheadStart:], "</section>")
	if mastheadEnd < 0 {
		t.Fatal("draft-clock-panel masthead never closes ahead of </section>")
	}
	masthead := body[mastheadStart : mastheadStart+mastheadEnd]

	if !strings.Contains(masthead, "Week") {
		t.Errorf("post-draft masthead does not name the week: %s", masthead)
	}
	if strings.Contains(masthead, "ready-count-tag") {
		t.Errorf("post-draft masthead still shows the draft-night READY fraction: %s", masthead)
	}
	if strings.Contains(masthead, "PICKS") {
		t.Errorf("post-draft masthead still shows the draft pick count: %s", masthead)
	}
	if strings.Contains(masthead, "Draft") {
		t.Errorf("post-draft masthead still shows the draft date line: %s", masthead)
	}
	if !strings.Contains(masthead, "SEATS") {
		t.Errorf("post-draft masthead dropped the seat count: %s", masthead)
	}

	seatsLinkAt := strings.Index(body, "Manage seats and managers")
	if seatsLinkAt < 0 {
		t.Fatal("task board is missing the Manage seats and managers row")
	}
	seatsRowEnd := strings.Index(body[seatsLinkAt:], "</a>")
	if seatsRowEnd < 0 {
		t.Fatal("Manage seats and managers row never closes")
	}
	seatsRow := body[seatsLinkAt : seatsLinkAt+seatsRowEnd]
	if strings.Contains(seatsRow, "READY</span>") && !strings.Contains(seatsRow, "WEEK") {
		t.Errorf("post-draft task board still shows the bare seat-ready fraction instead of the week's own status: %s", seatsRow)
	}
}

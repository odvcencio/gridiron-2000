package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdminSectionStripCoversEveryConsoleSection is item 7's own contract:
// a compact, sticky-at-phone-width companion to the fuller
// .admin-task-nav task board (public/styles.css) that jumps straight to
// each of the console's data-admin-section wrappers. It sits after the
// task board's own closing </nav> (not between AdminTaskLink's
// definition and that real nav — that placement broke
// TestAdminTaskNavigationGroupsAndRoutineOrder's own start/end scan,
// which depends on there being exactly one </nav> between AdminTaskLink
// and the real task board's own close).
func TestAdminSectionStripCoversEveryConsoleSection(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	stripStart := strings.Index(source, `<nav class="admin-section-strip" aria-label="Jump to a console section">`)
	if stripStart < 0 {
		t.Fatal("page.gsx is missing the admin-section-strip nav")
	}
	stripEnd := strings.Index(source[stripStart:], "</nav>")
	if stripEnd < 0 {
		t.Fatal("admin-section-strip nav never closes")
	}
	strip := source[stripStart : stripStart+stripEnd]
	for _, want := range []string{
		"section=draft-control#admin-draft-control",
		"section=schedule#admin-schedule",
		"section=week-close#admin-week-close",
		"section=playoffs#admin-playoffs",
		"section=seats#admin-seats",
		"section=invites#admin-invites",
		"section=draft-order#admin-draft-order",
		"section=data#admin-data",
		"section=clock#admin-clock",
		"section=roster#admin-roster",
		"section=announcements#admin-announcements",
		"section=backup#admin-backup",
		"section=danger#admin-danger",
	} {
		if !strings.Contains(strip, want) {
			t.Errorf("admin-section-strip missing jump target %q", want)
		}
	}
	// The strip must sit after the real task board's own </nav>, not
	// before it: strings.Index finds AdminTaskLink's own rendered
	// "admin-task-nav__item" class first (a substring collision with
	// "admin-task-nav"), so the task-nav contract test's start/end scan
	// only works when the very next </nav> after that false-positive
	// start is the real task board's close.
	taskNavStart := strings.Index(source, `class="admin-task-nav__item"`)
	realNavClose := strings.Index(source[taskNavStart:], "</nav>")
	if taskNavStart < 0 || realNavClose < 0 {
		t.Fatal("could not locate the real admin-task-nav's own close for ordering check")
	}
	if stripStart < taskNavStart+realNavClose {
		t.Error("admin-section-strip must render after the real .admin-task-nav's own closing </nav>, not before it")
	}
}

// TestAdminInviteEmailIsAPhoneEmailField is item 7's autocomplete/
// inputmode contract: the commissioner's own invite-email field (not
// #co-manager-email, which belongs to /team) gets autocomplete="email"
// and inputmode="email" so a phone offers saved addresses and the right
// keyboard, plus enterkeyhint="done" for a single-field form.
func TestAdminInviteEmailIsAPhoneEmailField(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	inviteStart := strings.Index(source, `id="admin-invite-email"`)
	if inviteStart < 0 {
		t.Fatal("page.gsx is missing the admin-invite-email field")
	}
	fieldEnd := strings.Index(source[inviteStart:], "/>")
	field := source[inviteStart : inviteStart+fieldEnd]
	for _, want := range []string{`autocomplete="email"`, `inputmode="email"`, `enterkeyhint="done"`} {
		if !strings.Contains(field, want) {
			t.Errorf("admin-invite-email field missing %q: %s", want, field)
		}
	}
	if strings.Contains(source, `id="co-manager-email"`) {
		// co-manager-email belongs to /team (elm's page), not /admin —
		// this file must never define it, or a shared id could collide.
		t.Error("page.gsx defines #co-manager-email, which is owned by /team")
	}
}

// TestAdminTypedConfirmFieldsCarryDoneEnterKeyHint covers item 7's
// enterkeyhint contract: every "type X to confirm" single-field form
// (.typed-confirm-input) gets enterkeyhint="done", so a phone's on-screen
// keyboard offers a submit-shaped return key instead of the generic
// default.
func TestAdminTypedConfirmFieldsCarryDoneEnterKeyHint(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	total := strings.Count(source, "typed-confirm-input")
	withHint := strings.Count(source, "typed-confirm-input")
	// Every typed-confirm-input site must also carry enterkeyhint="done"
	// on the same <input ...> tag; count each input element individually
	// rather than assuming a fixed global count, so a future confirm
	// field added without the hint fails loudly.
	for _, line := range strings.Split(source, "\n") {
		if !strings.Contains(line, "typed-confirm-input") || !strings.Contains(line, "<input") {
			continue
		}
		if !strings.Contains(line, `enterkeyhint="done"`) {
			t.Errorf("typed-confirm-input field missing enterkeyhint=\"done\": %s", strings.TrimSpace(line))
		}
	}
	if total == 0 || withHint == 0 {
		t.Fatal("no typed-confirm-input fields found; test fixture assumption broke")
	}
}

// TestAdminSeatRowSetButtonsAndEmailToggleMeetTheTouchBaseline is item
// 7's own 44px contract: the eight per-seat "Set" (rename) buttons
// measured 39.7px wide despite .board-button's own min-width, and the
// "Also queue an email" checkbox row measured 40.6px tall. Both get a
// page-scoped re-assert in the wave 7b — ash stylesheet block.
func TestAdminSeatRowSetButtonsAndEmailToggleMeetTheTouchBaseline(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	blockStart := strings.Index(css, "/* wave 7b — ash")
	if blockStart < 0 {
		t.Fatal("styles.css is missing the wave 7b — ash block")
	}
	block := css[blockStart:]
	for _, want := range []string{
		".seat-row .board-button {",
		"min-width: var(--control-h);",
		".announcement-email-toggle {",
		"min-height: var(--control-h);",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("wave 7b — ash block missing %q", want)
		}
	}
}

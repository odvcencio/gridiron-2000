package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// mastheadEyebrowManagerPages (Decision 2, J1 F28, wave E) names the
// manager pages aspen owns, walked by
// TestManagerMastheadEyebrowsCarryNoSectionNumbers below. The console
// (/admin) is deliberately excluded: it keeps its own numbering (cedar
// unified it in rev 110), the one place the audit's recommendation
// leaves it.
var mastheadEyebrowManagerPages = []string{
	"app/page.gsx",
	"app/team/page.gsx",
	"app/matchups/page.gsx",
	"app/players/page.gsx",
	"app/pickem/page.gsx",
	"app/locker/page.gsx",
	"app/wire/page.gsx",
	"app/settings/page.gsx",
}

// mastheadNumberedEyebrowLine matches a trimmed source line that opens
// with a two-digit "NN // " index — the retired "00 // POST-DRAFT //
// PRESEASON" style eyebrow prefix, whether it sits inline in a
// single-line span ("<span ...>00 // ANNOUNCEMENTS</span>") or as its
// own line inside a multi-line one (pickem's "01 // WEEK" line ahead of
// "{data.week}").
var mastheadNumberedEyebrowLine = regexp.MustCompile(`(?m)^\s*(?:<span[^>]*>)?\s*\d{2} //\s`)

// TestManagerMastheadEyebrowsCarryNoSectionNumbers pins Decision 2 (J1
// F28): manager pages carry no "NN // " section-index numbering in any
// masthead or section eyebrow — the console (/admin) is the one page
// that keeps its own numbering, and it is not in this walk.
func TestManagerMastheadEyebrowsCarryNoSectionNumbers(t *testing.T) {
	for _, path := range mastheadEyebrowManagerPages {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if loc := mastheadNumberedEyebrowLine.FindString(string(source)); loc != "" {
			t.Errorf("%s: still carries a numbered section eyebrow (%q) — Decision 2 drops section numbers on manager pages", path, strings.TrimSpace(loc))
		}
	}
}

// TestManagerMastheadEyebrowsDoNotRepeatTheirHeading is a regression pin
// for the two exact-repeat violations J1 F28's evidence named directly
// (the players masthead read "PLAYER POOL" over an h1 that also read
// "PLAYER POOL"; the locker masthead read "LOCKER ROOM" over an h1 that
// read "Locker Room"). Both eyebrows now name the page's job instead of
// restating the heading.
func TestManagerMastheadEyebrowsDoNotRepeatTheirHeading(t *testing.T) {
	cases := []struct {
		path         string
		wantEyebrow  string
		wantHeading  string
		mustNotAgree string
	}{
		{"app/players/page.gsx", "ROSTER & WAIVERS", "<h1>PLAYER POOL</h1>", "PLAYER POOL"},
		{"app/locker/page.gsx", "LEAGUE MESSAGE BOARD", "<h1>Locker Room</h1>", "LOCKER ROOM"},
	}
	for _, c := range cases {
		source, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("read %s: %v", c.path, err)
		}
		page := string(source)
		if !strings.Contains(page, c.wantEyebrow) {
			t.Errorf("%s: masthead eyebrow no longer reads %q", c.path, c.wantEyebrow)
		}
		if !strings.Contains(page, c.wantHeading) {
			t.Errorf("%s: masthead h1 no longer reads %q", c.path, c.wantHeading)
		}
		signalLabel := regexp.MustCompile(`(?s)<span class="signal-label">.*?</span>`).FindString(page)
		if strings.Contains(strings.ToUpper(signalLabel), c.mustNotAgree) {
			t.Errorf("%s: masthead eyebrow still repeats the heading's own words (%q)", c.path, c.mustNotAgree)
		}
	}
}

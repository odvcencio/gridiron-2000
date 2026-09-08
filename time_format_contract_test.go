package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// timeFormatContractAbsolutePattern is the one league-local shape F20
// (gap-audit J6) requires every stored-instant display to converge on:
// "Jan 2, 3:04 PM MST" (Go's reference layout), title case, comma after
// the day, no leading zero on the day. Trades, announcements, and waiver
// resolutions (internal/league) already used it; the Wire kept its own
// uppercase, comma-free shape ("SEP 03 · 8:43 PM EDT") until this wave,
// and the Activity feed's own time element carried a stray double space
// before the " · " relative separator.
const timeFormatContractGoLayout = "Jan 2, 3:04 PM MST"

// timeFormatContractVisiblePattern matches the rendered text a manager
// reads on /wire and /activity: the absolute stamp, then " · " plus a
// relative phrase where one is available. Used by the browser/e2e waves;
// kept here as the documented shape this source-level contract pins.
var timeFormatContractVisiblePattern = regexp.MustCompile(`^[A-Z][a-z]{2} \d{1,2}, \d{1,2}:\d{2} (AM|PM) [A-Z]{2,4}( · .+)?$`)

func TestTimeFormatContractPatternIsWellFormed(t *testing.T) {
	for _, want := range []struct {
		name string
		text string
		ok   bool
	}{
		{"absolute plus relative", "Sep 4, 5:31 AM EDT · 6 minutes ago", true},
		{"absolute only", "Sep 4, 5:31 AM EDT", true},
		{"old wire shape rejected", "SEP 03 · 8:43 PM EDT · 8 hours ago", false},
		{"double space rejected", "Sep 4, 5:28 AM EDT  · 6 minutes ago", false},
	} {
		if got := timeFormatContractVisiblePattern.MatchString(want.text); got != want.ok {
			t.Errorf("%s: pattern.MatchString(%q) = %v, want %v", want.name, want.text, got, want.ok)
		}
	}
}

// TestWireAndActivityTimeFormattersShareTheCanonicalGoLayout pins F20's
// source-level contract: the Signal Wire's formatWireTime and
// internal/league's activityMaps/leagueAbsoluteTimeStamp all format a
// stored instant with the exact same Go time layout — one format, not
// three independently maintained near-copies that can drift apart again.
func TestWireAndActivityTimeFormattersShareTheCanonicalGoLayout(t *testing.T) {
	sources := map[string]string{
		"app/wire/page.server.go":    `value.In(location).Format("` + timeFormatContractGoLayout + `")`,
		"internal/league/service.go": `.Format("` + timeFormatContractGoLayout + `")`,
	}
	for path, want := range sources {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(body), want) {
			t.Errorf("%s missing the canonical layout %q", path, want)
		}
	}
	// The wire must not have grown a second, divergent time layout.
	wireSource, err := os.ReadFile("app/wire/page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, retired := range []string{`Format("Jan 02 · 3:04 PM MST")`, "strings.ToUpper(value.In(location)"} {
		if strings.Contains(string(wireSource), retired) {
			t.Errorf("app/wire/page.server.go still carries the retired wire-only time shape %q", retired)
		}
	}
}

// TestLockerAndHomeTimeFormattersConvergeOnLeagueTimeStamp is J6 F20's
// own residue (wave E): the Locker Room and the home dashboard still
// carried their own time formats after the wire/activity convergence
// above — the Locker Room's TimeLabel hand-rolled the exact same
// absolute Format call with no relative phrase, and the home dashboard's
// announcement list computed a second, separate relative field
// (posted_ago) instead of reading the one converged answer
// (Service.LeagueTimeStamp / its unexported sibling leagueTimeStamp)
// every other league timestamp already uses.
func TestLockerAndHomeTimeFormattersConvergeOnLeagueTimeStamp(t *testing.T) {
	lockerSource, err := os.ReadFile("internal/league/locker.go")
	if err != nil {
		t.Fatal(err)
	}
	locker := string(lockerSource)
	if !strings.Contains(locker, "TimeLabel:        s.leagueTimeStamp(post.PostedAt),") {
		t.Error("internal/league/locker.go's TimeLabel no longer calls the converged s.leagueTimeStamp helper")
	}
	if strings.Contains(locker, `Format("`+timeFormatContractGoLayout+`")`) {
		t.Error("internal/league/locker.go still hand-rolls the canonical Go layout instead of calling leagueTimeStamp")
	}

	serviceSource, err := os.ReadFile("internal/league/service.go")
	if err != nil {
		t.Fatal(err)
	}
	service := string(serviceSource)
	if !strings.Contains(service, `"posted_at": s.leagueTimeStamp(a.PostedAt),`) {
		t.Error("internal/league/service.go's announcementListMaps no longer calls the converged s.leagueTimeStamp helper for posted_at")
	}
	if strings.Contains(service, `"posted_ago": relativeTime(now, a.PostedAt),`) {
		t.Error("internal/league/service.go's announcementListMaps still computes a separate posted_ago field")
	}

	homeSource, err := os.ReadFile("app/page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	home := string(homeSource)
	if strings.Contains(home, "note.posted_ago") {
		t.Error("app/page.gsx still reads note.posted_ago — the home announcement list must read only the converged note.posted_at")
	}
	if !strings.Contains(home, "{note.posted_at}") {
		t.Error("app/page.gsx no longer renders note.posted_at")
	}
}

// TestActivityTimeElementHasNoStrayWhitespace pins the double-space
// regression's fix: the <time> element's own children render with no
// whitespace text node between the absolute stamp and the relative
// phrase's leading " · " (F20, gap-audit J6 — "Sep 4, 5:28 AM EDT  ·
// 6 minutes ago" read with two spaces before the separator).
func TestActivityTimeElementHasNoStrayWhitespace(t *testing.T) {
	body, err := os.ReadFile("app/activity/page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, want := range []string{
		`<time class="mono" datetime={move.TimeISO}>{move.Time}<If cond={move.TimeRelative != ""}> · {move.TimeRelative}</If></time>`,
		`<time class="mono">{move.Time}<If cond={move.TimeRelative != ""}> · {move.TimeRelative}</If></time>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("app/activity/page.gsx missing the single-line (no stray whitespace) time element %q", want)
		}
	}
}

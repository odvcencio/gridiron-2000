package league

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLoginDataBuildsSafeOAuthTarget(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/login?next=%2Fdraft%3Fweek%3D1", nil)
	data := service.LoginData(request, true)

	if got := data["oauth_start"]; got != "/auth/google/start?next=%2Fdraft%3Fweek%3D1" {
		t.Errorf("oauth_start = %q, want encoded draft target", got)
	}
	if got := data["return_path"]; got != "/draft?week=1" {
		t.Errorf("return_path = %q, want /draft?week=1", got)
	}
	if got := data["has_return_path"]; got != true {
		t.Errorf("has_return_path = %v, want true", got)
	}

	request, _ = http.NewRequest(http.MethodGet, "/login?next=https://evil.example/", nil)
	data = service.LoginData(request, true)
	if got := data["oauth_start"]; got != "/auth/google/start?next=%2F" {
		t.Errorf("external oauth_start = %q, want root fallback", got)
	}

	request, _ = http.NewRequest(http.MethodGet, "/login?next=%2Fdraft%3Fweek%3D1&next=https%3A%2F%2Fevil.example%2F", nil)
	data = service.LoginData(request, true)
	if got := data["oauth_start"]; got != "/auth/google/start?next=%2Fdraft%3Fweek%3D1" {
		t.Errorf("duplicate next oauth_start = %q, want first safe target only", got)
	}

	request, _ = http.NewRequest(http.MethodGet, "/login?next=https%3A%2F%2Fevil.example%2F&next=%2Fdraft", nil)
	data = service.LoginData(request, true)
	if got := data["oauth_start"]; got != "/auth/google/start?next=%2F" {
		t.Errorf("hostile-first duplicate oauth_start = %q, want root fallback", got)
	}
}

func TestLeagueMapFormatBlurbMatchesConfiguredFormat(t *testing.T) {
	service := newTestService(t, false)
	for mode, want := range map[string]string{
		"DYNASTY": "dynasty format",
		"REDRAFT": "redraft format",
		"":        "custom fantasy format",
	} {
		service.cfg.ModeLabel = mode
		if got := service.leagueMap()["format_blurb"]; got != want {
			t.Errorf("mode %q format_blurb = %q, want %q", mode, got, want)
		}
	}
}

func TestDraftSummaryNamesScheduledWindowAndTimezone(t *testing.T) {
	service := newTestService(t, false)
	service.draftAt = time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	service.draftTZ = time.FixedZone("EDT", -4*60*60)
	service.cfg.Timezone = "America/New_York"

	before := service.draftSummary(service.draftAt.Add(-time.Minute))
	if got := before["status_label"]; got != "SCHEDULED WINDOW" {
		t.Errorf("before status_label = %v, want SCHEDULED WINDOW", got)
	}
	if got := before["window_reached"]; got != false {
		t.Errorf("before window_reached = %v, want false", got)
	}
	if got := before["timezone"]; got != "Eastern Time" {
		t.Errorf("timezone = %v, want the friendly label for the configured IANA timezone", got)
	}
	if got := before["event_label"]; got != "LEAGUE DRAFT" {
		t.Errorf("event_label = %v, want LEAGUE DRAFT", got)
	}
	if got := before["status_note"]; got == "" {
		t.Error("scheduled window must explain commissioner-controlled opening")
	}

	after := service.draftSummary(service.draftAt.Add(time.Minute))
	if got := after["status_label"]; got != "AWAITING COMMISSIONER" {
		t.Errorf("after status_label = %v, want AWAITING COMMISSIONER", got)
	}
	if got := after["window_reached"]; got != true {
		t.Errorf("after window_reached = %v, want true", got)
	}
}

// A draft date that is unset, or absurdly far out (the neutral reference
// league ships a 2098-12-31 placeholder), must never render as a real
// scheduled window: the 2026-09-01 UX audit found a live "26419d 16:51:31"
// countdown on three surfaces. The summary declares the date unpublished,
// suppresses the countdown, and explains who sets the date.
func TestDraftSummaryGuardsUnpublishedDraftDates(t *testing.T) {
	service := newTestService(t, false)
	service.cfg.Timezone = "America/New_York"
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	service.draftAt = time.Date(2098, 12, 31, 19, 0, 0, 0, time.UTC)
	summary := service.draftSummary(now)
	if got := summary["published"]; got != false {
		t.Fatalf("2098 placeholder published = %v, want false", got)
	}
	if got := summary["countdown_label"]; got != "" {
		t.Errorf("2098 placeholder countdown_label = %q, want empty", got)
	}
	if got := summary["at"]; got != "" {
		t.Errorf("2098 placeholder at = %q, want empty so no client countdown starts", got)
	}
	if got := summary["status_label"]; got != "NOT SCHEDULED" {
		t.Errorf("status_label = %v, want NOT SCHEDULED", got)
	}
	if got := summary["date"]; got != "TBD" {
		t.Errorf("date = %v, want TBD", got)
	}
	if got := summary["long_date"]; got != "Draft time not published yet" {
		t.Errorf("long_date = %v, want the honest empty state", got)
	}
	if note, _ := summary["status_note"].(string); !strings.Contains(note, "commissioner") {
		t.Errorf("status_note = %q, want a plain-language commissioner explanation", note)
	}

	service.draftAt = time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)
	near := service.draftSummary(now)
	if got := near["published"]; got != true {
		t.Fatalf("near-date published = %v, want true", got)
	}
	if got, _ := near["countdown_label"].(string); got == "" {
		t.Error("a near, real draft date must keep its countdown")
	}
}

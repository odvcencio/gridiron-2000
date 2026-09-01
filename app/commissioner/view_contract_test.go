package commissioner

import (
	"os"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
	"m31labs.dev/gosx/route"
)

// TestCommissionerTimeFormattingConvertsToLeagueLocation is the wave-1
// audit fix for view.go:592's displayTime: HQ used to format a summary
// timestamp with no zone conversion at all (Go defaults to the value's own
// embedded offset, which for the UTC instants commissionerhq actually
// ships is a literal "UTC" abbreviation) — the reported "Jan 1, 2099 ·
// 12:00 AM UTC" / "GENERATED Sep 1, 2026 · 8:45 PM UTC" bug. displayTime
// now takes the caller's *time.Location explicitly (never a package-level
// league.Default() read inside a formatting helper) and converts into it,
// so the same UTC instant renders in whatever zone the caller supplies —
// the local league's LeagueLocation() in production.
func TestCommissionerTimeFormattingConvertsToLeagueLocation(t *testing.T) {
	value := time.Date(2026, time.August, 22, 20, 0, 0, 0, time.UTC) // 20:00 UTC
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	if got := displayTime(eastern, value); !strings.Contains(got, "4:00 PM EDT") {
		t.Fatalf("displayTime did not convert into the supplied league location: %q", got)
	}
	if got := displayTime(time.UTC, value); !strings.Contains(got, "8:00 PM UTC") {
		t.Fatalf("displayTime(time.UTC, ...) = %q, want the unconverted UTC clock face", got)
	}
	if got, want := isoTime(value), "2026-08-22T20:00:00Z"; got != want {
		t.Fatalf("ISO time = %q, want %q (isoTime never localizes)", got, want)
	}
	if got := displayTime(eastern, time.Time{}); got != "—" {
		t.Fatalf("displayTime of a zero time = %q, want the em dash placeholder", got)
	}
}

// TestCommissionerDraftDateGuardsSentinelAndAddsRelativeText is the /commissioner
// half of the wave-1 sentinel/timezone audit finding: a peer summary
// carrying the neutral placeholder draft instant (config.go's
// placeholderDraftAt, 2099-01-01) must render "Not published yet" rather
// than a fabricated calendar fact, and a genuinely published, already-past
// draft meeting gains a relative label next to its absolute time.
func TestCommissionerDraftDateGuardsSentinelAndAddsRelativeText(t *testing.T) {
	now := time.Date(2026, time.September, 1, 20, 0, 0, 0, time.UTC)
	sentinelDraftAt := time.Date(2099, time.January, 1, 0, 0, 0, 0, time.UTC)

	unpublished := cardView(commissionerhq.FleetEntry{
		PeerID: "g2k", PublicURL: "https://gridiron.example",
		Summary: commissionerhq.Summary{
			Instance: commissionerhq.Instance{Name: "GRIDIRON 2000", PublicURL: "https://gridiron.example"},
			Draft:    commissionerhq.Draft{ScheduledAt: sentinelDraftAt},
		},
	}, now, time.UTC, false)
	if unpublished.DraftAt != "Not published yet" {
		t.Fatalf("DraftAt with a sentinel draft date = %q, want the unpublished guard text", unpublished.DraftAt)
	}
	if unpublished.DraftAtISO != "" {
		t.Fatalf("DraftAtISO with a sentinel draft date = %q, want empty", unpublished.DraftAtISO)
	}
	if unpublished.DraftAtRelative != "" {
		t.Fatalf("DraftAtRelative with a sentinel draft date = %q, want empty", unpublished.DraftAtRelative)
	}

	pastScheduled := now.Add(-3 * time.Hour)
	published := cardView(commissionerhq.FleetEntry{
		PeerID: "g2k", PublicURL: "https://gridiron.example",
		Summary: commissionerhq.Summary{
			Instance: commissionerhq.Instance{Name: "GRIDIRON 2000", PublicURL: "https://gridiron.example"},
			Draft:    commissionerhq.Draft{ScheduledAt: pastScheduled},
		},
	}, now, time.UTC, false)
	if published.DraftAt == "Not published yet" {
		t.Fatal("DraftAt with a real, recent draft date rendered the unpublished guard text")
	}
	if published.DraftAtRelative != "3 hours ago" {
		t.Fatalf("DraftAtRelative for a real past draft date = %q, want \"3 hours ago\"", published.DraftAtRelative)
	}

	// The rendered draft-control panel must actually show both: the guard
	// text with no fabricated calendar line, and the relative label next
	// to a real published date's absolute time.
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	unpublishedHTML, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": readoutFromView(fleetPageView{Location: time.UTC, Cards: []fleetCardView{unpublished}}, true, true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unpublishedHTML, "Not published yet") {
		t.Fatalf("rendered draft control panel omitted the unpublished guard text: %s", unpublishedHTML)
	}
	if strings.Contains(unpublishedHTML, "2099") {
		t.Fatalf("rendered draft control panel leaked the sentinel year: %s", unpublishedHTML)
	}
	// 2026-09-01 audit finding 2: an unpublished draft date rendered
	// <time datetime="">Not published yet</time> — an empty datetime
	// attribute is worse than none; the guard text must render with no
	// <time> element (or no datetime attribute) at all.
	if strings.Contains(unpublishedHTML, `datetime="">Not published yet`) {
		t.Fatalf("rendered draft control panel emitted an empty datetime attribute: %s", unpublishedHTML)
	}

	publishedHTML, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": readoutFromView(fleetPageView{Location: time.UTC, Cards: []fleetCardView{published}}, true, true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(publishedHTML, "(3 hours ago)") {
		t.Fatalf("rendered draft control panel omitted the relative-time label: %s", publishedHTML)
	}
	if !strings.Contains(publishedHTML, `datetime="`+published.DraftAtISO+`"`) {
		t.Fatalf("rendered draft control panel omitted a real datetime attribute for a published draft date: %s", publishedHTML)
	}
}

// TestOpenDataRowsCarryRelativeTimeAndOmitEmptyDatetime is the wave-2
// audit fix (finding 2): the NFL DATA list's SCHEDULES/PLAYER STATS/etc.
// rows rendered an absolute stamp with no relative marker at all, with no
// <time> element to carry a datetime attribute. A dataset that has never
// synced (zero LastUpdated) must not get a fabricated instant either.
func TestOpenDataRowsCarryRelativeTimeAndOmitEmptyDatetime(t *testing.T) {
	now := time.Date(2026, time.September, 1, 20, 0, 0, 0, time.UTC)
	updated := now.Add(-3 * time.Hour)
	data := commissionerhq.OpenData{
		Schedules:   commissionerhq.DatasetStatus{State: "ready", LastUpdated: updated},
		PlayerStats: commissionerhq.DatasetStatus{State: "ready"}, // never synced: zero LastUpdated
	}
	rows := openDataRows(data, now, time.UTC)
	if len(rows) != 6 {
		t.Fatalf("len(rows) = %d, want 6", len(rows))
	}
	schedules := rows[0]
	if schedules["has_updated"] != true || schedules["updated_relative"] != "3 hours ago" || schedules["updated_iso"] != "2026-09-01T17:00:00Z" {
		t.Fatalf("schedules row = %+v", schedules)
	}
	playerStats := rows[1]
	if playerStats["has_updated"] != false || playerStats["updated_relative"] != "" || playerStats["updated_iso"] != "" || playerStats["updated"] != "—" {
		t.Fatalf("never-synced player stats row = %+v, want an honest empty/em-dash state", playerStats)
	}

	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	card := cardView(commissionerhq.FleetEntry{
		PeerID: "g2k", PublicURL: "https://gridiron.example",
		Summary: commissionerhq.Summary{Instance: commissionerhq.Instance{Name: "GRIDIRON 2000", PublicURL: "https://gridiron.example"}, OpenData: data},
	}, now, time.UTC, false)
	html, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": readoutFromView(fleetPageView{Location: time.UTC, Cards: []fleetCardView{card}}, true, true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `datetime="2026-09-01T17:00:00Z"`) {
		t.Fatalf("rendered NFL DATA list omitted a real datetime attribute for a synced dataset: %s", html)
	}
	if !strings.Contains(html, "(3 hours ago)") {
		t.Fatalf("rendered NFL DATA list omitted the relative-time label: %s", html)
	}
	if strings.Contains(html, `datetime="">`) {
		t.Fatalf("rendered NFL DATA list emitted an empty datetime attribute: %s", html)
	}
}

// TestFleetReadoutGeneratedAtCarriesRelativeMarker is the wave-2 audit
// fix (finding 2): the masthead's GENERATED stamp rendered no relative
// marker at all.
func TestFleetReadoutGeneratedAtCarriesRelativeMarker(t *testing.T) {
	now := time.Date(2026, time.September, 1, 20, 0, 0, 0, time.UTC)
	view := buildFleetView(nil, now, time.UTC)
	data := view.toData()
	if data["generated_at_relative"] != "just now" {
		t.Fatalf("generated_at_relative = %v, want %q", data["generated_at_relative"], "just now")
	}
	readout := readoutFromView(view, true, true)
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	html, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": readout},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "GENERATED") || !strings.Contains(html, "just now") {
		t.Fatalf("rendered masthead omitted the GENERATED relative marker: %s", html)
	}
}

// TestEmptyFleetReadoutMastheadNeverEmitsEmptyDatetime is the wave-2 audit
// fix (finding 2): emptyFleetReadout's GeneratedAt stub ("—") carries no
// real instant, so GeneratedAtISO stays empty — the masthead must not
// render <time datetime="">GENERATED —</time> for that stub state.
func TestEmptyFleetReadoutMastheadNeverEmitsEmptyDatetime(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	html, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": emptyFleetReadout(true, false)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, `datetime="">`) {
		t.Fatalf("empty fleet readout emitted an empty datetime attribute: %s", html)
	}
	if !strings.Contains(html, "GENERATED —") {
		t.Fatalf("empty fleet readout dropped the GENERATED stub text: %s", html)
	}
}

// TestFleetCardLocalInstanceLinksAreRelativeRemoteStaysAbsolute is the
// wave-1 audit fix for view.go's HQ links: summary.Instance.PublicURL
// falls back to defaultConfigURL ("http://localhost:8080", config.go) on
// any deployment that never set league.json's url field, which made every
// "Open league →" / "Draft controls →" / "Schedule →" / "Data →" / "Open
// data →" link on the LOCAL card bounce to a dead localhost origin. The
// local card (buildFleetView's index 0, per Fleet()'s documented
// ordering) now links with root-relative paths regardless of what
// PublicURL carries; only a remote peer card keeps the absolute URL,
// since that is the only way to reach a different origin.
func TestFleetCardLocalInstanceLinksAreRelativeRemoteStaysAbsolute(t *testing.T) {
	now := time.Date(2026, time.September, 1, 20, 0, 0, 0, time.UTC)
	attention := []commissionerhq.Attention{{Code: "pool_stale", Severity: "warning", Area: "pool", Message: "Pool data is stale"}}
	localSummary := commissionerhq.Summary{
		Instance:  commissionerhq.Instance{Name: "GRIDIRON 2000", PublicURL: "http://localhost:8080"},
		Attention: attention,
	}
	remoteSummary := commissionerhq.Summary{
		Instance:  commissionerhq.Instance{Name: "PEER LEAGUE", PublicURL: "https://peer.gridiron.example"},
		Attention: attention,
	}
	local := cardView(commissionerhq.FleetEntry{PeerID: "local", PublicURL: "http://localhost:8080", Summary: localSummary}, now, time.UTC, true)
	remote := cardView(commissionerhq.FleetEntry{PeerID: "peer", PublicURL: "https://peer.gridiron.example", Summary: remoteSummary}, now, time.UTC, false)

	relativeWant := map[string]string{
		"HomeURL":          "/",
		"AdminURL":         "/admin",
		"DraftURL":         "/draft",
		"AdminDraftURL":    "/admin?section=draft-control#admin-draft-control",
		"AdminScheduleURL": "/admin?section=schedule#admin-schedule",
		"AdminDataURL":     "/admin?section=data#admin-data",
	}
	got := map[string]string{
		"HomeURL": local.HomeURL, "AdminURL": local.AdminURL, "DraftURL": local.DraftURL,
		"AdminDraftURL": local.AdminDraftURL, "AdminScheduleURL": local.AdminScheduleURL,
		"AdminDataURL": local.AdminDataURL,
	}
	for key, want := range relativeWant {
		if got[key] != want {
			t.Errorf("local card %s = %q, want relative path %q", key, got[key], want)
		}
	}
	if len(local.Attention) != 1 || local.Attention[0].OwnerURL != "/admin?section=data#admin-data" {
		t.Fatalf("local card's data-attention owner_url = %+v, want a relative /admin?section=data link", local.Attention)
	}

	if !strings.HasPrefix(remote.HomeURL, "https://peer.gridiron.example") {
		t.Errorf("remote card HomeURL = %q, want the absolute PublicURL preserved", remote.HomeURL)
	}
	if !strings.HasPrefix(remote.AdminDraftURL, "https://peer.gridiron.example") {
		t.Errorf("remote card AdminDraftURL = %q, want the absolute PublicURL preserved", remote.AdminDraftURL)
	}
	if len(remote.Attention) != 1 || !strings.HasPrefix(remote.Attention[0].OwnerURL, "https://peer.gridiron.example") {
		t.Fatalf("remote card's attention owner_url = %+v, want the absolute PublicURL preserved", remote.Attention)
	}
}

func TestWeekCloseBadgePrecedenceAndBoundedWaitingReason(t *testing.T) {
	longReason := strings.Repeat("readiness detail ", 20)
	cases := []struct {
		name       string
		close      commissionerhq.WeekClose
		badge      string
		waiting    bool
		reasonWant string
	}{
		{
			name:  "final wins over ready",
			close: commissionerhq.WeekClose{Final: true, Ready: true, Reason: "stale final reason"},
			badge: "FINAL",
		},
		{
			name:  "ready wins over waiting",
			close: commissionerhq.WeekClose{Ready: true, Reason: "stale ready reason"},
			badge: "READY",
		},
		{
			name:       "waiting carries bounded reason",
			close:      commissionerhq.WeekClose{Reason: longReason},
			badge:      "WAITING",
			waiting:    true,
			reasonWant: "readiness detail ",
		},
		{
			name:       "waiting has honest default reason",
			close:      commissionerhq.WeekClose{},
			badge:      "WAITING",
			waiting:    true,
			reasonWant: "Waiting for the week-close readiness checks.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := weekCloseBadge(tc.close); got != tc.badge {
				t.Fatalf("badge = %q, want %q", got, tc.badge)
			}
			got := weekCloseWaitingReason(tc.close)
			if tc.reasonWant == "" {
				if got != "" {
					t.Fatalf("non-waiting reason = %q, want empty", got)
				}
			} else {
				if !strings.HasPrefix(got, tc.reasonWant) {
					t.Fatalf("waiting reason = %q, want prefix %q", got, tc.reasonWant)
				}
				if len([]rune(got)) > 161 {
					t.Fatalf("waiting reason is unbounded: %d runes", len([]rune(got)))
				}
			}
			card := cardView(commissionerhq.FleetEntry{
				PeerID:    "fixture",
				PublicURL: "https://fixture.example",
				Summary: commissionerhq.Summary{
					Instance: commissionerhq.Instance{Name: "Fixture", PublicURL: "https://fixture.example"},
					Season:   commissionerhq.Season{WeekClose: tc.close},
				},
			}, time.Now(), time.UTC, false)
			payload := card.toMap()
			if got := payload["week_close_badge"]; got != tc.badge {
				t.Fatalf("render badge = %#v, want %q", got, tc.badge)
			}
			if got := payload["week_close_waiting"]; got != tc.waiting {
				t.Fatalf("render waiting flag = %#v, want %t", got, tc.waiting)
			}
			if got := payload["week_close_waiting_reason"]; got != weekCloseWaitingReason(tc.close) {
				t.Fatalf("render reason = %#v, want %q", got, weekCloseWaitingReason(tc.close))
			}
		})
	}
}

func TestCommissionerWeekCloseRenderContracts(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		"{card.week_close_badge}",
		"<If cond={card.week_close_waiting}>",
		"{card.week_close_waiting_reason}",
		"GAMES FINAL",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("commissioner week-close render missing %q", want)
		}
	}
	finals := finalText(commissionerhq.Schedule{FinalWeeks: 1, WeekCount: 2, FinalMatchups: 5, TotalMatchups: 10})
	if !strings.Contains(finals, "5/10 matchups final") || strings.Contains(finals, "games final") {
		t.Fatalf("season final label = %q, want matchup wording and no ambiguous game wording", finals)
	}
}

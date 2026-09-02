package matchups

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchupsWeekNavIsStickyAndSnapScrollsOnPhone is item 1's own
// sticky/snap contract: .pickem-weeknav (shared with /pickem — see
// WeekBrowserProps' doc comment, page.gsx) must carry position: sticky
// pinned under the fixed top bar (var(--mobile-bar-height), the same
// offset #main-content's own scroll-margin-top uses) plus a mandatory
// horizontal scroll-snap axis, inside the phone/tablet media block, and
// every direct child that scrolls along the strip must opt into that
// snap axis with its own scroll-snap-align.
func TestMatchupsWeekNavIsStickyAndSnapScrollsOnPhone(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(styles)
	blockStart := strings.Index(source, "/* wave 7b — ash")
	if blockStart < 0 {
		t.Fatal("styles.css is missing the wave 7b — ash block")
	}
	block := source[blockStart:]
	mediaStart := strings.Index(block, "@media (max-width: 56.1875rem) {")
	if mediaStart < 0 {
		t.Fatal("wave 7b — ash block is missing the phone/tablet media query")
	}
	media := block[mediaStart:]
	navStart := strings.Index(media, ".pickem-weeknav {")
	if navStart < 0 {
		t.Fatal("wave 7b — ash media block is missing the .pickem-weeknav rule")
	}
	navRule := media[navStart : navStart+strings.Index(media[navStart:], "}")]
	for _, want := range []string{
		"position: sticky",
		"top: var(--mobile-bar-height)",
		"scroll-snap-type: x mandatory",
		"overflow-x: auto",
	} {
		if !strings.Contains(navRule, want) {
			t.Errorf(".pickem-weeknav phone rule missing %q: %s", want, navRule)
		}
	}
	if !strings.Contains(media, "scroll-snap-align: start") {
		t.Error("wave 7b — ash media block never opts a .pickem-weeknav child into scroll-snap-align")
	}
}

// TestMatchupsIsGameDay is matchupsIsGameDay's own unit contract: only
// LIVE, PAUSED, or FINAL read as "game day"; LEDGER, the Go zero value ""
// (the preseason fixture's own live_state, page_render_test.go — no
// schedule published yet), and a missing/malformed status_line all read
// as not-game-day, so the page never claims a score is worth leading with
// before one actually exists.
func TestMatchupsIsGameDay(t *testing.T) {
	for _, tt := range []struct {
		name      string
		liveState string
		want      bool
	}{
		{"ledger", "LEDGER", false},
		{"live", "LIVE", true},
		{"paused", "PAUSED", true},
		{"final", "FINAL", true},
		{"empty (preseason zero value)", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := matchupsIsGameDay(map[string]any{"live_state": tt.liveState})
			if got != tt.want {
				t.Fatalf("matchupsIsGameDay(%q) = %v, want %v", tt.liveState, got, tt.want)
			}
		})
	}
	if got := matchupsIsGameDay(nil); got != false {
		t.Fatalf("matchupsIsGameDay(nil) = %v, want false", got)
	}
}

// TestMatchupsPrimaryActionOnlyForAnEditableViewerMatchup is
// matchupsPrimaryAction's own unit contract (item 1's primary_action
// wiring): the PageActionBar only gets a "Set lineup" verb when the
// featured card is genuinely the viewer's own matchup AND there is a
// still-editable next week to send it to. A "Featured" (non-viewer) card,
// or a viewer card with no next editable week, must not invent one.
func TestMatchupsPrimaryActionOnlyForAnEditableViewerMatchup(t *testing.T) {
	for _, tt := range []struct {
		name        string
		isViewer    bool
		hasNextWeek bool
		wantNil     bool
	}{
		{"viewer with next week", true, true, false},
		{"non-viewer featured card", false, true, true},
		{"viewer with no next week", true, false, true},
		{"neither", false, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			myMatchup := FeaturedMatchupData{IsViewer: tt.isViewer, HasNextWeek: tt.hasNextWeek, NextWeek: 5, NextLineupHref: "/team?week=5"}
			action := matchupsPrimaryAction(myMatchup)
			if tt.wantNil {
				if action != nil {
					t.Fatalf("matchupsPrimaryAction(%+v) = %v, want nil", myMatchup, action)
				}
				return
			}
			if action == nil {
				t.Fatalf("matchupsPrimaryAction(%+v) = nil, want a set action", myMatchup)
			}
			if action["href"] != "/team?week=5" || action["kind"] != "link" {
				t.Fatalf("matchupsPrimaryAction(%+v) = %v, want href /team?week=5 and kind link", myMatchup, action)
			}
			if label, _ := action["label"].(string); label != "Set lineup for Week 5" {
				t.Fatalf("matchupsPrimaryAction(%+v) label = %q, want %q", myMatchup, label, "Set lineup for Week 5")
			}
		})
	}
}

// TestMatchupsScoreOrderFlipsWithLiveState is item 1's own render contract
// ("order flips with live state"): the live fixture (at least one matchup
// LIVE this week) renders MatchupScoreBlock's own .matchup-layout wrapper
// before .matchup-status-line, so a phone reader hits the score before the
// provenance prose; every non-game-day fixture (preseason, scheduled —
// both LiveState LEDGER) keeps the original status-line-first order, since
// there is no score yet worth leading with.
func TestMatchupsScoreOrderFlipsWithLiveState(t *testing.T) {
	for _, tt := range []struct {
		fixture    string
		scoreFirst bool
	}{
		{"live", true},
		{"preseason", false},
		{"scheduled", false},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
			cmd.Env = append(os.Environ(), "MATCHUPS_RENDER_FIXTURE="+tt.fixture,
				"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture process: %v\n%s", err, output)
			}
			body := string(output)
			layoutAt := strings.Index(body, `class="matchup-layout"`)
			statusAt := strings.Index(body, `class="matchup-status-line"`)
			if layoutAt < 0 || statusAt < 0 {
				t.Fatalf("fixture %s missing matchup-layout or matchup-status-line: %s", tt.fixture, body)
			}
			gotScoreFirst := layoutAt < statusAt
			if gotScoreFirst != tt.scoreFirst {
				t.Fatalf("fixture %s: matchup-layout before matchup-status-line = %v, want %v (layout at %d, status at %d)", tt.fixture, gotScoreFirst, tt.scoreFirst, layoutAt, statusAt)
			}
		})
	}
}

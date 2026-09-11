package matchups

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestFeaturedMatchupDataCentrePhaseByLiveState is A1's own view-model
// contract (matchup redesign 2026-09-07): the featured/scorebug header's
// centre label and state class switch on live_state alone, independent
// of every other field, so PhaseLabel and StateClass can never disagree
// about which of the three phases (before kickoff, live, after the week)
// a matchup is in.
func TestFeaturedMatchupDataCentrePhaseByLiveState(t *testing.T) {
	cases := []struct {
		liveState      string
		wantPhase      string
		wantStateClass string
	}{
		{"LEDGER", "PROJ", "state--pre"},
		{"", "PROJ", "state--pre"},
		{"LIVE", "", "state--live"},
		{"PAUSED", "", "state--live"},
		{"FINAL", "FINAL", "state--final"},
	}
	for _, c := range cases {
		t.Run(c.liveState, func(t *testing.T) {
			got := featuredMatchupData(map[string]any{
				"has_matchup": true,
				"live_state":  c.liveState,
				"mine":        map[string]any{"id": "team-1"},
				"theirs":      map[string]any{"id": "team-2"},
			})
			if got.PhaseLabel != c.wantPhase {
				t.Errorf("PhaseLabel = %q, want %q", got.PhaseLabel, c.wantPhase)
			}
			if got.StateClass != c.wantStateClass {
				t.Errorf("StateClass = %q, want %q", got.StateClass, c.wantStateClass)
			}
		})
	}
}

// TestMatchupStatusBlockRendersClosedEarlyBadge pins F3 (J4 console
// gap-audit): a forced close can finalize a week while its real NFL games
// are not final — every score risking 0.0 from a missed player-stat join
// — and the results page had nothing to distinguish that from an honest
// FINAL. The status line must carry a CLOSED EARLY badge, gated on the
// server-computed data.status_line.closed_early flag (internal/league's
// LiveSnapshot.ClosedEarly), as a plain static element separate from the
// live-bound state chip (data-gosx-live-bind="liveState") so a later live
// poll can never silently overwrite it.
func TestMatchupStatusBlockRendersClosedEarlyBadge(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	if !strings.Contains(markup, `<If cond={data.status_line.closed_early}>`) {
		t.Fatal("MatchupStatusBlock does not gate on data.status_line.closed_early")
	}
	if !strings.Contains(markup, "CLOSED EARLY") {
		t.Fatal("MatchupStatusBlock is missing the CLOSED EARLY badge text")
	}
	closedEarlyAt := strings.Index(markup, `<If cond={data.status_line.closed_early}>`)
	// The chip binds the readable label; the raw token keeps its own key
	// for the polled contract (2026-09-10).
	liveBindAt := strings.Index(markup, `data-gosx-live-bind="liveStateLabel"`)
	if closedEarlyAt < 0 || liveBindAt < 0 {
		t.Fatal("could not locate both the closed-early badge and the live-bound state chip")
	}
	badgeBlockEnd := strings.Index(markup[closedEarlyAt:], "</If>")
	if badgeBlockEnd < 0 {
		t.Fatal("closed-early <If> block has no matching </If>")
	}
	badgeBlock := markup[closedEarlyAt : closedEarlyAt+badgeBlockEnd]
	if strings.Contains(badgeBlock, "data-gosx-live-bind") {
		t.Error("the closed-early badge must be a plain static element, not live-bound (a live poll must never overwrite it)")
	}
}

// TestStarterCellDataProjDefaultsToHonestZero covers A2's two honest
// fallbacks: a filled player with no source forecast is unavailable, while
// an empty slot remains the honest zero PROJ 0.0 rather than a blank cell.
func TestStarterCellDataProjDefaultsToHonestZero(t *testing.T) {
	withProj := starterCellData(map[string]any{"player_id": "p-09", "proj": "14.2"}, false)
	if withProj.Proj != "14.2" {
		t.Fatalf("Proj with a source value = %q, want %q", withProj.Proj, "14.2")
	}
	withoutProj := starterCellData(map[string]any{"player_id": "p-09"}, false)
	if withoutProj.Proj != "—" {
		t.Fatalf("Proj with no source value = %q, want unavailable %q", withoutProj.Proj, "—")
	}
	emptySlot := starterCellData(nil, false)
	if emptySlot.Proj != "0.0" {
		t.Fatalf("Proj for a nil row = %q, want the honest zero %q", emptySlot.Proj, "0.0")
	}
}

// TestFeaturedMatchupDataBenchesWireThrough covers A4: mine_bench/
// theirs_bench convert into BenchRowData verbatim, keyed by the same
// field names benchRowMaps (service.go) emits.
func TestFeaturedMatchupDataBenchesWireThrough(t *testing.T) {
	got := featuredMatchupData(map[string]any{
		"has_matchup": true,
		"mine":        map[string]any{"id": "team-1"},
		"theirs":      map[string]any{"id": "team-2"},
		"mine_bench": []map[string]any{
			{"player_name": "Backup One", "position": "RB", "nfl_team": "KC", "proj": "8.4"},
		},
		"theirs_bench": []map[string]any{
			{"player_name": "Backup Two", "position": "WR", "nfl_team": "MIA", "proj": "6.1"},
		},
	})
	if len(got.MineBench) != 1 || got.MineBench[0].PlayerName != "Backup One" || got.MineBench[0].Proj != "8.4" {
		t.Fatalf("MineBench = %+v, want one Backup One row at 8.4", got.MineBench)
	}
	if len(got.TheirsBench) != 1 || got.TheirsBench[0].PlayerName != "Backup Two" || got.TheirsBench[0].Proj != "6.1" {
		t.Fatalf("TheirsBench = %+v, want one Backup Two row at 6.1", got.TheirsBench)
	}
}

// TestRedesignedFeaturedTableRendersSlotProjTotalsAndBenches is the
// render-level check for A2/A4: the slot table carries a PROJ column and
// the slot label per row, a totals row sums PROJ and PTS, and the
// Benches disclosure renders closed by default.
func TestRedesignedFeaturedTableRendersSlotProjTotalsAndBenches(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"MATCHUPS_RENDER_FIXTURE=scheduled",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture process: %v\n%s", err, output)
	}
	body := string(output)
	for _, want := range []string{
		`class="section-index" role="columnheader" aria-label="`,
		` projected points">Proj</span>`,
		`class="slot-row__slot" role="cell">`,
		`class="matchup-pairs-totals" role="row"`,
		`class="matchup-pairs-totals__label" role="cell">Total</span>`,
		`class="matchup-benches">`,
		`<summary>Benches</summary>`,
		`class="proj starter-cell__proj" role="cell"> <span class="projection-cell projection-value"`,
		`data-gosx-live-bind="starterOriginalProj.`,
		`class="projection-tip"`, `ORIGINAL PROJECTION`,
		`class="starter-progress__ring"`, `data-gosx-live-bind="starterProgressQ1.`, `data-gosx-live-bind="starterProgressSummary.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("redesigned featured table missing %q: %s", want, body)
		}
	}

	// The Benches <details> is closed by default (no "open" attribute) so
	// the featured table still reads as the primary surface.
	if idx := strings.Index(body, `class="matchup-benches"`); idx >= 0 {
		tagEnd := strings.Index(body[idx:], ">")
		if tagEnd < 0 {
			t.Fatal("matchup-benches details tag never closes")
		}
		if strings.Contains(body[idx:idx+tagEnd], " open") {
			t.Error("Benches disclosure rendered open by default, want closed")
		}
	}

	// PROJ always renders a real number, never borrowing PTS's own
	// not-yet-known dash placeholder.
	projValueRE := regexp.MustCompile(`class="proj starter-cell__proj" role="cell">\s*<span class="projection-cell projection-value"[^>]*>\s*<span class="projection-value__number" data-gosx-live-bind="starterOriginalProj\.[^"]*">(\d+\.\d)</span>`)
	if !projValueRE.MatchString(body) {
		t.Error("no starter-cell__proj cell rendered a real decimal PROJ value")
	}
}

func TestMatchupProjectionAndNavigationAffordancesRender(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"MATCHUPS_RENDER_FIXTURE=scheduled",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture process: %v\n%s", err, output)
	}
	body := string(output)
	for _, want := range []string{
		`class="matchup-team-link"`,
		`data-gosx-link`,
		`href="/team?team=`,
		`class="projection-tip"`,
		`ORIGINAL PROJECTION`,
		`Weekly source forecast`,
		`class="starter-progress__ring"`,
		`data-gosx-live-bind="starterProgressQ1.`,
		`data-gosx-live-bind="starterProgressSummary.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("matchup fixture missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"60 sec", "60 SEC", "60 s fallback", "60 seconds", "60. seconds"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("matchup fixture retained meaningless polling copy %q", forbidden)
		}
	}
}

// TestStarterCellRendersThePointsCellsOwnDashVerbatim covers A2's other
// half of the truthful-state contract at the render layer this page
// owns: PTS shows "—" until the weekly ledger posts (never 0.0) — the
// exact text starterCellData/StarterCell must pass through unchanged.
// The truth decision of WHEN a row's points read "—" versus an honest
// 0.0 belongs to the ledger (internal/league/matchup_ledger.go,
// starterGameNotStarted and friends, already covered by
// matchup_score_truth_test.go/live_state_test.go); this only proves the
// page's own StarterCellData/StarterCell never substitutes or drops that
// text on the way to the DOM.
func TestStarterCellRendersThePointsCellsOwnDashVerbatim(t *testing.T) {
	cell := starterCellData(map[string]any{
		"player_id": "p-09", "live_key": "team-1_QB", "points": "—", "proj": "18.4",
	}, false)
	if cell.Points != "—" {
		t.Fatalf("StarterCellData.Points = %q, want the ledger's own dash verbatim", cell.Points)
	}
	if cell.Proj != "18.4" {
		t.Fatalf("StarterCellData.Proj = %q, want the real projection, not the dash", cell.Proj)
	}
}

// TestRedesignedHeaderRetiresStillToPlayJargon covers A1's item 5: the
// fixed "N of M starters still to play" sentence is retired for the
// plain-words sentence (stillToPlaySentence, internal/league), while the
// live-bind machinery it replaced stays present (in a visually-hidden
// fallback) so the existing stillToPlay/stillToPlayTotal live binds are
// extended, not removed.
func TestRedesignedHeaderRetiresStillToPlayJargon(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsPageFixtureProcess$")
	cmd.Env = append(os.Environ(), "MATCHUPS_RENDER_FIXTURE=live",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture process: %v\n%s", err, output)
	}
	body := string(output)
	if strings.Contains(body, "starters still to play</small>") {
		t.Error("featured header still renders the retired \"N of M starters still to play\" sentence shape")
	}
	for _, want := range []string{
		`data-gosx-live-bind="stillToPlaySentence.`,
		`class="visually-hidden" data-gosx-live-bind="stillToPlay.`,
		`class="visually-hidden" data-gosx-live-bind="stillToPlayTotal.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q: %s", want, body)
		}
	}
}

func TestProjectionWeekNoticeIsVisibleAndScoped(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	for _, want := range []string{
		`data-gosx-live-bind="projectionNote"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("MatchupStatusBlock missing projection-week disclosure %q", want)
		}
	}
	styles, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(styles), ".matchups-page .matchup-status-line__projection") {
		t.Fatal("projection-week disclosure style is not scoped to the Matchups page")
	}
	if !strings.Contains(string(styles), ".matchups-page .matchup-status-line__projection:empty") {
		t.Fatal("empty projection-week disclosure must not reserve a status-line row")
	}
}

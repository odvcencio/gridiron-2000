package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// statTipSummaryTag matches one <summary class="…stat-tip__summary…"> open
// tag. It must match the whole tag, not the bare "stat-tip__summary"
// substring: the news trigger's class is "stat-tip__summary
// stat-tip__summary--news", which contains that substring twice and would
// double-count if counted with strings.Count.
var statTipSummaryTag = regexp.MustCompile(`<summary class="[^"]*stat-tip__summary[^"]*"`)

// TestPlayerDetailsUseNativeDisclosureAcrossSurfaces protects one invariant:
// every player detail popover — the projection/availability "identity" tip
// and the newspaper-icon "news" tip added beside it — opens through a native
// <details class="stat-tip…"><summary class="…stat-tip__summary…"> pair, not
// a JS-driven tooltip. It does not pin an incidental total summary count;
// instead it counts identity tips and news tips separately (each surface
// wires the news tip into a different pool-row component, so the two counts
// move independently) and checks the structural rule that every stat-tip
// details block owns exactly one stat-tip__summary and one stat-tip__panel,
// and that no legacy hover/focus-within/role=tooltip affordance remains.
func TestPlayerDetailsUseNativeDisclosureAcrossSurfaces(t *testing.T) {
	tests := []struct {
		name string
		path string
		// identityTips counts <details class="stat-tip"> and the team page's
		// <details class="player-identity stat-tip">: the projection/
		// availability popover on the player's name.
		identityTips int
		// newsTips counts <details class="stat-tip stat-tip--news">: the
		// commissioner's newspaper-icon headline popover, wired in beside the
		// identity tip in each pool-row template (board, draft, players).
		// Blitz and team do not carry a news tip yet.
		newsTips int
	}{
		{name: "team", path: filepath.Join("team", "page.gsx"), identityTips: 2, newsTips: 0},
		{name: "draft", path: filepath.Join("draft", "page.gsx"), identityTips: 1, newsTips: 3},
		{name: "big board", path: filepath.Join("board", "page.gsx"), identityTips: 2, newsTips: 2},
		{name: "blitz", path: filepath.Join("blitz", "page.gsx"), identityTips: 3, newsTips: 0},
		// The player pool renders its pool row through one component,
		// PlayerPoolRegion(), which Page() embeds directly instead of
		// hand-duplicating (item 1's root-cause fix, 2026-09-02
		// route-crawl finding — rowan). One native disclosure, one news
		// tip, both keyboard- and touch-operable after a live region
		// swap, since the initial page and the fragment poll now render
		// from the identical source.
		{name: "player pool", path: filepath.Join("players", "page.gsx"), identityTips: 1, newsTips: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceBytes, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			source := string(sourceBytes)
			gotIdentity := strings.Count(source, `<details class="stat-tip">`) +
				strings.Count(source, `<details class="player-identity stat-tip">`)
			if gotIdentity != tt.identityTips {
				t.Fatalf("identity stat-tip details = %d, want %d", gotIdentity, tt.identityTips)
			}
			gotNews := strings.Count(source, `<details class="stat-tip stat-tip--news">`)
			if gotNews != tt.newsTips {
				t.Fatalf("news stat-tip details = %d, want %d", gotNews, tt.newsTips)
			}
			wantSummaries := tt.identityTips + tt.newsTips
			if got := len(statTipSummaryTag.FindAllString(source, -1)); got != wantSummaries {
				t.Fatalf("stat-tip summaries = %d, want %d (identity + news tips)", got, wantSummaries)
			}
			if got := strings.Count(source, `stat-tip__panel`); got != wantSummaries {
				t.Fatalf("stat-tip panels = %d, want %d (identity + news tips)", got, wantSummaries)
			}
			for _, forbidden := range []string{
				`role="tooltip"`,
				`stat-tip" tabindex="0"`,
				`stat-tip__panel" aria-hidden="true"`,
				`stat-tip:hover`,
				`stat-tip:focus-within`,
			} {
				if strings.Contains(source, forbidden) {
					t.Errorf("legacy stat-tip affordance remains: %q", forbidden)
				}
			}
			if !strings.Contains(source, `<summary class="`) {
				t.Error("player detail trigger must be a native summary")
			}
			if tt.newsTips > 0 && !strings.Contains(source, `aria-label={"News for " + `) {
				t.Error("news stat-tip trigger must carry an accessible name")
			}
		})
	}

	players, err := os.ReadFile(filepath.Join("players", "page.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Projection", "Availability", "waiver_resolves", "matchup_detail", "needs_drop",
	} {
		if !strings.Contains(string(players), want) {
			t.Errorf("Player Pool detail context missing %q", want)
		}
	}

	team, err := os.ReadFile(filepath.Join("team", "page.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	teamSource := string(team)
	for _, want := range []string{
		`<If cond={slot.has_player == false}>`,
		`<div class="slot-empty mono">EMPTY</div>`,
		`<div class="slot-empty mono">AWAITING DRAFT</div>`,
	} {
		if !strings.Contains(teamSource, want) {
			t.Errorf("Team empty roster preview contract missing %q", want)
		}
	}

	stylesBytes, err := os.ReadFile(filepath.Join("..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, want := range []string{
		`.stat-tip[open] > .stat-tip__panel`,
		`.stat-tip__summary:focus-visible`,
		`touch-action: manipulation;`,
		`@media (max-width: 38rem)`,
		`min-height: 2.75rem;`,
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("native stat-tip stylesheet contract missing %q", want)
		}
	}
	for _, forbidden := range []string{".stat-tip:hover", ".stat-tip:focus-within"} {
		if strings.Contains(styles, forbidden) {
			t.Errorf("stylesheet still depends on legacy stat-tip selector %q", forbidden)
		}
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlayerDetailsUseNativeDisclosureAcrossSurfaces(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		detailTags int
	}{
		{name: "team", path: filepath.Join("team", "page.gsx"), detailTags: 2},
		{name: "draft", path: filepath.Join("draft", "page.gsx"), detailTags: 1},
		{name: "big board", path: filepath.Join("board", "page.gsx"), detailTags: 2},
		{name: "blitz", path: filepath.Join("blitz", "page.gsx"), detailTags: 3},
		{name: "player pool", path: filepath.Join("players", "page.gsx"), detailTags: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceBytes, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			source := string(sourceBytes)
			gotDetails := strings.Count(source, `<details class="stat-tip">`) +
				strings.Count(source, `<details class="player-identity stat-tip">`)
			if gotDetails != tt.detailTags {
				t.Fatalf("stat-tip details = %d, want %d", gotDetails, tt.detailTags)
			}
			if got := strings.Count(source, `stat-tip__summary`); got != tt.detailTags {
				t.Fatalf("stat-tip summaries = %d, want %d", got, tt.detailTags)
			}
			if got := strings.Count(source, `stat-tip__panel`); got != tt.detailTags {
				t.Fatalf("stat-tip panels = %d, want %d", got, tt.detailTags)
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

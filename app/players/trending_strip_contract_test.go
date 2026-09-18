package players

import (
	"os"
	"strings"
	"testing"
)

// TestPlayerPoolTrendingStripContract pins the pool's "trending this week"
// strip: it renders inside PlayerPoolRegion (the one copy the page and the
// fragment share), only when has_trending is set, from trending_adds and
// trending_drops (trendingMoves, internal/league/trending.go), and its
// stylesheet rules exist with adds in the accent and drops muted.
func TestPlayerPoolTrendingStripContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	region := source[strings.Index(source, "func PlayerPoolRegion() Node {"):]
	region = region[:strings.Index(region, `<div class="pool-filter-rail" id="pool-search">`)]
	for _, want := range []string{
		`<If cond={data.has_trending}>`,
		`class="pool-trending"`,
		`<Each of={data.trending_adds} as="move">`,
		`<Each of={data.trending_drops} as="move">`,
		`pool-trending__move--add`,
		`pool-trending__move--drop`,
		`{move.Name}`, `{move.Count}`,
	} {
		if !strings.Contains(region, want) {
			t.Errorf("PlayerPoolRegion is missing the trending strip piece %q", want)
		}
	}
	if strings.Count(source, `class="pool-trending"`) != 1 {
		t.Errorf("the trending strip must render from exactly one place (PlayerPoolRegion), found %d", strings.Count(source, `class="pool-trending"`))
	}

	css, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(css)
	for _, want := range []string{".pool-trending {", ".pool-trending__move--add {", ".pool-trending__move--drop {"} {
		if !strings.Contains(styles, want) {
			t.Errorf("styles.css is missing %q", want)
		}
	}
	at := strings.Index(styles, ".pool-trending {")
	block := styles[at : at+strings.Index(styles[at:], "}")]
	if !strings.Contains(block, "font-size: var(--type-xs)") {
		t.Errorf(".pool-trending must size with the sub-body token, not a literal: %s", block)
	}
}

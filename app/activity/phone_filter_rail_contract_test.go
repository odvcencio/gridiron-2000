package activity

import (
	"os"
	"strings"
	"testing"
)

// TestActivityFilterRailIsTwoColumnsOnAPhone pins the phone-width layout
// of /activity's filter rail. The rail is a plain search form: two
// labelled selects, a labelled search field, and a submit. The shared
// .pool-search-bar phone rules wrap every label into its own full-width
// 44px row, which is right for a bar standing alone but stacked this
// rail to 406px on a 390x844 phone and put the first transaction at
// 1085px (TestBrowserAshJobInFoldAcrossOwnedRoutes logs it as a known
// gap). Scoped to #activity-filters, the rail becomes a two-column grid:
// each label keeps its 44px tap-target height beside its own control on
// one row. Nothing is hidden; wider screens are untouched.
func TestActivityFilterRailIsTwoColumnsOnAPhone(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	rule := func(selector string) string {
		t.Helper()
		at := strings.Index(css, selector+" {")
		if at < 0 {
			t.Fatalf("styles.css has no %q rule", selector)
		}
		block := css[at : at+strings.Index(css[at:], "}")]
		media := strings.LastIndex(css[:at], "@media (width <= 38rem)")
		if media < 0 || strings.Contains(css[media:at], "\n}\n\n") {
			t.Fatalf("%q must sit inside a phone-width (38rem) media block", selector)
		}
		return block
	}
	bar := rule("#activity-filters .pool-search-bar")
	if !strings.Contains(bar, "display: grid") || !strings.Contains(bar, "grid-template-columns: auto minmax(0, 1fr)") {
		t.Fatalf("phone rail must be a label | control grid: %s", bar)
	}
	label := rule("#activity-filters .pool-search-bar label")
	if !strings.Contains(label, "min-height: var(--control-h)") {
		t.Fatalf("phone rail labels must keep the 44px tap-target floor: %s", label)
	}
	if block := rule("#activity-filters .pool-search-bar .filter-button"); !strings.Contains(block, "grid-column: 1 / -1") {
		t.Fatalf("phone rail buttons must span the grid: %s", block)
	}
}

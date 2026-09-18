package pickem

import (
	"os"
	"strings"
	"testing"
)

// TestPickemPhoneFoldCompaction pins the phone-width compaction of the
// Pick'em page head. On a 390x844 phone the first game row sat at 1313px
// (TestBrowserAshJobInFoldAcrossOwnedRoutes logs it as a known gap): the
// record strip stacked its three tiles one per row, and the picks counter
// rendered as a tall boxed panel. At phone width the record strip keeps
// its three columns and the counter collapses to one row, both scoped to
// .pickem-page so the draft war room's own panels are untouched.
func TestPickemPhoneFoldCompaction(t *testing.T) {
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
	if block := rule(".pickem-page .pickem-record"); !strings.Contains(block, "repeat(3, minmax(0, 1fr))") {
		t.Fatalf("phone record strip must keep three columns: %s", block)
	}
	if block := rule(".pickem-page .draft-clock-panel"); !strings.Contains(block, "grid-template-columns") {
		t.Fatalf("phone picks counter must lay its label and count out on one row: %s", block)
	}
}

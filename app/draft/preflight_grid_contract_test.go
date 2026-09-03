package draft

import (
	"os"
	"strings"
	"testing"
)

// TestDraftPreflightSpansThePaneGrid pins the yarrow re-audit hotfix: the
// pre-draft checklist strip is the first grid child of .draft-panes and
// must span every column, or the pool pane collapses to the 300px track
// and the manager's own pane wraps to a 2px row (findings 1-3, main
// 5ee2a65). A missing rule is the whole bug, so the pin is on the rule.
func TestDraftPreflightSpansThePaneGrid(t *testing.T) {
	css, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	source := string(css)
	at := strings.Index(source, ".draft-panes > .draft-preflight {")
	if at < 0 {
		t.Fatal("styles.css has no .draft-panes > .draft-preflight rule; the strip will steal a pane track")
	}
	block := source[at:]
	if end := strings.Index(block, "}"); end > 0 {
		block = block[:end]
	}
	if !strings.Contains(block, "grid-column: 1 / -1;") {
		t.Fatalf(".draft-preflight must span the pane grid (grid-column: 1 / -1); got %q", block)
	}
}

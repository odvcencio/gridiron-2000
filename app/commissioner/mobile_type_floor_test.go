package commissioner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommissionerRefreshStatusMeetsTheTypeFloor is item 8's own
// contract: .commissioner-hq__refresh-status used to render at 0.72rem
// (11.52px), under the 13px sub-body floor every other status/freshness
// line in the app holds. It now uses var(--type-xs), the same token the
// shared floor is built from.
func TestCommissionerRefreshStatusMeetsTheTypeFloor(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	ruleStart := strings.Index(css, "\n.commissioner-hq__refresh-status {")
	if ruleStart < 0 {
		t.Fatal("styles.css is missing the .commissioner-hq__refresh-status rule")
	}
	rule := css[ruleStart : ruleStart+strings.Index(css[ruleStart:], "}")]
	if !strings.Contains(rule, "font-size: var(--type-xs);") {
		t.Errorf(".commissioner-hq__refresh-status rule missing font-size: var(--type-xs): %s", rule)
	}
	if strings.Contains(rule, "0.72rem") {
		t.Errorf(".commissioner-hq__refresh-status rule still carries the sub-floor literal 0.72rem: %s", rule)
	}
}

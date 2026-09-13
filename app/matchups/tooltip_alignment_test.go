package matchups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScoreTooltipAmountsAlignWithTotalAfterWrapping(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(styles)
	start := strings.Index(source, ".points-tip__rows {")
	if start < 0 {
		t.Fatal("missing tooltip rows style")
	}
	end := strings.Index(source[start:], "}")
	if end < 0 {
		t.Fatal("unclosed tooltip rows style")
	}
	block := source[start : start+end]
	for _, want := range []string{"white-space: pre-wrap;", "text-align-last: right;", "font-variant-numeric: tabular-nums;", "overflow-wrap: anywhere;"} {
		if !strings.Contains(block, want) {
			t.Errorf("tooltip must preserve decimal alignment and bounded wrapping: missing %q", want)
		}
	}
}

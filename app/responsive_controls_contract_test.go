package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhoneControlsKeepFingerSizedTargets(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read shared styles: %v", err)
	}
	css := string(styles)
	for _, want := range []string{
		"@media (max-width: 38rem)",
		".hero-command,",
		".page-masthead,",
		".draft-masthead,",
		`.page input:not([type])`,
		`.page input[type="search"]`,
		`.page label:has(input[type="checkbox"])`,
		`.page input[type="file"]::file-selector-button`,
		"min-height: 2.75rem",
		"min-width: 2.75rem",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("phone control CSS missing %q", want)
		}
	}
}

package app

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// h1Block matches one <h1>...</h1> element, including any attributes and
// its full text content, across source lines.
var h1Block = regexp.MustCompile(`(?s)<h1[^>]*>.*?</h1>`)

// TestH1NeverHardBreaksItsOwnName is the discovery guard for wave-6 audit
// item 1 (second pass — rowan). A literal <br> inside <h1> contributes no
// separator to the accessible name: adjacent same-line text nodes fuse
// with no space at all ("PLAY<br>THE<br>ROOM." read as "PLAYTHEROOM." to
// assistive tech), and even a <br> between separate source lines could
// not be trusted to insert one either. Every masthead h1 in the app now
// renders as a single plain-text phrase and leaves line-breaking to CSS
// (h1's own text-wrap: balance, public/styles.css) instead of a manual
// tag, so this scans every .gsx source file for a regression.
func TestH1NeverHardBreaksItsOwnName(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".gsx" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, block := range h1Block.FindAllString(string(source), -1) {
			if regexp.MustCompile(`(?i)<br\b`).MatchString(block) {
				t.Errorf("%s has a <br> inside <h1>, which contributes no accessible-name separator: %s", path, block)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

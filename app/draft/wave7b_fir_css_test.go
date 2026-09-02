package draft

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestDraftTypeFloorNoFontSizeBelow13pxInDraftSection is wave 7b item 3's
// own contract test: no font-size in the whole draft war-room section
// (from the "/* Draft war room */" marker to end of file — this file's
// own append-only convention keeps every later worker's own draft-scoped
// rule inside that same span) drops below the 0.8125rem (13px) floor,
// whether spelled as a bare rem literal or a px literal. var(--type-*)
// tokens are excluded (they clamp at or above the floor already at the
// default, comfortable density — body[data-density="compact"]'s own
// narrower clamp is a deliberate, documented user opt-in, not a floor
// violation this test polices).
func TestDraftTypeFloorNoFontSizeBelow13pxInDraftSection(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	marker := strings.Index(css, "/* Draft war room */")
	if marker < 0 {
		t.Fatal("stylesheet missing the \"/* Draft war room */\" marker this test scopes from")
	}
	section := css[marker:]

	remPattern := regexp.MustCompile(`font-size:\s*\.?(\d+(?:\.\d+)?)rem`)
	for _, m := range remPattern.FindAllStringSubmatch(section, -1) {
		value, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		if value < 0.8125 {
			t.Errorf("draft section font-size %srem is below the 0.8125rem (13px) floor: %q", m[1], m[0])
		}
	}

	pxPattern := regexp.MustCompile(`font-size:\s*(\d+(?:\.\d+)?)px`)
	for _, m := range pxPattern.FindAllStringSubmatch(section, -1) {
		value, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		if value < 13 {
			t.Errorf("draft section font-size %spx is below the 13px floor: %q", m[1], m[0])
		}
	}
}

// TestDraftCommandPillStylesheetContract is wave 7b item 1's own CSS
// contract test: the floating pill's structural rules exist — the base
// (desktop, unconditional) display: none on every pill-only element, the
// phone-width block that turns them back on and hides
// .draft-command__room in their place, the accent+pulse on-clock cue, and
// the reduced-motion override for it.
func TestDraftCommandPillStylesheetContract(t *testing.T) {
	raw, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, want := range []string{
		".draft-command__pill-meta,", ".draft-command__pill-status {", ".draft-command__pill-toggle {",
		".draft-command__pill-row {", "display: contents;",
		".draft-command__pill-caret {", ".draft-command__sheet {",
		"body.is-on-clock .draft-command__inner {", "animation: draft-pill-pulse",
		"@keyframes draft-pill-pulse {",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing draft command pill rule %q", want)
		}
	}
	if !strings.Contains(css, "body.is-on-clock .draft-command__inner {\n    animation: none;\n  }") &&
		!strings.Contains(css, "prefers-reduced-motion: reduce") {
		t.Error("stylesheet missing a prefers-reduced-motion override for the pill's own pulse animation")
	}
}

package help

import (
	"os"
	"strings"
	"testing"
)

// TestHelpHasABackToTopLink is item 6's own contract for /help: the
// shared .guide-toc jump list is already sticky at every width (see
// app/scoring's TestScoringTOCIsStickyWithScrollMarginAndBackToTop for
// the full shared-stylesheet assertion, plus why item 6's "horizontally
// scrolling" wording is deliberately not implemented for this class), and
// .guide-section already carries scroll-margin-top (public/styles.css).
// This page's own remaining piece is the "Back to top" affordance.
func TestHelpHasABackToTopLink(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, `<nav class="guide-toc help-toc" aria-label="Help center sections">`) {
		t.Error("page.gsx lost the help center jump list nav")
	}
	if !strings.Contains(source, `<a class="access-link back-to-top-link" href="#main-content">↑ Back to top</a>`) {
		t.Error("page.gsx missing the Back to top link")
	}
}

// TestHelpMappingRowHeadStaysVisibleAndStackedOnPhones is item 11's
// migration-table half (2026-09-02 audit): .help-mapping-row--head went
// display: none at the 48rem breakpoint, leaving its three
// role="columnheader" cells at 0×0 — real headers gone from the
// accessibility tree, not merely hidden from view.
func TestHelpMappingRowHeadStaysVisibleAndStackedOnPhones(t *testing.T) {
	styles, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	if !strings.Contains(css, ".help-mapping-table .help-mapping-row--head {\n    display: grid;\n  }") {
		t.Error("stylesheet must keep .help-mapping-row--head visible (display: grid) at the 48rem breakpoint")
	}
	overrideStart := strings.Index(css, ".help-mapping-row--head {\n    display: none;\n  }")
	if overrideStart < 0 {
		t.Fatal("stylesheet no longer carries the original display: none rule this test's override must beat")
	}
	winStart := strings.Index(css, ".help-mapping-table .help-mapping-row--head {\n    display: grid;\n  }")
	if winStart < overrideStart {
		t.Error(".help-mapping-table .help-mapping-row--head's display: grid override must come AFTER display: none in file order to win the cascade at equal specificity")
	}
}

// TestHelpGuideConsoleValuesWrapInsteadOfOverflowing is item 11's
// masthead-console half (2026-09-02 audit): aside.masthead-console.
// guide-console's own value column had no minimum-width override, so a
// long value (a full git SHA, before this same audit's short-hash fix)
// held the row to its own unbreakable width — 83px past the viewport at
// 1280px, invisible rather than clipped, since body already hides
// overflow-x.
func TestHelpGuideConsoleValuesWrapInsteadOfOverflowing(t *testing.T) {
	styles, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	ruleStart := strings.Index(css, ".guide-console > div > strong {")
	if ruleStart < 0 {
		t.Fatal("stylesheet missing .guide-console > div > strong rule")
	}
	ruleEnd := strings.Index(css[ruleStart:], "}")
	rule := css[ruleStart : ruleStart+ruleEnd]
	for _, want := range []string{"min-width: 0", "overflow-wrap: anywhere", "max-width"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".guide-console > div > strong must set %q: %s", want, rule)
		}
	}
}

// TestVerifiedSourceSHAIsShortenedForDisplay is item 11's short-hash
// half (2026-09-02 audit): /help and /help/<topic> printed the full
// 40-character VerifiedSourceSHA straight into an anonymous visitor's
// masthead and prose. ShortSHA's own 7-character form is what every
// render site must show now, with the full value only reachable through
// a title attribute.
func TestVerifiedSourceSHAIsShortenedForDisplay(t *testing.T) {
	if got, want := ShortSHA(VerifiedSourceSHA), VerifiedSourceSHA[:7]; got != want {
		t.Fatalf("ShortSHA(VerifiedSourceSHA) = %q, want %q", got, want)
	}
	if len(ShortSHA(VerifiedSourceSHA)) != 7 {
		t.Fatalf("ShortSHA(VerifiedSourceSHA) = %q, want exactly 7 characters", ShortSHA(VerifiedSourceSHA))
	}
	indexSource, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	topicSource, err := os.ReadFile("_topic_id/page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{string(indexSource), string(topicSource)} {
		if !strings.Contains(source, "data.source_sha_short") {
			t.Error("page.gsx must render data.source_sha_short, not the full data.source_sha, for visible text")
		}
		if !strings.Contains(source, "title={data.source_sha}") {
			t.Error("page.gsx must keep the full SHA reachable through a title attribute")
		}
	}
}

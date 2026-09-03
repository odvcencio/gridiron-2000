package main

import (
	"strings"
	"testing"
)

// TestStylesheetDropsRedundantMediaQueryReasserts is wave-7 re-audit item
// 9's own decisive contract test (yew, stylesheet weight): four rules
// this file used to repeat inside a NARROWER media query than one that
// already covers the exact same width range are gone from the narrow
// query, while the WIDER rule that alone now carries each of them still
// stands — a duplicate-block merge that changes zero computed style
// (the narrower query's own width range was always a strict subset of
// the wider one's, so removing the redundant copy leaves every viewport
// governed by the identical declaration it always was). Each check
// counts exact-text occurrences (indentation included) rather than
// parsing block boundaries: this file repeats several media-query
// conditions verbatim at unrelated points (four separate "@media
// (max-width: 54rem)" openers alone), so "the first block matching this
// opener" is not a reliable way to name the specific one under test —
// an exact literal count is unambiguous regardless of which numbered
// occurrence it is.
func TestStylesheetDropsRedundantMediaQueryReasserts(t *testing.T) {
	css := readStylesheet(t)

	for _, tt := range []struct {
		name string
		text string
		want int
	}{
		{
			name: ".hero-command gap re-assert",
			text: ".hero-command {\n    gap: var(--space-xl);\n  }",
			want: 1,
		},
		{
			name: ".pickem-row flex-wrap collapse",
			text: ".pickem-row {\n    display: flex;\n    flex-wrap: wrap;\n    align-items: center;\n    gap: var(--space-sm);\n  }",
			want: 2, // the surviving 54rem copy, plus the rail-band's own separate 900-1100px range (a real, non-redundant duplicate — see that block's own doc comment).
		},
		{
			name: ".pickem-buttons, .pickem-market flex re-assert",
			text: ".pickem-buttons,\n  .pickem-market {\n    flex: 1 1 100%;\n  }",
			want: 2, // 54rem copy plus the rail-band's own separate range, same reasoning as .pickem-row above.
		},
		{
			name: ".pickem-buttons, .pickem-status justify-content re-assert",
			text: ".pickem-buttons,\n  .pickem-status {\n    justify-content: flex-start;\n  }",
			want: 2, // 54rem copy plus the rail-band's own separate range, same reasoning as .pickem-row above.
		},
		{
			// This exact combined-selector form (both properties on one
			// standalone .pickem-buttons rule, no comma list) only ever
			// existed in the dropped 38rem block — the surviving 54rem and
			// rail-band copies both split the two properties across
			// .pickem-buttons,.pickem-market and .pickem-buttons,.pickem-
			// status, never combined onto .pickem-buttons alone.
			name: "narrow (38rem) .pickem-buttons combined re-assert (dropped)",
			text: ".pickem-buttons {\n    flex: 1 1 100%;\n    justify-content: flex-start;\n  }",
			want: 0,
		},
		{
			name: "unconditional .seat-row flex-wrap collapse",
			text: "\n.seat-row {\n  display: flex;\n  flex-wrap: wrap;\n  align-items: center;\n  gap: var(--space-sm);\n}",
			want: 1,
		},
		{
			name: "narrow (38rem) .seat-row re-assert (dropped)",
			text: "\n  .seat-row {\n    display: flex;\n    flex-wrap: wrap;\n    align-items: center;\n    gap: var(--space-sm);\n  }",
			want: 0,
		},
		{
			name: ".pickem-boards single-column collapse",
			text: ".pickem-boards {\n    grid-template-columns: 1fr;\n  }",
			want: 1,
		},
		{
			name: ".pickem-record single-column collapse (not a duplicate, must stay)",
			text: ".pickem-record {\n    grid-template-columns: 1fr;\n  }",
			want: 1,
		},
	} {
		if got := strings.Count(css, tt.text); got != tt.want {
			t.Errorf("%s: %q appears %d time(s), want %d", tt.name, tt.text, got, tt.want)
		}
	}
}

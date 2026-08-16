package emailkit

import (
	"strings"
	"unicode/utf8"
)

// wrapText greedily wraps s into lines of at most width-1 display columns:
// a word joins the current line only while the resulting line stays
// strictly under width; a candidate that would reach width starts a new
// line instead. Width is measured in runes, not bytes (finding m4): a
// multi-byte character such as "·" or "—" counts as the one column it
// actually occupies, not the two or three bytes it takes to encode. A
// single word at or past width is never broken, so a long URL or token
// still occupies one line (the caller decides whether width even applies —
// table rows and URLs never wrap, per spec section 4.3 rule 3). Multiple
// whitespace runs — including embedded newlines — collapse to single
// spaces, matching normal paragraph reflow and incidentally making a
// wrapped value immune to the newline-forging finding m3 covers for
// fields that are NOT routed through here. An empty or all-whitespace s
// returns nil.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	current := words[0]
	currentWidth := utf8.RuneCountInString(current)
	for _, word := range words[1:] {
		wordWidth := utf8.RuneCountInString(word)
		candidateWidth := currentWidth + 1 + wordWidth // +1 for the joining space
		if candidateWidth < width {
			current = current + " " + word
			currentWidth = candidateWidth
			continue
		}
		lines = append(lines, current)
		current = word
		currentWidth = wordWidth
	}
	lines = append(lines, current)
	return lines
}

// textSafe collapses every run of control characters (including \r, \n,
// and \t) in s into a single space, so a text-renderer field that is NOT
// routed through wrapText's own strings.Fields normalization (a title, a
// label, a table cell, a list item) cannot forge extra lines in the text
// part the way the paired HTML part never could (finding m3): a StatTable
// cell or Shell field with an embedded newline could otherwise fabricate a
// whole extra block of text that states something the HTML part does not.
// Route every text renderer and every Shell field through this before
// writing. The fast path returns s unchanged when it carries no control
// character, avoiding an allocation for the overwhelmingly common case.
func textSafe(s string) string {
	clean := true
	for _, r := range s {
		if isControlRune(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if isControlRune(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = r == ' '
		b.WriteRune(r)
	}
	return b.String()
}

// textSafeAll applies textSafe to every cell of a row (or a header row).
func textSafeAll(cells []string) []string {
	out := make([]string, len(cells))
	for i, cell := range cells {
		out[i] = textSafe(cell)
	}
	return out
}

// isControlRune reports whether r is a C0 control character, including
// \r, \n, and \t.
func isControlRune(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// panelValueColumn returns the column at which every panel row's value
// (and every continuation line of a wrapped value) begins: a 2-space
// indent, the longest sanitized label (finding m3), then 4 spaces, per
// spec section 4.2 ("aligned to the longest label + 4 spaces"). Label
// width is measured in runes (finding m4). Panel.appendText calls this
// directly (finding nit 3): it previously inlined the same arithmetic, so
// this helper had no production caller and only a test exercised it.
func panelValueColumn(rows []PanelRow) int {
	longest := 0
	for _, row := range rows {
		if n := utf8.RuneCountInString(textSafe(row.Label)); n > longest {
			longest = n
		}
	}
	return 2 + longest + 4
}

// padLabel left-justifies label to width display columns with trailing
// spaces, measuring width in runes rather than bytes (finding m4).
func padLabel(label string, width int) string {
	n := utf8.RuneCountInString(label)
	if n >= width {
		return label
	}
	return label + strings.Repeat(" ", width-n)
}

// columnWidths computes the display width of each column across a header
// row and every data row, so a text table's columns line up without
// wrapping any cell (table rows never wrap, per spec section 4.3 rule 3).
// Width is measured in runes (finding m4). The column count is sized from
// whichever is wider, the header or the widest data row (finding m8): a
// StatTable with no header previously sized a zero-length widths slice
// from len(header) alone, which silently dropped every row cell whose
// column index had no corresponding header column.
func columnWidths(header []string, rows [][]string) []int {
	numCols := len(header)
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	widths := make([]int, numCols)
	for i, cell := range header {
		widths[i] = utf8.RuneCountInString(cell)
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

// joinRow renders one table row with each cell left-justified to its
// column width and a 2-space gutter between columns (spec section 4.2:
// "two-space-guttered columns").
func joinRow(cells []string, widths []int) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		if i < len(widths)-1 {
			parts[i] = padLabel(cell, widths[i])
			continue
		}
		parts[i] = cell // last column never trails with padding
	}
	return strings.Join(parts, "  ")
}

// dashRow renders the underline row beneath a text table's header: one
// run of dashes per column, sized to that column's width.
func dashRow(widths []int) string {
	cells := make([]string, len(widths))
	for i, width := range widths {
		cells[i] = strings.Repeat("-", width)
	}
	return strings.Join(cells, "  ")
}

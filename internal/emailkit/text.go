package emailkit

import "strings"

// wrapText greedily wraps s into lines of at most width-1 columns: a word
// joins the current line only while the resulting line stays strictly
// under width; a candidate that would reach width starts a new line
// instead. A single word at or past width is never broken, so a long URL
// or token still occupies one line (the caller decides whether width even
// applies — table rows and URLs never wrap, per spec section 4.3 rule 3).
// Multiple whitespace runs collapse to single spaces, matching normal
// paragraph reflow. An empty or all-whitespace s returns nil.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if len(candidate) < width {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
	}
	lines = append(lines, current)
	return lines
}

// panelValueColumn returns the column at which every panel row's value
// (and every continuation line of a wrapped value) begins: a 2-space
// indent, the longest label, then 4 spaces, per spec section 4.2
// ("aligned to the longest label + 4 spaces").
func panelValueColumn(rows []PanelRow) int {
	longest := 0
	for _, row := range rows {
		if len(row.Label) > longest {
			longest = len(row.Label)
		}
	}
	return 2 + longest + 4
}

// padLabel left-justifies label to width columns with trailing spaces.
func padLabel(label string, width int) string {
	if len(label) >= width {
		return label
	}
	return label + strings.Repeat(" ", width-len(label))
}

// columnWidths computes the display width of each column across a header
// row and every data row, so a text table's columns line up without
// wrapping any cell (table rows never wrap, per spec section 4.3 rule 3).
func columnWidths(header []string, rows [][]string) []int {
	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = len(cell)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if len(cell) > widths[i] {
				widths[i] = len(cell)
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

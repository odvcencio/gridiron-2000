package emailkit

import (
	"strconv"
	"strings"
)

// Headline is the invite's "YOU'RE IN." pattern: a big uppercase title and
// a lede sentence below it.
// HTML: 32px 800-weight uppercase title, 15px lede below.
// Text: TITLE on its own line (uppercase), blank line, wrapped lede.
type Headline struct{ Title, Lede string }

func (h Headline) appendHTML(b *strings.Builder) {
	b.WriteString(`<tr>
<td style="padding:14px 32px 0 32px;">
<div style="color:`)
	b.WriteString(ColorInk)
	b.WriteString(`; font-family:`)
	b.WriteString(FontSans)
	b.WriteString(`; font-size:32px; font-weight:800; letter-spacing:-0.03em; text-transform:uppercase; line-height:1.08;">`)
	b.WriteString(escapeHTML(h.Title))
	b.WriteString(`</div>
<div style="color:`)
	b.WriteString(ColorBody)
	b.WriteString(`; font-family:`)
	b.WriteString(FontSans)
	b.WriteString(`; font-size:15px; line-height:1.6; margin-top:12px;">`)
	b.WriteString(escapeHTML(h.Lede))
	b.WriteString(`</div>
</td>
</tr>
`)
}

func (h Headline) appendText(b *strings.Builder) {
	b.WriteString(h.Title)
	if h.Lede == "" {
		return
	}
	b.WriteString("\n\n")
	b.WriteString(strings.Join(wrapText(h.Lede, TextWrapWidth), "\n"))
}

// PanelRow is one labeled fact in a Panel.
type PanelRow struct{ Label, Value string }

// Panel is the invite's bordered info card. Rows render as an 88px mono
// uppercase label + sans value.
// Text twin: "  LABEL     value" aligned to the longest label + 4 spaces.
type Panel struct{ Rows []PanelRow }

func (p Panel) appendHTML(b *strings.Builder) {
	b.WriteString(`<tr>
<td style="padding:24px 32px 0 32px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%; border:1px solid `)
	b.WriteString(ColorBorder)
	b.WriteString(`; border-radius:4px; background-color:`)
	b.WriteString(ColorPanel)
	b.WriteString(`;">
`)
	for i, row := range p.Rows {
		b.WriteString(`<tr>
<td style="padding:16px 20px;`)
		if i < len(p.Rows)-1 {
			b.WriteString(` border-bottom:1px solid `)
			b.WriteString(ColorBorder)
			b.WriteString(`;`)
		}
		b.WriteString(`">
<span style="display:inline-block; width:88px; color:`)
		b.WriteString(ColorMuted)
		b.WriteString(`; font-family:`)
		b.WriteString(FontMono)
		b.WriteString(`; font-size:11px; letter-spacing:0.06em; text-transform:uppercase; vertical-align:top;">`)
		b.WriteString(escapeHTML(row.Label))
		b.WriteString(`</span>
<span style="color:`)
		b.WriteString(ColorInk)
		b.WriteString(`; font-family:`)
		b.WriteString(FontSans)
		b.WriteString(`; font-size:14px;">`)
		b.WriteString(escapeHTML(row.Value))
		b.WriteString(`</span>
</td>
</tr>
`)
	}
	b.WriteString(`</table>
</td>
</tr>
`)
}

func (p Panel) appendText(b *strings.Builder) {
	if len(p.Rows) == 0 {
		return
	}
	maxLabel := 0
	for _, row := range p.Rows {
		if len(row.Label) > maxLabel {
			maxLabel = len(row.Label)
		}
	}
	col := 2 + maxLabel + 4
	valueWidth := TextWrapWidth - col
	lines := make([]string, 0, len(p.Rows))
	for _, row := range p.Rows {
		wrapped := wrapText(row.Value, valueWidth)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		lines = append(lines, "  "+padLabel(row.Label, maxLabel)+"    "+wrapped[0])
		indent := strings.Repeat(" ", col)
		for _, cont := range wrapped[1:] {
			lines = append(lines, indent+cont)
		}
	}
	b.WriteString(strings.Join(lines, "\n"))
}

// CTA is the aqua button. Text twin: "  -> LABEL: URL". Label may carry a
// trailing " →" for the HTML button face; the text twin strips it since
// the "->" prefix already supplies the arrow.
type CTA struct{ Label, URL string }

func (c CTA) appendHTML(b *strings.Builder) {
	b.WriteString(`<tr>
<td align="center" style="padding:28px 32px 0 32px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0">
<tr>
<td style="border-radius:2px; background-color:`)
	b.WriteString(ColorAccent)
	b.WriteString(`;">
<a href="`)
	b.WriteString(escapeHTML(c.URL))
	b.WriteString(`" style="display:inline-block; padding:14px 30px; color:`)
	b.WriteString(ColorGround)
	b.WriteString(`; font-family:`)
	b.WriteString(FontSans)
	b.WriteString(`; font-size:14px; font-weight:800; letter-spacing:0.04em; text-transform:uppercase; text-decoration:none; border-radius:2px;">`)
	b.WriteString(escapeHTML(c.Label))
	b.WriteString(`</a>
</td>
</tr>
</table>
</td>
</tr>
`)
}

func (c CTA) appendText(b *strings.Builder) {
	label := strings.TrimRight(c.Label, " →") // trim trailing " →"
	b.WriteString("  -> ")
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(c.URL)
}

// StatTable is a bordered table: mono uppercase header row, sans body
// rows. Mark selects an accent-colored row (the recipient), -1 for none.
// Text twin: two-space-guttered columns, header row, dashes underline.
// The marked row carries a "* " line prefix in text (every other row and
// the header/dash rows carry "  "), so the accent color's meaning survives
// in plain text too (spec section 4.3, rule 5).
type StatTable struct {
	Title  string // mono kicker above, "LABEL //" style; "" hides it
	Header []string
	Rows   [][]string
	Mark   int
}

func (s StatTable) appendHTML(b *strings.Builder) {
	b.WriteString(`<tr>
<td style="padding:24px 32px 0 32px;">
`)
	if s.Title != "" {
		b.WriteString(`<div style="color:`)
		b.WriteString(ColorMuted)
		b.WriteString(`; font-family:`)
		b.WriteString(FontMono)
		b.WriteString(`; font-size:11px; letter-spacing:0.08em; text-transform:uppercase; margin-bottom:12px;">`)
		b.WriteString(escapeHTML(s.Title))
		b.WriteString(` //</div>
`)
	}
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%; border:1px solid `)
	b.WriteString(ColorBorder)
	b.WriteString(`; border-radius:4px;">
<tr>
`)
	for _, cell := range s.Header {
		b.WriteString(`<td style="padding:10px 14px; border-bottom:1px solid `)
		b.WriteString(ColorBorder)
		b.WriteString(`; color:`)
		b.WriteString(ColorMuted)
		b.WriteString(`; font-family:`)
		b.WriteString(FontMono)
		b.WriteString(`; font-size:11px; letter-spacing:0.06em; text-transform:uppercase;">`)
		b.WriteString(escapeHTML(cell))
		b.WriteString(`</td>
`)
	}
	b.WriteString(`</tr>
`)
	for i, row := range s.Rows {
		rowColor := ColorBody
		background := ""
		if i == s.Mark {
			rowColor = ColorAccent
			background = ` background-color:` + ColorPanel + `;`
		}
		border := ""
		if i < len(s.Rows)-1 {
			border = ` border-bottom:1px solid ` + ColorBorder + `;`
		}
		b.WriteString(`<tr style="`)
		b.WriteString(background)
		b.WriteString(`">
`)
		for _, cell := range row {
			b.WriteString(`<td style="padding:10px 14px;`)
			b.WriteString(border)
			b.WriteString(` color:`)
			b.WriteString(rowColor)
			b.WriteString(`; font-family:`)
			b.WriteString(FontSans)
			b.WriteString(`; font-size:14px;">`)
			b.WriteString(escapeHTML(cell))
			b.WriteString(`</td>
`)
		}
		b.WriteString(`</tr>
`)
	}
	b.WriteString(`</table>
</td>
</tr>
`)
}

func (s StatTable) appendText(b *strings.Builder) {
	var lines []string
	if s.Title != "" {
		lines = append(lines, s.Title+" //")
	}
	widths := columnWidths(s.Header, s.Rows)
	if len(s.Header) > 0 {
		lines = append(lines, "  "+joinRow(s.Header, widths))
		lines = append(lines, "  "+dashRow(widths))
	}
	for i, row := range s.Rows {
		marker := "  "
		if i == s.Mark {
			marker = "* "
		}
		lines = append(lines, marker+joinRow(row, widths))
	}
	b.WriteString(strings.Join(lines, "\n"))
}

// PickList is the invite's "NEXT STEPS //" numbered list. Aqua mono
// numerals.
// Text twin: "  1. item" lines under "LABEL //".
type PickList struct {
	Title string
	Items []string
}

func (p PickList) appendHTML(b *strings.Builder) {
	b.WriteString(`<tr>
<td style="padding:24px 32px 0 32px;">
<div style="color:`)
	b.WriteString(ColorMuted)
	b.WriteString(`; font-family:`)
	b.WriteString(FontMono)
	b.WriteString(`; font-size:11px; letter-spacing:0.08em; text-transform:uppercase; margin-bottom:12px;">`)
	b.WriteString(escapeHTML(p.Title))
	b.WriteString(` //</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
`)
	for i, item := range p.Items {
		b.WriteString(`<tr><td style="padding:5px 0; color:`)
		b.WriteString(ColorBody)
		b.WriteString(`; font-family:`)
		b.WriteString(FontSans)
		b.WriteString(`; font-size:14px;"><span style="color:`)
		b.WriteString(ColorAccent)
		b.WriteString(`; font-family:`)
		b.WriteString(FontMono)
		b.WriteString(`; font-weight:700;">`)
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(`.</span>&nbsp; `)
		b.WriteString(escapeHTML(item))
		b.WriteString(`</td></tr>
`)
	}
	b.WriteString(`</table>
</td>
</tr>
`)
}

func (p PickList) appendText(b *strings.Builder) {
	lines := make([]string, 0, len(p.Items)+1)
	lines = append(lines, p.Title+" //")
	for i, item := range p.Items {
		lines = append(lines, "  "+strconv.Itoa(i+1)+". "+item)
	}
	b.WriteString(strings.Join(lines, "\n"))
}

// Note is one body paragraph. Text twin: wrapped paragraph.
type Note struct{ Text string }

func (n Note) appendHTML(b *strings.Builder) {
	b.WriteString(`<tr>
<td style="padding:24px 32px 0 32px;">
<div style="color:`)
	b.WriteString(ColorBody)
	b.WriteString(`; font-family:`)
	b.WriteString(FontSans)
	b.WriteString(`; font-size:14px; line-height:1.6;">`)
	b.WriteString(escapeHTML(n.Text))
	b.WriteString(`</div>
</td>
</tr>
`)
}

func (n Note) appendText(b *strings.Builder) {
	b.WriteString(strings.Join(wrapText(n.Text, TextWrapWidth), "\n"))
}

// Divider is a border-top rule. Text twin: a blank line — the blank-line
// separators Render already places before and after every block double up
// around a Divider (which itself emits nothing), reading as one extra
// line of vertical space.
type Divider struct{}

func (Divider) appendHTML(b *strings.Builder) {
	b.WriteString(`<tr>
<td style="padding:20px 32px 0 32px;">
<div style="border-top:1px solid `)
	b.WriteString(ColorBorder)
	b.WriteString(`;"></div>
</td>
</tr>
`)
}

func (Divider) appendText(*strings.Builder) {}

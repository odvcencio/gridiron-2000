// Package emailkit is the notification email design system: a shell plus
// seven block primitives extracted from the league invite email
// (internal/league/admin.go:160-256). Every block renders its own HTML and
// its own text twin from one struct, so multipart drift becomes impossible
// by construction (spec section 4.3, rule 1).
//
// The package is stdlib-only and imports nothing from league, mailer, or
// fantasy (spec section 4.1). Callers in the league package fill Shell and
// build blocks; emailkit never sees a subject line or a recipient.
package emailkit

import (
	"html"
	"strconv"
	"strings"
)

// Palette and metrics, extracted verbatim from inviteEmailHTMLTemplate
// (internal/league/admin.go:160-256).
const (
	ColorGround = "#070A16" // page ground
	ColorCard   = "#0B1024" // 600px card ground
	ColorPanel  = "#131B3B" // info-panel ground
	ColorBorder = "#26305A"
	ColorAccent = "#38E8FF" // aqua: signal line, CTA, list numerals
	ColorInk    = "#F7F4EA" // headlines, values
	ColorBody   = "#C2CAE1" // body text
	ColorMuted  = "#8995B8" // mono labels
	ColorFaint  = "#5C6690" // footer joke

	CardWidth = 600

	FontSans = "-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"
	FontMono = "'SFMono-Regular',Consolas,Menlo,monospace"

	// TextWrapWidth is the text-part wrap column. A word is added to the
	// current line only while the resulting line stays strictly under this
	// column (see wrapText in text.go); a candidate that would reach or
	// pass column 72 starts a new line instead. This reproduces the spec's
	// section 5 worked example byte-for-byte.
	TextWrapWidth = 72
)

// Shell frames one email: header bar, signal line, body blocks, footer.
// All fields are operator- or user-influenced and are HTML-escaped at
// render time. The league package fills identity fields from league.json
// (cfg.Name, cfg.ShortCode, cfg.Tagline, cfg.URL per the productization
// spec), so every template rebrands with the config.
type Shell struct {
	Wordmark   string // "GRIDIRON 2000"
	ShortCode  string // "G2K" badge
	Tagline    string // "DYNASTY FANTASY LEAGUE"
	Signal     string // "DRAFT LIVE // PICK 3.07" — rendered "● {Signal}"
	Signoff    string // "— The Commissioner"
	FooterJoke string // cfg.Copy.FooterLine
	PrefLine   string // "You hold a seat in {name}. {category} alerts: on."
	PrefURL    string // "{url}/settings"
}

// Block is one email content primitive. Its methods are unexported, so
// only types declared in this package (blocks.go) can implement it: the
// block set is closed by design. A block cannot appear in one rendered
// part without the other; the pairing is structural, not disciplined
// (spec section 4.2).
type Block interface {
	appendHTML(b *strings.Builder) // escapes every field
	appendText(b *strings.Builder) // raw text, wrapped at 72 columns
}

// Render walks the shell and every block once and emits both parts: the
// header bar, the signal line, each block in order, and the footer. Text
// sections are separated by one blank line; the footer's four lines are
// not blank-separated from each other. HTML escapes every shell field;
// text emits every shell field raw (spec section 4.3, rule 2).
func Render(shell Shell, blocks []Block) (text, html string) {
	return renderText(shell, blocks), renderHTML(shell, blocks)
}

func renderText(shell Shell, blocks []Block) string {
	var b strings.Builder
	b.WriteString(shell.Wordmark)
	b.WriteString(" // ")
	b.WriteString(shell.Tagline)
	b.WriteString("\n\n* ")
	b.WriteString(shell.Signal)
	for _, block := range blocks {
		b.WriteString("\n\n")
		block.appendText(&b)
	}
	b.WriteString("\n\n")
	b.WriteString(shell.Signoff)
	b.WriteString("\n")
	b.WriteString(shell.FooterJoke)
	b.WriteString("\n")
	b.WriteString(shell.PrefLine)
	b.WriteString("\nManage: ")
	b.WriteString(shell.PrefURL)
	return b.String()
}

func renderHTML(shell Shell, blocks []Block) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>`)
	b.WriteString(escapeHTML(shell.Wordmark))
	b.WriteString(`</title>
</head>
<body style="margin:0; padding:0; background-color:`)
	b.WriteString(ColorGround)
	b.WriteString(`;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%; background-color:`)
	b.WriteString(ColorGround)
	b.WriteString(`; margin:0; padding:0;">
<tr>
<td align="center" style="padding:32px 16px;">
<table role="presentation" width="`)
	b.WriteString(strconv.Itoa(CardWidth))
	b.WriteString(`" cellpadding="0" cellspacing="0" border="0" style="width:`)
	b.WriteString(strconv.Itoa(CardWidth))
	b.WriteString(`px; max-width:`)
	b.WriteString(strconv.Itoa(CardWidth))
	b.WriteString(`px; background-color:`)
	b.WriteString(ColorCard)
	b.WriteString(`; border:1px solid `)
	b.WriteString(ColorBorder)
	b.WriteString(`; border-radius:4px;">
<tr>
<td style="padding:24px 32px 20px 32px; border-bottom:1px solid `)
	b.WriteString(ColorBorder)
	b.WriteString(`;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0">
<tr>
<td width="44" valign="middle" style="padding-right:12px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="border:1px solid `)
	b.WriteString(ColorAccent)
	b.WriteString(`; border-radius:2px;"><tr><td style="width:40px; height:40px; text-align:center; vertical-align:middle; color:`)
	b.WriteString(ColorAccent)
	b.WriteString(`; font-family:`)
	b.WriteString(FontMono)
	b.WriteString(`; font-size:13px; font-weight:700;">`)
	b.WriteString(escapeHTML(shell.ShortCode))
	b.WriteString(`</td></tr></table>
</td>
<td valign="middle">
<div style="color:`)
	b.WriteString(ColorInk)
	b.WriteString(`; font-family:`)
	b.WriteString(FontSans)
	b.WriteString(`; font-size:19px; font-weight:800; letter-spacing:-0.02em; text-transform:uppercase;">`)
	b.WriteString(escapeHTML(shell.Wordmark))
	b.WriteString(`</div>
<div style="color:`)
	b.WriteString(ColorMuted)
	b.WriteString(`; font-family:`)
	b.WriteString(FontMono)
	b.WriteString(`; font-size:11px; letter-spacing:0.08em; text-transform:uppercase; margin-top:3px;">`)
	b.WriteString(escapeHTML(shell.Tagline))
	b.WriteString(`</div>
</td>
</tr>
</table>
</td>
</tr>
<tr>
<td style="padding:20px 32px 0 32px;">
<div style="color:`)
	b.WriteString(ColorAccent)
	b.WriteString(`; font-family:`)
	b.WriteString(FontMono)
	b.WriteString(`; font-size:12px; letter-spacing:0.08em; text-transform:uppercase;">&#9679; `)
	b.WriteString(escapeHTML(shell.Signal))
	b.WriteString(`</div>
</td>
</tr>
`)
	for _, block := range blocks {
		block.appendHTML(&b)
	}
	b.WriteString(`<tr>
<td style="padding:28px 32px 32px 32px;">
<div style="border-top:1px solid `)
	b.WriteString(ColorBorder)
	b.WriteString(`; padding-top:20px; color:`)
	b.WriteString(ColorBody)
	b.WriteString(`; font-family:`)
	b.WriteString(FontSans)
	b.WriteString(`; font-size:14px;">`)
	b.WriteString(escapeHTML(shell.Signoff))
	b.WriteString(`</div>
<div style="color:`)
	b.WriteString(ColorFaint)
	b.WriteString(`; font-family:`)
	b.WriteString(FontMono)
	b.WriteString(`; font-size:10px; letter-spacing:0.04em; text-transform:uppercase; margin-top:10px;">`)
	b.WriteString(escapeHTML(shell.FooterJoke))
	b.WriteString(`</div>
<div style="color:`)
	b.WriteString(ColorFaint)
	b.WriteString(`; font-family:`)
	b.WriteString(FontMono)
	b.WriteString(`; font-size:10px; letter-spacing:0.04em; margin-top:6px;">`)
	b.WriteString(escapeHTML(shell.PrefLine))
	b.WriteString(` Manage: <a href="`)
	b.WriteString(escapeHTML(shell.PrefURL))
	b.WriteString(`" style="color:`)
	b.WriteString(ColorMuted)
	b.WriteString(`;">`)
	b.WriteString(escapeHTML(shell.PrefURL))
	b.WriteString(`</a></div>
</td>
</tr>
</table>
</td>
</tr>
</table>
</body>
</html>
`)
	return b.String()
}

// escapeHTML is the one HTML-escape entry point every block and the shell
// use, so the escaping rule (spec section 4.3, rule 2) is enforced in one
// place.
func escapeHTML(s string) string {
	return html.EscapeString(s)
}

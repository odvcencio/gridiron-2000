package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// gsxReturnMarkupPattern matches the line that opens a component's markup
// block: "return <..." (a plain element or a fragment, "return <>") or,
// less commonly, "return (<...". GoSX components in this codebase never
// wrap a JSX return in an intervening statement, so the line that starts
// with "return" and is immediately followed by "<" (through at most one
// open paren) is always the markup block's own first line.
var gsxReturnMarkupPattern = regexp.MustCompile(`^\s*return\s+\(?<`)

// gsxLineCommentPattern and gsxBlockCommentPattern match a Go-style
// comment sitting at the start of a line (after leading whitespace) —
// the shape a GoSX markup block never strips at compile time the way
// plain Go source does. See TestNoCommentLinesInsideGSXMarkup's own doc
// comment for the full defect this catches.
var (
	gsxLineCommentPattern  = regexp.MustCompile(`^\s*//`)
	gsxBlockCommentPattern = regexp.MustCompile(`^\s*/\*`)
)

// gsxMarkupCommentSite is one line flagged inside a return <...> markup
// block.
type gsxMarkupCommentSite struct {
	file string
	line int
	text string
}

// gsxLeadingWhitespaceLen counts the leading spaces/tabs on a line, used
// to find the markup block's own closing brace: the first "}"-only line
// at a shallower indent than the "return <..." line that opened the
// block.
func gsxLeadingWhitespaceLen(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' || r == '\t' {
			n++
			continue
		}
		break
	}
	return n
}

// scanGSXFileForMarkupComments walks one .gsx file's lines, tracking
// "inside markup" from a line matching gsxReturnMarkupPattern until the
// closing "}" at a shallower indent (that function or component's own
// close), and flags every "//" or "/*" line found inside that span.
//
// One shape is allowed without being flagged: a bare "//" with nothing
// else on the line. This is the site's own "LABEL // detail" divider
// glyph — every section-index span in app/*.gsx uses the same
// convention (e.g. "01 // MATCHUP PREVIEW"), traced back to the original
// "GRIDIRON 2000 // Eight seats. One trophy." tagline. Between two
// independently tagged elements (app/page.gsx's live-bound score-ticker,
// app/layout.gsx's footer divider), it can render on its own line with
// no adjacent word — but a genuine leftover developer comment is never
// contentless: every confirmed defect this test exists to catch (the
// wave-7 app/team/page.gsx re-audit comment, and app/draft/page.gsx's
// own matching leaks) carries real explanatory prose after the "//" on
// every line. See navigation_layout_test.go's assertNoStrayCommentLines
// and app/team/page_render_test.go's assertNoStrayCommentLines for the
// matching render-level guards, which apply the identical exception for
// the identical reason.
func scanGSXFileForMarkupComments(path string) ([]gsxMarkupCommentSite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var sites []gsxMarkupCommentSite
	insideMarkup := false
	markupIndent := 0
	for i, line := range lines {
		if !insideMarkup {
			if gsxReturnMarkupPattern.MatchString(line) {
				insideMarkup = true
				markupIndent = gsxLeadingWhitespaceLen(line)
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "}" && gsxLeadingWhitespaceLen(line) < markupIndent {
			insideMarkup = false
			continue
		}
		switch {
		case gsxBlockCommentPattern.MatchString(line):
			sites = append(sites, gsxMarkupCommentSite{file: path, line: i + 1, text: trimmed})
		case gsxLineCommentPattern.MatchString(line) && trimmed != "//":
			sites = append(sites, gsxMarkupCommentSite{file: path, line: i + 1, text: trimmed})
		}
	}
	return sites, nil
}

// TestNoCommentLinesInsideGSXMarkup is a contract test against the
// wave-7 re-audit's production defect: a Go-style "//" (or "/* */")
// comment line placed inside a .gsx markup block (between the tags of a
// "return <...>") is never stripped the way a plain Go comment is — GoSX
// renders it as visible text. app/team/page.gsx:609-617 shipped exactly
// this defect (a nine-line developer comment inside the roster-shape
// slot markup, repeating once per slot — 72 visible lines on /team) and
// app/draft/page.gsx carries its own matching leak this test also
// flags. The fix is always to move the comment onto the enclosing
// function or component's own Go doc comment, or into Go code above the
// return, never to leave it inside the markup — see
// TeamLineupRegion's own doc comment (app/team/page.gsx) for the fixed
// shape.
//
// This walks every app/**/*.gsx file, not just the files this worker
// owns: app/draft/page.gsx, app/board/page.gsx, and app/players/page.gsx
// belong to a sibling worker fixing the same class of defect in
// parallel, so this test may stay red on those three files until that
// worker's own fix merges. The failure message lists every remaining
// site as "file:line: text" specifically so a coordinator reading the
// test log can see what is left, and by whom.
func TestNoCommentLinesInsideGSXMarkup(t *testing.T) {
	var files []string
	err := filepath.WalkDir("app", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".gsx") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk app/ for .gsx files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("found no .gsx files under app/ — walk is broken")
	}
	sort.Strings(files)

	var allSites []gsxMarkupCommentSite
	for _, file := range files {
		sites, err := scanGSXFileForMarkupComments(file)
		if err != nil {
			t.Fatalf("scan %s: %v", file, err)
		}
		allSites = append(allSites, sites...)
	}

	if len(allSites) == 0 {
		return
	}
	var report strings.Builder
	for _, site := range allSites {
		fmt.Fprintf(&report, "%s:%d: %s\n", site.file, site.line, site.text)
	}
	t.Errorf("found %d Go-style comment line(s) inside .gsx markup (GoSX renders these as visible page text, not source-only comments):\n%s", len(allSites), report.String())
}

package emailkit

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// readGolden loads testdata/name and fails the test if it is missing.
func readGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(raw)
}

// --- Test-plan item 1: per-block primitive golden tests -------------------
//
// Each block renders its own HTML and text twin directly (bypassing Shell),
// against a fixed fixture, and is compared to testdata/block_{name}.{txt,html}.

func TestBlockGoldens(t *testing.T) {
	cases := []struct {
		name  string
		block Block
	}{
		{"headline", Headline{
			Title: "THE TAPE IS SEALED.",
			Lede:  "Twenty-two managers set their boards, and the room delivered every single pick on schedule without one dropped connection or a single contested trade.",
		}},
		{"panel", Panel{Rows: []PanelRow{
			{Label: "DRAFT", Value: "Sat · Aug 22 at 4:00 PM ET"},
			{Label: "VENUE", Value: "During the Dolphins preseason game — bring both screens, the second one is for the board."},
			{Label: "YOUR KEY", Value: "Sign in with Google as manager@example.com"},
		}}},
		{"cta", CTA{Label: "SEE THE BOARD →", URL: "https://gridiron.draco.quest/draft"}},
		{"stattable", StatTable{
			Title:  "AROUND THE LEAGUE",
			Header: []string{"SLOT", "TEAM", "MANAGER"},
			Rows: [][]string{
				{"1", "Aqua 1", "Dana"},
				{"2", "Orange 3", "Marcus"},
				{"3", "Aqua 4", "Priya"},
			},
			MarkRow: 2, // 1-based (finding m5): marks the same "Orange 3 / Marcus" row the old Mark: 1 did
		}},
		{"picklist", PickList{
			Title: "NEXT STEPS",
			Items: []string{
				"Claim your seat",
				"Rename your team",
				"Build your Big Board",
				"Read the Rules page",
			},
		}},
		{"note", Note{Text: "Further auto picks stay quiet until you return. The recap lists everything once the draft wraps, so nothing you missed goes unrecorded."}},
		{"divider", Divider{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var text, html strings.Builder
			tc.block.appendText(&text)
			tc.block.appendHTML(&html)

			wantText := readGolden(t, "block_"+tc.name+".txt")
			wantHTML := readGolden(t, "block_"+tc.name+".html")

			if text.String() != wantText {
				t.Errorf("text mismatch:\n got: %q\nwant: %q", text.String(), wantText)
			}
			if html.String() != wantHTML {
				t.Errorf("html mismatch:\n got: %q\nwant: %q", html.String(), wantHTML)
			}
		})
	}
}

// --- Section 5 worked example: a normative fixture ------------------------
//
// The composition and its rendered text part are given verbatim in the
// spec. Render must reproduce the text block byte-for-byte; that output
// then serves as testdata/n5_worked_example.txt, a golden.

func onTheClockShell() Shell {
	return Shell{
		Wordmark:   "GRIDIRON 2000",
		ShortCode:  "G2K",
		Tagline:    "DYNASTY FANTASY LEAGUE",
		Signal:     "DRAFT LIVE // PICK 3.07 // OVERALL 23",
		Signoff:    "— The Commissioner",
		FooterJoke: "GRIDIRON 2000 · Eight seats. One trophy. Permanent group-chat evidence.",
		PrefLine:   "You hold a seat in GRIDIRON 2000. Draft-room alerts: on.",
		PrefURL:    "https://gridiron.draco.quest/settings",
	}
}

func onTheClockBlocks() []Block {
	return []Block{
		Headline{
			Title: "YOU'RE ON THE CLOCK.",
			Lede: "Twenty-two picks are on the tape. The board just turned to " +
				"your seat and stopped. Everything from here is your call — " +
				"or your autopick's.",
		},
		Panel{Rows: []PanelRow{
			{Label: "PICK", Value: "3.07 · overall 23"},
			{Label: "CLOCK", Value: "0:20 away cap armed — the server reads " +
				"you as AWAY. Reconnect and your full 1:30 comes back."},
			{Label: "YOUR BOARD", Value: "Jahmyr Gibbs · RB · DET still sits " +
				"at #1. Two of your top five are already gone."},
		}},
		CTA{Label: "TAKE YOUR PICK →", URL: "https://gridiron.draco.quest/draft"},
		Note{Text: "If the cap hits zero, the server drafts the top of your Big Board for " +
			"you. No board? Best available by ADP. Either way the tape reads AUTO " +
			"next to your name — forever. One tap fixes that."},
	}
}

func TestOnTheClockWorkedExampleMatchesSpec(t *testing.T) {
	text, html := Render(onTheClockShell(), onTheClockBlocks())

	want := readGolden(t, "n5_worked_example.txt")
	if text != want {
		t.Fatalf("text does not match the section 5 worked example.\n got:\n%s\nwant:\n%s", text, want)
	}

	// finding M2: an HTML golden pairs with the .txt one, per spec section
	// 4.3 rule 4 ("compare both parts against testdata/{name}.txt and
	// testdata/{name}.html"). Previously only the .txt golden existed, and
	// the substring checks that stood in for the HTML side did not catch
	// escapeHTML(shell.Signoff) or escapeHTML(shell.PrefLine) being deleted
	// from renderHTML — both passed the whole suite before this golden
	// existed.
	wantHTML := readGolden(t, "n5_worked_example.html")
	if html != wantHTML {
		t.Fatalf("html does not match the section 5 worked example.\n got:\n%s\nwant:\n%s", html, wantHTML)
	}
}

// --- Test-plan item 3: escaping --------------------------------------------
//
// Attacker-influenced values (player names, team names, broadcast text,
// URLs) render escaped in HTML and raw in text.

func TestEscaping(t *testing.T) {
	nasty := `<script>alert(1)</script> & "quoted" 'single'`
	wantEscaped := `&lt;script&gt;alert(1)&lt;/script&gt; &amp; &#34;quoted&#34; &#39;single&#39;`

	blocks := []Block{
		Headline{Title: nasty, Lede: nasty},
		Panel{Rows: []PanelRow{{Label: nasty, Value: nasty}}},
		CTA{Label: nasty, URL: nasty},
		StatTable{Title: nasty, Header: []string{nasty}, Rows: [][]string{{nasty}}},
		PickList{Title: nasty, Items: []string{nasty}},
		Note{Text: nasty},
	}

	for _, block := range blocks {
		t.Run(fmt.Sprintf("%T", block), func(t *testing.T) {
			var text, html strings.Builder
			block.appendText(&text)
			block.appendHTML(&html)

			if !strings.Contains(text.String(), nasty) {
				t.Errorf("text must carry the raw value unescaped:\n%s", text.String())
			}
			if strings.Contains(html.String(), "<script>") {
				t.Errorf("html must not carry an unescaped <script> tag:\n%s", html.String())
			}
			if !strings.Contains(html.String(), wantEscaped) {
				t.Errorf("html must carry the html.EscapeString form:\n%s", html.String())
			}
		})
	}
}

func TestShellFieldsEscapeInHTMLRawInText(t *testing.T) {
	shell := Shell{
		Wordmark:   `<script>x</script>`,
		ShortCode:  `&"'`,
		Tagline:    `<b>bold</b>`,
		Signal:     `<i>italic</i>`,
		Signoff:    `<u>u</u>`,
		FooterJoke: `<script>y</script>`,
		PrefLine:   `<script>z</script>`,
		PrefURL:    `https://example.com/?a=<script>`,
	}
	text, html := Render(shell, nil)

	for _, raw := range []string{shell.Wordmark, shell.Tagline, shell.Signal, shell.Signoff, shell.FooterJoke, shell.PrefLine, shell.PrefURL} {
		if !strings.Contains(text, raw) {
			t.Errorf("text missing raw shell value %q", raw)
		}
	}
	if strings.Contains(html, "<script>x</script>") || strings.Contains(html, "<script>y</script>") || strings.Contains(html, "<script>z</script>") {
		t.Errorf("html must escape every shell field:\n%s", html)
	}
}

// --- Test-plan item 4: reflective pairing test -----------------------------
//
// Walks every exported block type via reflection, sets every exported
// string-bearing field to a unique sentinel, and asserts both rendered
// parts carry every sentinel. A block that adds a field without wiring it
// into both renderers fails here (spec section 4.3, rule 1).

// sentinelFieldValue wraps sentinel s in an http(s) URL when fieldName
// names a URL field (CTA.URL, Shell.PrefURL): finding nit 9 makes those
// fields render only when they carry an allowed scheme, so the reflective
// pairing test needs a value that survives the scheme check.
func sentinelFieldValue(fieldName, sentinel string) string {
	if strings.HasSuffix(fieldName, "URL") {
		return "https://example.com/" + sentinel
	}
	return sentinel
}

// sentinelValue sets every exported string field (including []string,
// []PanelRow, and [][]string fields) on the struct pointed to by v to a
// unique sentinel value made only of letters and digits, so HTML escaping
// never changes it, and returns every sentinel used.
func sentinelValue(v reflect.Value, prefix string, counter *int) []string {
	var sentinels []string
	elem := v.Elem()
	typ := elem.Type()
	for i := 0; i < elem.NumField(); i++ {
		field := elem.Field(i)
		fieldType := typ.Field(i)
		if !fieldType.IsExported() {
			continue
		}
		name := prefix + fieldType.Name
		switch field.Kind() {
		case reflect.String:
			*counter++
			s := fmt.Sprintf("ZQSENTINEL%s%d", name, *counter)
			// finding nit 9: a URL field only reaches either rendered part
			// when it parses as an http(s) URL, so its stored value must
			// look like one; the sentinel substring asserted below stays
			// exactly s, since it is still a substring of the full value.
			field.SetString(sentinelFieldValue(fieldType.Name, s))
			sentinels = append(sentinels, s)
		case reflect.Slice:
			elemType := fieldType.Type.Elem()
			switch {
			case elemType.Kind() == reflect.String:
				*counter++
				s := fmt.Sprintf("ZQSENTINEL%s%d", name, *counter)
				field.Set(reflect.ValueOf([]string{s}))
				sentinels = append(sentinels, s)
			case elemType.Kind() == reflect.Slice && elemType.Elem().Kind() == reflect.String:
				*counter++
				s := fmt.Sprintf("ZQSENTINEL%s%d", name, *counter)
				field.Set(reflect.ValueOf([][]string{{s}}))
				sentinels = append(sentinels, s)
			case elemType.Kind() == reflect.Struct:
				one := reflect.New(elemType)
				inner := sentinelValue(one, name, counter)
				sliceVal := reflect.MakeSlice(fieldType.Type, 1, 1)
				sliceVal.Index(0).Set(one.Elem())
				field.Set(sliceVal)
				sentinels = append(sentinels, inner...)
			}
		}
	}
	return sentinels
}

func TestBlockFieldPairingReflective(t *testing.T) {
	blockTypes := []reflect.Type{
		reflect.TypeOf(Headline{}),
		reflect.TypeOf(Panel{}),
		reflect.TypeOf(CTA{}),
		reflect.TypeOf(StatTable{}),
		reflect.TypeOf(PickList{}),
		reflect.TypeOf(Note{}),
	}

	for _, typ := range blockTypes {
		t.Run(typ.Name(), func(t *testing.T) {
			ptr := reflect.New(typ)
			counter := 0
			sentinels := sentinelValue(ptr, typ.Name(), &counter)
			if len(sentinels) == 0 {
				t.Fatalf("%s exposed no exported string-bearing field to test", typ.Name())
			}
			block, ok := ptr.Elem().Interface().(Block)
			if !ok {
				t.Fatalf("%s does not implement Block", typ.Name())
			}
			text, html := Render(minimalShell(), []Block{block})
			for _, s := range sentinels {
				if !strings.Contains(text, s) {
					t.Errorf("%s: text part missing sentinel %s (field not rendered in text)", typ.Name(), s)
				}
				if !strings.Contains(html, s) {
					t.Errorf("%s: html part missing sentinel %s (field not rendered in html)", typ.Name(), s)
				}
			}
		})
	}
}

// shellHTMLOnlyFields lists the Shell fields the design spec scopes to the
// HTML part only (spec section 4.2, header bar: "HTML: the invite header
// verbatim; text: {WORDMARK} // {TAGLINE} on line one" — ShortCode names
// the badge that line omits by design, not by omission).
// TestShellFieldPairingReflective checks every other exported Shell field
// against both parts.
var shellHTMLOnlyFields = map[string]bool{"ShortCode": true}

// TestShellFieldPairingReflective extends the reflective pairing check to
// Shell itself (finding M2): the block-level version above never covered
// Shell, and deleting escapeHTML(shell.Signoff) or escapeHTML(shell.PrefLine)
// from renderHTML passed the whole suite before this test existed. Every
// exported Shell string field the design does not explicitly scope to HTML
// alone must appear in both rendered parts, the same invariant
// TestBlockFieldPairingReflective proves for blocks. Shell has no slice
// fields, so sentinelValue's returned sentinels line up 1:1, in order, with
// reflect.TypeOf(Shell{})'s exported fields — the same order this walks.
func TestShellFieldPairingReflective(t *testing.T) {
	shellType := reflect.TypeOf(Shell{})
	var shell Shell
	counter := 0
	sentinels := sentinelValue(reflect.ValueOf(&shell), "Shell", &counter)
	if len(sentinels) != shellType.NumField() {
		t.Fatalf("sentinelValue produced %d sentinels for %d Shell fields; the 1:1 field order this test relies on broke", len(sentinels), shellType.NumField())
	}
	text, html := Render(shell, nil)
	for i, s := range sentinels {
		fieldName := shellType.Field(i).Name
		if !strings.Contains(html, s) {
			t.Errorf("html part missing sentinel %s for field %s", s, fieldName)
		}
		if shellHTMLOnlyFields[fieldName] {
			continue
		}
		if !strings.Contains(text, s) {
			t.Errorf("text part missing sentinel %s for field %s (field not rendered in text)", s, fieldName)
		}
	}
}

func minimalShell() Shell {
	return Shell{
		Wordmark:   "TEST LEAGUE",
		ShortCode:  "TL",
		Tagline:    "TEST TAGLINE",
		Signal:     "TEST SIGNAL",
		Signoff:    "— Test",
		FooterJoke: "Test footer.",
		PrefLine:   "Test pref line.",
		PrefURL:    "https://example.com/settings",
	}
}

// --- Shell/Render structural checks ----------------------------------------

func TestRenderHeaderSignalAndFooterOrder(t *testing.T) {
	shell := minimalShell()
	text, html := Render(shell, []Block{Note{Text: "Body copy."}})

	wantPrefix := "TEST LEAGUE // TEST TAGLINE\n\n* TEST SIGNAL\n\nBody copy.\n\n— Test\nTest footer.\nTest pref line.\nManage: https://example.com/settings"
	if text != wantPrefix {
		t.Errorf("text = %q, want %q", text, wantPrefix)
	}
	if !strings.HasPrefix(html, "<!DOCTYPE html>") {
		t.Errorf("html must start with a doctype:\n%s", html)
	}
	if !strings.Contains(html, "Body copy.") {
		t.Errorf("html missing block content")
	}
}

func TestRenderWithNoBlocksStillRendersShell(t *testing.T) {
	text, html := Render(minimalShell(), nil)
	if !strings.Contains(text, "TEST LEAGUE // TEST TAGLINE") {
		t.Errorf("text missing header: %q", text)
	}
	if !strings.Contains(text, "Manage: https://example.com/settings") {
		t.Errorf("text missing footer: %q", text)
	}
	if !strings.Contains(html, "TEST LEAGUE") {
		t.Errorf("html missing wordmark")
	}
}

// --- text.go: wrap width and column alignment ------------------------------

func TestWrapTextNeverReachesWidth(t *testing.T) {
	long := strings.Repeat("word ", 40)
	for _, line := range wrapText(long, TextWrapWidth) {
		if len(line) >= TextWrapWidth {
			t.Errorf("line %q has length %d, want < %d", line, len(line), TextWrapWidth)
		}
	}
}

func TestWrapTextSingleLongWordNeverBreaks(t *testing.T) {
	url := "https://gridiron.draco.quest/draft?utm_source=notification&utm_medium=email&utm_campaign=on-the-clock-reminder"
	lines := wrapText(url, TextWrapWidth)
	if len(lines) != 1 || lines[0] != url {
		t.Errorf("a single long token must occupy one unbroken line, got %v", lines)
	}
}

func TestWrapTextEmpty(t *testing.T) {
	if got := wrapText("   ", TextWrapWidth); got != nil {
		t.Errorf("wrapText(whitespace) = %v, want nil", got)
	}
	if got := wrapText("", TextWrapWidth); got != nil {
		t.Errorf("wrapText(\"\") = %v, want nil", got)
	}
}

func TestPanelColumnAlignsToLongestLabelPlusFour(t *testing.T) {
	rows := []PanelRow{{Label: "A", Value: "x"}, {Label: "LONGEST LABEL", Value: "y"}}
	if got, want := panelValueColumn(rows), 2+len("LONGEST LABEL")+4; got != want {
		t.Errorf("panelValueColumn = %d, want %d", got, want)
	}
}

// --- StatTable and PickList text-twin details -------------------------------

func TestStatTableTextMarksRecipientRow(t *testing.T) {
	table := StatTable{
		Header: []string{"SLOT", "TEAM"},
		Rows: [][]string{
			{"1", "Aqua 1"},
			{"2", "Aqua 2"},
		},
		MarkRow: 2, // 1-based (finding m5): marks the same second row the old Mark: 1 did
	}
	var b strings.Builder
	table.appendText(&b)
	lines := strings.Split(b.String(), "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "* ") {
		t.Errorf("marked row must carry a leading \"* \": %q", lines[len(lines)-1])
	}
	if !strings.HasPrefix(lines[len(lines)-2], "  ") || strings.HasPrefix(lines[len(lines)-2], "* ") {
		t.Errorf("unmarked row must carry a leading \"  \" (no marker): %q", lines[len(lines)-2])
	}
}

// TestStatTableZeroMarkRowMarksNoRow checks finding m5: the zero value of
// MarkRow (a builder that never sets it) means "no row is marked", not
// "row 1 is marked" the way a 0-based field's zero value used to.
func TestStatTableZeroMarkRowMarksNoRow(t *testing.T) {
	table := StatTable{
		Header: []string{"SLOT"},
		Rows:   [][]string{{"1"}, {"2"}},
	}
	var text, html strings.Builder
	table.appendText(&text)
	table.appendHTML(&html)
	if strings.Contains(text.String(), "* ") {
		t.Errorf("no row should carry the marker when MarkRow is the zero value: %q", text.String())
	}
	if strings.Contains(html.String(), ColorAccent) {
		t.Errorf("no row should render in the accent color when MarkRow is the zero value:\n%s", html.String())
	}
}

func TestCTATextStripsTrailingArrow(t *testing.T) {
	var b strings.Builder
	CTA{Label: "BUILD YOUR BOARD →", URL: "https://example.com/board"}.appendText(&b)
	want := "  -> BUILD YOUR BOARD: https://example.com/board"
	if b.String() != want {
		t.Errorf("cta text = %q, want %q", b.String(), want)
	}
}

// TestCTATextTrimSuffixNotCutset checks finding nit 2: the old
// strings.TrimRight(label, " →") treated " →" as a cutset — strip any
// trailing run of ' ' or '→', in either order or quantity — not a literal
// suffix. Under that bug, "GO →→" would have lost both arrows and the
// space down to "GO", and a label of "→" alone would have been stripped
// entirely to "". TrimSuffix removes only one exact, literal trailing
// " →", so neither of those cases (whose actual trailing bytes are not
// literally " →") gets touched; only a real " →" suffix is removed.
func TestCTATextTrimSuffixNotCutset(t *testing.T) {
	cases := []struct{ label, want string }{
		{"GO →→", "GO →→"},                         // old cutset bug: "GO"
		{"→", "→"},                                 // old cutset bug: ""
		{"BUILD YOUR BOARD", "BUILD YOUR BOARD"},   // no trailing " →" at all
		{"BUILD YOUR BOARD →", "BUILD YOUR BOARD"}, // a literal " →" suffix is still trimmed
	}
	for _, tc := range cases {
		var b strings.Builder
		CTA{Label: tc.label, URL: "https://example.com"}.appendText(&b)
		want := "  -> " + tc.want + ": https://example.com"
		if b.String() != want {
			t.Errorf("CTA{Label: %q}.appendText = %q, want %q", tc.label, b.String(), want)
		}
	}
}

// --- Finding m3: text renderers must not pass raw control characters ------

// TestTextSafeCollapsesControlCharsToOneSpace checks the textSafe helper
// directly: every run of control characters (including \r, \n, \t)
// collapses to one space, and clean input passes through unchanged.
func TestTextSafeCollapsesControlCharsToOneSpace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"clean text", "clean text"},
		{"a\nb", "a b"},
		{"a\r\nb", "a b"},
		{"a\n\n\nb", "a b"},
		{"a\tb", "a b"},
		{"José · —", "José · —"}, // multi-byte runes are not control characters
	}
	for _, tc := range cases {
		if got := textSafe(tc.in); got != tc.want {
			t.Errorf("textSafe(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestStatTableCellNewlineCannotForgeExtraLines checks finding m3's
// concrete scenario: a StatTable cell carrying an embedded newline (for
// example a hostile team or player name) cannot fabricate an extra text
// line that states something the paired HTML part does not show. Before
// the fix, this cell could forge what looked like a whole extra row.
func TestStatTableCellNewlineCannotForgeExtraLines(t *testing.T) {
	forged := "Real Cell\n* 99  FORGED ROW  not really here"
	table := StatTable{
		Header: []string{"SLOT", "TEAM"},
		Rows:   [][]string{{"1", forged}},
	}
	var b strings.Builder
	table.appendText(&b)
	lines := strings.Split(b.String(), "\n")
	if len(lines) != 3 { // title-less: header line, dash line, one data line
		t.Fatalf("StatTable text produced %d lines, want 3 (the forged newline must not add a line): %q", len(lines), b.String())
	}
	if strings.Contains(b.String(), "FORGED ROW") == false {
		t.Fatalf("the cell's own text must still survive, sanitized: %q", b.String())
	}
}

// TestHeadlineAndPickListAndPanelSanitizeUnwrappedFields checks finding m3
// across the other fields that are not routed through wrapText's own
// whitespace normalization: Headline.Title, Panel row Label, PickList
// Title and Items.
func TestHeadlineAndPickListAndPanelSanitizeUnwrappedFields(t *testing.T) {
	dirty := "line one\nline two"
	clean := "line one line two"

	var headline strings.Builder
	Headline{Title: dirty}.appendText(&headline)
	if headline.String() != clean {
		t.Errorf("Headline.Title = %q, want sanitized %q", headline.String(), clean)
	}

	var panel strings.Builder
	Panel{Rows: []PanelRow{{Label: dirty, Value: "v"}}}.appendText(&panel)
	if strings.Contains(panel.String(), "\n"+"line two") {
		t.Errorf("Panel label newline was not sanitized: %q", panel.String())
	}
	if !strings.Contains(panel.String(), clean) {
		t.Errorf("Panel label must still carry its sanitized text: %q", panel.String())
	}

	var picklist strings.Builder
	PickList{Title: dirty, Items: []string{dirty}}.appendText(&picklist)
	lines := strings.Split(picklist.String(), "\n")
	if len(lines) != 2 { // title line + one item line; a real newline would add a third
		t.Fatalf("PickList text produced %d lines, want 2: %q", len(lines), picklist.String())
	}
}

// --- Finding m8: a StatTable without a header must still align ------------

// TestStatTableWithoutHeaderSizesFromWidestRow checks finding m8:
// columnWidths previously sized its widths slice from len(header) alone,
// so a StatTable with no header (empty Header slice) produced a
// zero-length widths slice and silently dropped every row cell. Column
// widths must instead come from the widest row when there is no header.
func TestStatTableWithoutHeaderSizesFromWidestRow(t *testing.T) {
	table := StatTable{
		Rows: [][]string{
			{"a", "bb"},
			{"ccc", "d"},
		},
	}
	var b strings.Builder
	table.appendText(&b)
	got := b.String()
	want := "  a    bb\n  ccc  d"
	if got != want {
		t.Errorf("headerless StatTable text = %q, want %q", got, want)
	}
}

// --- Finding m9: an empty PickList title must not emit a bare line/div ----

// TestPickListEmptyTitleSkipsInBothParts checks finding m9: PickList with
// an empty Title previously emitted a bare " //" line in text and an
// empty kicker div in HTML; both must be skipped instead, mirroring
// StatTable's own "" hides it doc comment.
func TestPickListEmptyTitleSkipsInBothParts(t *testing.T) {
	list := PickList{Items: []string{"one", "two"}}
	var text, html strings.Builder
	list.appendText(&text)
	list.appendHTML(&html)

	if strings.HasPrefix(text.String(), " //") || strings.Contains(text.String(), "\n //") {
		t.Errorf("text must not carry a bare \" //\" title line: %q", text.String())
	}
	wantText := "  1. one\n  2. two"
	if text.String() != wantText {
		t.Errorf("text = %q, want %q", text.String(), wantText)
	}
	if strings.Contains(html.String(), "//</div>") {
		t.Errorf("html must not carry an empty kicker div: %s", html.String())
	}
}

// --- Finding nit 1: Divider must produce exactly one blank line -----------

// TestDividerProducesExactlyOneBlankLine checks finding nit 1: two blocks
// separated by a Divider must show exactly one blank line between them in
// text, the same spacing two blocks show with no Divider between them at
// all — not the three blank lines the un-special-cased separator math
// used to stack up around Divider's empty appendText output.
func TestDividerProducesExactlyOneBlankLine(t *testing.T) {
	withDivider, _ := Render(minimalShell(), []Block{Note{Text: "A."}, Divider{}, Note{Text: "B."}})
	withoutDivider, _ := Render(minimalShell(), []Block{Note{Text: "A."}, Note{Text: "B."}})

	if withDivider != withoutDivider {
		t.Errorf("a Divider must not change the text part's spacing:\n with divider: %q\nwithout divider: %q", withDivider, withoutDivider)
	}
	if !strings.Contains(withDivider, "A.\n\nB.") {
		t.Errorf("expected exactly one blank line between A. and B.: %q", withDivider)
	}
}

// --- Finding nit 9: CTA.URL and Shell.PrefURL enforce an http(s) allowlist -

func TestCTANonHTTPSchemeRendersLabelWithoutLinkOrURL(t *testing.T) {
	cases := []string{
		"javascript:alert(1)",
		"data:text/html,evil",
		"mailto:a@example.com",
		"/relative/path", // no scheme at all
		"",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			cta := CTA{Label: "CLICK ME", URL: url}
			var text, html strings.Builder
			cta.appendText(&text)
			cta.appendHTML(&html)

			if strings.Contains(html.String(), "<a ") {
				t.Errorf("html must not carry a link for scheme %q:\n%s", url, html.String())
			}
			if !strings.Contains(html.String(), "CLICK ME") {
				t.Errorf("html must still carry the label for scheme %q:\n%s", url, html.String())
			}
			wantText := "  -> CLICK ME"
			if text.String() != wantText {
				t.Errorf("text = %q, want %q (label only, no URL line)", text.String(), wantText)
			}
		})
	}
}

func TestCTAHTTPAndHTTPSSchemesRenderLink(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://example.com"} {
		t.Run(url, func(t *testing.T) {
			cta := CTA{Label: "CLICK ME", URL: url}
			var text, html strings.Builder
			cta.appendText(&text)
			cta.appendHTML(&html)

			if !strings.Contains(html.String(), `<a href="`+url+`"`) {
				t.Errorf("html must carry a link for scheme %q:\n%s", url, html.String())
			}
			wantText := "  -> CLICK ME: " + url
			if text.String() != wantText {
				t.Errorf("text = %q, want %q", text.String(), wantText)
			}
		})
	}
}

func TestShellPrefURLNonHTTPSchemeRendersLabelWithoutLinkOrURL(t *testing.T) {
	shell := minimalShell()
	shell.PrefURL = "javascript:alert(1)"
	text, html := Render(shell, nil)

	if strings.Contains(html, "<a ") {
		t.Errorf("html footer must not carry a link for a non-http(s) PrefURL:\n%s", html)
	}
	if !strings.Contains(html, "Manage:") {
		t.Errorf("html footer must still carry the \"Manage:\" label:\n%s", html)
	}
	if strings.Contains(text, "javascript:") {
		t.Errorf("text footer must not carry the unsafe URL: %q", text)
	}
	if !strings.HasSuffix(text, "Manage: ") {
		t.Errorf("text footer must end with the bare \"Manage: \" label, no URL: %q", text)
	}
}

func TestShellPrefURLHTTPSSchemeRendersLink(t *testing.T) {
	shell := minimalShell()
	shell.PrefURL = "https://example.com/settings"
	text, html := Render(shell, nil)

	if !strings.Contains(html, `<a href="https://example.com/settings"`) {
		t.Errorf("html footer must carry the link:\n%s", html)
	}
	if !strings.HasSuffix(text, "Manage: https://example.com/settings") {
		t.Errorf("text footer must carry the URL: %q", text)
	}
}

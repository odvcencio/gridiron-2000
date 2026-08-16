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
			Mark: 1,
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

	// The HTML part carries the same facts in the invite's dress; every
	// value that appears in the text part must also appear in HTML
	// (spec section 4.3, rule 1).
	for _, want := range []string{
		"YOU&#39;RE ON THE CLOCK.",
		"3.07 · overall 23",
		"0:20 away cap armed",
		"Jahmyr Gibbs",
		"TAKE YOUR PICK",
		"https://gridiron.draco.quest/draft",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
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
			field.SetString(s)
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
		Mark: 1,
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

func TestStatTableNoMarkMarksNoRow(t *testing.T) {
	table := StatTable{
		Header: []string{"SLOT"},
		Rows:   [][]string{{"1"}, {"2"}},
		Mark:   -1,
	}
	var text, html strings.Builder
	table.appendText(&text)
	table.appendHTML(&html)
	if strings.Contains(text.String(), "* ") {
		t.Errorf("no row should carry the marker when Mark is -1: %q", text.String())
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

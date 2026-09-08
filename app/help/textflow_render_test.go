package help

import (
	"strings"
	"testing"
)

// TestHelpTopicCardTitleAndSummaryUseTextBlock pins the textflow wave
// (2026-09-05): a topic corpus card's own title (<h4>) and summary (<p>)
// render through <TextBlock> — maxLines={2} on the title, maxLines={3}
// on the summary — instead of plain, unbounded elements.
func TestHelpTopicCardTitleAndSummaryUseTextBlock(t *testing.T) {
	body := renderHelpRoute(t, "/")
	at := strings.Index(body, `class="guide-card help-topic-card"`)
	if at < 0 {
		t.Fatal("no .help-topic-card in the rendered help index")
	}
	segment := body[at:]
	h4At := strings.Index(segment, "<h4")
	if h4At < 0 {
		t.Fatal("topic card has no <h4")
	}
	h4Tag := segment[h4At : h4At+strings.Index(segment[h4At:], ">")]
	if !strings.Contains(h4Tag, "data-gosx-text-layout") || !strings.Contains(h4Tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("topic card title missing TextBlock maxLines=2 attrs: %s", h4Tag)
	}
	pAt := strings.Index(segment, "<p")
	if pAt < 0 {
		t.Fatal("topic card has no <p")
	}
	pTag := segment[pAt : pAt+strings.Index(segment[pAt:], ">")]
	if !strings.Contains(pTag, "data-gosx-text-layout") || !strings.Contains(pTag, `data-gosx-text-layout-max-lines="3"`) {
		t.Errorf("topic card summary missing TextBlock maxLines=3 attrs: %s", pTag)
	}
}

// TestHelpGlossaryAndMigrationRowsUseTextBlock pins the glossary term/
// definition pair and the migration table's own row cells rendering
// through <TextBlock>.
func TestHelpGlossaryAndMigrationRowsUseTextBlock(t *testing.T) {
	body := renderHelpRoute(t, "/")
	glossaryAt := strings.Index(body, `class="help-glossary-entry"`)
	if glossaryAt < 0 {
		t.Fatal("no .help-glossary-entry in the rendered help index")
	}
	glossarySeg := body[glossaryAt:]
	dfnAt := strings.Index(glossarySeg, "<dfn")
	if dfnAt < 0 {
		t.Fatal("glossary entry has no <dfn")
	}
	dfnTag := glossarySeg[dfnAt : dfnAt+strings.Index(glossarySeg[dfnAt:], ">")]
	if !strings.Contains(dfnTag, "data-gosx-text-layout") {
		t.Errorf("glossary term missing data-gosx-text-layout: %s", dfnTag)
	}

	mappingAt := strings.Index(body, `class="help-mapping-row"`)
	if mappingAt < 0 {
		t.Fatal("no .help-mapping-row in the rendered help index")
	}
	mappingSeg := body[mappingAt:]
	strongAt := strings.Index(mappingSeg, "<strong")
	if strongAt < 0 {
		t.Fatal("migration row has no <strong")
	}
	strongTag := mappingSeg[strongAt : strongAt+strings.Index(mappingSeg[strongAt:], ">")]
	if !strings.Contains(strongTag, "data-gosx-text-layout") {
		t.Errorf("migration row cell missing data-gosx-text-layout: %s", strongTag)
	}
}

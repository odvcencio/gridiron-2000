package main

import (
	"os"
	"strings"
	"testing"
)

// TestPendingFormStateIsVisible covers wave-6 audit item 9: GoSX's managed
// form runtime (client/runtime/host/navigation.ts submitForm) sets
// data-gosx-form-state="pending" on the <form> itself during a background
// POST, but this stylesheet carried no rule keyed on that attribute before
// this wave — a manager saw no visible change while a submit was in
// flight. This pins the global rule that makes it visible: the submit
// button dims, stops accepting further clicks, and gains a "Saving…"
// suffix.
func TestPendingFormStateIsVisible(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)

	stateSelector := `form[data-gosx-form-state="pending"] button[type="submit"]`
	stateIndex := strings.Index(css, stateSelector+" {")
	if stateIndex < 0 {
		t.Fatalf("no %q rule", stateSelector)
	}
	end, err := cssBlockEnd(css, strings.Index(css[stateIndex:], "{")+stateIndex)
	if err != nil {
		t.Fatalf("find rule end: %v", err)
	}
	declarations := css[stateIndex:end]
	for _, want := range []string{"opacity: 0.6;", "pointer-events: none;", "cursor: not-allowed;"} {
		if !strings.Contains(declarations, want) {
			t.Errorf("%s omitted %q", stateSelector, want)
		}
	}

	afterSelector := stateSelector + "::after"
	afterIndex := strings.Index(css, afterSelector+" {")
	if afterIndex < 0 {
		t.Fatalf("no %q rule", afterSelector)
	}
	afterEnd, err := cssBlockEnd(css, strings.Index(css[afterIndex:], "{")+afterIndex)
	if err != nil {
		t.Fatalf("find ::after rule end: %v", err)
	}
	if !strings.Contains(css[afterIndex:afterEnd], `content: " · Saving…";`) {
		t.Error(afterSelector + " omitted its \"Saving…\" content")
	}
}

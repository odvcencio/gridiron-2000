package draft

import (
	"os"
	"strings"
	"testing"
)

func TestAutopickTimingCopyMatchesPersistedClockSemantics(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)

	for _, truth := range []string{
		"uses your Big Board, then best available",
		"does not reset this turn's grace",
		"if grace has elapsed, the next clock tick may pick",
		"will not pick while you are present",
		"saved deadline still applies",
		"being marked away can shorten it",
	} {
		if !strings.Contains(source, truth) {
			t.Errorf("draft autopick copy omits truthful engine behavior %q", truth)
		}
	}

	for _, falsePromise := range []string{
		"picks after a short grace",
		"keep the full pick clock",
		"starts a fresh grace",
		"resets the grace",
	} {
		if strings.Contains(source, falsePromise) {
			t.Errorf("draft autopick copy still promises %q", falsePromise)
		}
	}
}

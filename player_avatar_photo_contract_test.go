package main

import (
	"os"
	"strings"
	"testing"
)

// TestTeamPlayerPhotoThumbnailKeepsSquareUprightGeometry ensures the shared
// decorative mark transform does not shear real player photographs or let a
// non-square source image stretch a flex identity row.
func TestTeamPlayerPhotoThumbnailKeepsSquareUprightGeometry(t *testing.T) {
	stylesBytes, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	ruleAt := strings.Index(styles, ".player-avatar--photo {")
	if ruleAt < 0 {
		t.Fatal("player photo rule is missing")
	}
	ruleEnd := strings.Index(styles[ruleAt:], "}")
	if ruleEnd < 0 {
		t.Fatal("player photo rule has no closing brace")
	}
	rule := styles[ruleAt : ruleAt+ruleEnd]
	for _, want := range []string{
		"display: block;",
		"width: 2.6rem;",
		"height: 2.6rem;",
		"flex: 0 0 2.6rem;",
		"object-fit: cover;",
		"object-position: center;",
		"transform: none;",
	} {
		if !strings.Contains(rule, want) {
			t.Errorf("player photo rule missing %q: %s", want, rule)
		}
	}
}

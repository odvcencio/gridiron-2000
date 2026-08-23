package main

import (
	"os"
	"strings"
	"testing"
)

func TestMobileContentControlsKeepTouchBaseline(t *testing.T) {
	styles, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(styles)
	start := strings.LastIndex(css, "@media (max-width: 38rem)")
	if start < 0 {
		t.Fatal("stylesheet is missing the late phone breakpoint")
	}
	phone := css[start:]
	for _, want := range []string{
		".site-frame a:not(p a):not(li a):not(nav a):not(footer a)",
		".site-frame a[data-gosx-link]:not(p a):not(li a):not(nav a):not(footer a)",
		"display: inline-flex",
		"align-items: center",
		".site-frame button:not(.badge-option)",
		".site-frame .wire-filter",
		".site-frame summary",
		".site-frame input[type=\"submit\"]",
		".site-frame input:not([type=\"hidden\"]):not([type=\"radio\"]):not([type=\"checkbox\"])",
		".site-frame select",
		".site-frame textarea",
		".site-frame input[type=\"file\"]::file-selector-button",
		"min-height: 2.75rem",
		"min-block-size: 2.75rem",
		"min-width: 0",
		"max-width: 100%",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("mobile touch baseline omitted %q", want)
		}
	}
	for _, forbidden := range []string{
		".site-frame a[data-gosx-link]:not(p a):not(li a),",
		".site-frame input[type=\"hidden\"]",
		".site-frame input[type=\"radio\"]",
		".site-frame input[type=\"checkbox\"]",
		".site-frame .position-chip",
		".site-frame .autopick-badge",
	} {
		if strings.Contains(phone, forbidden) {
			t.Errorf("mobile touch baseline must not stretch excluded element %q", forbidden)
		}
	}
}

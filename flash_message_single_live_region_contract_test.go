package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFlashMessagePagesCarryOnlyOneLiveRegion pins J3 F27: every result
// message (a lineup save, a Pick'em pick, a claim filed, a post, a
// preference saved) was announced twice — once by the page's own
// server-rendered notice wrapper (aria-live="polite" around
// .flash-message) and a second time by the framework's single global
// toast host (app/layout.gsx's data-gosx-toast-host, which
// client/runtime/host/navigation.ts's presentManagedFormToast already
// gives role="status"/"alert" after every managed-form action settles).
// Two live regions held the identical text.
//
// The fix keeps the page's own notice wrapper for a visible, permanent
// record of the result (useful with no JavaScript, where no toast can
// ever appear) but drops its aria-live marking, since the framework's
// one toast host is already the page's one live-announcing surface once
// JavaScript is running. This test pins that each named page's notice
// wrapper carries no aria-live attribute, so a rendered flash can only
// ever be announced by the one framework toast.
func TestFlashMessagePagesCarryOnlyOneLiveRegion(t *testing.T) {
	cases := []struct {
		route     string
		wrapClass string
	}{
		{"team", "notice-stack"},
		{"players", "notice-stack"},
		{"trades", "notice-stack"},
		{"pickem", "notice-stack"},
		{"settings", "notice-stack"},
		{"locker", "locker-notice"},
	}
	for _, c := range cases {
		path := filepath.Join("app", c.route, "page.gsx")
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		page := string(source)
		wantOpen := `<div class="` + c.wrapClass + `">`
		if !strings.Contains(page, wantOpen) {
			t.Errorf("%s: notice wrapper must render as %q with no aria-live (the framework toast is the one live region)", path, wantOpen)
		}
		liveOpen := `<div class="` + c.wrapClass + `" aria-live`
		if strings.Contains(page, liveOpen) {
			t.Errorf("%s: notice wrapper still carries aria-live, duplicating the framework toast's own live region", path)
		}
	}

	layoutSource, err := os.ReadFile(filepath.Join("app", "layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	layout := string(layoutSource)
	if strings.Count(layout, "data-gosx-toast-host") != 1 {
		t.Fatal("app/layout.gsx must render exactly one shared toast host")
	}
	if !strings.Contains(layout, `class="toast-stack"`) || !strings.Contains(layout, `aria-live="polite"`) {
		t.Fatal("app/layout.gsx's shared toast host must remain the one live region backing every page's result feedback")
	}
}

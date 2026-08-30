package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLayoutProvidesAccessibleFloatingManagedActionFeedback(t *testing.T) {
	layout, err := os.ReadFile("layout.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(layout)
	for _, want := range []string{
		`class="toast-stack"`,
		`data-gosx-toast-host`,
		`aria-live="polite"`,
		`aria-relevant="additions"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("layout must carry %q", want)
		}
	}

	styles, err := os.ReadFile("../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	for _, want := range []string{
		`.toast-stack {`,
		`position: fixed`,
		`pointer-events: none`,
		`.gosx-toast--success`,
		`.gosx-toast--error`,
		`.gosx-toast__dismiss`,
		`@media (prefers-reduced-motion: reduce)`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("managed feedback styles must carry %q", want)
		}
	}
}

func TestPageActionsUseSharedRedirectFeedbackInventory(t *testing.T) {
	// Lineup mutations and the live Draft Room share managed-success helpers
	// now; native submissions still call RedirectWithNotice through them.
	// 2026-08-30 review, finding 3: run-waivers (app/admin) adds one.
	const wantRedirects = 47
	const wantRedirectBacks = 12
	redirects := 0
	redirectBacks := 0
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "page.server.go" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(source)
		redirects += strings.Count(text, "actionui.RedirectWithNotice(")
		redirectBacks += strings.Count(text, "actionui.RedirectBackWithNotice(")
		if strings.Contains(text, `session.AddFlash(ctx.Request, "notice"`) {
			t.Errorf("%s bypasses shared redirect feedback with a raw notice flash", path)
		}
		if strings.Contains(text, "ctx.Redirect(") {
			t.Errorf("%s bypasses shared redirect feedback with a raw success redirect", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if redirectBacks != wantRedirectBacks {
		t.Fatalf("RedirectBackWithNotice inventory = %d, want %d", redirectBacks, wantRedirectBacks)
	}
	if redirects != wantRedirects {
		t.Fatalf("RedirectWithNotice inventory = %d, want %d", redirects, wantRedirects)
	}
}

func TestSeatTrimNoticePreservesScheduleResetGuidance(t *testing.T) {
	source, err := os.ReadFile("admin/page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scheduleBefore", "Existing unplayed schedule cleared; regenerate it for the kept teams.", "actionui.RedirectWithNotice(ctx, \"/admin\", notice)"} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("admin seat-trim feedback must preserve %q", want)
		}
	}
}

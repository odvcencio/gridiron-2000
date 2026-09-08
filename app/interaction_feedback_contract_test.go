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
	// UI pass 2026-08-30 (P1-6): settings/page.server.go's setDensityPreference adds one.
	// GC-4: locker/page.server.go's locker-post and locker-remove actions add two.
	// 38 after wave 1 of the gap-audit plan: every managed mutation in
	// team, players, trades, pickem, locker and draft now redirects through
	// one per-package *MutationSuccess / draftActionSuccess helper instead
	// of a per-action call, so the direct-call inventory fell while the
	// number of redirecting actions rose (see mutation_response_shape_test
	// in each package).
	// Wave 2 gap-audit item 1: every admin action must return the
	// commissioner to the section it started from. app/admin/page.server.go
	// converted its 27 remaining direct RedirectWithNotice(ctx, "/admin",
	// ...) calls to RedirectBackWithNotice(ctx, adminSectionTarget(<section>),
	// ...), so redirects fell by 27 (38 -> 11) and redirectBacks rose by the
	// same 27 (12 -> 39).
	// Item 4, 2026-08-31 post-wave audit: app/team/page.server.go adds a
	// "team-name-reset" action (league.Service.ResetTeamName's explicit
	// counterpart to the now-blank-rejecting "team-rename"), one more
	// RedirectBackWithNotice call (39 -> 40).
	// Practice draft (internal/league/practice.go, 2026-09-04):
	// app/draft/practice/page.server.go adds four direct
	// RedirectWithNotice calls — practice-start/restart (one shared
	// handler), practice-leave, make-pick, and toggle-autopick — each a
	// native-or-managed 303 into the practice room or back to the real
	// one (11 -> 15). The practice module deliberately does not reuse
	// app/draft's own draftActionSuccess (unexported, and its target is
	// the real room's path).
	// J3 F8 (2026-09-07 truth pass): pickemSetAction's own success path
	// now names the picked game's own row and calls
	// actionui.RedirectWithNoticeToRow instead of RedirectWithNotice — a
	// managed pick must keep that row's fragment, unlike RedirectWithNotice's
	// own generic-anchor stripping (15 -> 14). team/page.server.go's
	// lineupMutationSuccess still calls RedirectWithNotice unchanged for
	// its own no-single-row case (SET BEST LINEUP), so this inventory's
	// count there is unaffected.
	// Commissioner roster correction (2026-09-07): app/admin/page.server.go
	// adds one RedirectBackWithNotice call for the "roster-correction"
	// action's own successful commit (40 -> 41).
	// Team lineup and bench redesign (2026-09-07, section-B item 4):
	// app/team/page.server.go's new benchMutationSuccess helper (the
	// bench row's Drop action, anchored to #bench) adds one more
	// RedirectWithNotice call (14 -> 15).
	const wantRedirects = 15
	const wantRedirectBacks = 41
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
	// Wave 2 gap-audit item 1: seat-trim now returns to the draft-order
	// section it was submitted from instead of a hard "/admin" redirect.
	for _, want := range []string{"scheduleBefore", "Existing unplayed schedule cleared; regenerate it for the kept teams.", "actionui.RedirectBackWithNotice(ctx, adminSectionTarget(\"draft-order\"), notice)"} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("admin seat-trim feedback must preserve %q", want)
		}
	}
}

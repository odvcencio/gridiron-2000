package app

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/app/blitz"
	"gridiron-2000/app/board"
	"gridiron-2000/app/locker"
	"gridiron-2000/app/scoring"
	"gridiron-2000/app/settings"
	"gridiron-2000/app/team"
	"gridiron-2000/app/trades"
	"gridiron-2000/internal/actionui"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
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
	// J1 F34 (2026-09-07 UX pass): app/board/page.server.go adds one
	// direct RedirectBackWithNotice call — board-clear-drafted, the Big
	// Board's own bulk "Clear drafted players" action (40 -> 41).
	// J6 F15 (2026-09-08 wave C): app/wire/page.server.go's tip action
	// moved from RedirectBackWithNotice to RedirectBackWithScopedNotice
	// so the Wire's confirmation renders only on the Wire (42 -> 41).
	// J6 F15 residue (2026-09-08 wave E): the seven pages the wave C
	// caveat named all moved off the shared, unscoped helpers, so a
	// confirmation from one of them can no longer appear on another.
	// Neither actionui.RedirectWithScopedNotice,
	// actionui.RedirectBackWithScopedNotice, nor
	// actionui.RedirectWithScopedNoticeToRow match this test's own
	// substring checks (see internal/actionui/feedback_test.go for their
	// own inventory), so every migrated call drops straight out of both
	// counts below:
	//   app/locker/page.server.go: 1 RedirectWithNotice -> RedirectWithScopedNotice
	//   app/blitz/page.server.go: 2 RedirectBackWithNotice -> RedirectBackWithScopedNotice
	//   app/settings/page.server.go: 2 RedirectWithNotice -> RedirectWithScopedNotice
	//   app/scoring/page.server.go: 2 RedirectWithNotice -> RedirectWithScopedNotice
	//   app/team/page.server.go: 2 RedirectWithNotice -> RedirectWithScopedNotice,
	//     2 RedirectWithNoticeToRow -> RedirectWithScopedNoticeToRow (uncounted
	//     both before and after), 4 RedirectBackWithNotice -> RedirectBackWithScopedNotice
	//   app/board/page.server.go: 5 RedirectBackWithNotice -> RedirectBackWithScopedNotice
	//   app/trades/page.server.go: 1 RedirectWithNotice -> RedirectWithScopedNotice
	// RedirectWithNotice: 15 - 1 - 2 - 2 - 2 - 1 = 7.
	// RedirectBackWithNotice: 41 - 2 - 4 - 5 = 30.
	// Decision 3 (J5 F11, wave E): app/login/page.server.go's new
	// "co-manager-join" action calls actionui.RedirectWithNotice twice —
	// once on failure (back to /login), once on success (to /, where the
	// arrival panel reads the co_manager_bound flash it also sets)
	// (7 -> 9). It is not one of the seven pages the scoped-notice
	// migration above touched.
	const wantRedirects = 9
	const wantRedirectBacks = 30
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

// TestScopedNoticePostedOnOnePageNeverRendersOnAnother is J6 F15's
// residue fix, pinned across the seven pages the wave C caveat named
// (locker, blitz, settings, scoring, team, board, trades): before this
// migration every one of them read the same untagged session flash
// every other page's own action wrote (app/wire's own F15 fix, wave C,
// already covers /wire and its neighbor /locker at the actionui layer).
// This test posts each page's real action through its real NoticeRoute
// constant and the exact actionui helper that page now calls, then
// confirms only that SAME page's own ScopedNotice read ever sees the
// confirmation — a different page's read, even one right next door in
// the nav, finds nothing.
func TestScopedNoticePostedOnOnePageNeverRendersOnAnother(t *testing.T) {
	pages := []struct {
		name   string
		route  string
		post   func(ctx *action.Context)
		notice string
	}{
		{"locker", locker.NoticeRoute, func(ctx *action.Context) {
			actionui.RedirectWithScopedNotice(ctx, locker.NoticeRoute, "/locker?page=2", "Posted.")
		}, "Posted."},
		{"blitz", blitz.NoticeRoute, func(ctx *action.Context) {
			actionui.RedirectBackWithScopedNotice(ctx, blitz.NoticeRoute, "/blitz", "Entry saved.")
		}, "Entry saved."},
		{"settings", settings.NoticeRoute, func(ctx *action.Context) {
			actionui.RedirectWithScopedNotice(ctx, settings.NoticeRoute, "/settings", "Data density set to Compact.")
		}, "Data density set to Compact."},
		{"scoring", scoring.NoticeRoute, func(ctx *action.Context) {
			actionui.RedirectWithScopedNotice(ctx, scoring.NoticeRoute, "/scoring", "Passing TD updated.")
		}, "Passing TD updated."},
		{"team", team.NoticeRoute, func(ctx *action.Context) {
			actionui.RedirectBackWithScopedNotice(ctx, team.NoticeRoute, "/team?identity=edit#team-identity", "Team renamed to West 4.")
		}, "Team renamed to West 4."},
		{"board", board.NoticeRoute, func(ctx *action.Context) {
			actionui.RedirectBackWithScopedNotice(ctx, board.NoticeRoute, "/board#board-pool", "Ja'Marr Chase added to your board.")
		}, "Ja'Marr Chase added to your board."},
		{"trades", trades.NoticeRoute, func(ctx *action.Context) {
			actionui.RedirectWithScopedNotice(ctx, trades.NoticeRoute, "/trades", "Offer sent.")
		}, "Offer sent."},
	}

	// Every NoticeRoute string must be unique, or two pages would share
	// one flash key and this whole fix would be a no-op (F15's original
	// defect, reintroduced by a copy-paste route string).
	seen := map[string]string{}
	for _, page := range pages {
		if owner, dup := seen[page.route]; dup {
			t.Fatalf("%s and %s share the NoticeRoute %q; a shared route key reopens F15", owner, page.name, page.route)
		}
		seen[page.route] = page.name
	}

	for _, posted := range pages {
		t.Run(posted.name, func(t *testing.T) {
			registry := action.NewRegistry()
			registry.Register("post", func(ctx *action.Context) error {
				posted.post(ctx)
				return nil
			})
			manager := session.MustNew("interaction-feedback-contract-secret-"+posted.name, session.Options{
				CookieName:    "interaction_feedback_contract_" + posted.name,
				AllowInsecure: true,
			})
			req := httptest.NewRequest(http.MethodPost, "/__actions/post", strings.NewReader(""))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "text/html,application/xhtml+xml")
			req.SetPathValue("name", "post")
			postRes := httptest.NewRecorder()
			manager.Middleware(registry).ServeHTTP(postRes, req)
			if postRes.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", postRes.Code, http.StatusSeeOther)
			}
			cookies := postRes.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("wrote %d cookies, want 1", len(cookies))
			}

			for _, reader := range pages {
				getReq := httptest.NewRequest(http.MethodGet, "/", nil)
				getReq.AddCookie(cookies[0])
				getRes := httptest.NewRecorder()
				manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					notice, ok := actionui.ScopedNotice(r, reader.route)
					if reader.name == posted.name {
						if !ok || notice != posted.notice {
							t.Fatalf("%s's own ScopedNotice read = %q, %v, want its own posted notice %q", reader.name, notice, ok, posted.notice)
						}
						return
					}
					if ok {
						t.Fatalf("%s posted a notice, but %s's own ScopedNotice read saw it too: %q", posted.name, reader.name, notice)
					}
				})).ServeHTTP(getRes, getReq)
			}
		})
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

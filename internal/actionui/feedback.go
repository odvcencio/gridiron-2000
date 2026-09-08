// Package actionui keeps progressive action feedback consistent across pages.
package actionui

import (
	"fmt"
	"net/http"
	"strings"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
)

// stripFragment drops a same-origin target's "#..." suffix, if any, and
// leaves its path and query string untouched. See the doc comment on
// RedirectBackWithNotice for why a managed redirect must never carry a
// section anchor.
func stripFragment(target string) string {
	if before, _, found := strings.Cut(target, "#"); found {
		return before
	}
	return target
}

// RedirectWithNotice preserves the existing native POST-redirect-GET notice
// while returning the same message to GoSX-managed forms. JavaScript-capable
// pages can display it immediately in the shared toast host; native forms keep
// the server-rendered flash fallback.
//
// A managed redirect target's own fragment is stripped first (item 2, wave 8
// hotfix): see RedirectBackWithNotice's doc comment for the full mechanism.
// A caller-supplied target such as teamLineupTarget's "...#lineup" or
// waiverRedirectTarget's "...#waivers" is a page-section anchor meant for a
// full-page navigation's native scroll-into-view, not a managed toast-and-
// stay redirect.
func RedirectWithNotice(ctx *action.Context, target, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	managed := action.WantsJSON(ctx.Request)
	if message != "" && !managed {
		session.AddFlash(ctx.Request, "notice", message)
	}
	if managed {
		target = stripFragment(target)
	}
	ctx.RedirectWithMessage(target, message)
}

// RedirectWithNoticeToRow is RedirectWithNotice without the fragment
// strip (J3 F8): a managed lineup save or Pick'em pick must land on the
// one specific row it just changed, not stay wherever the toast
// happened to appear. RedirectWithNotice's own fragment strip exists for
// a DIFFERENT case — a generic, page-section anchor ("#board-pool",
// "#lineup") that added nothing on a managed save and actively
// re-scrolled the page (see RedirectBackWithNotice's own doc comment for
// that regression). A caller here names one still-present element id the
// save actually changed (a lineup slot, a Pick'em game row); scrolling a
// managed request there is the wanted behavior in that case, not the
// disruptive top-of-section jump the wave-8 hotfix retired. Only pass a
// target whose fragment is that specific row's id — never a section
// anchor — or this reintroduces the exact regression RedirectWithNotice
// exists to avoid.
func RedirectWithNoticeToRow(ctx *action.Context, target, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message != "" && !action.WantsJSON(ctx.Request) {
		session.AddFlash(ctx.Request, "notice", message)
	}
	ctx.RedirectWithMessage(target, message)
}

// noticeFlashKey scopes a flash to the page that owns it (F15, gap-audit
// J6): every page's action handler used to write the SAME "notice" flash
// key, and every page's own Load read that same key back — so a manager
// who submitted a form on one page and opened a different one before
// following its redirect saw that OTHER page's confirmation instead. A
// route-scoped key means only the page that wrote it, reading this same
// scoped key back, will ever see it; another page's own unscoped
// "notice" read is untouched and cannot accidentally consume it either.
func noticeFlashKey(route string) string {
	return "notice:" + route
}

// RedirectBackWithScopedNotice is RedirectBackWithNotice, but the flash it
// writes is readable only by ScopedNotice(request, route) for the SAME
// route — see noticeFlashKey's doc comment. New call sites should prefer
// this over RedirectBackWithNotice; existing pages migrate as they are
// touched, not all at once, since each migration is a paired write/read
// change on one page.
func RedirectBackWithScopedNotice(ctx *action.Context, route, fallback, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	managed := action.WantsJSON(ctx.Request)
	if message != "" && !managed {
		session.AddFlash(ctx.Request, noticeFlashKey(route), message)
	}
	if managed {
		ctx.RedirectWithMessage(stripFragment(fallback), message)
		return
	}
	ctx.RedirectBackWithMessage(fallback, message)
}

// RedirectWithScopedNotice is RedirectWithNotice, but the flash it writes
// is readable only by ScopedNotice(request, route) for the SAME route —
// see noticeFlashKey's doc comment. Use this, not
// RedirectBackWithScopedNotice, for a caller whose form carries no
// return_to field and always lands on one explicit target: it keeps
// RedirectWithNotice's own "always this target" behavior and only adds
// the route scope (J6 F15 residue, wave E).
func RedirectWithScopedNotice(ctx *action.Context, route, target, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	managed := action.WantsJSON(ctx.Request)
	if message != "" && !managed {
		session.AddFlash(ctx.Request, noticeFlashKey(route), message)
	}
	if managed {
		target = stripFragment(target)
	}
	ctx.RedirectWithMessage(target, message)
}

// RedirectWithScopedNoticeToRow is RedirectWithNoticeToRow, scoped the
// same way RedirectWithScopedNotice scopes RedirectWithNotice: the row
// fragment a managed save keeps (J3 F8) is untouched; only the flash key
// gains the route scope so another page's own read never sees it (J6
// F15 residue, wave E).
func RedirectWithScopedNoticeToRow(ctx *action.Context, route, target, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message != "" && !action.WantsJSON(ctx.Request) {
		session.AddFlash(ctx.Request, noticeFlashKey(route), message)
	}
	ctx.RedirectWithMessage(target, message)
}

// ScopedNotice reads back the one flash RedirectBackWithScopedNotice (or a
// future scoped writer) stored for route, and only for route: a flash
// written for a different page is left alone here, exactly as if this
// page had never called Flashes at all.
func ScopedNotice(r *http.Request, route string) (string, bool) {
	store := session.Current(r)
	if store == nil {
		return "", false
	}
	flashes := store.Flashes(noticeFlashKey(route))
	if len(flashes) == 0 {
		return "", false
	}
	return strings.TrimSpace(fmt.Sprint(flashes[0])), true
}

// RedirectBackWithNotice preserves the existing native POST-redirect-GET
// notice while returning the same message and submitted same-origin target to
// GoSX-managed forms. When no valid target was submitted, fallback is used.
//
// A managed request (action.WantsJSON) always redirects to the
// fragment-stripped fallback instead of asking gosx/action to resolve
// against the request's own submitted return_to (action.Context has no
// exported way to read that value back out with only its fragment removed:
// action.redirectBackTarget prefers requestReturnTarget, an unexported
// helper that reads an unexported context key gosx/action's own
// serveHandler attaches during form parsing — nothing in this package's
// import surface can inspect or override it). Every real call site builds
// fallback from the SAME per-page filter/section state (pos/q/page, an
// admin section id, ...) the page used to render that hidden return_to
// field in the first place, so the two already agree on destination; the
// only difference production ever exercises is the trailing section anchor
// ("#board-pool", "#waivers", "#lineup", ...) this fix removes.
//
// The anchor caused a real regression (commissioner: "moving players on my
// big board doesn't feel interactive, it resets the scroll"): GoSX's
// runtime only skips its post-redirect scroll-to-hash when the managed
// JSON response's "redirect" field carries no "#..." fragment
// (client/runtime/host/navigation.ts, submitManagedActionForm). Every
// managed save landed back on the anchor and re-scrolled the page, even
// though the viewer never left it. A plain (no-JS) POST is a full-page
// navigation, where landing on the section anchor is the wanted, existing
// behavior — action.WantsJSON stays false there, so this function still
// calls RedirectBackWithMessage unchanged and keeps the fragment.
func RedirectBackWithNotice(ctx *action.Context, fallback, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	managed := action.WantsJSON(ctx.Request)
	if message != "" && !managed {
		session.AddFlash(ctx.Request, "notice", message)
	}
	if managed {
		ctx.RedirectWithMessage(stripFragment(fallback), message)
		return
	}
	ctx.RedirectBackWithMessage(fallback, message)
}

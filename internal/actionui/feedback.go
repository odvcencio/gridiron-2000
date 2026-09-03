// Package actionui keeps progressive action feedback consistent across pages.
package actionui

import (
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

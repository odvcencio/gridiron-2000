// Package actionui keeps progressive action feedback consistent across pages.
package actionui

import (
	"strings"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
)

// RedirectWithNotice preserves the existing native POST-redirect-GET notice
// while returning the same message to GoSX-managed forms. JavaScript-capable
// pages can display it immediately in the shared toast host; native forms keep
// the server-rendered flash fallback.
func RedirectWithNotice(ctx *action.Context, target, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message != "" && !action.WantsJSON(ctx.Request) {
		session.AddFlash(ctx.Request, "notice", message)
	}
	ctx.RedirectWithMessage(target, message)
}

// RedirectBackWithNotice preserves the existing native POST-redirect-GET
// notice while returning the same message and submitted same-origin target to
// GoSX-managed forms. When no valid target was submitted, fallback is used.
func RedirectBackWithNotice(ctx *action.Context, fallback, message string) {
	if ctx == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message != "" && !action.WantsJSON(ctx.Request) {
		session.AddFlash(ctx.Request, "notice", message)
	}
	ctx.RedirectBackWithMessage(fallback, message)
}

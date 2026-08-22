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
	if message != "" {
		session.AddFlash(ctx.Request, "notice", message)
	}
	ctx.RedirectWithMessage(target, message)
}

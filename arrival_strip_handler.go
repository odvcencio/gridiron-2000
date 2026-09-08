package main

import (
	"net/http"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/session"
)

// arrivalStripDismisser is the one method arrivalStripDismissHandler needs
// from *league.Service — matching avatarUploader's own narrow-interface
// pattern (avatar_handlers.go) so the handler stays trivially testable
// against a fake.
type arrivalStripDismisser interface {
	DismissArrivalStrip(r *http.Request) error
}

// arrivalStripDismissHandler backs GET /arrival-strip/dismiss (J5 F37).
// The home page's own template (app/page.gsx) must stay link-only — no
// <form> element, a deliberate, actively-tested constraint
// (TestHomepageActionCenterTypedAdapterRendersLinkOnly) — so the strip's
// Dismiss control is a plain <a href>, not a managed POST form the way
// every other mutation in this app works. A GET here is a conscious,
// narrow exception: DismissArrivalStrip is fully idempotent, reversible
// (a manager can always revisit the pages the strip links to), and
// touches nothing but one member's own cosmetic chrome flag — the worst
// outcome of a forged request is that member's own onboarding hint
// disappears one visit early, not a roster, scoring, or league-state
// change. Every other mutating route in this app stays POST+CSRF
// (app_build.go's csrfMutationCapableTarget); this one route is the sole,
// explicit exception, and is not added to that whitelist because GET
// requests never pass through session.Manager.Protect's CSRF check in
// the first place.
func arrivalStripDismissHandler(svc arrivalStripDismisser) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := svc.DismissArrivalStrip(r); err != nil {
			session.AddFlash(r, "notice", "Sign in to dismiss this strip.")
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}

var _ arrivalStripDismisser = (*league.Service)(nil)

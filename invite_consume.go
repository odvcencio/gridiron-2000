package main

import (
	"net/http"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// inviteLinkStore is the seam invite consume needs from league.Default():
// a read-only lookup for the GET confirmation page and the atomic,
// single-use consume the POST performs. league.Service satisfies it.
type inviteLinkStore interface {
	LookupInviteLinkByToken(token string) (league.InviteLink, bool, error)
	ConsumeInviteLink(token, consumedEmail string, now time.Time) (bool, error)
}

// registerInviteConsumeRoutes mounts Tier 0's consume surface (design
// section 6.2) on router: a read-only GET confirmation page and a POST-only
// consume action. It is called only from the CONFIGURED app — there is no
// league to sign in to before setup completes, so this route does not
// exist during SETUP or BootFailClosed.
func registerInviteConsumeRoutes(router *route.Router, manager *auth.Manager, membership googleMembership, store inviteLinkStore) {
	router.Add(route.Route{Pattern: "/auth/invite/{token}", Handler: inviteConsumeGetHandler(store)})
	router.Handle("POST /auth/invite/{token}", inviteConsumePostHandler(manager, membership, store))
}

func inviteConsumeGetHandler(store inviteLinkStore) route.PageHandler {
	return func(ctx *route.RouteContext) gosx.Node {
		ctx.NoStore()
		token := ctx.Param("token")
		link, found, err := store.LookupInviteLinkByToken(token)
		if err != nil || !found {
			// Enumeration of /auth/invite/<x> returns one generic invalid
			// page, no email, constant shape (design section 6.4).
			return inviteConsumeMessageNode("This invite link is not valid.", "Ask your commissioner for a new link.", false)
		}
		switch link.StateAt(time.Now()) {
		case league.InviteLinkUnused:
			return inviteConsumeConfirmNode(ctx, link)
		case league.InviteLinkConsumed:
			return inviteConsumeMessageNode("This invite link was already used.", "Ask your commissioner for a new link.", false)
		case league.InviteLinkExpired:
			return inviteConsumeMessageNode("This invite link has expired.", "Ask your commissioner for a new link.", false)
		default: // league.InviteLinkRevoked
			return inviteConsumeMessageNode("This invite link was revoked.", "Ask your commissioner for a new link.", false)
		}
	}
}

func inviteConsumePostHandler(manager *auth.Manager, membership googleMembership, store inviteLinkStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		link, found, err := store.LookupInviteLinkByToken(token)
		if err != nil || !found {
			session.AddFlash(r, "notice", "This invite link is not valid. Ask your commissioner for a new link.")
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ok, err := store.ConsumeInviteLink(token, link.Email, time.Now())
		if err != nil || !ok {
			session.AddFlash(r, "notice", "This invite link has already been used, expired, or was revoked. Ask your commissioner for a new link.")
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user := auth.User{ID: link.Email, Email: link.Email, Name: link.Email}
		completeSignIn(w, r, manager, membership, user, "/", completeSignInOptions{
			NotAdmittedRedirect: "/login?error=invite",
			ErrorRedirect:       "/login?error=oauth",
		})
	})
}

// inviteConsumeConfirmNode renders the design's exact confirmation shape:
// "This link signs you in as <email> to <league name>. ENTER LEAGUE →" as a
// POST button — the token in the URL is never itself the credential a form
// submits; access logs redact the /auth/invite/ path segment separately
// (design section 6.4), and consume is POST-only so the token cannot be
// replayed by a GET (link prefetch, a mail scanner, browser history).
func inviteConsumeConfirmNode(ctx *route.RouteContext, link league.InviteLink) gosx.Node {
	csrf := session.Token(ctx.Request)
	leagueName := league.Default().Config().Name
	return gosx.El("main", gosx.Attrs(gosx.Attr("class", "page"), gosx.Attr("id", "main-content")),
		gosx.El("section", gosx.Attrs(gosx.Attr("class", "login-stage")),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "login-poster")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "signal-label")), gosx.Text("INVITE LINK")),
				gosx.El("h1", nil, gosx.Text("This link signs you in as "+link.Email+" to "+leagueName+".")),
			),
			gosx.El("aside", gosx.Attrs(gosx.Attr("class", "login-console")),
				ctx.Form(
					gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", ctx.Request.URL.Path)),
					gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf_token"), gosx.Attr("value", csrf))),
					gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit"), gosx.Attr("class", "button button--primary")), gosx.Text("ENTER LEAGUE →")),
				),
			),
		),
	)
}

// inviteConsumeMessageNode renders every non-unused truthful state: the
// message names exactly what happened (never a generic error), and, unless
// this is the constant-shape unknown-token page, always points the visitor
// back to their commissioner. ok is unused today (every branch here is a
// refusal) but keeps the signature ready for a future confirmed-consumed
// success variant without a breaking change.
func inviteConsumeMessageNode(headline, detail string, _ bool) gosx.Node {
	return gosx.El("main", gosx.Attrs(gosx.Attr("class", "page"), gosx.Attr("id", "main-content")),
		gosx.El("section", gosx.Attrs(gosx.Attr("class", "login-stage")),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "login-poster")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "signal-label")), gosx.Text("INVITE LINK")),
				gosx.El("h1", nil, gosx.Text(headline)),
				gosx.El("p", nil, gosx.Text(detail)),
				gosx.El("a", gosx.Attrs(gosx.Attr("href", "/login"), gosx.Attr("class", "button button--ghost")), gosx.Text("Go to sign in")),
			),
		),
	)
}

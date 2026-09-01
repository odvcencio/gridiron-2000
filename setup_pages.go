package main

import (
	"fmt"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// setupFlashNode renders any pending "notice" flash as the same
// flash-message element the rest of the app uses (app/login/page.gsx),
// keeping SETUP-state copy visually consistent with everything after it.
func setupFlashNode(ctx *route.RouteContext) gosx.Node {
	if store := session.Current(ctx.Request); store != nil {
		if flashes := store.Flashes("notice"); len(flashes) > 0 {
			return gosx.El("p", gosx.Attrs(gosx.Attr("class", "flash-message")), gosx.Text(fmt.Sprint(flashes[0])))
		}
	}
	return gosx.Fragment()
}

// setupTokenEntryNode renders the boot-token entry form: the one thing an
// unauthorized /setup visitor can do. The token itself never appears here —
// it was printed at boot, stdout and log, per design section 3.3.
func setupTokenEntryNode(ctx *route.RouteContext) gosx.Node {
	csrf := session.Token(ctx.Request)
	return gosx.El("main", gosx.Attrs(gosx.Attr("class", "page"), gosx.Attr("id", "main-content")),
		gosx.El("section", gosx.Attrs(gosx.Attr("class", "login-stage")),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "login-poster")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "signal-label")), gosx.Text("FIRST-BOOT SETUP")),
				gosx.El("h1", nil, gosx.Text("This Gridiron instance is not configured yet")),
				gosx.El("p", nil, gosx.Text("A setup token was printed to the server console and log when this process started. Enter it below to begin building your league.")),
				gosx.El("p", nil, gosx.Text("Lost the token, or the browser crashed? Restart the container: a restart in setup state always prints a fresh token.")),
			),
			gosx.El("aside", gosx.Attrs(gosx.Attr("class", "login-console")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "section-index")), gosx.Text("SETUP TOKEN")),
				gosx.El("h2", nil, gosx.Text("Enter the console token")),
				setupFlashNode(ctx),
				ctx.Form(
					gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", "/setup")),
					gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf_token"), gosx.Attr("value", csrf))),
					gosx.El("label", gosx.Attrs(gosx.Attr("for", "setup-token")), gosx.Text("Setup token")),
					gosx.El("input", gosx.Attrs(
						gosx.Attr("type", "text"), gosx.Attr("id", "setup-token"), gosx.Attr("name", "token"),
						gosx.Attr("autocomplete", "off"), gosx.Attr("autocapitalize", "off"), gosx.Attr("spellcheck", "false"),
						gosx.Attr("required", true), gosx.Attr("placeholder", "the token printed at boot"),
					)),
					gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit"), gosx.Attr("class", "button button--primary")), gosx.Text("Continue")),
				),
			),
		),
	)
}

// metaRefreshNode is the zero-bespoke-JS answer to "a Route/PageHandler
// cannot issue a raw HTTP redirect" (gosx's plain route.Route contract
// returns a rendered Node, not response control — only a raw http.Handler,
// registered via router.Handle, can call http.Redirect). A meta-refresh
// plus a visible fallback link works in every browser (including with
// JS/refresh disabled, via the link) and needs no client script. Used for
// the bare /setup root's "go to the first incomplete step" bounce and the
// review step's "come back once every step is done" bounce.
func metaRefreshNode(target, message string) gosx.Node {
	return gosx.El("main", gosx.Attrs(gosx.Attr("class", "page"), gosx.Attr("id", "main-content")),
		gosx.RawHTML(`<meta http-equiv="refresh" content="0; url=`+target+`">`),
		gosx.El("p", nil, gosx.Text(message+" ")),
		gosx.El("a", gosx.Attrs(gosx.Attr("href", target), gosx.Attr("data-gosx-link", true)), gosx.Text("Continue →")),
	)
}

// setupCompletionNode renders the design's hybrid-restart completion page
// (section 4.5, step 6): the invite links, one final display, the setup
// summary, and truthful restart copy — a supervised restart's imminent
// exit, or a plain instruction to restart manually. Every /setup request
// renders this once rt.SetCompletion has been called, regardless of which
// step or action it targeted: the wizard's job is over.
func setupCompletionNode(ctx *route.RouteContext, result wizardCommitResult) gosx.Node {
	var body []any
	body = append(body, gosx.Attrs(gosx.Attr("class", "page"), gosx.Attr("id", "main-content")))
	body = append(body,
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "signal-label")), gosx.Text("SETUP COMPLETE")),
		gosx.El("h1", nil, gosx.Text("Your league is configured")),
		gosx.El("p", nil, gosx.Text("league.json is written at "+result.ConfigPath+".")),
	)
	if len(result.InviteLinks) > 0 {
		body = append(body, gosx.El("h2", nil, gosx.Text("Invite links (shown once — copy them now)")))
		var items []any
		for _, link := range result.InviteLinks {
			items = append(items, gosx.El("li", nil, gosx.Text(link.Email+": "+link.URL)))
		}
		body = append(body, gosx.El("ul", items...))
		body = append(body, gosx.El("p", nil, gosx.Text("Each link signs in as its member's email, once. /admin can mint a replacement later.")))
	}
	if result.Supervised {
		body = append(body,
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "flash-message")), gosx.Text("The server restarts now.")),
			gosx.RawHTML(`<meta http-equiv="refresh" content="3; url=/">`),
		)
	} else {
		body = append(body,
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "flash-message")), gosx.Text("Restart this process now to finish loading your league. This instance does not restart itself.")),
		)
	}
	return gosx.El("main", body...)
}

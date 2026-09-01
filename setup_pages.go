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

// setupWizardEntryNode renders the post-claim landing. Slice 3 replaces
// this placeholder with the real step-1 wizard page; the token-claim gate,
// session binding, and health/route isolation this slice builds do not
// change underneath it.
func setupWizardEntryNode(ctx *route.RouteContext) gosx.Node {
	return gosx.El("main", gosx.Attrs(gosx.Attr("class", "page"), gosx.Attr("id", "main-content")),
		gosx.El("section", gosx.Attrs(gosx.Attr("class", "login-stage")),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "login-poster")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "signal-label")), gosx.Text("SETUP TOKEN ACCEPTED")),
				gosx.El("h1", nil, gosx.Text("You're verified for setup")),
				gosx.El("p", nil, gosx.Text("The setup wizard runs from here: league identity, teams, scoring, roster shape, draft meeting, waivers, trades, membership, and your commissioner account.")),
			),
		),
	)
}

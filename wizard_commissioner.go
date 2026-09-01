package main

import (
	"net/http"
	"strings"

	"gridiron-2000/internal/setupwizard"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// registerCommissionerStep mounts step 9 (design section 4.1): the
// commissioner's own email (becomes COMMISSIONER_EMAILS) and optional
// IDENTITY_ALIASES entries for that same commissioner. Neither field is
// part of league.json (design section 4.3), so there is nothing here for
// LoadConfigBytes to validate — only a well-formedness check on the email
// shape itself, the same shallow check MintInviteLink already applies.
func registerCommissionerStep(router *route.Router, rt *SetupRuntime) {
	const slug = "commissioner"
	router.Add(route.Route{Pattern: "/setup/" + slug, Handler: wizardPageGuard(rt, func(ctx *route.RouteContext) gosx.Node {
		state := rt.Wizard.View()
		email := state.Draft.CommissionerEmail
		aliasLines := strings.Join(aliasesToLines(state.Draft.IdentityAliases), "\n")
		formError := ""
		if message, form, ok := consumeWizardError(ctx.Request, slug); ok {
			formError = message
			email = form["commissioner_email"]
			aliasLines = form["aliases"]
		}
		fields := []wizardField{
			{Name: "commissioner_email", Label: "Commissioner email", Value: email, Help: "This identity becomes COMMISSIONER_EMAILS. Commissioner powers work at every auth tier, including Tier 0 — a nudge, never a requirement, to add a stronger sign-in method later."},
			{
				Name: "aliases", Label: "Additional identities for this same commissioner (optional)", Kind: wizardFieldTextarea,
				Value: aliasLines,
				Help:  "One email per line. Each signs in as the same commissioner (IDENTITY_ALIASES) — useful for a work and personal Google account, for example.",
			},
		}
		return wizardStepPage(ctx, state.Status, slug, "Commissioner account", fields, formError, nil)
	})})
	router.Handle("POST /setup/"+slug, wizardActionGuard(rt, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			session.AddFlash(r, "notice", "That submission could not be read. Try again.")
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		form := flattenForm(r)
		email := strings.TrimSpace(form["commissioner_email"])
		if email == "" || !strings.Contains(email, "@") {
			stashWizardError(r, slug, "Enter a valid commissioner email address.", form)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		var aliases []setupwizard.IdentityAlias
		var invalid []string
		for _, line := range linesFromTextarea(form["aliases"]) {
			if line == email {
				continue
			}
			if !strings.Contains(line, "@") {
				invalid = append(invalid, line)
				continue
			}
			aliases = append(aliases, setupwizard.IdentityAlias{Alias: line, Canonical: email})
		}
		if len(invalid) > 0 {
			stashWizardError(r, slug, "Not a valid email: "+strings.Join(invalid, ", "), form)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		if err := rt.Wizard.SetCommissioner(email, aliases); err != nil {
			stashWizardError(r, slug, err.Error(), form)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		next := setupwizard.NextStepSlug(slug)
		http.Redirect(w, r, "/setup/"+next, http.StatusSeeOther)
	})))
}

func aliasesToLines(aliases []setupwizard.IdentityAlias) []string {
	lines := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		lines = append(lines, alias.Alias)
	}
	return lines
}

package main

import (
	"net/http"
	"strings"

	"gridiron-2000/internal/setupwizard"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// registerMembershipStep mounts step 8 (design section 4.1): admission
// posture (an optional bare domain) and the Tier 0 member email list.
// Member emails are not a league.json field — they become Store.AddInvite
// entries and minted invite_links rows only at the final commit (design
// section 4.5, step 3); this step only records the intent.
func registerMembershipStep(router *route.Router, rt *SetupRuntime) {
	const slug = "membership"
	router.Add(route.Route{Pattern: "/setup/" + slug, Handler: wizardPageGuard(rt, func(ctx *route.RouteContext) gosx.Node {
		state := rt.Wizard.View()
		config := state.Draft.Config
		memberEmails := state.Draft.MemberEmails
		formError := ""
		if message, form, ok := consumeWizardError(ctx.Request, slug); ok {
			formError = message
			config.Membership.AllowedDomain = form["allowed_domain"]
			memberEmails = linesFromTextarea(form["member_emails"])
		}
		fields := []wizardField{
			{
				Name: "allowed_domain", Label: "Allowed email domain (optional)", Value: config.Membership.AllowedDomain,
				Help: "Leave blank for no domain gate. A bare domain like example.com, never an email address.",
			},
			{
				Name: "member_emails", Label: "Member emails (one per line)", Kind: wizardFieldTextarea, Rows: 8,
				Value: strings.Join(memberEmails, "\n"),
				Help:  "Each email gets a single-use Tier 0 invite link at commit — no mail is sent to it at this tier; you hand out the link yourself.",
			},
		}
		return wizardStepPage(ctx, state.Status, slug, "Membership and invites", fields, formError, nil)
	})})
	router.Handle("POST /setup/"+slug, wizardActionGuard(rt, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			session.AddFlash(r, "notice", "That submission could not be read. Try again.")
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		form := flattenForm(r)
		emails := linesFromTextarea(form["member_emails"])
		var invalid []string
		for _, email := range emails {
			if !strings.Contains(email, "@") {
				invalid = append(invalid, email)
			}
		}
		if len(invalid) > 0 {
			stashWizardError(r, slug, "Not a valid email: "+strings.Join(invalid, ", "), form)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		current := rt.Wizard.View().Draft.Config
		candidate := current
		candidate.Membership.AllowedDomain = form["allowed_domain"]
		if _, err := rt.Wizard.ApplyStep(slug, candidate); err != nil {
			stashWizardError(r, slug, err.Error(), form)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		if err := rt.Wizard.SetMemberEmails(emails); err != nil {
			stashWizardError(r, slug, err.Error(), form)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		next := setupwizard.NextStepSlug(slug)
		http.Redirect(w, r, "/setup/"+next, http.StatusSeeOther)
	})))
}

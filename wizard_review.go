package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"gridiron-2000/internal/setupwizard"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// osExit is os.Exit behind a variable so a test can observe/replace the
// hybrid restart's real process-exit side effect without ever calling it
// for real (see defaultRestartHook, setup_app.go).
var osExit = os.Exit

// wizardCommitResult is what the completion page (setupCompletionNode)
// needs after a successful commit.
type wizardCommitResult struct {
	ConfigPath  string
	InviteLinks []mintedInviteLink
	Supervised  bool
}

// registerReviewStep mounts step 13 (design section 4.1): the full
// rendered league.json, warnings, a typed confirmation phrase (the
// league's own short code), and the COMMIT action. GET refuses to render
// the review itself until every earlier step is DONE (design section 4.2:
// "The review step requires a clean pass with no placeholders") — it
// bounces to the first incomplete step instead.
func registerReviewStep(router *route.Router, rt *SetupRuntime) {
	const slug = "review"
	router.Add(route.Route{Pattern: "/setup/" + slug, Handler: wizardPageGuard(rt, func(ctx *route.RouteContext) gosx.Node {
		state := rt.Wizard.View()
		if !state.Status.AllDone() {
			return metaRefreshNode("/setup/"+state.Status.FirstIncompleteStep(), "Finish the earlier steps first.")
		}
		formError := ""
		if message, _, ok := consumeWizardError(ctx.Request, slug); ok {
			formError = message
		}
		_, warnings, _ := setupwizard.Validate(state.Draft.Config)
		return renderReviewPage(ctx, state, formError, warnings)
	})})
	router.Handle("POST /setup/"+slug, wizardActionGuard(rt, commitActionHandler(rt)))
}

func renderReviewPage(ctx *route.RouteContext, state setupwizard.State, formError string, warnings []string) gosx.Node {
	pretty, _ := json.MarshalIndent(state.Draft.Config, "", "  ")
	csrf := session.Token(ctx.Request)

	var page []any
	page = append(page, gosx.Attrs(gosx.Attr("class", "page wizard-page"), gosx.Attr("id", "main-content")))
	page = append(page, wizardStepNavNode(state.Status, "review"))
	page = append(page, gosx.El("h1", nil, gosx.Text("Review and confirm")))
	if formError != "" {
		page = append(page, gosx.El("p", gosx.Attrs(gosx.Attr("class", "flash-message")), gosx.Text(formError)))
	}
	for _, warning := range warnings {
		page = append(page, gosx.El("p", gosx.Attrs(gosx.Attr("class", "wizard-warning")), gosx.Text("Note: "+warning)))
	}
	page = append(page,
		gosx.El("h2", nil, gosx.Text("league.json")),
		gosx.El("pre", gosx.Attrs(gosx.Attr("class", "wizard-review-json")), gosx.Text(string(pretty))),
		gosx.El("h2", nil, gosx.Text("Commissioner")),
		gosx.El("p", nil, gosx.Text(state.Draft.CommissionerEmail)),
		gosx.El("h2", nil, gosx.Text("Members invited at commit")),
		gosx.El("p", nil, gosx.Text(strings.Join(state.Draft.MemberEmails, ", "))),
	)
	page = append(page, gosx.El("a", gosx.Attrs(gosx.Attr("href", "/setup/"+setupwizard.PrevStepSlug("review")), gosx.Attr("class", "button button--ghost"), gosx.Attr("data-gosx-link", true)), gosx.Text("← Back")))
	page = append(page, ctx.Form(
		gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", "/setup/review")),
		gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf_token"), gosx.Attr("value", csrf))),
		gosx.El("label", gosx.Attrs(gosx.Attr("for", "field-confirm")), gosx.Text("Type the league short code ("+state.Draft.Config.League.ShortCode+") to confirm and commit")),
		gosx.El("input", gosx.Attrs(gosx.Attr("type", "text"), gosx.Attr("id", "field-confirm"), gosx.Attr("name", "confirm"), gosx.Attr("autocomplete", "off"))),
		gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit"), gosx.Attr("class", "button button--primary")), gosx.Text("COMMIT")),
	))
	return gosx.El("main", page...)
}

func commitActionHandler(rt *SetupRuntime) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const slug = "review"
		state := rt.Wizard.View()
		if !state.Status.AllDone() {
			http.Redirect(w, r, "/setup/"+state.Status.FirstIncompleteStep(), http.StatusSeeOther)
			return
		}
		if err := r.ParseForm(); err != nil {
			session.AddFlash(r, "notice", "That submission could not be read. Try again.")
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		confirm := strings.TrimSpace(r.PostFormValue("confirm"))
		if confirm != state.Draft.Config.League.ShortCode {
			stashWizardError(r, slug, "Type the exact league short code ("+state.Draft.Config.League.ShortCode+") to confirm.", nil)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}

		dataDir := dataDirFromEnv()
		commitCtx, err := newCommitContext(rt.Store, state.Draft, dataDir, appVersion, time.Now())
		if err != nil {
			stashWizardError(r, slug, "Setup could not start the commit: "+err.Error(), nil)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}
		if err := commitWizard(commitCtx); err != nil {
			stashWizardError(r, slug, "Commit failed and was not completed: "+err.Error()+". Fix the issue and try again; your progress is saved.", nil)
			http.Redirect(w, r, "/setup/"+slug, http.StatusSeeOther)
			return
		}

		supervised := strings.TrimSpace(os.Getenv("GRIDIRON_SUPERVISED")) == "1"
		rt.SetCompletion(wizardCommitResult{
			ConfigPath:  dataDir + "/league.json",
			InviteLinks: commitCtx.mintedLinks,
			Supervised:  supervised,
		})
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		go func() {
			time.Sleep(500 * time.Millisecond)
			rt.Restart()
		}()
	})
}

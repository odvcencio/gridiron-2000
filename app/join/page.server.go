package join

import (
	"errors"
	"gridiron-2000/internal/actionui"
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().SignupData(ctx.Request)
			data["has_signup_error"] = false
			data["signup_error"] = ""
			data["team_name_value"] = ""
			data["selected_motif"] = ""
			if badges, ok := data["badge_grid"].([]league.UnclaimedBadgeOption); ok && len(badges) > 0 {
				// A concrete default means the normal path cannot fail merely
				// because the manager missed a visually-hidden radio control.
				// The action remains authoritative if this motif is claimed in
				// the interval between render and submit.
				data["selected_motif"] = badges[0].Slug
			}
			if view, ok := ctx.ActionState("signup-claim"); ok {
				// Managed actions retain submitted values after validation. Do
				// not make an invitee retype a team name or rediscover their
				// badge choice after a race or another correctable error.
				data["team_name_value"] = view.Value("team_name")
				data["selected_motif"] = view.Value("motif")
				message := view.Message()
				if message == "" {
					message = view.Error("motif")
					if message == "" {
						message = view.Error("team_name")
					}
				}
				if message != "" {
					data["has_signup_error"] = true
					data["signup_error"] = message
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Fantasy Signup")},
				Description: "Claim an open fantasy franchise seat: name your team and pick a badge.",
			}, nil
		},
		Actions: route.FileActions{
			// signup-claim is the fantasy-signup atomic claim (build item
			// 2): team name + badge motif in one seat claim. See
			// league.Service.ClaimFantasySeat for the write sequence and
			// its rollback contract on a later failure (for example a
			// motif race).
			"signup-claim": func(ctx *action.Context) error {
				team, err := league.Default().ClaimFantasySeat(ctx.Request, ctx.FormData["team_name"], ctx.FormData["motif"])
				if err != nil {
					return signupClaimValidation(ctx, err)
				}
				actionui.RedirectWithNotice(ctx, "/team", "Welcome to "+team.Name+".")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

// signupClaimField consumes the typed service-boundary attribution. It
// deliberately declines to mark persistence/identity and admission failures
// on an input control: those are form-level conditions, not bad field values.
func signupClaimField(err error) (string, bool) {
	var validation *league.ClaimValidationError
	if !errors.As(err, &validation) ||
		errors.Is(err, league.ErrInternal) ||
		errors.Is(err, league.ErrPersistenceIndeterminate) {
		return "", false
	}
	return validation.Field.FormKey(), true
}

func signupClaimValidation(ctx *action.Context, err error) error {
	if field, ok := signupClaimField(err); ok {
		return actionui.Validation(ctx, "join", field, err)
	}
	var formData map[string]string
	if ctx != nil {
		formData = ctx.FormData
	}
	return action.Validation(actionui.Message("join", err), nil, formData)
}

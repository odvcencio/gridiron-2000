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
			// SignupData owns the badge picker and legacy seat counters; the
			// public-entry projection owns whether this viewer may actually
			// claim. Keep the two boundaries together so stale /join renders
			// never expose a form that ClaimFantasySeat must reject.
			if _, ok := data["public_entry"]; !ok {
				data["public_entry"] = league.Default().PublicEntryData(ctx.Request)
			}
			data["has_signup_error"] = false
			data["signup_error"] = ""
			data["team_name_value"] = ""
			data["selected_motif"] = ""
			if badges, ok := data["badge_grid"].([]league.UnclaimedBadgeOption); ok {
				// Rewrap into badgeMaskOption, unconditionally (even an empty
				// grid), so data["badge_grid"] is always the one type
				// page.gsx's <Each> renders: a mask-image swatch needs the
				// same immutably-cacheable "?v=" href
				// app/team/page.server.go's badgeGridProps stamps onto its own
				// grid, from the one place that reads the file
				// (league.Service.MotifMaskHref, internal/league/avatar.go) —
				// see badgeMaskOption's own doc comment for why this wraps
				// rather than extends league.UnclaimedBadgeOption.
				wrapped := badgeMaskOptions(badges)
				data["badge_grid"] = wrapped
				if len(wrapped) > 0 {
					// A concrete default means the normal path cannot fail
					// merely because the manager missed a visually-hidden
					// radio control. The action remains authoritative if
					// this motif is claimed in the interval between render
					// and submit.
					data["selected_motif"] = wrapped[0].Slug
				}
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

// badgeMaskOption is league.UnclaimedBadgeOption (Slug, Name) plus MaskHref,
// the mask-image swatch's public URL with a content-hash "?v=" query — see
// league.Service.MotifMaskHref's own doc comment (internal/league/avatar.go)
// for why that hash lives there rather than being computed here or in
// page.gsx. A wrapping struct, not an added field on UnclaimedBadgeOption
// itself: that type is internal/league's own, shared with every other
// UnclaimedBadgeOption reader, and MaskHref is a join-page rendering
// concern, not a league-data fact.
type badgeMaskOption struct {
	Slug     string
	Name     string
	MaskHref string
}

// badgeMaskOptions wraps every entry in badges with its MaskHref, preserving
// order (badges[0] stays the "selected_motif" default the Load closure
// above reads back out).
func badgeMaskOptions(badges []league.UnclaimedBadgeOption) []badgeMaskOption {
	out := make([]badgeMaskOption, 0, len(badges))
	for _, badge := range badges {
		out = append(out, badgeMaskOption{
			Slug:     badge.Slug,
			Name:     badge.Name,
			MaskHref: league.Default().MotifMaskHref(badge.Slug),
		})
	}
	return out
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

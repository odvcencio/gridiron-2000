package login

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gridiron-2000/internal/actionui"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			configured := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")) != "" && strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")) != ""
			data := league.Default().LoginData(ctx.Request, configured)
			// seat_meter (gap-audit item 8, wave 4 — linden): marks each seat
			// taken/open by text, not colour alone, and carries the open-seat
			// count as the meter's own accessible name. Merged in here
			// (rather than inside LoginData/service.go) the same way
			// has_notice/notice below are — see SeatMeterData's own doc
			// comment for why this stays out of service.go.
			data["seat_meter"] = league.Default().SeatMeterData()
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("League Access")},
				Description: "Sign in with Google to check league admission and team access.",
			}, nil
		},
		Actions: route.FileActions{
			// co-manager-join is Decision 3's own explicit confirm (J5
			// F11): a co-manager invite used to bind on this identity's
			// first sign-in (main.go's old BindCoManagerOnSignIn call),
			// which consumed the invite before the pending confirm state
			// (PublicEntryCoManagerPending) could ever render. The seat
			// binds only here now, when the invited person themselves
			// chooses Join.
			"co-manager-join": func(ctx *action.Context) error {
				member, err := league.Default().ConfirmCoManagerJoin(ctx.Request)
				if err != nil {
					actionui.RedirectWithNotice(ctx, "/login", "That invite is no longer available. Ask the primary manager or commissioner to resend it.")
					return nil
				}
				teamLabel := league.Default().TeamLabel(member.TeamID)
				primaryName := league.Default().PrimaryNameForTeam(member.TeamID, member.Email)
				// F11a: a dedicated flash for the home page's first-session
				// arrival panel (app/page.server.go's coManagerWelcomePanel)
				// — the generic notice below never says what a shared seat
				// grants.
				session.AddFlash(ctx.Request, "co_manager_bound", map[string]any{
					"team_name":          teamLabel,
					"primary_first_name": league.FirstName(primaryName),
				})
				actionui.RedirectWithNotice(ctx, "/", league.CoManagerWelcomeFlash(teamLabel, primaryName))
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

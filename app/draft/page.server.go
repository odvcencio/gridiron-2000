package draft

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// DraftTeamCard is the typed data.teams entry spread into strict
// DraftTeam. It deliberately does not share DraftTeamProps' name (page.gsx
// declares that name itself, for gosx's own strict-component check): the
// tier-2 spread boundary (strictSpreadProps) proves struct values field by
// field, so DraftTeamCard only needs to structurally cover DraftTeamProps,
// and a distinct name here avoids colliding with page.gsx's own
// declaration when gosx build's strict-component check merges the two
// files' types.
type DraftTeamCard struct {
	OnClock        bool
	Tone           string
	HasAvatarImage bool
	AvatarImageURL string
	Name           string
	Abbreviation   string
	Presence       string
	Manager        string
	Division       string
	Ready          bool
	Autopick       bool
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func draftRedirectTarget(pos, query, page string) string {
	values := url.Values{}
	if pos != "" {
		values.Set("pos", pos)
	}
	if query != "" {
		values.Set("q", query)
	}
	if parsed, err := strconv.Atoi(page); err == nil && parsed > 1 {
		values.Set("page", strconv.Itoa(parsed))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/draft?" + encoded
	}
	return "/draft"
}

type draftBreakdownRowView struct {
	Scored bool
	Label  string
	Calc   string
	Points string
}

type draftPlayerCardView struct {
	ID              string
	Name            string
	Position        string
	NFLTeam         string
	Projection      string
	Rank            string
	Detail          string
	Headshot        string
	HasHeadshot     bool
	Jersey          string
	HasBreakdown    bool
	Breakdown       []draftBreakdownRowView
	BreakdownTotal  string
	HasHist         bool
	Hist            string
	Search          string
	HasDraftCapital bool
	DraftCapital    string
	HasOpponent     bool
	Opponent        string
	HasMatchup      bool
	MatchupTier     string
	MatchupChip     string
	MatchupDetail   string
}

func draftBreakdownProps(raw []map[string]any) []draftBreakdownRowView {
	out := make([]draftBreakdownRowView, 0, len(raw))
	for _, row := range raw {
		out = append(out, draftBreakdownRowView{
			Scored: boolField(row, "scored"),
			Label:  stringField(row, "label"),
			Calc:   stringField(row, "calc"),
			Points: stringField(row, "points"),
		})
	}
	return out
}

func draftPlayerProps(raw []map[string]any) []draftPlayerCardView {
	out := make([]draftPlayerCardView, 0, len(raw))
	for _, player := range raw {
		breakdown, _ := player["breakdown"].([]map[string]any)
		out = append(out, draftPlayerCardView{
			ID:              stringField(player, "id"),
			Name:            stringField(player, "name"),
			Position:        stringField(player, "position"),
			NFLTeam:         stringField(player, "nfl_team"),
			Projection:      stringField(player, "projection"),
			Rank:            stringField(player, "rank"),
			Detail:          stringField(player, "detail"),
			Headshot:        stringField(player, "headshot"),
			HasHeadshot:     boolField(player, "has_headshot"),
			Jersey:          stringField(player, "jersey"),
			HasBreakdown:    boolField(player, "has_breakdown"),
			Breakdown:       draftBreakdownProps(breakdown),
			BreakdownTotal:  stringField(player, "breakdown_total"),
			HasHist:         boolField(player, "has_hist"),
			Hist:            stringField(player, "hist"),
			Search:          stringField(player, "search"),
			HasDraftCapital: boolField(player, "has_draft_capital"),
			DraftCapital:    stringField(player, "draft_capital"),
			HasOpponent:     boolField(player, "has_opponent"),
			Opponent:        stringField(player, "opponent"),
			HasMatchup:      boolField(player, "has_matchup"),
			MatchupTier:     stringField(player, "matchup_tier"),
			MatchupChip:     stringField(player, "matchup_chip"),
			MatchupDetail:   stringField(player, "matchup_detail"),
		})
	}
	return out
}

// draftTeamProps converts DraftData's map[string]any "teams" slice into
// typed DraftTeamCard values so the draft-room grid's {...team} spread
// into strict DraftTeam proves clean: the tier-2 spread boundary rejects a
// map[string]any source outright (it "cannot prove field coverage").
func draftTeamProps(raw []map[string]any) []DraftTeamCard {
	out := make([]DraftTeamCard, 0, len(raw))
	for _, team := range raw {
		out = append(out, DraftTeamCard{
			OnClock:        boolField(team, "on_clock"),
			Tone:           stringField(team, "tone"),
			HasAvatarImage: boolField(team, "has_avatar_image"),
			AvatarImageURL: stringField(team, "avatar_image_url"),
			Name:           stringField(team, "name"),
			Abbreviation:   stringField(team, "abbreviation"),
			Presence:       stringField(team, "presence"),
			Manager:        stringField(team, "manager"),
			Division:       stringField(team, "division"),
			Ready:          boolField(team, "ready"),
			Autopick:       boolField(team, "autopick"),
		})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			// A page load is a heartbeat too; the 4s poll takes over after boot.
			league.Default().RecordPresence(ctx.Request, time.Now())
			data := league.Default().DraftData(ctx.Request)
			if teams, ok := data["teams"].([]map[string]any); ok {
				data["teams"] = draftTeamProps(teams)
			}
			if players, ok := data["available"].([]map[string]any); ok {
				data["available"] = draftPlayerProps(players)
			}
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_pick_error"] = false
			data["pick_error"] = ""
			for _, name := range []string{"make-pick", "draft-start"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("player_id"); message != "" {
						data["has_pick_error"] = true
						data["pick_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Draft Room")},
				Description: "The live " + league.SeatCountWord() + "-team snake draft room.",
			}, nil
		},
		Actions: route.FileActions{
			"draft-start": func(ctx *action.Context) error {
				if strings.TrimSpace(ctx.FormData["confirm"]) != "START" {
					message := "type START to confirm"
					return action.Validation(message, map[string]string{"player_id": message}, ctx.FormData)
				}
				started, err := league.Default().AdminStartDraft(ctx.Request)
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				message := "Draft was already live; the original clock is unchanged."
				if started {
					message = "Draft started. Pick one is on the clock."
				}
				actionui.RedirectWithNotice(ctx, "/draft", message)
				return nil
			},
			// The commissioner clock controls render on THIS page (the
			// clock panel in page.gsx) and actionPath resolves against the
			// draft module, so the five clock actions must be registered
			// here as well as on /admin — the service methods carry the
			// requireCommissioner gate, so this is routing, not authority.
			// Before this registration the draft-room clock forms posted
			// into a 404 (found during the gosx v0.46 adoption pass).
			"clock-pause": func(ctx *action.Context) error {
				if err := league.Default().AdminPauseClock(ctx.Request); err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				actionui.RedirectWithNotice(ctx, "/draft", "Pick clock paused.")
				return nil
			},
			"clock-resume": func(ctx *action.Context) error {
				if err := league.Default().AdminResumeClock(ctx.Request); err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				actionui.RedirectWithNotice(ctx, "/draft", "Pick clock resumed.")
				return nil
			},
			"clock-force-autopick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().AdminForceAutopick(ctx.Request)
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				actionui.RedirectWithNotice(ctx, "/draft", fmt.Sprintf("Pick %d: %s auto-selects %s.", pick.Number, team.Name, player.Name))
				return nil
			},
			"clock-extend": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"player_id": message}, ctx.FormData)
				}
				if err := league.Default().AdminExtendClock(ctx.Request, secs); err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				actionui.RedirectWithNotice(ctx, "/draft", fmt.Sprintf("Clock extended by %d seconds.", secs))
				return nil
			},
			"clock-set-duration": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"player_id": message}, ctx.FormData)
				}
				if err := league.Default().AdminSetClockSeconds(ctx.Request, secs); err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				actionui.RedirectWithNotice(ctx, "/draft", fmt.Sprintf("Pick clock set to %d seconds.", secs))
				return nil
			},
			"toggle-ready": func(ctx *action.Context) error {
				ready, teamName, err := league.Default().ToggleReady(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "checked out"
				if ready {
					status = "locked in"
				}
				actionui.RedirectWithNotice(ctx, "/draft", fmt.Sprintf("%s is %s for draft night.", teamName, status))
				return nil
			},
			"make-pick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().MakePick(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"player_id": err.Error()}, ctx.FormData)
				}
				actionui.RedirectWithNotice(ctx, draftRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), fmt.Sprintf("Pick %d: %s selects %s.", pick.Number, team.Name, player.Name))
				return nil
			},
			"toggle-autopick": func(ctx *action.Context) error {
				on, teamName, err := league.Default().ToggleAutopick(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "off"
				if on {
					status = "on"
				}
				actionui.RedirectWithNotice(ctx, "/draft", fmt.Sprintf("Autopick is %s for %s.", status, teamName))
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

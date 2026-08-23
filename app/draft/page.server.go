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
	TeamID         string
	OnClock        bool
	Tone           string
	HasAvatarImage bool
	AvatarImageURL string
	Name           string
	Abbreviation   string
	Presence       string
	PresenceLabel  string
	PresenceDetail string
	OperatorCount  int
	Manager        string
	Division       string
	Ready          bool
	Autopick       bool
}

type DraftSeatControlCard struct {
	TeamID         string
	Name           string
	Manager        string
	PresenceLabel  string
	PresenceDetail string
	OnClock        bool
	Ready          bool
	Autopick       bool
	Action         string
	ReadyAction    string
	CSRF           string
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func parseSeatAutopick(raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("AUTO mode must be exactly true or false")
	}
}

func parseSeatReady(raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("Ready must be exactly true or false")
	}
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func intField(m map[string]any, key string) int {
	value, _ := m[key].(int)
	return value
}

func mapField(m map[string]any, key string) map[string]any {
	value, _ := m[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func draftActionPath(name string) string { return "/draft/__actions/" + name }

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
	CanDraft        bool
}

type draftRoomView struct {
	Data          map[string]any
	CSRF          string
	Actions       map[string]string
	StatusSummary string
}

type draftWorkspaceView struct {
	Data           map[string]any
	Players        []draftPlayerCardView
	CSRF           string
	MakePickAction string
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
			CanDraft:        boolField(player, "draft_eligible"),
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
			TeamID:         stringField(team, "id"),
			OnClock:        boolField(team, "on_clock"),
			Tone:           stringField(team, "tone"),
			HasAvatarImage: boolField(team, "has_avatar_image"),
			AvatarImageURL: stringField(team, "avatar_image_url"),
			Name:           stringField(team, "name"),
			Abbreviation:   stringField(team, "abbreviation"),
			Presence:       stringField(team, "presence"),
			PresenceLabel:  stringField(team, "presence_label"),
			PresenceDetail: stringField(team, "presence_detail"),
			OperatorCount:  intField(team, "operator_count"),
			Manager:        stringField(team, "manager"),
			Division:       stringField(team, "division"),
			Ready:          boolField(team, "ready"),
			Autopick:       boolField(team, "autopick"),
		})
	}
	return out
}

func draftSeatControlProps(raw []map[string]any) []DraftSeatControlCard {
	out := make([]DraftSeatControlCard, 0, len(raw))
	for _, team := range raw {
		// AUTO delegates a real manager's future turns. Open franchises have
		// neither an operator nor a Big Board owner, so presenting a control
		// for them would invent authority and a selection strategy.
		if !boolField(team, "claimed") {
			continue
		}
		out = append(out, DraftSeatControlCard{
			TeamID:         stringField(team, "id"),
			Name:           stringField(team, "name"),
			Manager:        stringField(team, "manager"),
			PresenceLabel:  stringField(team, "presence_label"),
			PresenceDetail: stringField(team, "presence_detail"),
			OnClock:        boolField(team, "on_clock"),
			Ready:          boolField(team, "ready"),
			Autopick:       boolField(team, "autopick"),
			Action:         draftActionPath("seat-autopick"),
			ReadyAction:    draftActionPath("seat-ready"),
		})
	}
	return out
}

func prepareDraftData(data map[string]any) map[string]any {
	teams, _ := data["teams"].([]map[string]any)
	players, _ := data["available"].([]map[string]any)
	typedTeams := draftTeamProps(teams)
	typedPlayers := draftPlayerProps(players)
	data["teams"] = typedTeams
	data["seat_controls"] = draftSeatControlProps(teams)
	data["available"] = typedPlayers

	viewData := make(map[string]any, len(data)+1)
	for key, value := range data {
		viewData[key] = value
	}
	workspaceURL := draftWorkspaceFragmentURL(data)
	viewData["workspace_fragment_url"] = workspaceURL
	data["workspace_fragment_url"] = workspaceURL
	room := draftRoomView{Data: viewData, Actions: map[string]string{
		"draft_start": draftActionPath("draft-start"), "toggle_ready": draftActionPath("toggle-ready"),
		"toggle_autopick": draftActionPath("toggle-autopick"), "clock_pause": draftActionPath("clock-pause"),
		"clock_resume": draftActionPath("clock-resume"), "clock_extend": draftActionPath("clock-extend"),
		"clock_duration": draftActionPath("clock-set-duration"), "clock_autopick": draftActionPath("clock-force-autopick"),
		"seat_autopick": draftActionPath("seat-autopick"), "seat_ready": draftActionPath("seat-ready"),
	}}
	room.StatusSummary = draftRoomStatus(viewData)
	data["room"] = room
	data["workspace"] = draftWorkspaceView{
		Data: viewData, Players: typedPlayers, MakePickAction: draftActionPath("make-pick"),
	}
	return data
}

func draftWorkspaceFragmentURL(data map[string]any) string {
	values := url.Values{}
	if pos := stringField(data, "pool_position"); pos != "" {
		values.Set("pos", pos)
	}
	if query := stringField(data, "pool_query"); query != "" {
		values.Set("q", query)
	}
	if page := intField(data, "pool_page"); page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/draft/fragment/workspace?" + encoded
	}
	return "/draft/fragment/workspace"
}

func draftRoomStatus(data map[string]any) string {
	if boolField(mapField(data, "draft"), "complete") {
		return fmt.Sprintf("Draft complete; %d picks locked; the clock is stopped.", intField(data, "pick_number"))
	}
	onClock := stringField(mapField(data, "on_clock"), "abbreviation")
	clockView := mapField(data, "clock")
	clockPhrase := "the clock is not running"
	if boolField(clockView, "paused") {
		clockPhrase = "clock paused"
	} else if boolField(clockView, "armed") {
		clockPhrase = "clock running"
	}
	return fmt.Sprintf("Pick %d; %s on the clock; %d of %d ready; %s.", intField(data, "pick_number"), onClock, intField(data, "ready_count"), intField(data, "manager_count"), clockPhrase)
}

func attachDraftRequestState(data map[string]any, request *http.Request) map[string]any {
	room, _ := data["room"].(draftRoomView)
	workspace, _ := data["workspace"].(draftWorkspaceView)
	token := session.Token(request)
	room.CSRF = token
	workspace.CSRF = token
	seats, _ := room.Data["seat_controls"].([]DraftSeatControlCard)
	for i := range seats {
		seats[i].CSRF = token
	}
	room.Data["seat_controls"] = seats
	workspace.Data["seat_controls"] = seats
	data["room"] = room
	data["workspace"] = workspace
	return data
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			// A page load is a heartbeat too; the 4s poll takes over after boot.
			league.Default().RecordPresence(ctx.Request, time.Now())
			data := attachDraftRequestState(prepareDraftData(league.Default().DraftData(ctx.Request)), ctx.Request)
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
					return actionui.Validation(ctx, "draft", "player_id", err)
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
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/draft", "Pick clock paused.")
				return nil
			},
			"clock-resume": func(ctx *action.Context) error {
				if err := league.Default().AdminResumeClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				actionui.RedirectWithNotice(ctx, "/draft", "Pick clock resumed.")
				return nil
			},
			"clock-force-autopick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().AdminForceAutopick(ctx.Request)
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
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
					return actionui.Validation(ctx, "draft", "player_id", err)
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
					return actionui.Validation(ctx, "draft", "player_id", err)
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
					return actionui.Validation(ctx, "draft", "player_id", err)
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
			"seat-autopick": func(ctx *action.Context) error {
				onRaw := ctx.FormData["on"]
				on, err := parseSeatAutopick(onRaw)
				if err != nil {
					return actionui.Validation(ctx, "draft", "on", err)
				}
				teamID := strings.TrimSpace(ctx.FormData["team_id"])
				if err := league.Default().AdminSetAutopick(ctx.Request, teamID, on); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "manual control restored"
				if on {
					status = "AUTO mode enabled"
				}
				actionui.RedirectWithNotice(ctx, "/draft", fmt.Sprintf("%s for %s.", status, teamID))
				return nil
			},
			// seat-ready sets a claimed seat's Ready flag on the commissioner's
			// own authority (compare toggle-ready, the manager's own path).
			// It is restricted to claimed seats, matching seat-autopick: an
			// unclaimed seat has no manager whose readiness this could assert.
			"seat-ready": func(ctx *action.Context) error {
				onRaw := ctx.FormData["on"]
				on, err := parseSeatReady(onRaw)
				if err != nil {
					return actionui.Validation(ctx, "draft", "on", err)
				}
				teamID := strings.TrimSpace(ctx.FormData["team_id"])
				if err := league.Default().AdminSetReady(ctx.Request, teamID, on); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "checked out"
				if on {
					status = "locked in"
				}
				actionui.RedirectWithNotice(ctx, "/draft", fmt.Sprintf("%s is %s for draft night.", teamID, status))
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

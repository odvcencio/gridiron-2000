package draft

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
	"sort"
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
	Claimed        bool
	Ready          bool
	Autopick       bool
	BoardCount     int
	BoardGap       bool
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
	BoardCount     int
	BoardGap       bool
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

func draftActionSuccess(ctx *action.Context, target, message string) error {
	if action.WantsJSON(ctx.Request) {
		return ctx.Success(message, map[string]any{"value": "refresh"})
	}
	actionui.RedirectWithNotice(ctx, target, message)
	return nil
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
	// Taken marks a personal-queue entry someone else already drafted (the
	// row still shows, struck through client-side, rather than silently
	// disappearing). Always false for an available-pool entry, which
	// excludes drafted players by construction.
	Taken bool
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

// draftCommandView backs the always-visible command bar region: the
// on-clock team, the pick clock, the room summary, the sound and
// commissioner controls, and (while seated) the ready/autopick controls.
type draftCommandView struct {
	Data          map[string]any
	CSRF          string
	Actions       map[string]string
	StatusSummary string
}

// draftHistoryView backs the pick-tape pane. Task 7 adds the typed
// pick/board/team fields the tabs need; for now DraftHistory reads Data
// directly, the same untyped pattern draftRoomView's tape used before.
// Since is the "?since=" tape cursor (draftTapeSinceKey, fragment.go): -1
// means "unset", the pane's full DraftHistory render; a non-negative value
// switches draftRegionView to DraftTapeRows and Rows carries its rows.
type draftHistoryView struct {
	Data  map[string]any
	Since int
	Rows  []map[string]any
}

// draftAvailableView backs the available-players pane: the pool list plus
// the make-pick and queue-add actions every eligible row's buttons post to.
// Actions also backs DraftPickBar's mobile ready/autopick prompt (V1): the
// pick bar and the available pane already share this one view.
type draftAvailableView struct {
	Data           map[string]any
	Players        []draftPlayerCardView
	CSRF           string
	MakePickAction string
	QueueAddAction string
	Actions        map[string]string
}

// draftQueueView backs the "my team" pane: the viewer's full personal
// queue (including already-taken entries, Task 5a's DraftMyTeam), the
// roster-needs tally, the queue-remove action a taken row's Clear button
// posts to, and (V1) the Room segment's own ready/autopick controls, which
// moved here off the command bar.
type draftQueueView struct {
	Data              map[string]any
	Queue             []draftPlayerCardView
	CSRF              string
	QueueRemoveAction string
	Actions           map[string]string
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
			Taken:           boolField(player, "taken"),
		})
	}
	return out
}

// draftTapeRowsSince builds the tape pane's "?since=" partial: every pick
// numbered above since, newest first, each preceded by one round-header
// row the first time that round appears in the (descending) sequence. It
// copies pickMaps' own shape (service.go) plus is_round/tape_key, so
// DraftTapeRows reads a pick row exactly the way DraftHistory's full
// render already does (pick.player.name, pick.team.abbreviation, ...).
func draftTapeRowsSince(data map[string]any, since int) []map[string]any {
	picksRaw, _ := data["picks"].([]map[string]any)
	type tapeEntry struct {
		number int
		pick   map[string]any
	}
	entries := make([]tapeEntry, 0, len(picksRaw))
	for _, pick := range picksRaw {
		if number := intField(pick, "number"); number > since {
			entries = append(entries, tapeEntry{number: number, pick: pick})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].number > entries[j].number })
	rows := make([]map[string]any, 0, len(entries)*2)
	lastRound := -1
	for _, entry := range entries {
		round := intField(entry.pick, "round")
		if round != lastRound {
			rows = append(rows, map[string]any{
				"is_round": true, "round": round,
				"tape_key": "round-" + strconv.Itoa(round),
			})
			lastRound = round
		}
		row := make(map[string]any, len(entry.pick)+2)
		for key, value := range entry.pick {
			row[key] = value
		}
		row["is_round"] = false
		row["tape_key"] = "pick-" + strconv.Itoa(entry.number)
		rows = append(rows, row)
	}
	return rows
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
			Claimed:        boolField(team, "claimed"),
			Ready:          boolField(team, "ready"),
			Autopick:       boolField(team, "autopick"),
			BoardCount:     intField(team, "board_count"),
			BoardGap:       boolField(team, "board_gap"),
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
			BoardCount:     intField(team, "board_count"),
			BoardGap:       boolField(team, "board_gap"),
			Action:         draftActionPath("seat-autopick"),
			ReadyAction:    draftActionPath("seat-ready"),
		})
	}
	return out
}

// prepareDraftData never writes back into its data parameter: every
// derived field lands on viewData or output, two maps built fresh on each
// call. A caller that hands the same map to two requests in a row (the
// service always builds a fresh one per request, but a repeated-fixture
// test — TestCommandFragmentETagIgnoresTicksButChangesWithTheDeadline —
// deliberately does not) must keep reading the same raw teams/available/
// queue lists on the second call, not the first call's typed leftovers.
func prepareDraftData(data map[string]any) map[string]any {
	teams, _ := data["teams"].([]map[string]any)
	players, _ := data["available"].([]map[string]any)
	queueRaw, _ := data["queue"].([]map[string]any)
	typedTeams := draftTeamProps(teams)
	typedPlayers := draftPlayerProps(players)
	typedQueue := draftPlayerProps(queueRaw)

	// viewData is the one map every view's Data field shares (room,
	// workspace, command, history, available, queue below). It must never
	// itself gain one of those keys: draftRegionETag's json.Marshal of
	// semanticDraftRegionView's copy would otherwise walk a cycle back
	// through that key's own nested .Data. output, built after it, is the
	// top-level map prepareDraftData actually returns; nothing ETags
	// output itself, so it is free to carry them.
	viewData := make(map[string]any, len(data)+4)
	for key, value := range data {
		viewData[key] = value
	}
	viewData["teams"] = typedTeams
	viewData["seat_controls"] = draftSeatControlProps(teams)
	// has_adp gates the available pane's VS ADP header and cell (Task 6
	// execution note 5): Task 7 flips this once playerMap carries a real
	// adp/value_vs_adp pair, so the column stays hidden rather than showing
	// an empty header over an empty cell in the meantime.
	viewData["has_adp"] = false
	viewData["available_search_placeholder"] = fmt.Sprintf("Search %d available", intField(data, "available_count"))
	viewData["workspace_fragment_url"] = draftWorkspaceFragmentURL(data)

	actions := map[string]string{
		"draft_start": draftActionPath("draft-start"), "toggle_ready": draftActionPath("toggle-ready"),
		"toggle_autopick": draftActionPath("toggle-autopick"), "clock_pause": draftActionPath("clock-pause"),
		"clock_resume": draftActionPath("clock-resume"), "clock_extend": draftActionPath("clock-extend"),
		"clock_duration": draftActionPath("clock-set-duration"), "clock_autopick": draftActionPath("clock-force-autopick"),
		"seat_autopick": draftActionPath("seat-autopick"), "seat_ready": draftActionPath("seat-ready"),
	}
	room := draftRoomView{Data: viewData, Actions: actions}
	room.StatusSummary = draftRoomStatus(viewData)

	output := make(map[string]any, len(viewData)+8)
	for key, value := range viewData {
		output[key] = value
	}
	output["room"] = room
	output["workspace"] = draftWorkspaceView{
		Data: viewData, Players: typedPlayers, MakePickAction: draftActionPath("make-pick"),
	}
	// live_mode/shell_modifier drive the shell root's own attributes
	// (data-draft-live-mode, the --final class variant); "fallback" is the
	// only mode until Task 8 pins v0.53.10 and switches to target mode.
	output["live_mode"] = "fallback"
	output["shell_modifier"] = ""
	if complete, _ := mapField(data, "draft")["complete"].(bool); complete {
		output["shell_modifier"] = " draft-shell--final"
	}
	output["command"] = draftCommandView{Data: viewData, Actions: actions, StatusSummary: room.StatusSummary}
	// Since defaults to -1 ("unset"): draftRegionView (fragment.go) renders
	// the full DraftHistory pane until attachDraftFragmentSince overwrites
	// it with a "?since=" request's cursor and precomputed Rows.
	output["history"] = draftHistoryView{Data: viewData, Since: -1}
	output["available"] = draftAvailableView{
		Data: viewData, Players: typedPlayers,
		MakePickAction: draftActionPath("make-pick"), QueueAddAction: draftActionPath("queue-add"), Actions: actions,
	}
	output["queue"] = draftQueueView{
		Data: viewData, Queue: typedQueue, QueueRemoveAction: draftActionPath("queue-remove"), Actions: actions,
	}
	return output
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
	command, _ := data["command"].(draftCommandView)
	available, _ := data["available"].(draftAvailableView)
	queue, _ := data["queue"].(draftQueueView)
	token := session.Token(request)
	room.CSRF = token
	workspace.CSRF = token
	command.CSRF = token
	available.CSRF = token
	queue.CSRF = token
	seats, _ := room.Data["seat_controls"].([]DraftSeatControlCard)
	for i := range seats {
		seats[i].CSRF = token
	}
	room.Data["seat_controls"] = seats
	workspace.Data["seat_controls"] = seats
	data["room"] = room
	data["workspace"] = workspace
	data["command"] = command
	data["available"] = available
	data["queue"] = queue
	return data
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			ctx.Runtime().BindHub(draftLiveHubName, draftLiveBindingPath(), nil)
			// The initial page view is an attendance claim. The body heartbeat
			// keeps presence current while hub events own draft-state convergence.
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
			data["force_current_pick_confirm"] = ""
			// Keep the submitted optimistic token in the validation render.
			// Recomputing a fresh token here would turn a stale form into a
			// newly-authorized form if another browser changed the draft while
			// the first submission was being corrected.
			for _, name := range []string{"clock-force-autopick", "clock-extend"} {
				if view, ok := ctx.ActionState(name); ok {
					if view.Error("player_id") == "" {
						continue
					}
					if name == "clock-force-autopick" {
						data["force_current_pick_confirm"] = view.Value("confirm")
					}
					if submitted := strings.TrimSpace(view.Value("current_pick_token")); submitted != "" {
						data["current_pick_token"] = submitted
						if clock, ok := data["clock"].(map[string]any); ok {
							clock["current_pick_token"] = submitted
							clock["action_token"] = submitted
						}
					}
				}
			}
			for _, name := range []string{"make-pick", "draft-start", "clock-force-autopick", "clock-extend"} {
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
				return draftActionSuccess(ctx, "/draft", message)
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
				return draftActionSuccess(ctx, "/draft", "Pick clock paused.")
			},
			"clock-resume": func(ctx *action.Context) error {
				if err := league.Default().AdminResumeClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", "Pick clock resumed.")
			},
			"clock-force-autopick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().AdminForceAutopick(ctx.Request, ctx.FormData["confirm"], ctx.FormData["current_pick_token"])
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Pick %d: %s auto-selects %s.", pick.Number, team.Name, player.Name))
			},
			"clock-extend": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"player_id": message}, ctx.FormData)
				}
				if err := league.Default().AdminExtendClock(ctx.Request, secs, ctx.FormData["current_pick_token"]); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Clock extended by %d seconds.", secs))
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
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Pick clock set to %d seconds.", secs))
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
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("%s is %s for draft night.", teamName, status))
			},
			"make-pick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().MakePick(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, draftRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), fmt.Sprintf("Pick %d: %s selects %s.", pick.Number, team.Name, player.Name))
			},
			// queue-add/queue-remove back the shell's own Big Board controls
			// (the available pane's "+ Queue" button and the my-team pane's
			// "Clear" button on a taken row): BoardAdd/BoardRemove already
			// carry the ownership and validation rules, so these are routing
			// only, the same shape as make-pick above.
			"queue-add": func(ctx *action.Context) error {
				player, err := league.Default().BoardAdd(ctx.Request, ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				target := draftRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"])
				return draftActionSuccess(ctx, target, fmt.Sprintf("%s added to your queue.", player.Name))
			},
			"queue-remove": func(ctx *action.Context) error {
				if err := league.Default().BoardRemove(ctx.Request, ctx.FormData["player_id"]); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				target := draftRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"])
				return draftActionSuccess(ctx, target, "Removed from your queue.")
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
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Autopick is %s for %s.", status, teamName))
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
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("%s for %s.", status, league.Default().TeamLabel(teamID)))
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
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("%s is %s for draft night.", league.Default().TeamLabel(teamID), status))
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

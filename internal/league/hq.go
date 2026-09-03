package league

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ActionCenterStage is the finite manager-season journey shown above the
// dashboard's unchanged matchup/announcement/standings/activity overview.
type ActionCenterStage string

const (
	ActionCenterEntry              ActionCenterStage = "entry"
	ActionCenterPreDraft           ActionCenterStage = "predraft"
	ActionCenterDraftLive          ActionCenterStage = "draft_live"
	ActionCenterPostDraftPreseason ActionCenterStage = "postdraft_preseason"
	ActionCenterRegularSeason      ActionCenterStage = "regular_season"
	ActionCenterPlayoffs           ActionCenterStage = "playoffs"
	ActionCenterSeasonComplete     ActionCenterStage = "season_complete"
)

type ActionCenterPriority string

const (
	ActionCenterPriorityEntry       ActionCenterPriority = "entry"
	ActionCenterPriorityOnClock     ActionCenterPriority = "on_clock"
	ActionCenterPriorityDeadline    ActionCenterPriority = "deadline"
	ActionCenterPriorityStable      ActionCenterPriority = "stable"
	ActionCenterPriorityPreparation ActionCenterPriority = "preparation"
	ActionCenterPriorityInfo        ActionCenterPriority = "informational"
)

func actionCenterPriorityRank(p ActionCenterPriority) int {
	switch p {
	case ActionCenterPriorityEntry:
		return 0
	case ActionCenterPriorityOnClock:
		return 1
	case ActionCenterPriorityDeadline:
		return 2
	case ActionCenterPriorityStable:
		return 3
	case ActionCenterPriorityPreparation:
		return 4
	default:
		return 5
	}
}

// These facts intentionally retain exact Pick'em buckets; one unpicked count
// cannot distinguish a future lock from a game missed after kickoff.
type ActionCenterPickemFacts struct {
	Week            int
	GameCount       int
	PickedCount     int
	OpenUnpicked    int
	LockedUnpicked  int
	NextOpenLock    time.Time
	HasNextOpenLock bool
}

type ActionCenterLineupFacts struct {
	Week            int
	Problems        int
	FirstKickoff    time.Time
	HasFirstKickoff bool
}

type ActionCenterTradeFacts struct {
	IncomingOpen       int
	AcceptedReview     int
	OutgoingOpen       int
	NextReviewDeadline time.Time
	HasReviewDeadline  bool
	TradeDeadline      time.Time
	HasTradeDeadline   bool
}

type ActionCenterWaiverFacts struct {
	OpenClaims int
	NextRun    time.Time
	HasNextRun bool
}

// ActionCenterFacts is the service-to-pure-model boundary. BuildActionCenter
// performs no store, clock, or request access.
type ActionCenterFacts struct {
	Now                 time.Time
	Location            *time.Location
	EntryState          PublicEntryState
	EntryStateLabel     string
	EntryHeadline       string
	EntryActionHref     string
	EntryActionLabel    string
	EntryDetail         string
	Admitted            bool
	HasSeat             bool
	TeamID              string
	TeamName            string
	Commissioner        bool
	DraftStarted        bool
	DraftComplete       bool
	DraftAt             time.Time
	ViewerOnClock       bool
	OnClockTeamName     string
	ClockDeadline       time.Time
	ClockPaused         bool
	Ready               bool
	BoardCount          int
	SeatCapacity        int
	ClaimedSeats        int
	ReadySeats          int
	DraftOrderSet       bool
	DraftPoolCount      int
	DraftPoolTarget     int
	SeasonPhase         string
	ScheduleExists      bool
	WeekCloseWeek       int
	WeekCloseFinal      bool
	WeekCloseReady      bool
	WeekCloseGamesFinal int
	WeekCloseGamesTotal int
	WeekCloseStatsFresh bool
	WeekCloseReason     string
	Pickem              ActionCenterPickemFacts
	Lineup              ActionCenterLineupFacts
	Trades              ActionCenterTradeFacts
	Waivers             ActionCenterWaiverFacts
}

type ActionCenterAction struct {
	ID            string
	Priority      ActionCenterPriority
	PriorityLabel string
	Label         string
	Detail        string
	Href          string
	DueAt         time.Time
	HasDueAt      bool
	DueLabel      string
	Urgent        bool
	Primary       bool
	// NativeNavigation keeps OAuth and other browser hand-off endpoints out
	// of GoSX's managed-link fetch/prefetch path.
	NativeNavigation bool
}

// ActionCenterActionCard is the render-time shape used by the typed home
// adapter. Its name intentionally mirrors the strict GSX card declaration;
// GoSX proves the nested slice by shape and stable type name at render time.
type ActionCenterActionCard struct {
	ID               string
	Priority         string
	PriorityLabel    string
	Label            string
	Detail           string
	Href             string
	DueAt            string
	HasDueAt         bool
	DueLabel         string
	Urgent           bool
	Primary          bool
	NativeNavigation bool
}

type ActionCenter struct {
	Stage               ActionCenterStage
	StageLabel          string
	Heading             string
	Summary             string
	HasActions          bool
	ActionCount         int
	Actions             []ActionCenterAction
	HasCommissioner     bool
	CommissionerActions []ActionCenterAction
}

// BuildActionCenter is pure and deterministic for a fixed facts instant.
func BuildActionCenter(f ActionCenterFacts) ActionCenter {
	c := ActionCenter{Stage: resolveActionCenterStage(f)}
	c.StageLabel, c.Heading, c.Summary = actionCenterCopy(c.Stage, f)
	var actions []ActionCenterAction
	if a := entryAction(f); a != nil {
		actions = append(actions, *a)
	}
	if f.HasSeat {
		if a := draftAction(f); a.ID != "" {
			actions = append(actions, a)
		}
		if c.Stage != ActionCenterSeasonComplete {
			if a := lineupAction(f); a != nil {
				actions = append(actions, *a)
			}
			actions = append(actions, pickemActions(f)...)
			actions = append(actions, tradeActions(f)...)
			if a := waiverAction(f); a != nil {
				actions = append(actions, *a)
			}
		}
		actions = append(actions, preparationActions(f)...)
	}
	sortActionCenterActions(actions)
	if len(actions) == 0 {
		actions = append(actions, informationalAction(f))
	}
	c.Actions, c.HasActions, c.ActionCount = actions, true, len(actions)
	if f.Commissioner {
		c.CommissionerActions = commissionerActions(f)
		c.HasCommissioner = len(c.CommissionerActions) > 0
	}
	return c
}

func resolveActionCenterStage(f ActionCenterFacts) ActionCenterStage {
	if !f.HasSeat || !f.Admitted ||
		f.EntryState == PublicEntryAuthenticatedPending ||
		f.EntryState == PublicEntryCoManagerPending ||
		f.EntryState == PublicEntryAdmittedSeatlessOpen ||
		f.EntryState == PublicEntryAdmittedSeatlessFull {
		return ActionCenterEntry
	}
	if f.DraftStarted && !f.DraftComplete {
		return ActionCenterDraftLive
	}
	if f.DraftComplete {
		switch f.SeasonPhase {
		case PhaseRegularSeason:
			return ActionCenterRegularSeason
		case PhasePlayoffs:
			return ActionCenterPlayoffs
		case PhaseSeasonComplete:
			return ActionCenterSeasonComplete
		default:
			return ActionCenterPostDraftPreseason
		}
	}
	return ActionCenterPreDraft
}

func actionCenterCopy(stage ActionCenterStage, f ActionCenterFacts) (string, string, string) {
	switch stage {
	case ActionCenterEntry:
		stageLabel := "ENTRY // NEXT STEP"
		headline := "GET INTO THE LEAGUE."
		if strings.TrimSpace(f.EntryStateLabel) != "" {
			stageLabel = strings.TrimSpace(f.EntryStateLabel)
		}
		if strings.TrimSpace(f.EntryHeadline) != "" {
			headline = strings.TrimSpace(f.EntryHeadline)
		}
		return stageLabel, headline, "Complete admission or claim a franchise before setting up the season."
	case ActionCenterPreDraft:
		return "PRE-DRAFT // GET READY", "BUILD YOUR SEASON.", "Finish the manager decisions that make draft night and Week 1 count."
	case ActionCenterDraftLive:
		if f.ViewerOnClock {
			return "DRAFT LIVE // YOUR TURN", "YOU ARE ON THE CLOCK.", "The draft is live. Open the room before the clock moves."
		}
		return "DRAFT LIVE // FOLLOW THE ROOM", "THE ROOM IS LIVE.", "The commissioner opened the draft. Follow the room for your turn."
	case ActionCenterPostDraftPreseason:
		return "POST-DRAFT // PRESEASON", "TURN PICKS INTO A SEASON.", "Set the lineup, make open Pick'em calls, and get ready for kickoff."
	case ActionCenterRegularSeason:
		return "REGULAR SEASON // TODAY", "KEEP THE SEASON MOVING.", "Deadlines and manager decisions stay visible in the order they matter."
	case ActionCenterPlayoffs:
		return "PLAYOFFS // POSTSEASON MODE", "POSTSEASON MODE IS LIVE.", "Follow the commissioner's published postseason plan, set the right lineup, and finish open picks."
	case ActionCenterSeasonComplete:
		return "SEASON COMPLETE // RECORD", "THE SEASON IS IN THE BOOKS.", "Review the final record, standings, and league wire."
	default:
		return "LEAGUE HQ", "YOUR NEXT MOVE.", "Open a surface below to continue."
	}
}

func entryAction(f ActionCenterFacts) *ActionCenterAction {
	if f.HasSeat || f.EntryState == PublicEntryPrimary || f.EntryState == PublicEntryCoManager {
		return nil
	}
	href, label, detail := strings.TrimSpace(f.EntryActionHref), strings.TrimSpace(f.EntryActionLabel), strings.TrimSpace(f.EntryDetail)
	if href == "" {
		href = "/guide#identity"
	}
	if label == "" {
		label = "Complete league entry"
	}
	if detail == "" {
		detail = "Your account needs one deliberate entry step before league controls unlock."
	}
	return &ActionCenterAction{
		ID: "entry", Priority: ActionCenterPriorityEntry, PriorityLabel: "NEXT STEP",
		Label: label, Detail: detail, Href: href, Primary: true,
		NativeNavigation: strings.HasPrefix(href, "/auth/"),
	}
}

func draftAction(f ActionCenterFacts) ActionCenterAction {
	if f.DraftStarted && !f.DraftComplete && f.ViewerOnClock {
		detail := "Open the draft room to make your pick."
		if f.OnClockTeamName != "" {
			detail = fmt.Sprintf("%s is on the clock. Open the draft room to make your pick.", f.OnClockTeamName)
		}
		if f.ClockPaused {
			detail = "The commissioner paused the clock. Open the room to follow the state."
		}
		return ActionCenterAction{ID: "draft-on-clock", Priority: ActionCenterPriorityOnClock, PriorityLabel: "ON THE CLOCK", Label: "Open draft room", Detail: detail, Href: "/draft", DueAt: f.ClockDeadline, HasDueAt: !f.ClockDeadline.IsZero(), DueLabel: "CLOCK DEADLINE", Urgent: !f.ClockPaused, Primary: true}
	}
	if f.DraftStarted && !f.DraftComplete {
		return ActionCenterAction{ID: "draft-live", Priority: ActionCenterPriorityInfo, PriorityLabel: "LIVE", Label: "Follow draft room", Detail: "The commissioner opened the draft. Follow the room for your turn.", Href: "/draft"}
	}
	return ActionCenterAction{}
}

func lineupAction(f ActionCenterFacts) *ActionCenterAction {
	if !f.DraftComplete || resolveActionCenterStage(f) == ActionCenterSeasonComplete {
		return nil
	}
	week := f.Lineup.Week
	if week <= 0 {
		week = 1
	}
	href := fmt.Sprintf("/team?week=%d", week)
	if f.Lineup.Problems > 0 {
		detail := fmt.Sprintf("%d lineup slot", f.Lineup.Problems) + pluralSuffix(f.Lineup.Problems) + " need attention before kickoff."
		return &ActionCenterAction{ID: "lineup", Priority: ActionCenterPriorityDeadline, PriorityLabel: "BEFORE KICKOFF", Label: "Fix your lineup", Detail: detail, Href: href, DueAt: f.Lineup.FirstKickoff, HasDueAt: f.Lineup.HasFirstKickoff, DueLabel: "FIRST KICKOFF", Urgent: true, Primary: true}
	}
	return &ActionCenterAction{ID: "lineup-review", Priority: ActionCenterPriorityStable, PriorityLabel: "STABLE TASK", Label: "Review your lineup", Detail: fmt.Sprintf("Week %d starters and bench are ready to review.", week), Href: href}
}

func pickemActions(f ActionCenterFacts) []ActionCenterAction {
	if f.Pickem.GameCount == 0 || resolveActionCenterStage(f) == ActionCenterSeasonComplete {
		return nil
	}
	href := "/pickem"
	if f.Pickem.Week > 0 {
		href = fmt.Sprintf("/pickem?week=%d", f.Pickem.Week)
	}
	var out []ActionCenterAction
	if f.Pickem.OpenUnpicked > 0 {
		detail := fmt.Sprintf("%d open game", f.Pickem.OpenUnpicked) + pluralSuffix(f.Pickem.OpenUnpicked) + " still need a pick."
		out = append(out, ActionCenterAction{ID: "pickem-open", Priority: ActionCenterPriorityDeadline, PriorityLabel: "BEFORE LOCK", Label: "Make open Pick'em picks", Detail: detail, Href: href, DueAt: f.Pickem.NextOpenLock, HasDueAt: f.Pickem.HasNextOpenLock, DueLabel: "NEXT GAME LOCK", Urgent: true})
	}
	if f.Pickem.LockedUnpicked > 0 {
		out = append(out, ActionCenterAction{ID: "pickem-missed", Priority: ActionCenterPriorityDeadline, PriorityLabel: "LOCKED", Label: "Review Pick'em results", Detail: fmt.Sprintf("%d game", f.Pickem.LockedUnpicked) + pluralSuffix(f.Pickem.LockedUnpicked) + " locked without a pick; review the slate.", Href: href, Urgent: true})
	}
	if len(out) == 0 {
		out = append(out, ActionCenterAction{ID: "pickem-review", Priority: ActionCenterPriorityStable, PriorityLabel: "STABLE TASK", Label: "Review Pick'em HQ", Detail: fmt.Sprintf("Week %d has %d game", f.Pickem.Week, f.Pickem.GameCount) + pluralSuffix(f.Pickem.GameCount) + " on the slate.", Href: href})
	}
	return out
}

func tradeActions(f ActionCenterFacts) []ActionCenterAction {
	var out []ActionCenterAction
	if f.Trades.AcceptedReview > 0 {
		out = append(out, ActionCenterAction{ID: "trade-review", Priority: ActionCenterPriorityDeadline, PriorityLabel: "TRADE REVIEW", Label: "Review accepted trade", Detail: fmt.Sprintf("%d accepted trade", f.Trades.AcceptedReview) + pluralSuffix(f.Trades.AcceptedReview) + " still in the review window.", Href: "/trades", DueAt: f.Trades.NextReviewDeadline, HasDueAt: f.Trades.HasReviewDeadline, DueLabel: "REVIEW DEADLINE", Urgent: true})
	}
	if f.Trades.IncomingOpen > 0 {
		out = append(out, ActionCenterAction{ID: "trade-inbox", Priority: ActionCenterPriorityStable, PriorityLabel: "STABLE TASK", Label: "Review incoming trade", Detail: fmt.Sprintf("%d trade offer", f.Trades.IncomingOpen) + pluralSuffix(f.Trades.IncomingOpen) + " waiting in your inbox.", Href: "/trades", Primary: true})
	}
	if f.DraftComplete && f.Trades.HasTradeDeadline && f.Now.Before(f.Trades.TradeDeadline) {
		out = append(out, ActionCenterAction{ID: "trade-deadline", Priority: ActionCenterPriorityDeadline, PriorityLabel: "DEADLINE", Label: "Review trade desk", Detail: "Trades remain available until the configured league deadline.", Href: "/trades", DueAt: f.Trades.TradeDeadline, HasDueAt: true, DueLabel: "TRADE DEADLINE"})
	}
	if f.Trades.OutgoingOpen > 0 {
		out = append(out, ActionCenterAction{ID: "trade-outbox", Priority: ActionCenterPriorityInfo, PriorityLabel: "IN PROGRESS", Label: "Check trade outbox", Detail: fmt.Sprintf("%d outgoing trade", f.Trades.OutgoingOpen) + pluralSuffix(f.Trades.OutgoingOpen) + " in progress.", Href: "/trades"})
	}
	return out
}

func waiverAction(f ActionCenterFacts) *ActionCenterAction {
	if f.Waivers.OpenClaims <= 0 {
		return nil
	}
	detail := fmt.Sprintf("%d open waiver claim", f.Waivers.OpenClaims) + pluralSuffix(f.Waivers.OpenClaims) + " are filed for your team."
	priority, priorityLabel := ActionCenterPriorityStable, "STABLE TASK"
	urgent := false
	location := f.Location
	if location == nil {
		location = time.UTC
	}
	if f.Waivers.HasNextRun {
		priority, priorityLabel = ActionCenterPriorityDeadline, "WAIVER PROCESSING"
		urgent = !f.Now.Before(f.Waivers.NextRun)
		if urgent {
			detail += " Waiver processing is due now; review the claim resolution."
		} else {
			detail += fmt.Sprintf(" Processing is scheduled for %s; review the claim resolution.", f.Waivers.NextRun.In(location).Format("Mon Jan 2 · 3:04 PM MST"))
		}
	}
	return &ActionCenterAction{ID: "waiver-claims", Priority: priority, PriorityLabel: priorityLabel, Label: "Review waiver claims", Detail: detail, Href: "/players", DueAt: f.Waivers.NextRun, HasDueAt: f.Waivers.HasNextRun, DueLabel: "WAIVER PROCESSING", Urgent: urgent}
}

func preparationActions(f ActionCenterFacts) []ActionCenterAction {
	if f.DraftComplete || f.DraftStarted {
		return nil
	}
	var out []ActionCenterAction
	if f.BoardCount == 0 {
		out = append(out, ActionCenterAction{ID: "draft-board", Priority: ActionCenterPriorityPreparation, PriorityLabel: "PREPARATION", Label: "Build your Draft Board", Detail: "Save a shared player order before draft night. " + draftMeetingDetail(f), Href: "/board", DueAt: f.DraftAt, HasDueAt: !f.DraftAt.IsZero(), DueLabel: "DRAFT MEETING"})
	}
	if !f.Ready {
		out = append(out, ActionCenterAction{ID: "draft-ready", Priority: ActionCenterPriorityPreparation, PriorityLabel: "PREPARATION", Label: "Check in for the draft", Detail: "Mark this seat ready from the draft room when your board is set. " + draftMeetingDetail(f), Href: "/draft", DueAt: f.DraftAt, HasDueAt: !f.DraftAt.IsZero(), DueLabel: "DRAFT MEETING"})
	}
	return out
}

func draftMeetingDetail(f ActionCenterFacts) string {
	location := f.Location
	if location == nil {
		location = time.UTC
	}
	if f.DraftAt.IsZero() {
		return "The scheduled time is the meeting point; the commissioner starts the room intentionally."
	}
	return fmt.Sprintf("Draft meeting: %s. The scheduled time is the meeting point; the commissioner starts the room intentionally.", f.DraftAt.In(location).Format("Monday, January 2 · 3:04 PM MST"))
}

func informationalAction(f ActionCenterFacts) ActionCenterAction {
	switch resolveActionCenterStage(f) {
	case ActionCenterPreDraft:
		return ActionCenterAction{ID: "draft-info", Priority: ActionCenterPriorityInfo, PriorityLabel: "INFORMATION", Label: "Open draft room", Detail: "Review the schedule, order, and room protocol. " + draftMeetingDetail(f), Href: "/draft", DueAt: f.DraftAt, HasDueAt: !f.DraftAt.IsZero(), DueLabel: "DRAFT MEETING"}
	case ActionCenterPostDraftPreseason:
		return ActionCenterAction{ID: "matchups-info", Priority: ActionCenterPriorityInfo, PriorityLabel: "INFORMATION", Label: "Open matchup center", Detail: "The regular season begins when the published schedule opens.", Href: "/matchups"}
	case ActionCenterSeasonComplete:
		return ActionCenterAction{ID: "record-info", Priority: ActionCenterPriorityInfo, PriorityLabel: "INFORMATION", Label: "Review final record", Detail: "Open the matchup center for the completed season record.", Href: "/matchups"}
	default:
		return ActionCenterAction{ID: "matchups-info", Priority: ActionCenterPriorityInfo, PriorityLabel: "INFORMATION", Label: "Open matchup center", Detail: "Follow the league's current matchups and standings.", Href: "/matchups"}
	}
}

func commissionerActions(f ActionCenterFacts) []ActionCenterAction {
	if f.DraftComplete {
		if action := weekCloseAction(f); action != nil {
			return []ActionCenterAction{*action}
		}
		return nil
	}
	if f.DraftStarted {
		return []ActionCenterAction{{ID: "commissioner-clock", Priority: ActionCenterPriorityInfo, PriorityLabel: "COMMISSIONER", Label: "Operate live draft clock", Detail: "Open the existing commissioner clock controls.", Href: "/admin?section=clock#admin-clock"}}
	}
	seats := f.ClaimedSeats
	capacity := f.SeatCapacity
	if capacity <= 0 {
		capacity = seats
	}
	poolDetail := fmt.Sprintf("%d players in pool", f.DraftPoolCount)
	if f.DraftPoolTarget > 0 {
		poolDetail = fmt.Sprintf("%d/%d players in pool", f.DraftPoolCount, f.DraftPoolTarget)
	}
	order := "draft order is not set"
	if f.DraftOrderSet {
		order = "draft order is set"
	}
	readyDenom := seats
	if readyDenom <= 0 {
		readyDenom = capacity
	}
	detail := fmt.Sprintf("%d/%d seats claimed · %d/%d managers ready · %s · %s.", seats, capacity, f.ReadySeats, readyDenom, order, poolDetail)
	return []ActionCenterAction{{ID: "commissioner-start", Priority: ActionCenterPriorityInfo, PriorityLabel: "COMMISSIONER", Label: "Start and monitor draft", Detail: detail + " Open the existing draft controls when the room is ready.", Href: "/admin?section=draft-control#admin-draft-control"}}
}

func weekCloseAction(f ActionCenterFacts) *ActionCenterAction {
	if !f.ScheduleExists || f.WeekCloseWeek <= 0 || f.WeekCloseFinal || !f.WeekCloseReady {
		return nil
	}
	detail := fmt.Sprintf("Week %d is ready: %d/%d games are final and player stats are fresh. Use the normal close; forced close remains a separate override for a data stall.", f.WeekCloseWeek, f.WeekCloseGamesFinal, f.WeekCloseGamesTotal)
	return &ActionCenterAction{
		ID: "commissioner-week-close", Priority: ActionCenterPriorityDeadline, PriorityLabel: "READY TO CLOSE",
		Label: "Close scoring week", Detail: detail, Href: "/admin?section=week-close#admin-week-close",
		Urgent: true, Primary: true,
	}
}

func sortActionCenterActions(actions []ActionCenterAction) {
	sort.SliceStable(actions, func(i, j int) bool {
		ri, rj := actionCenterPriorityRank(actions[i].Priority), actionCenterPriorityRank(actions[j].Priority)
		if ri != rj {
			return ri < rj
		}
		if actions[i].HasDueAt != actions[j].HasDueAt {
			return actions[i].HasDueAt
		}
		if actions[i].HasDueAt && !actions[i].DueAt.Equal(actions[j].DueAt) {
			return actions[i].DueAt.Before(actions[j].DueAt)
		}
		return actions[i].ID < actions[j].ID
	})
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Data renders a stable map boundary; every key exists on every stage.
func (c ActionCenter) Data(location *time.Location) map[string]any {
	if location == nil {
		location = time.UTC
	}
	return map[string]any{
		"stage": string(c.Stage), "stage_label": c.StageLabel, "heading": c.Heading, "summary": c.Summary,
		"has_actions": c.HasActions, "action_count": c.ActionCount,
		"actions":                  actionCenterActionMaps(c.Actions, location),
		"has_commissioner_actions": c.HasCommissioner,
		"commissioner_actions":     actionCenterActionMaps(c.CommissionerActions, location),
	}
}

func actionCenterActionMaps(actions []ActionCenterAction, location *time.Location) []map[string]any {
	out := make([]map[string]any, 0, len(actions))
	for _, a := range actions {
		priority, label, due := string(a.Priority), a.PriorityLabel, ""
		if label == "" {
			label = strings.ToUpper(strings.ReplaceAll(priority, "_", " "))
		}
		dueLabel := a.DueLabel
		if a.HasDueAt {
			due = a.DueAt.Format(time.RFC3339)
			if dueLabel == "" {
				dueLabel = "DUE"
			}
			dueLabel += " · " + a.DueAt.In(location).Format("Mon Jan 2 · 3:04 PM MST")
		}
		out = append(out, map[string]any{
			"id": a.ID, "priority": priority, "priority_label": label, "label": a.Label,
			"detail": a.Detail, "href": a.Href, "due_at": due, "has_due_at": a.HasDueAt,
			"due_label": dueLabel, "urgent": a.Urgent, "primary": a.Primary,
			"native_navigation": a.NativeNavigation,
		})
	}
	return out
}

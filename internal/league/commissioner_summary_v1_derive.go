package league

import (
	"fmt"
	"sort"
	"strings"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

func commissionerV1Draft(t commissionerSummaryV1Tuple, eligible map[string]bool, ready, boardGaps int) *hqv1.Draft {
	cfg, state := t.config.Config, t.state
	expected := len(eligible)
	rounds := cfg.Rounds
	if state.RosterOverride != nil {
		rounds = rosterOverridePreset(*state.RosterOverride).Total()
	}
	pickCount := len(state.Picks)
	capacity := expected * rounds
	draftAt := cfg.DraftAt
	if !state.DraftAtOverride.IsZero() {
		draftAt = state.DraftAtOverride
	}
	stateName := "unscheduled"
	switch {
	case expected > 0 && rounds > 0 && pickCount == capacity:
		stateName = "complete"
	case pickCount > 0:
		stateName = "in_progress"
	case draftAt.IsZero():
		stateName = "unscheduled"
	case state.DraftStarted:
		stateName = "open"
	default:
		stateName = "scheduled"
	}
	orderStatus := "incomplete"
	orderReady := commissionerV1OrderReady(state.DraftOrder, eligible)
	if orderReady {
		orderStatus = "ready"
	}
	var scheduledAt *string
	if stateName == "scheduled" || stateName == "open" || stateName == "in_progress" {
		scheduledAt = v1String(commissionerV1Time(draftAt))
	}
	var onClock *string
	if (stateName == "open" || stateName == "in_progress") && orderReady && len(state.DraftOrder) > 0 {
		teamID := teamOnClock(state.DraftOrder, pickCount+1)
		onClock = v1String(teamID)
	}
	draft := &hqv1.Draft{
		State: stateName, ScheduledAt: scheduledAt, OrderStatus: v1String(orderStatus),
		PickCount: v1Int(pickCount), ReadyTeams: v1Int(ready), ExpectedTeams: v1Int(expected),
		OnClockTeamID: onClock, BoardGapCount: v1Int(boardGaps),
	}
	if expected > 0 && rounds > 0 {
		draft.DraftRounds = v1Int(rounds)
		draft.PickCapacity = v1Int(capacity)
	}
	return draft
}

func commissionerV1OrderReady(order []string, expected map[string]bool) bool {
	if len(order) != len(expected) || len(expected) == 0 {
		return false
	}
	seen := make(map[string]bool, len(order))
	for _, teamID := range order {
		if !expected[teamID] || seen[teamID] {
			return false
		}
		seen[teamID] = true
	}
	return len(seen) == len(expected)
}

func commissionerV1Phase(raw, draftState string, scheduleExists bool, seasonStart, now time.Time) string {
	switch raw {
	case PhaseRegularSeason:
		return "regular-season"
	case PhasePlayoffs:
		return "post-season"
	case PhaseSeasonComplete:
		return "complete"
	case "":
		if scheduleExists && !seasonStart.IsZero() && !now.Before(seasonStart) {
			return "regular-season"
		}
		switch draftState {
		case "unscheduled", "scheduled":
			return "pre-draft"
		case "open", "in_progress":
			return "draft"
		case "complete":
			return "preseason"
		default:
			return "unknown"
		}
	default:
		return "unknown"
	}
}

func commissionerV1Readiness(vacant, notReady, boardGaps int, draft *hqv1.Draft, data CommissionerSummaryV1DataSnapshot) *hqv1.Readiness {
	items := make([]hqv1.ReadinessItem, 0, 6)
	add := func(code, severity string, count int, label string) {
		if count > 0 {
			items = append(items, hqv1.ReadinessItem{Code: code, Severity: severity, Count: count, Label: label})
		}
	}
	add("vacant_team", "warning", vacant, fmt.Sprintf("%d team seat%s remain open", vacant, commissionerV1Plural(vacant)))
	add("team_not_ready", "warning", notReady, fmt.Sprintf("%d claimed team%s have not marked ready", notReady, commissionerV1Plural(notReady)))
	add("board_gap", "warning", boardGaps, fmt.Sprintf("%d claimed team%s have an empty Big Board", boardGaps, commissionerV1Plural(boardGaps)))
	if draft.State == "unscheduled" {
		add("draft_unscheduled", "warning", 1, "The draft meeting has not been scheduled")
	}
	if draft.OrderStatus != nil && *draft.OrderStatus == "incomplete" {
		add("draft_order_incomplete", "warning", 1, "The active draft order is incomplete")
	}
	if data.Quality == "degraded" || data.SourceState == "stale" || data.SourceState == "degraded" || data.SourceState == "unreachable" {
		add("data_stale", "warning", 1, "League source data is degraded or stale")
	}
	sort.SliceStable(items, func(i, j int) bool {
		if commissionerV1SortSeverity(items[i].Severity) != commissionerV1SortSeverity(items[j].Severity) {
			return commissionerV1SortSeverity(items[i].Severity) < commissionerV1SortSeverity(items[j].Severity)
		}
		return items[i].Code < items[j].Code
	})
	severity := "info"
	if len(items) > 0 {
		severity = items[0].Severity
	}
	return &hqv1.Readiness{Severity: severity, Items: items}
}

func commissionerV1Lineup(data CommissionerSummaryV1DataSnapshot) hqv1.Lineup {
	lineup := hqv1.Lineup{IssueCount: copyInt(data.LineupIssueCount)}
	if !data.NextLineupLockAt.IsZero() {
		lineup.NextLockAt = v1String(commissionerV1Time(data.NextLineupLockAt))
	}
	return lineup
}

func commissionerV1Waivers(cfg Config, state PersistedState, now time.Time) hqv1.Waivers {
	mode := strings.ToLower(strings.TrimSpace(cfg.Waivers.Mode))
	waivers := hqv1.Waivers{OpenClaims: v1Int(len(state.WaiverClaims))}
	if mode != "" {
		waivers.Mode = v1String(mode)
	}
	if !state.WaiversProcessedThrough.IsZero() {
		next := nextWaiverProcessingRun(cfg, state.WaiversProcessedThrough, now)
		if !next.IsZero() {
			waivers.NextRunAt = v1String(commissionerV1Time(next))
		}
	}
	return waivers
}

func commissionerV1Trades(state PersistedState) hqv1.Trades {
	pending, decisions := 0, 0
	for _, offer := range state.TradeOffers {
		switch offer.Status {
		case TradeStatusOpen:
			pending++
		case TradeStatusAccepted:
			pending++
			decisions++
		}
	}
	return hqv1.Trades{PendingCount: v1Int(pending), CommissionerDecisions: v1Int(decisions)}
}

func commissionerV1Pickem(state PersistedState, games []GameInfo, claimed map[string]bool, now time.Time) hqv1.Pickem {
	weeks := pickemWeeks(games)
	if len(weeks) == 0 {
		return hqv1.Pickem{}
	}
	week := pickemWeekAt(games, now)
	primaryByTeam := make(map[string]string, len(claimed))
	for key, member := range state.Members {
		if claimed[member.TeamID] && member.Role != "co" {
			primaryByTeam[member.TeamID] = key
		}
	}
	unpicked := 0
	var next time.Time
	for _, game := range gamesInWeek(games, week) {
		if game.Kickoff.IsZero() || !now.Before(game.Kickoff) {
			continue
		}
		if market, ok := state.PickemMarkets[game.ID]; ok && pickemMarketUnavailable(market) {
			continue
		}
		for teamID := range claimed {
			owner := primaryByTeam[teamID]
			if owner == "" || !validPick(game, state.Pickems[owner][game.ID]) {
				unpicked++
				if next.IsZero() || game.Kickoff.Before(next) {
					next = game.Kickoff
				}
			}
		}
	}
	pickem := hqv1.Pickem{Week: v1Int(week), Unpicked: v1Int(unpicked)}
	if !next.IsZero() {
		pickem.NextDeadlineAt = v1String(commissionerV1Time(next))
	}
	return pickem
}

func commissionerV1Configuration(cfg Config, teamCount int, blitzEnabled bool, now time.Time) hqv1.Configuration {
	format := commissionerV1Format(cfg.ModeLabel)
	rosterModel := "configured"
	scoringModel := commissionerV1Token(cfg.ScoringFormat, "configured")
	waiverMode := commissionerV1Token(cfg.Waivers.Mode, "configured")
	tradePolicy := "open"
	if deadline, ok := parseTradeDeadline(cfg); ok && !now.Before(deadline) {
		tradePolicy = "closed"
	}
	pickemEnabled := true
	return hqv1.Configuration{
		Format: &format, TeamCount: v1Int(teamCount), RosterModel: &rosterModel,
		ScoringModel: &scoringModel, WaiverMode: &waiverMode, TradePolicy: &tradePolicy,
		PickemEnabled: &pickemEnabled, BlitzEnabled: &blitzEnabled, Timezone: v1String(cfg.Timezone),
	}
}

func commissionerV1Token(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '+':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" || out[0] < 'a' || out[0] > 'z' || len(out) > 64 {
		return fallback
	}
	return out
}

func commissionerV1Links(configuration hqv1.Configuration) hqv1.Links {
	league, overview, join := "/", "/", "/join"
	draft, board, team := "/draft", "/board", "/team"
	players, trades, activity, commissioner := "/players", "/trades", "/activity", "/admin"
	links := hqv1.Links{
		League: &league, Overview: &overview, Join: &join, Draft: &draft, Board: &board,
		Team: &team, Players: &players, Trades: &trades, Activity: &activity, Commissioner: &commissioner,
	}
	if configuration.PickemEnabled != nil && *configuration.PickemEnabled {
		pickem := "/pickem"
		links.Pickem = &pickem
	}
	if configuration.BlitzEnabled != nil && *configuration.BlitzEnabled {
		blitz := "/blitz"
		links.Blitz = &blitz
	}
	return links
}

func commissionerV1Warnings(data CommissionerSummaryV1DataSnapshot) []hqv1.Warning {
	warnings := make([]hqv1.Warning, 0, 2)
	if data.Quality == "not_reported" || data.SourceState == "unreachable" {
		warnings = append(warnings, hqv1.Warning{Code: "source_not_reported", Severity: "warning", Summary: "League source health is not currently reportable."})
	} else if data.Quality == "degraded" || data.SourceState == "stale" || data.SourceState == "degraded" {
		warnings = append(warnings, hqv1.Warning{Code: "source_degraded", Severity: "warning", Summary: "League source data is retained but degraded or stale."})
	}
	sort.SliceStable(warnings, func(i, j int) bool {
		if commissionerV1SortSeverity(warnings[i].Severity) != commissionerV1SortSeverity(warnings[j].Severity) {
			return commissionerV1SortSeverity(warnings[i].Severity) < commissionerV1SortSeverity(warnings[j].Severity)
		}
		return warnings[i].Code < warnings[j].Code
	})
	return warnings
}

func commissionerV1Attention(leagueID string, vacant, notReady, boardGaps int, draft *hqv1.Draft, lineup hqv1.Lineup, trades hqv1.Trades, pickem hqv1.Pickem, data CommissionerSummaryV1DataSnapshot) []hqv1.AttentionItem {
	items := make([]hqv1.AttentionItem, 0, 9)
	add := func(code, category, severity, title, summary string, count int, due *string, href string) {
		if count <= 0 {
			return
		}
		items = append(items, hqv1.AttentionItem{
			Code: code, Category: category, Severity: severity, Title: title, Summary: summary,
			LeagueID: leagueID, DueAt: due, State: "open", Source: "league", Href: v1String(href),
		})
	}
	add("vacant_team", "membership", "warning", "Team seats remain open", fmt.Sprintf("%d team seat%s still need a primary manager.", vacant, commissionerV1Plural(vacant)), vacant, draft.ScheduledAt, "/join")
	add("team_not_ready", "draft_readiness", "warning", "Claimed teams are not ready", fmt.Sprintf("%d claimed team%s have not marked ready.", notReady, commissionerV1Plural(notReady)), notReady, draft.ScheduledAt, "/draft")
	add("board_gap", "board", "warning", "Big Boards need attention", fmt.Sprintf("%d claimed team%s have no ranked player targets.", boardGaps, commissionerV1Plural(boardGaps)), boardGaps, draft.ScheduledAt, "/board")
	if draft.OrderStatus != nil && *draft.OrderStatus == "incomplete" {
		add("draft_order_incomplete", "draft", "warning", "Draft order is incomplete", "Publish one valid position for every draft-eligible team.", 1, draft.ScheduledAt, "/draft")
	}
	if lineup.IssueCount != nil {
		add("lineup_issue", "lineup", "warning", "Lineups need attention", fmt.Sprintf("%d lineup issue%s remain before the next lock.", *lineup.IssueCount, commissionerV1Plural(*lineup.IssueCount)), *lineup.IssueCount, lineup.NextLockAt, "/team")
	}
	if trades.CommissionerDecisions != nil {
		add("trade_decision", "trades", "warning", "Trades await commissioner review", fmt.Sprintf("%d accepted trade%s await a decision.", *trades.CommissionerDecisions, commissionerV1Plural(*trades.CommissionerDecisions)), *trades.CommissionerDecisions, nil, "/trades")
	}
	if pickem.Unpicked != nil {
		add("pickem_gap", "pickem", "warning", "Pick'em selections remain", fmt.Sprintf("%d open selection%s remain before kickoff.", *pickem.Unpicked, commissionerV1Plural(*pickem.Unpicked)), *pickem.Unpicked, pickem.NextDeadlineAt, "/pickem")
	}
	if data.Quality == "not_reported" || data.SourceState == "unreachable" {
		add("source_not_reported", "commissioner", "warning", "Source health is not reported", "The league snapshot is available, but its source-health generation is unavailable.", 1, nil, "/admin")
	} else if data.Quality == "degraded" || data.SourceState == "stale" || data.SourceState == "degraded" {
		add("source_degraded", "commissioner", "warning", "Source data is degraded", "Review the league source state before relying on freshness-sensitive decisions.", 1, nil, "/admin")
	}
	commissionerV1SortAttention(items)
	return items
}

func commissionerV1Calendar(cfg Config, state PersistedState, draft *hqv1.Draft, lineup hqv1.Lineup, waivers hqv1.Waivers, pickem hqv1.Pickem, now time.Time) hqv1.Calendar {
	items := make([]hqv1.Deadline, 0, 8)
	add := func(code, category, title string, at *string, state, href string) {
		if at == nil {
			return
		}
		items = append(items, hqv1.Deadline{
			Code: code, Category: category, Title: title, At: at, Timezone: cfg.Timezone,
			RelativeText: commissionerV1Relative(now, *at), State: state, Href: v1String(href),
		})
	}
	if (draft.State == "scheduled" || draft.State == "open") && draft.ScheduledAt != nil && commissionerV1TimeAfter(*draft.ScheduledAt, now) {
		add("draft", "draft", "Draft meeting", draft.ScheduledAt, draft.State, "/draft")
	}
	if (draft.State == "open" || draft.State == "in_progress") && !state.ClockPaused && !state.ClockDeadline.IsZero() {
		add("draft_clock", "draft", "Current draft pick clock", v1String(commissionerV1Time(state.ClockDeadline)), "scheduled", "/draft")
	}
	add("lineup_lock", "lineup", "Next lineup lock", lineup.NextLockAt, "scheduled", "/team")
	add("waiver_run", "waivers", "Next waiver run", waivers.NextRunAt, "scheduled", "/players")
	add("pickem_lock", "pickem", "Next Pick'em lock", pickem.NextDeadlineAt, "scheduled", "/pickem")
	if deadline, ok := parseTradeDeadline(cfg); ok && now.Before(deadline) {
		add("trade_deadline", "trades", "Trade deadline", v1String(commissionerV1Time(deadline)), "scheduled", "/trades")
	}
	if !cfg.SeasonStartAt.IsZero() && now.Before(cfg.SeasonStartAt) {
		add("season_start", "league", "Season kickoff", v1String(commissionerV1Time(cfg.SeasonStartAt)), "scheduled", "/")
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].At == nil != (items[j].At == nil) {
			return items[i].At != nil
		}
		if items[i].At != nil && *items[i].At != *items[j].At {
			return commissionerV1TimeBefore(*items[i].At, *items[j].At)
		}
		return items[i].Code < items[j].Code
	})
	calendar := hqv1.Calendar{Deadlines: []hqv1.Deadline{}}
	if len(items) > 0 {
		first := items[0]
		calendar.NextDeadline = &first
		calendar.Deadlines = append(calendar.Deadlines, items[1:]...)
	}
	return calendar
}

func commissionerV1Relative(now time.Time, raw string) string {
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "time unavailable"
	}
	delta := at.Sub(now)
	past := delta < 0
	if past {
		delta = -delta
	}
	var value string
	switch {
	case delta < time.Minute:
		value = "less than a minute"
	case delta < time.Hour:
		minutes := int(delta.Round(time.Minute) / time.Minute)
		value = fmt.Sprintf("%d minute%s", minutes, commissionerV1Plural(minutes))
	case delta < 48*time.Hour:
		hours := int(delta.Round(time.Hour) / time.Hour)
		value = fmt.Sprintf("%d hour%s", hours, commissionerV1Plural(hours))
	default:
		days := int(delta.Round(24*time.Hour) / (24 * time.Hour))
		value = fmt.Sprintf("%d day%s", days, commissionerV1Plural(days))
	}
	if past {
		return value + " ago"
	}
	return "in " + value
}

func commissionerV1Activity(state PersistedState, teams map[string]Team, asOf time.Time) hqv1.RecentActivity {
	items := make([]hqv1.ActivityItem, 0, len(state.Picks)+len(state.Transactions))
	seen := make(map[string]bool, len(state.Picks)+len(state.Transactions))
	for _, pick := range state.Picks {
		if pick.MadeAt.IsZero() || pick.MadeAt.After(asOf) || pick.Number <= 0 {
			continue
		}
		teamName := commissionerV1TeamName(teams[pick.TeamID], pick.TeamID)
		player := commissionerV1SafeEntity(pick.PlayerID, "a player")
		id := fmt.Sprintf("draft-%d", pick.Number)
		if seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, hqv1.ActivityItem{
			ID: id, OccurredAt: commissionerV1Time(pick.MadeAt),
			Category: "draft", Summary: commissionerV1Text(fmt.Sprintf("%s selected %s.", teamName, player), "A team completed a draft selection."),
			Href: v1String("/draft"),
		})
	}
	for _, txn := range state.Transactions {
		if txn.ID == "" || txn.At.IsZero() || txn.At.After(asOf) {
			continue
		}
		teamName := commissionerV1TeamName(teams[txn.TeamID], txn.TeamID)
		action, players := activityLine(txn)
		summary := teamName + " completed a roster transaction."
		if safeAction := commissionerV1SafeEntity(action, ""); safeAction != "" {
			summary = teamName + " " + safeAction + "."
			if safePlayers := commissionerV1SafeEntity(players, ""); safePlayers != "" {
				summary = teamName + " " + safeAction + " " + safePlayers + "."
			}
		}
		id := commissionerV1SafeEntity(txn.ID, "")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, hqv1.ActivityItem{
			ID: id, OccurredAt: commissionerV1Time(txn.At),
			Category: "activity", Summary: commissionerV1Text(summary, "A team completed a roster transaction."),
			Href: v1String("/activity"),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OccurredAt != items[j].OccurredAt {
			return commissionerV1TimeBefore(items[j].OccurredAt, items[i].OccurredAt)
		}
		return items[i].ID < items[j].ID
	})
	truncated := len(items) > 20
	if truncated {
		items = items[:20]
	}
	return hqv1.RecentActivity{Items: items, Truncated: truncated, AsOf: commissionerV1Time(asOf)}
}

func commissionerV1TeamName(team Team, fallback string) string {
	if team.Name != "" {
		return commissionerV1Text(team.Name, "A team")
	}
	return commissionerV1SafeEntity(fallback, "A team")
}

func commissionerV1SafeEntity(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "@") || strings.Contains(strings.ToLower(value), "://") || strings.ContainsAny(value, "\r\n\t") {
		return fallback
	}
	return commissionerV1Text(value, fallback)
}

func commissionerV1DataHealth(data CommissionerSummaryV1DataSnapshot, now time.Time) hqv1.DataHealth {
	quality := commissionerV1Token(data.Quality, "not_reported")
	if quality != "healthy" && quality != "degraded" && quality != "not_reported" {
		quality = "not_reported"
	}
	if quality == "not_reported" || data.AsOf.IsZero() || data.AsOf.After(now) {
		return hqv1.DataHealth{Quality: "not_reported"}
	}
	health := hqv1.DataHealth{Quality: quality, PlayerCount: copyInt(data.PlayerCount)}
	mode := commissionerV1Token(data.SourceMode, "")
	if mode != "" {
		health.SourceMode = v1String(mode)
	}
	if state := commissionerV1Token(data.SourceState, ""); state != "" {
		if (mode == "cache" || mode == "cached") && state == "live" {
			state = "stale"
		}
		if mode == "offline" && state == "live" {
			state = "unreachable"
		}
		health.SourceState = v1String(state)
	}
	if value := commissionerV1DegradationLabel(data.DegradationCode); value != "" {
		health.LastDegradation = v1String(value)
	}
	if !data.LastSuccessAt.IsZero() && !data.LastSuccessAt.After(now) {
		health.LastSuccessAt = v1String(commissionerV1Time(data.LastSuccessAt))
	}
	health.AsOf = v1String(commissionerV1Time(data.AsOf))
	return health
}

// commissionerV1NormalizeData resolves cross-field source truth once before
// readiness, warnings, attention, and data-health are projected. Every surface
// therefore reports the same cached/offline generation semantics.
func commissionerV1NormalizeData(data CommissionerSummaryV1DataSnapshot) CommissionerSummaryV1DataSnapshot {
	mode := commissionerV1Token(data.SourceMode, "")
	state := commissionerV1Token(data.SourceState, "")
	switch state {
	case "cached":
		state = "stale"
	case "offline", "unavailable":
		state = "unreachable"
	}
	if (mode == "cache" || mode == "cached") && state == "live" {
		state = "stale"
	}
	if mode == "offline" && state == "live" {
		state = "unreachable"
	}
	data.SourceMode = mode
	data.SourceState = state
	return data
}

func commissionerV1DegradationLabel(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "awaiting_release":
		return "The upstream dataset is awaiting release."
	case "partial":
		return "The latest source generation is incomplete."
	case "rate_limited":
		return "The upstream source is temporarily rate limited."
	case "stale":
		return "The latest retained source generation is stale."
	case "unreachable":
		return "The upstream source is temporarily unreachable."
	default:
		return ""
	}
}

func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	return v1Int(*value)
}

func commissionerV1Plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func commissionerV1TimeBefore(left, right string) bool {
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr == nil && rightErr == nil {
		return leftTime.Before(rightTime)
	}
	return left < right
}

func commissionerV1TimeAfter(raw string, other time.Time) bool {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && parsed.After(other)
}

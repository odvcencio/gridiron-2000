package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxStringBytes    = 256
	maxLabelBytes     = 128
	maxReadinessItems = 32
	maxWarnings       = 32
	maxDeadlines      = 32
	maxAttentionItems = 64
	maxActivityItems  = 20
)

var (
	gitSHAExpression = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestExpression = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	tokenExpression  = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	severityRank     = map[string]int{"blocking": 0, "warning": 1, "info": 2}
	canonicalLinks   = map[string]string{
		"league": "/", "overview": "/", "join": "/join", "draft": "/draft",
		"board": "/board", "team": "/team", "players": "/players",
		"trades": "/trades", "pickem": "/pickem", "blitz": "/blitz",
		"activity": "/activity", "commissioner": "/admin",
	}
	categoryRoutes = map[string]string{
		"league": "/", "overview": "/", "membership": "/join", "join": "/join",
		"draft": "/draft", "draft_readiness": "/draft", "board": "/board",
		"lineup": "/team", "team": "/team", "waivers": "/players",
		"players": "/players", "trades": "/trades", "pickem": "/pickem",
		"blitz": "/blitz", "activity": "/activity", "commissioner": "/admin",
	}
	allowedQueries = map[string]map[string]bool{
		"/board":    {"position": true},
		"/players":  {"page": true, "position": true, "team": true},
		"/trades":   {"counterparty": true},
		"/pickem":   {"week": true},
		"/blitz":    {"week": true},
		"/activity": {"page": true, "team": true},
	}
	opaqueQueryValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	positionValue    = regexp.MustCompile(`^(QB|RB|WR|TE|K|DST|DEF|FLEX)$`)
)

func (s Summary) Validate() error {
	v := validator{availableRoutes: availableRoutes(s.Links)}
	if s.Capabilities == nil {
		v.add("capabilities must be an array, not null")
	}
	if s.Warnings == nil {
		v.add("warnings must be an array, not null")
	}
	if s.Calendar.Deadlines == nil {
		v.add("calendar.deadlines must be an array, not null")
	}
	if s.AttentionItems == nil {
		v.add("attention_items must be an array, not null")
	}
	if s.RecentActivity.Items == nil {
		v.add("recent_activity.items must be an array, not null")
	}
	v.equal("contract", s.Contract, ContractName)
	v.equal("schema_version", s.SchemaVersion, SchemaVersion)
	v.text("instance.id", s.Instance.ID, maxStringBytes)
	v.text("instance.league_id", s.Instance.LeagueID, maxStringBytes)
	v.text("league.name", s.League.Name, maxStringBytes)
	v.text("league.short_name", s.League.ShortName, maxStringBytes)
	v.token("league.format", s.League.Format)
	if s.League.Season < 1 {
		v.add("league.season must be positive")
	}
	v.timezone("league.timezone", s.League.Timezone)
	v.tokens("capabilities", s.Capabilities)
	v.token("competition.phase", s.Competition.Phase)
	v.teamCounts(s.Competition.Teams)
	v.draft(s)
	v.readiness(s)
	v.membership(s)
	v.count("lineup.issue_count", s.Lineup.IssueCount)
	v.utcOptional("lineup.next_lock_at", s.Lineup.NextLockAt)
	v.optionalToken("waivers.mode", s.Waivers.Mode)
	v.count("waivers.open_claims", s.Waivers.OpenClaims)
	v.utcOptional("waivers.next_run_at", s.Waivers.NextRunAt)
	v.count("trades.pending_count", s.Trades.PendingCount)
	v.count("trades.commissioner_decisions", s.Trades.CommissionerDecisions)
	v.count("pickem.week", s.Pickem.Week)
	v.count("pickem.unpicked", s.Pickem.Unpicked)
	v.utcOptional("pickem.next_deadline_at", s.Pickem.NextDeadlineAt)
	v.warnings(s.Warnings)
	v.calendar(s.Calendar)
	v.attention(s.AttentionItems, s.Instance.LeagueID)
	v.activity(s.RecentActivity)
	v.configuration(s.Configuration, s.Competition.Teams)
	v.configurationIdentity(s.Configuration, s.League)
	v.dataHealth(s.DataHealth)
	v.release(s.Release)
	v.links(s.Links, s.Capabilities, s.Configuration)
	v.utc("produced_at", s.ProducedAt)
	v.snapshotTimes(s)
	if len(v.errs) > 0 {
		return fmt.Errorf("invalid commissioner summary: %s", strings.Join(v.errs, "; "))
	}
	return nil
}

type validator struct {
	errs            []string
	availableRoutes map[string]bool
}

func (v *validator) add(message string) { v.errs = append(v.errs, message) }

func (v *validator) equal(path, got, want string) {
	if got != want {
		v.add(fmt.Sprintf("%s must equal %q", path, want))
	}
}

func (v *validator) text(path, value string, limit int) {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > limit {
		v.add(fmt.Sprintf("%s must be nonempty, trimmed valid UTF-8 of at most %d bytes", path, limit))
		return
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			v.add(path + " must not contain control characters")
			return
		}
	}
}

func (v *validator) optionalText(path string, value *string, limit int) {
	if value != nil {
		v.text(path, *value, limit)
	}
}

func (v *validator) privacyText(path, value string, limit int) {
	v.text(path, value, limit)
	lower := strings.ToLower(value)
	if strings.Contains(value, "@") || strings.Contains(lower, "://") {
		v.add(path + " must not contain member identity or a raw origin")
	}
}

func (v *validator) token(path, value string) {
	if !tokenExpression.MatchString(value) {
		v.add(path + " must be a normalized token")
	}
}

func (v *validator) optionalToken(path string, value *string) {
	if value != nil {
		v.token(path, *value)
	}
}

func (v *validator) tokens(path string, values []string) {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		v.token(fmt.Sprintf("%s[%d]", path, i), value)
		if _, ok := seen[value]; ok {
			v.add(fmt.Sprintf("%s contains duplicate %q", path, value))
		}
		seen[value] = struct{}{}
	}
}

func (v *validator) count(path string, value *int) {
	if value != nil && *value < 0 {
		v.add(path + " must be nonnegative or null")
	}
}

func (v *validator) utc(path, value string) {
	if _, ok := parseUTC(value); !ok {
		v.add(path + " must be an RFC 3339 UTC timestamp")
	}
}

func (v *validator) utcOptional(path string, value *string) {
	if value != nil {
		v.utc(path, *value)
	}
}

func parseUTC(value string) (time.Time, bool) {
	if !strings.HasSuffix(value, "Z") {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil && t.Location() == time.UTC
}

func (v *validator) timezone(path, value string) {
	if value == "" || value == "Local" || strings.TrimSpace(value) != value {
		v.add(path + " must be a valid IANA time-zone name")
		return
	}
	location, err := time.LoadLocation(value)
	if err != nil || location.String() != value {
		v.add(path + " must be a valid IANA time-zone name")
	}
}

func (v *validator) teamCounts(c TeamCounts) {
	v.count("competition.teams.occupied", c.Occupied)
	v.count("competition.teams.vacant", c.Vacant)
	v.count("competition.teams.total", c.Total)
	if allInts(c.Occupied, c.Vacant, c.Total) && *c.Occupied+*c.Vacant != *c.Total {
		v.add("competition occupied + vacant must equal total")
	}
	if allInts(c.Occupied, c.Total) && *c.Occupied > *c.Total {
		v.add("competition occupied teams must not exceed total teams")
	}
	if allInts(c.Vacant, c.Total) && *c.Vacant > *c.Total {
		v.add("competition vacant teams must not exceed total teams")
	}
}

func (v *validator) draft(s Summary) {
	hasDraft := hasCapability(s.Capabilities, "draft.v1")
	if s.Draft == nil {
		if hasDraft {
			v.add("draft is required when draft.v1 is advertised")
		}
		return
	}
	if !hasDraft {
		v.add("draft must be omitted or null without draft.v1")
	}
	d := *s.Draft
	v.token("draft.state", d.State)
	v.count("draft.pick_count", d.PickCount)
	v.count("draft.pick_capacity", d.PickCapacity)
	v.count("draft.ready_teams", d.ReadyTeams)
	v.count("draft.expected_teams", d.ExpectedTeams)
	v.count("draft.board_gap_count", d.BoardGapCount)
	if d.DraftRounds != nil && *d.DraftRounds <= 0 {
		v.add("draft.draft_rounds must be positive or null")
	}
	if d.OrderStatus != nil && *d.OrderStatus != "ready" && *d.OrderStatus != "incomplete" {
		v.add("draft.order_status must be ready, incomplete, or null")
	}
	if d.OrderStatus != nil && *d.OrderStatus == "ready" && d.ExpectedTeams == nil {
		v.add("draft.expected_teams is required when order_status is ready")
	}
	needsSchedule := d.State == "scheduled" || d.State == "open" || d.State == "in_progress"
	if needsSchedule != (d.ScheduledAt != nil) {
		v.add("draft.scheduled_at presence does not match draft state")
	}
	v.utcOptional("draft.scheduled_at", d.ScheduledAt)
	if d.PickCapacity != nil && *d.PickCapacity == 0 {
		v.add("draft.pick_capacity must be positive when reported")
	}
	if d.ExpectedTeams != nil && d.DraftRounds != nil {
		want := *d.ExpectedTeams * *d.DraftRounds
		if d.PickCapacity == nil || *d.PickCapacity != want {
			v.add("draft.pick_capacity must equal expected_teams * draft_rounds when both factors are known")
		}
	} else if d.PickCapacity != nil {
		v.add("draft.pick_capacity must be null when a capacity factor is unprovable")
	}
	if d.State == "scheduled" || d.State == "open" {
		if d.PickCount != nil && *d.PickCount != 0 {
			v.add("scheduled/open draft.pick_count must be zero")
		}
	}
	if d.State == "in_progress" && allInts(d.PickCount, d.PickCapacity) && (*d.PickCount <= 0 || *d.PickCount >= *d.PickCapacity) {
		v.add("in-progress pick_count must be between zero and capacity")
	}
	if d.State == "complete" && allInts(d.PickCount, d.PickCapacity) && *d.PickCount != *d.PickCapacity {
		v.add("complete draft.pick_count must equal capacity")
	}
	if allInts(d.PickCount, d.PickCapacity) {
		if *d.PickCount == *d.PickCapacity && d.State != "complete" {
			v.add("a complete pick count requires draft.state complete")
		}
		if *d.PickCount > 0 && *d.PickCount < *d.PickCapacity && d.State != "in_progress" {
			v.add("a nonzero incomplete pick count requires draft.state in_progress")
		}
	}
	clockAllowed := d.State == "open" || d.State == "in_progress"
	if !clockAllowed && d.OnClockTeamID != nil {
		v.add("draft.on_clock_team_id is not allowed in this state")
	}
	if clockAllowed && d.OrderStatus != nil && *d.OrderStatus == "ready" && d.OnClockTeamID == nil {
		v.add("draft.on_clock_team_id is required on an ordered open/in-progress draft")
	}
	v.optionalText("draft.on_clock_team_id", d.OnClockTeamID, maxStringBytes)
	if allInts(d.ReadyTeams, d.ExpectedTeams) && *d.ReadyTeams > *d.ExpectedTeams {
		v.add("draft.ready_teams must not exceed expected_teams")
	}
	if allInts(d.ExpectedTeams, s.Competition.Teams.Occupied) && *d.ExpectedTeams > *s.Competition.Teams.Occupied {
		v.add("draft.expected_teams must not exceed occupied teams")
	}
	if allInts(d.ExpectedTeams, s.Competition.Teams.Total) && *d.ExpectedTeams > *s.Competition.Teams.Total {
		v.add("draft.expected_teams must not exceed total teams")
	}
	if scheduledAt, ok := optionalParsedUTC(d.ScheduledAt); ok {
		if producedAt, producedOK := parseUTC(s.ProducedAt); producedOK {
			if d.State == "scheduled" && !producedAt.Before(scheduledAt) {
				v.add("draft.state scheduled requires produced_at before scheduled_at")
			}
			if d.State == "open" && producedAt.Before(scheduledAt) {
				v.add("draft.state open requires produced_at at or after scheduled_at")
			}
		}
	}
	switch s.Competition.Phase {
	case "pre-draft":
		if d.State != "unscheduled" && d.State != "scheduled" {
			v.add("competition.phase pre-draft is incompatible with draft state")
		}
	case "draft":
		if d.State != "open" && d.State != "in_progress" {
			v.add("competition.phase draft is incompatible with draft state")
		}
	case "preseason":
		if d.State != "complete" {
			v.add("competition.phase preseason requires a complete draft")
		}
	}
}

func (v *validator) readiness(s Summary) {
	hasReadiness := hasCapability(s.Capabilities, "readiness.v1")
	if s.Readiness == nil {
		if hasReadiness {
			v.add("readiness is required when readiness.v1 is advertised")
		}
		return
	}
	if !hasReadiness {
		v.add("readiness must be omitted or null without readiness.v1")
	}
	r := *s.Readiness
	v.severity("readiness.severity", r.Severity)
	if r.Items == nil {
		v.add("readiness.items must be an array, not null")
	}
	if len(r.Items) > maxReadinessItems {
		v.add("readiness.items exceeds 32 items")
	}
	seen := map[string]struct{}{}
	for i, item := range r.Items {
		path := fmt.Sprintf("readiness.items[%d]", i)
		v.token(path+".code", item.Code)
		v.severity(path+".severity", item.Severity)
		if item.Count < 0 {
			v.add(path + ".count must be nonnegative")
		}
		v.privacyText(path+".label", item.Label, maxLabelBytes)
		if _, ok := seen[item.Code]; ok {
			v.add("readiness.items contains duplicate code " + item.Code)
		}
		seen[item.Code] = struct{}{}
	}
}

func (v *validator) membership(s Summary) {
	m := s.Membership
	v.count("membership.claimed_teams", m.ClaimedTeams)
	v.count("membership.open_teams", m.OpenTeams)
	v.count("membership.pending_invites", m.PendingInvites)
	v.count("membership.primary_managers", m.PrimaryManagers)
	v.count("membership.co_managers", m.CoManagers)
	if allInts(m.ClaimedTeams, s.Competition.Teams.Occupied) && *m.ClaimedTeams != *s.Competition.Teams.Occupied {
		v.add("membership.claimed_teams must equal occupied teams")
	}
	if allInts(m.OpenTeams, m.ClaimedTeams, s.Competition.Teams.Total) && *m.OpenTeams+*m.ClaimedTeams != *s.Competition.Teams.Total {
		v.add("membership open + claimed teams must equal total")
	}
}

func (v *validator) warnings(items []Warning) {
	if len(items) > maxWarnings {
		v.add("warnings exceeds 32 items")
	}
	seen := map[string]struct{}{}
	for i, item := range items {
		path := fmt.Sprintf("warnings[%d]", i)
		v.token(path+".code", item.Code)
		v.severity(path+".severity", item.Severity)
		v.privacyText(path+".summary", item.Summary, maxStringBytes)
		if item.Source != nil {
			v.privacyText(path+".source", *item.Source, maxStringBytes)
		}
		if _, ok := seen[item.Code]; ok {
			v.add("warnings contains duplicate code " + item.Code)
		}
		seen[item.Code] = struct{}{}
	}
	if !sort.SliceIsSorted(items, func(i, j int) bool { return lessWarning(items[i], items[j]) }) {
		v.add("warnings are not in normative severity/code order")
	}
}

func (v *validator) calendar(c Calendar) {
	if len(c.Deadlines) > maxDeadlines {
		v.add("calendar.deadlines exceeds 32 items")
	}
	combined := make([]Deadline, 0, len(c.Deadlines)+1)
	if c.NextDeadline != nil {
		combined = append(combined, *c.NextDeadline)
	}
	combined = append(combined, c.Deadlines...)
	seen := map[string]struct{}{}
	for i, item := range combined {
		path := fmt.Sprintf("calendar[%d]", i)
		v.deadline(path, item)
		if _, ok := seen[item.Code]; ok {
			v.add("calendar contains duplicate code " + item.Code)
		}
		seen[item.Code] = struct{}{}
	}
	if !sort.SliceIsSorted(combined, func(i, j int) bool { return lessDeadline(combined[i], combined[j]) }) {
		v.add("calendar is not in normative time/code order")
	}
	if c.NextDeadline == nil && len(c.Deadlines) != 0 {
		v.add("calendar.next_deadline must be the first complete deadline")
	}
}

func (v *validator) deadline(path string, d Deadline) {
	v.token(path+".code", d.Code)
	v.token(path+".category", d.Category)
	v.text(path+".title", d.Title, maxStringBytes)
	v.utcOptional(path+".at", d.At)
	if d.At == nil && d.State != "unscheduled" {
		v.add(path + ".at may be null only when unscheduled")
	}
	v.timezone(path+".timezone", d.Timezone)
	v.text(path+".relative_text", d.RelativeText, maxStringBytes)
	v.token(path+".state", d.State)
	v.href(path+".href", d.Category, d.Href, true)
}

func (v *validator) attention(items []AttentionItem, leagueID string) {
	if len(items) > maxAttentionItems {
		v.add("attention_items exceeds 64 items")
	}
	seen := map[string]struct{}{}
	for i, item := range items {
		path := fmt.Sprintf("attention_items[%d]", i)
		v.token(path+".code", item.Code)
		v.token(path+".category", item.Category)
		v.severity(path+".severity", item.Severity)
		v.privacyText(path+".title", item.Title, maxStringBytes)
		v.privacyText(path+".summary", item.Summary, maxStringBytes)
		if item.LeagueID != leagueID {
			v.add(path + ".league_id does not match instance.league_id")
		}
		v.utcOptional(path+".due_at", item.DueAt)
		v.token(path+".state", item.State)
		v.token(path+".source", item.Source)
		v.href(path+".href", item.Category, item.Href, true)
		if _, ok := seen[item.Code]; ok {
			v.add("attention_items contains duplicate code " + item.Code)
		}
		seen[item.Code] = struct{}{}
	}
	if !sort.SliceIsSorted(items, func(i, j int) bool { return lessAttention(items[i], items[j]) }) {
		v.add("attention_items are not in normative severity/time/code order")
	}
}

func (v *validator) activity(a RecentActivity) {
	if len(a.Items) > maxActivityItems {
		v.add("recent_activity.items exceeds 20 items")
	}
	v.utc("recent_activity.as_of", a.AsOf)
	seen := map[string]struct{}{}
	for i, item := range a.Items {
		path := fmt.Sprintf("recent_activity.items[%d]", i)
		v.text(path+".id", item.ID, maxStringBytes)
		v.utc(path+".occurred_at", item.OccurredAt)
		v.token(path+".category", item.Category)
		v.privacyText(path+".summary", item.Summary, maxStringBytes)
		v.href(path+".href", item.Category, item.Href, false)
		if _, ok := seen[item.ID]; ok {
			v.add("recent_activity.items contains duplicate id " + item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	if !sort.SliceIsSorted(a.Items, func(i, j int) bool { return lessActivity(a.Items[i], a.Items[j]) }) {
		v.add("recent_activity.items are not in normative time/id order")
	}
}

func (v *validator) configuration(c Configuration, teams TeamCounts) {
	v.optionalToken("configuration.format", c.Format)
	v.count("configuration.team_count", c.TeamCount)
	v.optionalToken("configuration.roster_model", c.RosterModel)
	v.optionalToken("configuration.scoring_model", c.ScoringModel)
	v.optionalToken("configuration.waiver_mode", c.WaiverMode)
	v.optionalToken("configuration.trade_policy", c.TradePolicy)
	if c.Timezone != nil {
		v.timezone("configuration.timezone", *c.Timezone)
	}
	if allInts(c.TeamCount, teams.Total) && *c.TeamCount != *teams.Total {
		v.add("configuration.team_count must equal competition.teams.total")
	}
}

func (v *validator) configurationIdentity(c Configuration, league League) {
	if c.Format != nil && *c.Format != league.Format {
		v.add("configuration.format must equal league.format")
	}
	if c.Timezone != nil && *c.Timezone != league.Timezone {
		v.add("configuration.timezone must equal league.timezone")
	}
}

func (v *validator) dataHealth(h DataHealth) {
	v.token("data_health.quality", h.Quality)
	v.optionalToken("data_health.source_mode", h.SourceMode)
	v.optionalToken("data_health.source_state", h.SourceState)
	v.count("data_health.player_count", h.PlayerCount)
	if h.LastDegradation != nil {
		v.privacyText("data_health.last_degradation", *h.LastDegradation, maxStringBytes)
	}
	v.utcOptional("data_health.last_success_at", h.LastSuccessAt)
	v.utcOptional("data_health.as_of", h.AsOf)
	if h.Quality == "not_reported" && h.AsOf != nil {
		v.add("data_health.as_of must be null when quality is not_reported")
	}
	if h.Quality != "not_reported" && h.AsOf == nil {
		v.add("data_health.as_of is required when quality is reported")
	}
	if h.SourceState != nil {
		switch *h.SourceState {
		case "live", "stale", "degraded", "unreachable":
		default:
			// Additive enum values are accepted but have no inferred behavior.
		}
	}
	if h.SourceMode != nil && h.SourceState != nil && *h.SourceState == "live" {
		switch *h.SourceMode {
		case "cache", "cached", "offline":
			v.add("data_health.source_state must not be live for cached or offline source mode")
		}
	}
}

func (v *validator) release(r Release) {
	if !gitSHAExpression.MatchString(r.GitSHA) || r.GitSHA == strings.Repeat("0", 40) || r.GitSHA == strings.Repeat("f", 40) {
		v.add("release.git_sha must be a nonsentinel full lowercase Git SHA")
	}
	v.utc("release.built_at", r.BuiltAt)
	if r.ImageDigest != nil {
		digest := *r.ImageDigest
		if !digestExpression.MatchString(digest) || digest == "sha256:"+strings.Repeat("0", 64) || digest == "sha256:"+strings.Repeat("f", 64) {
			v.add("release.image_digest must be a nonsentinel sha256 digest or null")
		}
	}
}

func (v *validator) links(l Links, capabilities []string, configuration Configuration) {
	values := map[string]*string{
		"league": l.League, "overview": l.Overview, "join": l.Join, "draft": l.Draft,
		"board": l.Board, "team": l.Team, "players": l.Players, "trades": l.Trades,
		"pickem": l.Pickem, "blitz": l.Blitz, "activity": l.Activity,
		"commissioner": l.Commissioner,
	}
	for key, value := range values {
		if value != nil && *value != canonicalLinks[key] {
			v.add(fmt.Sprintf("links.%s must equal approved route %q or null", key, canonicalLinks[key]))
		}
	}
	for _, key := range []string{"league", "overview", "join", "team", "players", "trades", "activity", "commissioner"} {
		if values[key] == nil {
			v.add("links." + key + " is required for the core league surface")
		}
	}
	if hasCapability(capabilities, "draft.v1") && l.Draft == nil {
		v.add("links.draft is required when draft.v1 is advertised")
	}
	if hasCapability(capabilities, "draft.v1") && l.Board == nil {
		v.add("links.board is required when draft.v1 is advertised")
	}
	if !hasCapability(capabilities, "draft.v1") && (l.Draft != nil || l.Board != nil) {
		v.add("links.draft and links.board must be null when draft navigation is unsupported")
	}
	if configuration.PickemEnabled != nil && *configuration.PickemEnabled && l.Pickem == nil {
		v.add("links.pickem is required when Pick'em is enabled")
	}
	if configuration.PickemEnabled != nil && !*configuration.PickemEnabled && l.Pickem != nil {
		v.add("links.pickem must be null when Pick'em is disabled")
	}
	if configuration.BlitzEnabled != nil && *configuration.BlitzEnabled && l.Blitz == nil {
		v.add("links.blitz is required when Blitz is enabled")
	}
	if configuration.BlitzEnabled != nil && !*configuration.BlitzEnabled && l.Blitz != nil {
		v.add("links.blitz must be null when Blitz is disabled")
	}
}

func (v *validator) snapshotTimes(s Summary) {
	producedAt, ok := parseUTC(s.ProducedAt)
	if !ok {
		return
	}
	checkNotAfter := func(path string, value *string, ceiling time.Time) {
		if value == nil {
			return
		}
		if parsed, valid := parseUTC(*value); valid && parsed.After(ceiling) {
			v.add(path + " must not be after its snapshot time")
		}
	}
	built := s.Release.BuiltAt
	checkNotAfter("release.built_at", &built, producedAt)
	recentAsOf := s.RecentActivity.AsOf
	checkNotAfter("recent_activity.as_of", &recentAsOf, producedAt)
	checkNotAfter("data_health.as_of", s.DataHealth.AsOf, producedAt)
	checkNotAfter("data_health.last_success_at", s.DataHealth.LastSuccessAt, producedAt)
	if activityAsOf, valid := parseUTC(s.RecentActivity.AsOf); valid {
		for i, item := range s.RecentActivity.Items {
			occurred := item.OccurredAt
			checkNotAfter(fmt.Sprintf("recent_activity.items[%d].occurred_at", i), &occurred, activityAsOf)
		}
	}
}

func (v *validator) href(path, category string, value *string, requiredWhenAvailable bool) {
	approved, known := categoryRoutes[category]
	if value == nil {
		if known && requiredWhenAvailable && v.availableRoutes[approved] {
			v.add(path + " is required when an approved native route is available")
		}
		return
	}
	if !known {
		v.add(path + " must be null for an unregistered category")
		return
	}
	if !v.availableRoutes[approved] {
		v.add(path + " must be null when the corresponding native link is unavailable")
		return
	}
	if err := validateRoute(*value, approved); err != nil {
		v.add(path + ": " + err.Error())
	}
}

func validateRoute(value, approved string) error {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, `\`) || strings.Contains(value, "#") {
		return fmt.Errorf("must be an approved root-relative route")
	}
	u, err := url.ParseRequestURI(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || u.Path != approved || u.EscapedPath() != approved {
		return fmt.Errorf("must use approved path %q", approved)
	}
	if u.RawQuery == "" {
		return nil
	}
	allowed := allowedQueries[approved]
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || query.Encode() != u.RawQuery {
		return fmt.Errorf("query must use canonical encoding and order")
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 || !validQueryValue(key, values[0]) {
			return fmt.Errorf("query parameter %q is not approved", key)
		}
	}
	return nil
}

func validQueryValue(key, value string) bool {
	switch key {
	case "page":
		return positiveDecimal(value, 999999)
	case "week":
		return positiveDecimal(value, 99)
	case "position":
		return positionValue.MatchString(value)
	case "team", "counterparty":
		return opaqueQueryValue.MatchString(value)
	default:
		return false
	}
}

func positiveDecimal(value string, maximum int) bool {
	if value == "" || value[0] == '0' {
		return false
	}
	number, err := strconv.Atoi(value)
	return err == nil && number > 0 && number <= maximum
}

func (v *validator) severity(path, value string) {
	if _, ok := severityRank[value]; !ok {
		v.add(path + " must be info, warning, or blocking")
	}
}

func hasCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}

func availableRoutes(links Links) map[string]bool {
	available := map[string]bool{}
	for _, value := range []*string{
		links.League, links.Overview, links.Join, links.Draft, links.Board, links.Team,
		links.Players, links.Trades, links.Pickem, links.Blitz, links.Activity,
		links.Commissioner,
	} {
		if value != nil {
			available[*value] = true
		}
	}
	return available
}

func allInts(values ...*int) bool {
	for _, value := range values {
		if value == nil {
			return false
		}
	}
	return true
}

func optionalParsedUTC(value *string) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}
	return parseUTC(*value)
}

func lessDeadline(a, b Deadline) bool { return lessNullableTime(a.At, b.At, a.Code, b.Code, false) }

func lessAttention(a, b AttentionItem) bool {
	if severityRank[a.Severity] != severityRank[b.Severity] {
		return severityRank[a.Severity] < severityRank[b.Severity]
	}
	return lessNullableTime(a.DueAt, b.DueAt, a.Code, b.Code, false)
}

func lessWarning(a, b Warning) bool {
	if severityRank[a.Severity] != severityRank[b.Severity] {
		return severityRank[a.Severity] < severityRank[b.Severity]
	}
	return a.Code < b.Code
}

func lessActivity(a, b ActivityItem) bool {
	at, _ := parseUTC(a.OccurredAt)
	bt, _ := parseUTC(b.OccurredAt)
	if !at.Equal(bt) {
		return at.After(bt)
	}
	return a.ID < b.ID
}

func lessNullableTime(a, b *string, aKey, bKey string, descending bool) bool {
	if a == nil || b == nil {
		if a == nil && b == nil {
			return aKey < bKey
		}
		return a != nil
	}
	at, _ := parseUTC(*a)
	bt, _ := parseUTC(*b)
	if !at.Equal(bt) {
		if descending {
			return at.After(bt)
		}
		return at.Before(bt)
	}
	return aKey < bKey
}

func validateRequiredJSON(raw json.RawMessage) error {
	root, err := decodeObject(raw, "summary")
	if err != nil {
		return err
	}
	required(root, "summary", false,
		"contract", "schema_version", "instance", "league", "capabilities", "competition",
		"membership", "lineup", "waivers", "trades", "pickem", "warnings", "calendar",
		"attention_items", "recent_activity", "configuration", "data_health", "release", "links", "produced_at")
	if err := requiredError(root); err != nil {
		return err
	}
	if err := validateObjectField(root, "instance", "instance", []string{"id", "league_id"}, nil); err != nil {
		return err
	}
	if err := validateObjectField(root, "league", "league", []string{"name", "short_name", "format", "season", "timezone"}, nil); err != nil {
		return err
	}
	competition, err := objectField(root, "competition", "competition")
	if err != nil {
		return err
	}
	required(competition, "competition", false, "phase", "teams")
	if err := requiredError(competition); err != nil {
		return err
	}
	if err := validateObjectField(competition, "teams", "competition.teams", []string{"occupied", "vacant", "total"}, map[string]bool{"occupied": true, "vacant": true, "total": true}); err != nil {
		return err
	}
	if draftRaw, ok := root["draft"]; ok && !isNull(draftRaw) {
		if err := validateRawObject(draftRaw, "draft", []string{"state", "scheduled_at", "order_status", "pick_count", "pick_capacity", "draft_rounds", "ready_teams", "expected_teams", "on_clock_team_id", "board_gap_count"}, map[string]bool{"scheduled_at": true, "order_status": true, "pick_count": true, "pick_capacity": true, "draft_rounds": true, "ready_teams": true, "expected_teams": true, "on_clock_team_id": true, "board_gap_count": true}); err != nil {
			return err
		}
	}
	if readinessRaw, ok := root["readiness"]; ok && !isNull(readinessRaw) {
		readiness, err := decodeObject(readinessRaw, "readiness")
		if err != nil {
			return err
		}
		required(readiness, "readiness", false, "severity", "items")
		if err := requiredError(readiness); err != nil {
			return err
		}
		if err := validateArrayObjects(readiness["items"], "readiness.items", []string{"code", "severity", "count", "label"}, nil); err != nil {
			return err
		}
	}
	objects := []struct {
		name     string
		fields   []string
		nullable map[string]bool
	}{
		{"membership", []string{"claimed_teams", "open_teams", "pending_invites", "primary_managers", "co_managers"}, allNullable("claimed_teams", "open_teams", "pending_invites", "primary_managers", "co_managers")},
		{"lineup", []string{"issue_count", "next_lock_at"}, allNullable("issue_count", "next_lock_at")},
		{"waivers", []string{"mode", "open_claims", "next_run_at"}, allNullable("mode", "open_claims", "next_run_at")},
		{"trades", []string{"pending_count", "commissioner_decisions"}, allNullable("pending_count", "commissioner_decisions")},
		{"pickem", []string{"week", "unpicked", "next_deadline_at"}, allNullable("week", "unpicked", "next_deadline_at")},
		{"configuration", []string{"format", "team_count", "roster_model", "scoring_model", "waiver_mode", "trade_policy", "pickem_enabled", "blitz_enabled", "timezone"}, allNullable("format", "team_count", "roster_model", "scoring_model", "waiver_mode", "trade_policy", "pickem_enabled", "blitz_enabled", "timezone")},
		{"data_health", []string{"quality", "source_mode", "source_state", "player_count", "last_degradation", "last_success_at", "as_of"}, allNullable("source_mode", "source_state", "player_count", "last_degradation", "last_success_at", "as_of")},
		{"release", []string{"git_sha", "built_at", "image_digest"}, allNullable("image_digest")},
		{"links", []string{"league", "overview", "join", "draft", "board", "team", "players", "trades", "pickem", "blitz", "activity", "commissioner"}, allNullable("league", "overview", "join", "draft", "board", "team", "players", "trades", "pickem", "blitz", "activity", "commissioner")},
	}
	for _, object := range objects {
		if err := validateObjectField(root, object.name, object.name, object.fields, object.nullable); err != nil {
			return err
		}
	}
	if err := validateArrayObjects(root["warnings"], "warnings", []string{"code", "severity", "summary"}, nil); err != nil {
		return err
	}
	calendar, err := objectField(root, "calendar", "calendar")
	if err != nil {
		return err
	}
	required(calendar, "calendar", true, "next_deadline", "deadlines")
	if err := requiredError(calendar); err != nil {
		return err
	}
	if next := calendar["next_deadline"]; !isNull(next) {
		if err := validateRawObject(next, "calendar.next_deadline", deadlineFields(), map[string]bool{"at": true, "href": true}); err != nil {
			return err
		}
	}
	if err := validateArrayObjects(calendar["deadlines"], "calendar.deadlines", deadlineFields(), map[string]bool{"at": true, "href": true}); err != nil {
		return err
	}
	if err := validateArrayObjects(root["attention_items"], "attention_items", []string{"code", "category", "severity", "title", "summary", "league_id", "due_at", "state", "source", "href"}, map[string]bool{"due_at": true, "href": true}); err != nil {
		return err
	}
	recent, err := objectField(root, "recent_activity", "recent_activity")
	if err != nil {
		return err
	}
	required(recent, "recent_activity", false, "items", "truncated", "as_of")
	if err := requiredError(recent); err != nil {
		return err
	}
	if err := validateArrayObjects(recent["items"], "recent_activity.items", []string{"id", "occurred_at", "category", "summary"}, nil); err != nil {
		return err
	}
	return nil
}

const requiredErrorKey = "\x00required_error"

func required(object map[string]json.RawMessage, path string, nullableAll bool, fields ...string) {
	for _, field := range fields {
		raw, ok := object[field]
		if !ok || (!nullableAll && isNull(raw)) {
			object[requiredErrorKey] = json.RawMessage(fmt.Sprintf("%q", path+"."+field+" is required and must not be null"))
			return
		}
	}
}

func requiredError(object map[string]json.RawMessage) error {
	raw, ok := object[requiredErrorKey]
	if !ok {
		return nil
	}
	var message string
	_ = json.Unmarshal(raw, &message)
	return fmt.Errorf("invalid commissioner summary: %s", message)
}

func validateObjectField(parent map[string]json.RawMessage, field, path string, fields []string, nullable map[string]bool) error {
	raw, ok := parent[field]
	if !ok || isNull(raw) {
		return fmt.Errorf("invalid commissioner summary: %s is required and must not be null", path)
	}
	return validateRawObject(raw, path, fields, nullable)
}

func validateRawObject(raw json.RawMessage, path string, fields []string, nullable map[string]bool) error {
	object, err := decodeObject(raw, path)
	if err != nil {
		return err
	}
	for _, field := range fields {
		value, ok := object[field]
		if !ok || (isNull(value) && !nullable[field]) {
			return fmt.Errorf("invalid commissioner summary: %s.%s is required and must not be null", path, field)
		}
	}
	return nil
}

func objectField(parent map[string]json.RawMessage, field, path string) (map[string]json.RawMessage, error) {
	raw, ok := parent[field]
	if !ok || isNull(raw) {
		return nil, fmt.Errorf("invalid commissioner summary: %s is required and must not be null", path)
	}
	return decodeObject(raw, path)
}

func decodeObject(raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("invalid commissioner summary: %s must be an object", path)
	}
	return object, nil
}

func validateArrayObjects(raw json.RawMessage, path string, fields []string, nullable map[string]bool) error {
	if len(raw) == 0 || isNull(raw) {
		return fmt.Errorf("invalid commissioner summary: %s must be an array", path)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return fmt.Errorf("invalid commissioner summary: %s must be an array", path)
	}
	for i, item := range items {
		if err := validateRawObject(item, fmt.Sprintf("%s[%d]", path, i), fields, nullable); err != nil {
			return err
		}
	}
	return nil
}

func allNullable(fields ...string) map[string]bool {
	result := make(map[string]bool, len(fields))
	for _, field := range fields {
		result[field] = true
	}
	return result
}

func deadlineFields() []string {
	return []string{"code", "category", "title", "at", "timezone", "relative_text", "state", "href"}
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

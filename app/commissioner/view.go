package commissioner

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/commissionerhq"
	"gridiron-2000/internal/league"
)

// The commissioner readout deliberately uses a PII-free view model. It is
// shared by the full SSR page and its route/data contracts so every card
// uses one meaning for the whole commissioner readout.
type fleetPageView struct {
	GeneratedAt    time.Time
	Location       *time.Location
	Cards          []fleetCardView
	Attention      []attentionView
	LeagueCount    int
	ClaimedSeats   int
	TotalSeats     int
	DraftsLive     int
	AttentionCount int
	CriticalCount  int
	WarningCount   int
}

type attentionView struct {
	League    string
	PeerID    string
	Code      string
	Severity  string
	Count     int
	Message   string
	Area      string
	Section   string
	OwnerURL  string
	OwnerText string
}

type fleetCardView struct {
	Available bool
	PeerID    string
	Name      string
	ShortCode string
	Mode      string
	Season    int
	PublicURL string
	HostLabel string
	Error     string

	RuntimeReady     bool
	AppVersion       string
	FrameworkVersion string
	GitSHA           string
	Build            string
	GeneratedAt      string
	GeneratedAtISO   string

	Seats        int
	ClaimedSeats int
	ReadySeats   int
	SeatLedger   []map[string]any

	DraftStatus       string
	DraftStarted      bool
	DraftStartedAt    string
	DraftStartedAtISO string
	DraftAt           string
	DraftAtISO        string
	DraftAtRelative   string
	DraftRounds       int
	DraftPicks        int
	DraftSlots        int
	DraftOrder        string
	DraftOrderSet     bool
	ClockArmed        bool
	ClockPaused       bool
	ClockText         string
	DraftStartCopy    string

	SeasonPhase            string
	CurrentWeek            int
	SchedulePublished      bool
	ScheduleText           string
	ScheduleFinalText      string
	ScheduleRange          string
	RedrawLocked           bool
	RedrawLockReason       string
	WeekCloseWeek          int
	WeekCloseReady         bool
	WeekCloseGames         int
	WeekCloseTotal         int
	WeekCloseFinal         bool
	WeekCloseStats         bool
	WeekCloseStatsAt       string
	WeekCloseBadge         string
	WeekCloseWaiting       bool
	WeekCloseWaitingReason string
	PlayoffAvailable       bool
	PlayoffStatus          string
	PlayoffStatusLabel     string
	PlayoffSource          string
	PlayoffNextMatchups    int
	PlayoffNote            string

	PoolMode           string
	PoolActual         int
	PoolRosterCapacity int
	PoolTarget         int
	PoolCushion        int
	PoolShortfall      int
	PoolActualCoverage string
	PoolTargetCoverage string
	PoolRosterCoverage string
	PoolLastSync       string
	OpenData           []map[string]any

	Attention    []attentionView
	HasAttention bool

	HomeURL               string
	AdminURL              string
	DraftURL              string
	AdminDraftURL         string
	AdminScheduleURL      string
	AdminWeekCloseURL     string
	AdminSeatsURL         string
	AdminInvitesURL       string
	AdminOrderURL         string
	AdminDataURL          string
	AdminClockURL         string
	AdminRosterURL        string
	AdminAnnouncementsURL string
	AdminDangerURL        string
}

// buildFleetView assembles the fleet readout. location is the viewing
// commissioner's own LeagueLocation() (league.Default().LeagueLocation()
// in production, an explicit *time.Location in every test): every
// timestamp formatted below is converted into it rather than left in
// whatever offset the summary's wire JSON happened to carry (2026-09-01
// audit: displayTime with no location conversion rendered a literal "UTC"
// abbreviation on every card, not the commissioner's own league zone).
func buildFleetView(entries []commissionerhq.FleetEntry, generatedAt time.Time, location *time.Location) fleetPageView {
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	if location == nil {
		location = time.UTC
	}
	view := fleetPageView{GeneratedAt: generatedAt, Location: location, Cards: make([]fleetCardView, 0, len(entries))}
	for _, entry := range entries {
		card := cardView(entry, generatedAt, location)
		view.Cards = append(view.Cards, card)
		view.LeagueCount++
		if !card.Available {
			attention := attentionView{
				League: card.NameOrPeer(), PeerID: card.PeerID, Code: "peer_unavailable",
				Severity: "warning", Count: 1, Message: "This league did not answer.",
				Area: "runtime", Section: "data", OwnerURL: card.AdminDataURL,
				OwnerText: "Open league directly",
			}
			card.Attention = append(card.Attention, attention)
			card.HasAttention = true
			view.Cards[len(view.Cards)-1] = card
		} else {
			view.ClaimedSeats += card.ClaimedSeats
			view.TotalSeats += card.Seats
			if strings.EqualFold(card.DraftStatus, "LIVE") {
				view.DraftsLive++
			}
		}
		for _, attention := range card.Attention {
			view.Attention = append(view.Attention, attention)
			view.AttentionCount++
			switch strings.ToLower(attention.Severity) {
			case "critical":
				view.CriticalCount++
			case "warning":
				view.WarningCount++
			}
		}
	}
	sort.SliceStable(view.Attention, func(i, j int) bool {
		left, right := view.Attention[i], view.Attention[j]
		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		if left.League != right.League {
			return left.League < right.League
		}
		return left.Code < right.Code
	})
	return view
}

func severityRank(value string) int {
	switch strings.ToLower(value) {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

// cardView projects one peer's summary. now is the fleet-snapshot instant
// (buildFleetView's generatedAt) used only for DraftDatePublished's
// far-future-sentinel check and each field's relative-text label; location
// is the *time.Location every absolute timestamp below is displayed in.
func cardView(entry commissionerhq.FleetEntry, now time.Time, location *time.Location) fleetCardView {
	if !entry.Available() {
		publicURL := strings.TrimRight(entry.PublicURL, "/")
		card := fleetCardView{
			Available: false, PeerID: entry.PeerID, Name: entry.PeerID,
			PublicURL: publicURL, HostLabel: hostLabel(publicURL), Error: safePeerError(entry.Error),
			DraftStartCopy: "The commissioner starts drafts intentionally; a scheduled time is only the meeting point.",
		}
		card.setLinks(publicURL)
		return card
	}

	summary := entry.Summary
	publicURL := strings.TrimRight(summary.Instance.PublicURL, "/")
	if publicURL == "" {
		publicURL = strings.TrimRight(entry.PublicURL, "/")
	}
	// The scheduled draft meeting is the one HQ field that can carry the
	// neutral shipped placeholder (config.go's placeholderDraftAt,
	// 2099-01-01): DraftDatePublished is the same guard draftSummaryForState
	// (service.go) already applies to / and /guide. A published, already-
	// past meeting also gets a relative label; a future one does not (a
	// meeting three days out is not usefully described as "in 3 days" by
	// the same past-oriented RelativeTime the rest of the app uses for
	// freshness labels).
	draftAt, draftAtISO, draftAtRelative := "Not published yet", "", ""
	if league.DraftDatePublished(now, summary.Draft.ScheduledAt) {
		draftAt = displayTime(location, summary.Draft.ScheduledAt)
		draftAtISO = isoTime(summary.Draft.ScheduledAt)
		draftAtRelative = relativeText(now, summary.Draft.ScheduledAt)
	}
	card := fleetCardView{
		Available: true, PeerID: entry.PeerID, Name: summary.Instance.Name,
		ShortCode: summary.Instance.ShortCode, Mode: summary.Instance.Mode,
		Season: summary.Instance.Season, PublicURL: publicURL, HostLabel: hostLabel(publicURL),
		RuntimeReady: summary.Runtime.Ready, AppVersion: summary.Runtime.AppVersion,
		FrameworkVersion: summary.Runtime.FrameworkVersion, GitSHA: sourceSHAView(summary.Runtime.GitSHA),
		Build: summary.Runtime.Build, GeneratedAt: displayTime(location, summary.GeneratedAt),
		GeneratedAtISO: isoTime(summary.GeneratedAt), Seats: summary.Membership.Seats,
		ClaimedSeats: summary.Membership.ClaimedSeats, ReadySeats: summary.Membership.ReadySeats,
		DraftStatus: draftStatusLabel(summary.Draft.Status), DraftStarted: summary.Draft.Started,
		DraftStartedAt: displayTime(location, summary.Draft.StartedAt), DraftStartedAtISO: isoTime(summary.Draft.StartedAt),
		DraftAt: draftAt, DraftAtISO: draftAtISO, DraftAtRelative: draftAtRelative,
		DraftRounds: summary.Draft.Rounds, DraftPicks: summary.Draft.Picks,
		DraftSlots: summary.Pool.RosterCapacity, DraftOrder: orderText(summary.Draft.Order),
		DraftOrderSet: summary.Draft.OrderSet, ClockArmed: summary.Draft.ClockArmed,
		ClockPaused: summary.Draft.ClockPaused, ClockText: clockText(summary.Draft),
		DraftStartCopy: "The commissioner starts drafts intentionally; the scheduled time is only the meeting point.",
		SeasonPhase:    seasonPhaseLabel(summary.Season.Phase), CurrentWeek: summary.Season.CurrentWeek,
		SchedulePublished: summary.Season.Schedule.Published,
		ScheduleText:      scheduleText(summary.Season.Schedule), ScheduleFinalText: finalText(summary.Season.Schedule),
		ScheduleRange: scheduleRange(summary.Season.Schedule), RedrawLocked: summary.Season.Schedule.RedrawLocked,
		RedrawLockReason: summary.Season.Schedule.RedrawLockReason,
		WeekCloseWeek:    summary.Season.WeekClose.Week, WeekCloseReady: summary.Season.WeekClose.Ready,
		WeekCloseGames: summary.Season.WeekClose.GamesFinal, WeekCloseTotal: summary.Season.WeekClose.GamesTotal,
		WeekCloseFinal: summary.Season.WeekClose.Final, WeekCloseStats: summary.Season.WeekClose.StatsFresh,
		WeekCloseStatsAt: displayTime(location, summary.Season.WeekClose.StatsUpdatedAt),
		WeekCloseBadge:   weekCloseBadge(summary.Season.WeekClose), WeekCloseWaiting: !summary.Season.WeekClose.Final && !summary.Season.WeekClose.Ready,
		WeekCloseWaitingReason: weekCloseWaitingReason(summary.Season.WeekClose),
		PlayoffAvailable:       summary.Season.Playoffs.Available,
		PlayoffStatus:          summary.Season.Playoffs.Status,
		PlayoffStatusLabel:     summary.Season.Playoffs.StatusLabel,
		PlayoffSource:          summary.Season.Playoffs.Source,
		PlayoffNextMatchups:    summary.Season.Playoffs.NextMatchups,
		PlayoffNote:            summary.Season.Playoffs.Note,
		PoolMode:               poolModeLabel(summary.Pool.Mode), PoolActual: summary.Pool.Actual,
		PoolRosterCapacity: summary.Pool.RosterCapacity, PoolTarget: summary.Pool.Target,
		PoolCushion: summary.Pool.Cushion, PoolShortfall: summary.Pool.Shortfall,
		PoolActualCoverage: ratio(summary.Pool.ActualCoverage), PoolTargetCoverage: ratio(summary.Pool.TargetCoverage),
		PoolRosterCoverage: ratio(summary.Pool.RosterCoverage), PoolLastSync: displayTime(location, summary.Pool.LastSync),
	}
	for _, seat := range summary.Membership.SeatLedger {
		card.SeatLedger = append(card.SeatLedger, map[string]any{
			"seat": seat.Seat, "claimed": seat.Claimed, "ready": seat.Ready,
		})
	}
	card.OpenData = openDataRows(summary.OpenData, location)
	for _, item := range summary.Attention {
		section := attentionSection(item)
		attention := attentionView{
			League: card.NameOrPeer(), PeerID: card.PeerID, Code: item.Code,
			Severity: severityLabel(item.Severity), Count: item.Count, Message: item.Message,
			Area: item.Area, Section: section, OwnerURL: card.adminURLFor(section),
			OwnerText: attentionOwnerText(section),
		}
		card.Attention = append(card.Attention, attention)
	}
	card.HasAttention = len(card.Attention) > 0
	card.setLinks(publicURL)
	return card
}

func (card *fleetCardView) setLinks(base string) {
	base = strings.TrimRight(base, "/")
	card.HomeURL = base + "/"
	card.AdminURL = base + "/admin"
	card.DraftURL = base + "/draft"
	card.AdminDraftURL = qualifiedAdminURL(base, "draft-control")
	card.AdminScheduleURL = qualifiedAdminURL(base, "schedule")
	card.AdminWeekCloseURL = qualifiedAdminURL(base, "week-close")
	card.AdminSeatsURL = qualifiedAdminURL(base, "seats")
	card.AdminInvitesURL = qualifiedAdminURL(base, "invites")
	card.AdminOrderURL = qualifiedAdminURL(base, "draft-order")
	card.AdminDataURL = qualifiedAdminURL(base, "data")
	card.AdminClockURL = qualifiedAdminURL(base, "clock")
	card.AdminRosterURL = qualifiedAdminURL(base, "roster")
	card.AdminAnnouncementsURL = qualifiedAdminURL(base, "announcements")
	card.AdminDangerURL = qualifiedAdminURL(base, "danger")
}

func (card fleetCardView) adminURLFor(section string) string {
	if section == "" {
		return card.AdminURL
	}
	return qualifiedAdminURL(card.PublicURL, section)
}

func (card fleetCardView) NameOrPeer() string {
	if strings.TrimSpace(card.Name) != "" {
		return card.Name
	}
	return card.PeerID
}

func (view fleetPageView) toData() map[string]any {
	cards := make([]map[string]any, 0, len(view.Cards))
	for _, card := range view.Cards {
		cards = append(cards, card.toMap())
	}
	queue := make([]map[string]any, 0, len(view.Attention))
	for _, item := range view.Attention {
		queue = append(queue, item.toMap())
	}
	return map[string]any{
		"cards": cards, "attention_queue": queue, "league_count": view.LeagueCount,
		"claimed_seats": view.ClaimedSeats, "total_seats": view.TotalSeats,
		"drafts_live": view.DraftsLive, "attention_count": view.AttentionCount,
		"critical_count": view.CriticalCount, "warning_count": view.WarningCount,
		"generated_at": displayTime(view.Location, view.GeneratedAt), "generated_at_iso": isoTime(view.GeneratedAt),
	}
}

func (item attentionView) toMap() map[string]any {
	return map[string]any{
		"league": item.League, "peer_id": item.PeerID, "code": item.Code,
		"severity": item.Severity, "count": item.Count, "message": item.Message,
		"area": item.Area, "section": item.Section, "owner_url": item.OwnerURL,
		"owner_text": item.OwnerText,
	}
}

func (card fleetCardView) toMap() map[string]any {
	attention := make([]map[string]any, 0, len(card.Attention))
	for _, item := range card.Attention {
		attention = append(attention, item.toMap())
	}
	return map[string]any{
		"available": card.Available, "peer_id": card.PeerID, "name": card.Name,
		"short_code": card.ShortCode, "mode": card.Mode, "season": card.Season,
		"public_url": card.PublicURL, "host_label": card.HostLabel, "error": card.Error,
		"runtime_ready": card.RuntimeReady, "app_version": card.AppVersion,
		"framework_version": card.FrameworkVersion, "git_sha": card.GitSHA, "build": card.Build,
		"generated_at": card.GeneratedAt, "generated_at_iso": card.GeneratedAtISO,
		"seats": card.Seats, "claimed_seats": card.ClaimedSeats, "ready_seats": card.ReadySeats,
		"seat_ledger": card.SeatLedger, "draft_status": card.DraftStatus, "draft_started": card.DraftStarted,
		"draft_started_at": card.DraftStartedAt, "draft_started_at_iso": card.DraftStartedAtISO,
		"draft_at": card.DraftAt, "draft_at_iso": card.DraftAtISO, "draft_at_relative": card.DraftAtRelative, "draft_rounds": card.DraftRounds,
		"draft_picks": card.DraftPicks, "draft_slots": card.DraftSlots, "draft_order": card.DraftOrder,
		"draft_order_set": card.DraftOrderSet, "clock_armed": card.ClockArmed, "clock_paused": card.ClockPaused,
		"clock_text": card.ClockText, "draft_start_copy": card.DraftStartCopy,
		"season_phase": card.SeasonPhase, "current_week": card.CurrentWeek,
		"schedule_published": card.SchedulePublished, "schedule_text": card.ScheduleText,
		"schedule_final_text": card.ScheduleFinalText, "schedule_range": card.ScheduleRange,
		"redraw_locked": card.RedrawLocked, "redraw_lock_reason": card.RedrawLockReason,
		"week_close_week": card.WeekCloseWeek, "week_close_ready": card.WeekCloseReady,
		"week_close_games": card.WeekCloseGames, "week_close_total": card.WeekCloseTotal,
		"week_close_final": card.WeekCloseFinal, "week_close_stats": card.WeekCloseStats,
		"week_close_stats_at": card.WeekCloseStatsAt, "week_close_badge": card.WeekCloseBadge,
		"week_close_waiting": card.WeekCloseWaiting, "week_close_waiting_reason": card.WeekCloseWaitingReason,
		"playoff_available": card.PlayoffAvailable, "playoff_status": card.PlayoffStatus,
		"playoff_status_label": card.PlayoffStatusLabel, "playoff_source": card.PlayoffSource,
		"playoff_next_matchups": card.PlayoffNextMatchups, "playoff_note": card.PlayoffNote,
		"pool_mode": card.PoolMode, "pool_actual": card.PoolActual, "pool_roster_capacity": card.PoolRosterCapacity,
		"pool_target": card.PoolTarget, "pool_cushion": card.PoolCushion, "pool_shortfall": card.PoolShortfall,
		"pool_actual_coverage": card.PoolActualCoverage, "pool_target_coverage": card.PoolTargetCoverage,
		"pool_roster_coverage": card.PoolRosterCoverage, "pool_last_sync": card.PoolLastSync,
		"open_data": card.OpenData, "attention": attention, "has_attention": card.HasAttention,
		"home_url": card.HomeURL, "admin_url": card.AdminURL, "draft_url": card.DraftURL,
		"admin_draft_url": card.AdminDraftURL, "admin_schedule_url": card.AdminScheduleURL,
		"admin_week_close_url": card.AdminWeekCloseURL, "admin_seats_url": card.AdminSeatsURL,
		"admin_invites_url": card.AdminInvitesURL, "admin_order_url": card.AdminOrderURL,
		"admin_data_url": card.AdminDataURL, "admin_clock_url": card.AdminClockURL,
		"admin_roster_url": card.AdminRosterURL, "admin_announcements_url": card.AdminAnnouncementsURL,
		"admin_danger_url": card.AdminDangerURL,
	}
}

func openDataRows(data commissionerhq.OpenData, location *time.Location) []map[string]any {
	return []map[string]any{
		{"label": "SCHEDULES", "state": dataStateLabel(data.Schedules.State), "updated": displayTime(location, data.Schedules.LastUpdated)},
		{"label": "PLAYER STATS", "state": dataStateLabel(data.PlayerStats.State), "updated": displayTime(location, data.PlayerStats.LastUpdated)},
		{"label": "PREVIOUS STATS", "state": dataStateLabel(data.PlayerStatsPrev.State), "updated": displayTime(location, data.PlayerStatsPrev.LastUpdated)},
		{"label": "INJURIES", "state": dataStateLabel(data.Injuries.State), "updated": displayTime(location, data.Injuries.LastUpdated)},
		{"label": "TEAM STATS", "state": dataStateLabel(data.TeamStats.State), "updated": displayTime(location, data.TeamStats.LastUpdated)},
		{"label": "PLAY-BY-PLAY", "state": dataStateLabel(data.PlayByPlay.State), "updated": displayTime(location, data.PlayByPlay.LastUpdated)},
	}
}

func attentionSection(item commissionerhq.Attention) string {
	switch item.Code {
	case "draft_not_started", "draft_start_required", "draft_order_unset", "draft_clock_not_armed":
		return "draft-control"
	case "week_close_ready", "week_close_waiting":
		return "week-close"
	case "schedule_missing", "schedule_unpublished", "schedule_redraw_locked":
		return "schedule"
	case "week_close_blocked", "week_close_not_ready":
		return "week-close"
	case "seat_shortfall", "seats_unclaimed":
		return "seats"
	case "invite_pending", "invite_expired":
		return "invites"
	case "pool_shortfall", "pool_stale", "stats_stale", "injuries_stale":
		return "data"
	case "roster_shape":
		return "roster"
	case "announcement_pending":
		return "announcements"
	case "dangerous_state":
		return "danger"
	}
	switch strings.ToLower(item.Area) {
	case "draft":
		return "draft-control"
	case "schedule":
		return "schedule"
	case "week_close":
		return "week-close"
	case "membership", "seats":
		return "seats"
	case "invites":
		return "invites"
	case "data", "pool":
		return "data"
	case "clock":
		return "clock"
	case "roster":
		return "roster"
	case "announcements":
		return "announcements"
	case "danger":
		return "danger"
	default:
		return ""
	}
}

func attentionOwnerText(section string) string {
	if section == "" {
		return "Open league admin"
	}
	return "Open " + strings.ReplaceAll(section, "-", " ")
}

var knownAdminSections = map[string]bool{
	"draft-control": true, "schedule": true, "week-close": true, "seats": true,
	"invites": true, "draft-order": true, "data": true, "clock": true,
	"roster": true, "announcements": true, "danger": true,
}

func qualifiedAdminURL(publicURL, section string) string {
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if base == "" {
		return ""
	}
	if section == "" || !knownAdminSections[section] {
		return base + "/admin"
	}
	return base + "/admin?section=" + url.QueryEscape(section) + "#admin-" + section
}

func safePeerError(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "No response from this league."
	case "league unavailable", "league unavailable.":
		return "League unavailable."
	case "timed out", "timed out.", "request timed out", "request timed out.":
		return "Request timed out."
	case "trust mismatch", "trust mismatch.":
		return "This league did not answer."
	case "federation not enabled", "federation not enabled.":
		return "This league did not answer."
	case "incompatible summary", "incompatible summary.":
		return "This league did not answer."
	default:
		return "League unavailable."
	}
}

func hostLabel(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

func sourceSHAView(value string) string {
	return strings.TrimSpace(value)
}

// draftStatusLabels turns commissioner_summary.go's draft status constants
// into plain football labels for the fleet card. The default case is a
// safe neutral word, never the raw status token.
var draftStatusLabels = map[string]string{
	"scheduled": "SCHEDULED",
	"live":      "LIVE",
	"complete":  "COMPLETE",
}

func draftStatusLabel(value string) string {
	if label, ok := draftStatusLabels[strings.ToLower(strings.TrimSpace(value))]; ok {
		return label
	}
	return "UNAVAILABLE"
}

// seasonPhaseLabels turns league.Service.SeasonPhase's lifecycle constants
// into plain football labels. The default case is a safe neutral word,
// never the raw phase token.
var seasonPhaseLabels = map[string]string{
	"preseason":       "PRESEASON",
	"regular-season":  "REGULAR SEASON",
	"playoffs":        "PLAYOFFS",
	"season-complete": "SEASON COMPLETE",
}

func seasonPhaseLabel(value string) string {
	if label, ok := seasonPhaseLabels[strings.ToLower(strings.TrimSpace(value))]; ok {
		return label
	}
	return "UNAVAILABLE"
}

// poolModeLabels turns the player pool's runtime mode constants into plain
// football labels. The default case is a safe neutral word, never the raw
// mode token.
var poolModeLabels = map[string]string{
	"live":        "LIVE",
	"cached":      "CACHED SNAPSHOT",
	"stale":       "STALE SNAPSHOT",
	"degraded":    "DEGRADED SNAPSHOT",
	"offline":     "OFFLINE PLAYER LIST",
	"unavailable": "PLAYER DATA UNAVAILABLE",
}

func poolModeLabel(value string) string {
	if label, ok := poolModeLabels[strings.ToLower(strings.TrimSpace(value))]; ok {
		return label
	}
	return "UNAVAILABLE"
}

// severityLabels turns commissionerhq.Attention's severity constants into
// plain football labels. The default case is a safe neutral word, never
// the raw severity token. Casing here must stay in sync with the
// data-severity CSS selectors in public/styles.css.
var severityLabels = map[string]string{
	"info":     "INFO",
	"warning":  "WARNING",
	"critical": "CRITICAL",
}

func severityLabel(value string) string {
	if label, ok := severityLabels[strings.ToLower(strings.TrimSpace(value))]; ok {
		return label
	}
	return "NOTICE"
}

// dataStateLabels turns an open-data dataset's state constants into plain
// football labels. The default case is a safe neutral word, never the raw
// state token.
var dataStateLabels = map[string]string{
	"ready":            "READY",
	"waiting":          "WAITING",
	"disabled":         "OFF",
	"awaiting_release": "NOT OUT YET",
	"error":            "UNAVAILABLE",
}

func dataStateLabel(value string) string {
	if label, ok := dataStateLabels[strings.ToLower(strings.TrimSpace(value))]; ok {
		return label
	}
	return "UNAVAILABLE"
}

// displayTime formats value in location, the caller's explicit
// *time.Location (never resolved from a package-level global here — see
// buildFleetView's doc comment). Un-converted formatting used to print
// value's own embedded offset regardless of which league was viewing it,
// which for the UTC instants commissionerhq's wire summaries actually
// carry rendered a literal "UTC" abbreviation on every card instead of the
// viewing commissioner's own league zone (2026-09-01 audit).
func displayTime(location *time.Location, value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	if location == nil {
		location = time.UTC
	}
	return value.In(location).Format("Jan 2, 2006 · 3:04 PM MST")
}

func isoTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

// relativeText renders league.RelativeTime's compact "N unit(s) ago" label
// for a past instant. A zero or future value renders no relative text: a
// draft meeting three days out is not usefully described by the same
// past-oriented helper the rest of the app uses for freshness labels, and
// a fabricated "in the future" phrasing is worse than omitting the line.
func relativeText(now, value time.Time) string {
	if value.IsZero() || value.After(now) {
		return ""
	}
	return league.RelativeTime(now, value)
}

func weekCloseBadge(close commissionerhq.WeekClose) string {
	if close.Final {
		return "FINAL"
	}
	if close.Ready {
		return "READY"
	}
	return "WAITING"
}

func weekCloseWaitingReason(close commissionerhq.WeekClose) string {
	if weekCloseBadge(close) != "WAITING" {
		return ""
	}
	reason := strings.TrimSpace(close.Reason)
	if reason == "" {
		return "Waiting for the week-close readiness checks."
	}
	const maxReasonRunes = 160
	runes := []rune(reason)
	if len(runes) > maxReasonRunes {
		return string(runes[:maxReasonRunes]) + "…"
	}
	return reason
}

func ratio(value float64) string {
	return fmt.Sprintf("%.1f×", value)
}

func orderText(order []int) string {
	if len(order) == 0 {
		return "Default configured order"
	}
	parts := make([]string, 0, len(order))
	for _, ordinal := range order {
		parts = append(parts, strconv.Itoa(ordinal))
	}
	return strings.Join(parts, " · ")
}

func clockText(draft commissionerhq.Draft) string {
	if !draft.ClockArmed {
		return "Not armed"
	}
	if draft.ClockPaused {
		return "Paused"
	}
	if draft.ClockRemainingSec <= 0 {
		return "Expired"
	}
	return fmt.Sprintf("%dm %02ds remaining", draft.ClockRemainingSec/60, draft.ClockRemainingSec%60)
}

func scheduleRange(schedule commissionerhq.Schedule) string {
	if schedule.StartWeek == 0 && schedule.EndWeek == 0 {
		return "No week range"
	}
	return fmt.Sprintf("Weeks %d–%d", schedule.StartWeek, schedule.EndWeek)
}

func scheduleText(schedule commissionerhq.Schedule) string {
	if !schedule.Published {
		return "Schedule not published"
	}
	return fmt.Sprintf("%d weeks · %d matchups · current week %d", schedule.WeekCount, schedule.TotalMatchups, schedule.CurrentWeek)
}

func finalText(schedule commissionerhq.Schedule) string {
	return fmt.Sprintf("%d/%d weeks final · %d/%d matchups final", schedule.FinalWeeks, schedule.WeekCount, schedule.FinalMatchups, schedule.TotalMatchups)
}

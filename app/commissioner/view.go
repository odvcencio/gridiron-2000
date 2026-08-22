package commissioner

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

// The commissioner readout deliberately uses a PII-free view model. It is
// shared by the full SSR page and its route/data contracts so every card
// uses one meaning for the whole commissioner readout.
type fleetPageView struct {
	GeneratedAt    time.Time
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
	DraftRounds       int
	DraftPicks        int
	DraftSlots        int
	DraftOrder        string
	DraftOrderSet     bool
	ClockArmed        bool
	ClockPaused       bool
	ClockText         string
	DraftStartCopy    string

	SeasonPhase       string
	CurrentWeek       int
	SchedulePublished bool
	ScheduleText      string
	ScheduleFinalText string
	ScheduleRange     string
	RedrawLocked      bool
	RedrawLockReason  string
	WeekCloseWeek     int
	WeekCloseReady    bool
	WeekCloseGames    int
	WeekCloseTotal    int
	WeekCloseFinal    bool
	WeekCloseStats    bool
	WeekCloseStatsAt  string
	WeekCloseReason   string
	PlayoffAvailable  bool
	PlayoffNote       string

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

func buildFleetView(entries []commissionerhq.FleetEntry, generatedAt time.Time) fleetPageView {
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	view := fleetPageView{GeneratedAt: generatedAt, Cards: make([]fleetCardView, 0, len(entries))}
	for _, entry := range entries {
		card := cardView(entry)
		view.Cards = append(view.Cards, card)
		view.LeagueCount++
		if !card.Available {
			attention := attentionView{
				League: card.NameOrPeer(), PeerID: card.PeerID, Code: "peer_unavailable",
				Severity: "warning", Count: 1, Message: "League unavailable: " + card.Error,
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

func cardView(entry commissionerhq.FleetEntry) fleetCardView {
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
	card := fleetCardView{
		Available: true, PeerID: entry.PeerID, Name: summary.Instance.Name,
		ShortCode: summary.Instance.ShortCode, Mode: summary.Instance.Mode,
		Season: summary.Instance.Season, PublicURL: publicURL, HostLabel: hostLabel(publicURL),
		RuntimeReady: summary.Runtime.Ready, AppVersion: summary.Runtime.AppVersion,
		FrameworkVersion: summary.Runtime.FrameworkVersion, GitSHA: shortSHAView(summary.Runtime.GitSHA),
		Build: summary.Runtime.Build, GeneratedAt: displayTime(summary.GeneratedAt),
		GeneratedAtISO: isoTime(summary.GeneratedAt), Seats: summary.Membership.Seats,
		ClaimedSeats: summary.Membership.ClaimedSeats, ReadySeats: summary.Membership.ReadySeats,
		DraftStatus: upperWords(summary.Draft.Status), DraftStarted: summary.Draft.Started,
		DraftStartedAt: displayTime(summary.Draft.StartedAt), DraftStartedAtISO: isoTime(summary.Draft.StartedAt),
		DraftAt: displayTime(summary.Draft.ScheduledAt), DraftAtISO: isoTime(summary.Draft.ScheduledAt),
		DraftRounds: summary.Draft.Rounds, DraftPicks: summary.Draft.Picks,
		DraftSlots: summary.Pool.RosterCapacity, DraftOrder: orderText(summary.Draft.Order),
		DraftOrderSet: summary.Draft.OrderSet, ClockArmed: summary.Draft.ClockArmed,
		ClockPaused: summary.Draft.ClockPaused, ClockText: clockText(summary.Draft),
		DraftStartCopy: "The commissioner starts drafts intentionally; the scheduled time is only the meeting point.",
		SeasonPhase:    upperWords(summary.Season.Phase), CurrentWeek: summary.Season.CurrentWeek,
		SchedulePublished: summary.Season.Schedule.Published,
		ScheduleText:      scheduleText(summary.Season.Schedule), ScheduleFinalText: finalText(summary.Season.Schedule),
		ScheduleRange: scheduleRange(summary.Season.Schedule), RedrawLocked: summary.Season.Schedule.RedrawLocked,
		RedrawLockReason: summary.Season.Schedule.RedrawLockReason,
		WeekCloseWeek:    summary.Season.WeekClose.Week, WeekCloseReady: summary.Season.WeekClose.Ready,
		WeekCloseGames: summary.Season.WeekClose.GamesFinal, WeekCloseTotal: summary.Season.WeekClose.GamesTotal,
		WeekCloseFinal: summary.Season.WeekClose.Final, WeekCloseStats: summary.Season.WeekClose.StatsFresh,
		WeekCloseStatsAt: displayTime(summary.Season.WeekClose.StatsUpdatedAt), WeekCloseReason: summary.Season.WeekClose.Reason,
		PlayoffAvailable: summary.Season.Playoffs.Available, PlayoffNote: summary.Season.Playoffs.Note,
		PoolMode: upperWords(summary.Pool.Mode), PoolActual: summary.Pool.Actual,
		PoolRosterCapacity: summary.Pool.RosterCapacity, PoolTarget: summary.Pool.Target,
		PoolCushion: summary.Pool.Cushion, PoolShortfall: summary.Pool.Shortfall,
		PoolActualCoverage: ratio(summary.Pool.ActualCoverage), PoolTargetCoverage: ratio(summary.Pool.TargetCoverage),
		PoolRosterCoverage: ratio(summary.Pool.RosterCoverage), PoolLastSync: displayTime(summary.Pool.LastSync),
	}
	for _, seat := range summary.Membership.SeatLedger {
		card.SeatLedger = append(card.SeatLedger, map[string]any{
			"seat": seat.Seat, "claimed": seat.Claimed, "ready": seat.Ready,
		})
	}
	card.OpenData = openDataRows(summary.OpenData)
	for _, item := range summary.Attention {
		section := attentionSection(item)
		attention := attentionView{
			League: card.NameOrPeer(), PeerID: card.PeerID, Code: item.Code,
			Severity: upperWords(item.Severity), Count: item.Count, Message: item.Message,
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
		"generated_at": displayTime(view.GeneratedAt), "generated_at_iso": isoTime(view.GeneratedAt),
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
		"draft_at": card.DraftAt, "draft_at_iso": card.DraftAtISO, "draft_rounds": card.DraftRounds,
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
		"week_close_stats_at": card.WeekCloseStatsAt, "week_close_reason": card.WeekCloseReason,
		"playoff_available": card.PlayoffAvailable, "playoff_note": card.PlayoffNote,
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

func openDataRows(data commissionerhq.OpenData) []map[string]any {
	return []map[string]any{
		{"label": "SCHEDULES", "state": upperWords(data.Schedules.State), "updated": displayTime(data.Schedules.LastUpdated)},
		{"label": "PLAYER STATS", "state": upperWords(data.PlayerStats.State), "updated": displayTime(data.PlayerStats.LastUpdated)},
		{"label": "PREVIOUS STATS", "state": upperWords(data.PlayerStatsPrev.State), "updated": displayTime(data.PlayerStatsPrev.LastUpdated)},
		{"label": "INJURIES", "state": upperWords(data.Injuries.State), "updated": displayTime(data.Injuries.LastUpdated)},
		{"label": "TEAM STATS", "state": upperWords(data.TeamStats.State), "updated": displayTime(data.TeamStats.LastUpdated)},
		{"label": "PLAY-BY-PLAY", "state": upperWords(data.PlayByPlay.State), "updated": displayTime(data.PlayByPlay.LastUpdated)},
	}
}

func attentionSection(item commissionerhq.Attention) string {
	switch item.Code {
	case "draft_not_started", "draft_start_required", "draft_order_unset", "draft_clock_not_armed":
		return "draft-control"
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
	value = strings.TrimSpace(value)
	if value == "" {
		return "No response from this league."
	}
	if strings.IndexByte(value, 10) >= 0 || strings.IndexByte(value, 13) >= 0 {
		return "League unavailable."
	}
	return value
}

func hostLabel(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

func shortSHAView(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func upperWords(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "_", " "), "-", " "))
}

func displayTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("Jan 2, 2006 · 3:04 PM MST")
}

func isoTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
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
	return fmt.Sprintf("%d/%d weeks final · %d/%d games final", schedule.FinalWeeks, schedule.WeekCount, schedule.FinalMatchups, schedule.TotalMatchups)
}

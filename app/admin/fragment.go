package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

type adminAttentionReadoutProps struct {
	// SeasonStateSentence (F1, J4 console gap-audit) is the console's
	// top-line season summary in plain words, replacing the raw
	// "{Phase} · {DraftStatus}" concatenation that used to read as "the
	// season is over" during week 1.
	SeasonStateSentence string
	// DraftComplete (F1) gates the "Draft deadline" line below: a
	// completed draft's deadline is no longer news, so it renders only
	// while a draft is still pending.
	DraftComplete       bool
	Phase               string
	DraftStatus         string
	DraftDate           string
	DraftTime           string
	DraftPublished      bool
	ScheduleStatus string
	// ScheduleWeek (coordinator truth follow-up, wave C, 2026-09-08) is
	// the next OPEN week, not the schedule's start week — the same fact
	// nextOpenScheduleWeek (season.go) and the masthead's own
	// data.schedule.close.week already name, so a closed week 1 reads
	// Week 2 here too instead of staying stuck on Week 1.
	ScheduleWeek   int
	ScheduleReady  bool
	ScheduleReason string
	// ScheduleFinal (same follow-up) is true once every week in the
	// schedule is closed — nextOpenScheduleWeek then falls back to the
	// schedule's first week, which would otherwise misread as "week 1 is
	// still open." This card states the true, more useful fact instead.
	ScheduleFinal bool
	// ScheduleProgressSentence (coordinator addendum, wave C, 2026-09-08)
	// is weekProgressSentence's own words for this week — the same
	// function the attention line above already reads — so this card's
	// first line never glues a close-readiness reason ("waiting for 16
	// of 16 games to go final") onto a week that has not kicked off yet.
	ScheduleProgressSentence string
	SeatCount           int
	ClaimedCount        int
	ReadyCount          int
	InviteCount         int
	BoardGapCount       int
	// OpenClaimCount/TradesInReviewCount (F2, J4 console gap-audit) back
	// the post-draft week summary: once the draft is complete, the week's
	// own open work leads the panel instead of draft-night seat/board
	// telemetry.
	OpenClaimCount      int
	TradesInReviewCount int
	PresenceHere        int
	PresenceIdle        int
	PresenceAway        int
	PresenceNotSeen     int
	PresenceUnclaimed   int
	GeneratedAt         string
	GeneratedAtISO      string
	GeneratedAtRelative string
	Seats               []adminAttentionSeatView
	// HasNotCheckedIn/NotCheckedInSummary (F6 + F19, gap-audit J2): the
	// console used to report "READY 4 / 8" with no way to see WHO that
	// left out or act on it — the ready toggles exist only in the draft
	// room's own drawer. This names the claimed-but-not-ready seats by
	// their manager's own first name plain-language, next to a link
	// straight into the room where the toggle actually lives.
	HasNotCheckedIn     bool
	NotCheckedInSummary string
}

type adminAttentionSeatView struct {
	Name             string
	Abbreviation     string
	Manager          string
	ManagerFirstName string
	Claimed          bool
	Ready            bool
	Presence         string
	PresenceLabel    string
	PresenceDetail   string
	BoardCount       int
	BoardGap         bool
}

// firstNameOf returns name's first whitespace-separated token, or name
// itself when it carries no space (a single-word display name, or
// already empty).
func firstNameOf(name string) string {
	name = strings.TrimSpace(name)
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return name
}

func emptyAdminAttentionReadout() adminAttentionReadoutProps {
	return adminAttentionReadoutProps{Seats: []adminAttentionSeatView{}}
}

func adminAttentionReadoutFromData(data map[string]any) adminAttentionReadoutProps {
	view := emptyAdminAttentionReadout()
	view.SeasonStateSentence = stringValue(data, "season_state_sentence", "")
	view.Phase = stringValue(data, "phase", "unavailable")
	view.SeatCount = intValue(data, "seat_count")
	view.ClaimedCount = intValue(data, "claimed_count")
	view.ReadyCount = intValue(data, "ready_count")
	view.InviteCount = intValue(data, "invite_count")
	view.BoardGapCount = intValue(data, "board_gap_count")
	view.OpenClaimCount = intValue(data, "open_claim_count")
	view.TradesInReviewCount = intValue(data, "trades_in_review_count")
	view.PresenceHere = intValue(data, "presence_here")
	view.PresenceIdle = intValue(data, "presence_idle")
	view.PresenceAway = intValue(data, "presence_away")
	view.PresenceNotSeen = intValue(data, "presence_not_seen")
	view.PresenceUnclaimed = intValue(data, "presence_unclaimed")
	view.GeneratedAt = stringValue(data, "generated_at", "UNKNOWN")
	view.GeneratedAtISO = stringValue(data, "generated_at_iso", "")
	view.GeneratedAtRelative = stringValue(data, "generated_at_relative", "")
	if draft, ok := data["draft"].(map[string]any); ok {
		view.DraftStatus = stringValue(draft, "status", "UNKNOWN")
		// date/time (item 5, 2026-09-02 audit): the readout used to print
		// "at"'s own raw RFC3339 instant. date/time are the same
		// league-local formatted facts data.draft.date/data.draft.time
		// already show elsewhere on this page (draftSummaryForState).
		view.DraftDate = stringValue(draft, "date", "TBD")
		view.DraftTime = stringValue(draft, "time", "")
		view.DraftPublished = boolValue(draft, "published")
		view.DraftComplete = boolValue(draft, "complete")
	}
	if schedule, ok := data["schedule"].(map[string]any); ok {
		view.ScheduleStatus = stringValue(schedule, "status", "UNKNOWN")
		if close, ok := schedule["close"].(map[string]any); ok {
			// ScheduleWeek (coordinator truth follow-up, wave C,
			// 2026-09-08): this used to read schedule["week"], which
			// adminScheduleMap (admin.go) stamps with the schedule's own
			// START week, not the next OPEN one — a closed week 1 never
			// advanced this card to Week 2. close["week"] is the same
			// next-open-week fact the masthead already reads.
			view.ScheduleWeek = intValue(close, "week")
			view.ScheduleReady = boolValue(close, "ready")
			view.ScheduleReason = stringValue(close, "reason", "")
			view.ScheduleFinal = boolValue(close, "final")
			view.ScheduleProgressSentence = stringValue(close, "progress_sentence", "")
		}
	}
	if rawSeats, ok := data["seats"].([]map[string]any); ok {
		view.Seats = make([]adminAttentionSeatView, 0, len(rawSeats))
		notCheckedIn := make([]string, 0, len(rawSeats))
		for _, seat := range rawSeats {
			manager := stringValue(seat, "manager", "")
			claimed := boolValue(seat, "claimed")
			ready := boolValue(seat, "ready")
			view.Seats = append(view.Seats, adminAttentionSeatView{
				Name:             stringValue(seat, "name", "UNKNOWN"),
				Abbreviation:     stringValue(seat, "abbreviation", ""),
				Manager:          manager,
				ManagerFirstName: firstNameOf(manager),
				Claimed:          claimed,
				Ready:            ready,
				Presence:         stringValue(seat, "presence", "not_seen"),
				PresenceLabel:    stringValue(seat, "presence_label", "Not seen yet"),
				// FriendlyPresenceDetail (F19): "No room heartbeat since
				// this server started." read as a server error, not the
				// plain fact that nobody from that seat has opened the
				// room yet — the exact same rewrite the draft room's own
				// commissioner drawer already carries.
				PresenceDetail: league.FriendlyPresenceDetail(stringValue(seat, "presence_detail", "No presence report.")),
				BoardCount:     intValue(seat, "board_count"),
				BoardGap:       boolValue(seat, "board_gap"),
			})
			if claimed && !ready {
				label := strings.TrimSpace(manager)
				if label == "" {
					label = stringValue(seat, "name", "that seat")
				} else {
					label = firstNameOf(manager) + " (" + stringValue(seat, "name", "") + ")"
				}
				notCheckedIn = append(notCheckedIn, label)
			}
		}
		view.HasNotCheckedIn = len(notCheckedIn) > 0
		view.NotCheckedInSummary = strings.Join(notCheckedIn, ", ")
	}
	return view
}

func stringValue(data map[string]any, key, fallback string) string {
	value, ok := data[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func intValue(data map[string]any, key string) int {
	value, _ := data[key].(int)
	return value
}

func boolValue(data map[string]any, key string) bool {
	value, _ := data[key].(bool)
	return value
}

// AdminAttentionFragmentHandler serves the read-only local commissioner
// operations strip. It shares the console's authorization boundary but never
// invokes AdminData, Viewer, or a write action while polling.
func AdminAttentionFragmentHandler(service *league.Service) http.Handler {
	return adminAttentionFragmentHandler(
		adminAttentionAccess(service),
		func(request *http.Request) adminAttentionReadoutProps {
			if service == nil {
				return emptyAdminAttentionReadout()
			}
			return adminAttentionReadoutFromData(service.CommissionerAttentionDataReadOnly(request))
		},
		adminAttentionFragmentRender,
	)
}

func adminAttentionAccess(service *league.Service) func(*http.Request) (int, bool) {
	return func(request *http.Request) (int, bool) {
		if service == nil {
			return http.StatusServiceUnavailable, false
		}
		if !service.DemoMode() {
			if _, signedIn := service.CurrentUser(request); !signedIn {
				return http.StatusUnauthorized, false
			}
		}
		if !service.IsCommissioner(request) {
			return http.StatusForbidden, false
		}
		return 0, true
	}
}

type adminAttentionLoader func(*http.Request) adminAttentionReadoutProps
type adminAttentionRenderer func(adminAttentionReadoutProps) (string, error)

func adminAttentionFragmentHandler(
	access func(*http.Request) (int, bool),
	load adminAttentionLoader,
	render adminAttentionRenderer,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setAdminAttentionPrivacyHeaders(writer)
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if access == nil {
			http.Error(writer, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		if status, allowed := access(request); !allowed {
			http.Error(writer, http.StatusText(status), status)
			return
		}
		if load == nil || render == nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		html, err := render(load(request))
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		etag := adminAttentionETag(html)
		writer.Header().Set("ETag", etag)
		if adminAttentionETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setAdminAttentionPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func adminAttentionETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func adminAttentionETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func adminAttentionFragmentRender(props adminAttentionReadoutProps) (string, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return "", err
	}
	return route.RenderProgramComponent(program, "AdminAttentionReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": props},
	})
}

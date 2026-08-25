package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

const adminAttentionFragmentInterval = "4s"

type adminAttentionReadoutProps struct {
	Phase             string
	DraftStatus       string
	DraftAt           string
	ScheduleStatus    string
	ScheduleWeek      int
	ScheduleReady     bool
	ScheduleReason    string
	SeatCount         int
	ClaimedCount      int
	ReadyCount        int
	InviteCount       int
	BoardGapCount     int
	PresenceHere      int
	PresenceIdle      int
	PresenceAway      int
	PresenceNotSeen   int
	PresenceUnclaimed int
	GeneratedAt       string
	Seats             []adminAttentionSeatView
}

type adminAttentionSeatView struct {
	Name           string
	Abbreviation   string
	Claimed        bool
	Ready          bool
	Presence       string
	PresenceDetail string
	BoardCount     int
	BoardGap       bool
}

func emptyAdminAttentionReadout() adminAttentionReadoutProps {
	return adminAttentionReadoutProps{Seats: []adminAttentionSeatView{}}
}

func adminAttentionReadoutFromData(data map[string]any) adminAttentionReadoutProps {
	view := emptyAdminAttentionReadout()
	view.Phase = stringValue(data, "phase", "unavailable")
	view.SeatCount = intValue(data, "seat_count")
	view.ClaimedCount = intValue(data, "claimed_count")
	view.ReadyCount = intValue(data, "ready_count")
	view.InviteCount = intValue(data, "invite_count")
	view.BoardGapCount = intValue(data, "board_gap_count")
	view.PresenceHere = intValue(data, "presence_here")
	view.PresenceIdle = intValue(data, "presence_idle")
	view.PresenceAway = intValue(data, "presence_away")
	view.PresenceNotSeen = intValue(data, "presence_not_seen")
	view.PresenceUnclaimed = intValue(data, "presence_unclaimed")
	view.GeneratedAt = stringValue(data, "generated_at", "UNKNOWN")
	if draft, ok := data["draft"].(map[string]any); ok {
		view.DraftStatus = stringValue(draft, "status", "UNKNOWN")
		view.DraftAt = stringValue(draft, "at", "UNKNOWN")
	}
	if schedule, ok := data["schedule"].(map[string]any); ok {
		view.ScheduleStatus = stringValue(schedule, "status", "UNKNOWN")
		view.ScheduleWeek = intValue(schedule, "week")
		if close, ok := schedule["close"].(map[string]any); ok {
			view.ScheduleReady = boolValue(close, "ready")
			view.ScheduleReason = stringValue(close, "reason", "")
		}
	}
	if rawSeats, ok := data["seats"].([]map[string]any); ok {
		view.Seats = make([]adminAttentionSeatView, 0, len(rawSeats))
		for _, seat := range rawSeats {
			view.Seats = append(view.Seats, adminAttentionSeatView{
				Name:           stringValue(seat, "name", "UNKNOWN"),
				Abbreviation:   stringValue(seat, "abbreviation", ""),
				Claimed:        boolValue(seat, "claimed"),
				Ready:          boolValue(seat, "ready"),
				Presence:       stringValue(seat, "presence", "not_seen"),
				PresenceDetail: stringValue(seat, "presence_detail", "No presence report."),
				BoardCount:     intValue(seat, "board_count"),
				BoardGap:       boolValue(seat, "board_gap"),
			})
		}
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

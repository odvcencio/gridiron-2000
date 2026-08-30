package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

const (
	draftRoomRegion      = "room"
	draftWorkspaceRegion = "workspace"
	draftCommandRegion   = "command"
	draftTapeRegion      = "tape"
	draftAvailableRegion = "available"
	draftQueueRegion     = "queue"

	// draftTapeSinceKey is the tape pane's own "?since=" cursor: a
	// non-negative pick number below which the pane's rows are already on
	// screen. It shares its string with live.go's draftLiveSinceKey by
	// coincidence only (that one is the hub reconnect's fingerprint cursor,
	// on a different endpoint); the two never appear on the same request.
	draftTapeSinceKey = "since"
)

// RoomFragmentHandler returns the authoritative room chrome without the
// player workspace. Periodic GETs deliberately use DraftDataReadOnly so a
// browser tab can never provision membership or persist presence as a side
// effect of observing league state.
func RoomFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftRoomRegion, draftFragmentAccess(service), service.DraftDataReadOnly)
}

// WorkspaceFragmentHandler keeps the query-scoped player pool, personal board,
// and pick tape current without replacing the rest of the draft page.
func WorkspaceFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftWorkspaceRegion, draftFragmentAccess(service), service.DraftDataReadOnly)
}

// CommandFragmentHandler serves the shell's always-visible command bar
// region (Task 5a's app shell). Task 6 refines the ETag/?since behaviour
// this and the three handlers below share with RoomFragmentHandler and
// WorkspaceFragmentHandler above; the mount is load-bearing now, so the
// browser room's countdown and pick-label region swap on a real typed
// event instead of 404ing.
func CommandFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftCommandRegion, draftFragmentAccess(service), service.DraftDataReadOnly)
}

// TapeFragmentHandler serves the pick-history pane's swapped body.
func TapeFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftTapeRegion, draftFragmentAccess(service), service.DraftDataReadOnly)
}

// AvailableFragmentHandler serves the available-players pane's swapped
// body, including the position-filtered ?pos= refetch the chips drive.
func AvailableFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftAvailableRegion, draftFragmentAccess(service), service.DraftDataReadOnly)
}

// QueueFragmentHandler serves the "my team" pane's swapped body.
func QueueFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftQueueRegion, draftFragmentAccess(service), service.DraftDataReadOnly)
}

func draftFragmentAccess(service *league.Service) func(*http.Request) bool {
	return func(request *http.Request) bool {
		if service == nil {
			return false
		}
		if service.DemoMode() {
			return true
		}
		_, signedIn := service.CurrentUser(request)
		return signedIn
	}
}

func draftFragmentHandler(
	region string,
	allowed func(*http.Request) bool,
	load func(*http.Request) map[string]any,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if allowed == nil || !allowed(request) {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if load == nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		prepared := prepareDraftData(load(request))
		prepared = attachDraftFragmentSince(prepared, request)
		view, component, err := draftRegionView(prepared, region)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		etag, err := draftRegionETag(region, view)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		setDraftFragmentHeaders(writer, etag)
		if etagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}

		prepared = attachDraftRequestState(prepared, request)
		view, component, err = draftRegionView(prepared, region)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		program, err := route.LoadFileProgramHere("page.gsx")
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		html, err := route.RenderProgramComponent(program, component, route.ProgramRenderEnv{
			Values: map[string]any{"props": view},
		})
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func draftRegionView(data map[string]any, region string) (any, string, error) {
	switch region {
	case draftRoomRegion:
		view, ok := data["room"].(draftRoomView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftRoom", nil
	case draftWorkspaceRegion:
		view, ok := data["workspace"].(draftWorkspaceView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftWorkspace", nil
	case draftCommandRegion:
		view, ok := data["command"].(draftCommandView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftCommandBar", nil
	case draftTapeRegion:
		view, ok := data["history"].(draftHistoryView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		if view.Since >= 0 {
			return view, "DraftTapeRows", nil
		}
		return view, "DraftHistory", nil
	case draftAvailableRegion:
		view, ok := data["available"].(draftAvailableView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftAvailable", nil
	case draftQueueRegion:
		view, ok := data["queue"].(draftQueueView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftMyTeam", nil
	default:
		return nil, "", errInvalidDraftRegion
	}
}

// attachDraftFragmentSince copies a valid "?since=" into the tape pane's
// history view. A non-negative integer switches draftRegionView's tape case
// from the full DraftHistory render to DraftTapeRows — the rows newer than
// since alone, each preceded by its round header once. A missing,
// negative, or non-numeric "?since=" leaves Since at prepareDraftData's -1
// default, so every other fragment (and a plain GET /draft/fragment/tape)
// keeps rendering the full pane untouched.
func attachDraftFragmentSince(data map[string]any, request *http.Request) map[string]any {
	raw := strings.TrimSpace(request.URL.Query().Get(draftTapeSinceKey))
	if raw == "" {
		return data
	}
	since, err := strconv.Atoi(raw)
	if err != nil || since < 0 {
		return data
	}
	history, ok := data["history"].(draftHistoryView)
	if !ok {
		return data
	}
	history.Since = since
	history.Rows = draftTapeRowsSince(history.Data, since)
	data["history"] = history
	return data
}

var errInvalidDraftRegion = &draftRegionError{}

type draftRegionError struct{}

func (*draftRegionError) Error() string { return "invalid draft region" }

func setDraftFragmentHeaders(writer http.ResponseWriter, etag string) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", etag)
}

func draftRegionETag(region string, view any) (string, error) {
	payload := map[string]any{"region": region, "view": semanticDraftRegionView(view)}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return `"` + hex.EncodeToString(digest[:]) + `"`, nil
}

// semanticDraftRegionView excludes clock text derived only from wall time.
// The browser countdown owns those seconds between authoritative state
// changes; excluding them lets unchanged polls return a bodyless 304. Every
// draft region view shares this same treatment: room, workspace, and
// Task 6's four shell panes (command, tape/history, available, queue) all
// wrap the identical viewData map (prepareDraftData, page.server.go), so
// draftRegionData below extracts .Data by type switch alone.
func semanticDraftRegionView(view any) any {
	data, ok := draftRegionData(view)
	if !ok {
		return view
	}
	copyData := make(map[string]any, len(data))
	for key, value := range data {
		copyData[key] = value
	}
	if clock, ok := data["clock"].(map[string]any); ok {
		stable := make(map[string]any, len(clock))
		for key, value := range clock {
			if key != "server_now" && key != "remaining_seconds" && key != "remaining_label" {
				stable[key] = value
			}
		}
		copyData["clock"] = stable
	}
	if draft, ok := data["draft"].(map[string]any); ok {
		stable := make(map[string]any, len(draft))
		for key, value := range draft {
			if key != "countdown_label" && key != "days_until" {
				stable[key] = value
			}
		}
		copyData["draft"] = stable
	}
	return copyData
}

// draftRegionData extracts the shared viewData map from any of the six
// draft region view types. It returns (nil, false) for anything else, so
// semanticDraftRegionView's caller falls back to hashing the view as-is.
func draftRegionData(view any) (map[string]any, bool) {
	switch typed := view.(type) {
	case draftRoomView:
		return typed.Data, true
	case draftWorkspaceView:
		return typed.Data, true
	case draftCommandView:
		return typed.Data, true
	case draftHistoryView:
		return typed.Data, true
	case draftAvailableView:
		return typed.Data, true
	case draftQueueView:
		return typed.Data, true
	default:
		return nil, false
	}
}

func etagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

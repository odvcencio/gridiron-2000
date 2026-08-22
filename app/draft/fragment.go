package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

const (
	draftRoomRegion      = "room"
	draftWorkspaceRegion = "workspace"
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
	default:
		return nil, "", errInvalidDraftRegion
	}
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
// changes; excluding them lets unchanged polls return a bodyless 304.
func semanticDraftRegionView(view any) any {
	var data map[string]any
	switch typed := view.(type) {
	case draftRoomView:
		data = typed.Data
	case draftWorkspaceView:
		data = typed.Data
	default:
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

func etagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

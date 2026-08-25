package team

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// teamLineupFragmentInterval matches league.PollPeriod, the existing
// /api/league/version convergence cadence, so Team regions use the same
// declared freshness interval as the rest of the signed-in shell.
const teamLineupFragmentInterval = "4s"

// TeamLineupFragmentHandler serves the smallest authoritative Team region
// affected by lineup and roster-zone mutations. It deliberately loads through
// TeamDataReadOnly: a polling GET can observe persisted state, but cannot
// provision membership or mutate league state as a side effect.
func TeamLineupFragmentHandler(service *league.Service) http.Handler {
	return teamLineupFragmentHandler(
		teamFragmentAccess(service),
		func(request *http.Request) map[string]any {
			return service.TeamDataReadOnly(request)
		},
		teamLineupFragmentRender,
	)
}

func teamFragmentAccess(service *league.Service) func(*http.Request) bool {
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

type teamLineupFragmentRenderer func(map[string]any, *http.Request) (string, error)

func teamLineupFragmentHandler(
	allowed func(*http.Request) bool,
	load func(*http.Request) map[string]any,
	render teamLineupFragmentRenderer,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setTeamFragmentPrivacyHeaders(writer)
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if allowed == nil || !allowed(request) {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if load == nil || render == nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		data := prepareTeamData(load(request), request)
		// A seatless viewer must never receive a fragment rendered from the
		// default team fallback. The full Team page uses this same guard.
		if !boolField(data, "has_seat") {
			http.Error(writer, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		html, err := render(data, request)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		etag := teamFragmentETag(html)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("ETag", etag)
		if teamETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setTeamFragmentPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func teamFragmentETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func teamETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func teamLineupFragmentRender(data map[string]any, request *http.Request) (string, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return "", err
	}
	return route.RenderProgramComponent(program, "TeamLineupRegion", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": data,
			"csrf": map[string]any{
				"token": session.Token(request),
				"field": "csrf_token",
			},
		},
		Funcs: map[string]any{
			// The fragment is mounted at /team, so action paths are stable
			// even though it is rendered outside a RouteContext.
			"actionPath": func(name string) string { return "/team/__actions/" + name },
		},
	})
}

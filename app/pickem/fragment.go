package pickem

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

const pickemStateSignal = "$pickem.state.refresh"

// PickemFragmentHandler serves the smallest authoritative Pick'em surface:
// the selected week's counters, per-game market/pick/lock/result state, and
// both scoring boards. It uses the read-only service projection so polling
// cannot reconcile markets, backfill entry timestamps, provision membership,
// or otherwise mutate league state from a GET.
func PickemFragmentHandler(service *league.Service) http.Handler {
	return pickemFragmentHandlerWithRenderer(
		pickemFragmentAccess(service),
		func(request *http.Request) map[string]any {
			return preparePickemData(service.PickemDataReadOnly(request), request, "")
		},
		pickemFragmentRender,
	)
}

func pickemFragmentAccess(service *league.Service) func(*http.Request) bool {
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

type pickemFragmentLoader func(*http.Request) map[string]any
type pickemFragmentRenderer func(map[string]any, *http.Request) (string, error)

func pickemFragmentHandlerWithRenderer(
	allowed func(*http.Request) bool,
	load pickemFragmentLoader,
	render pickemFragmentRenderer,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setPickemFragmentPrivacyHeaders(writer)
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
		data := load(request)
		if data == nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		html, err := render(data, request)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		etag := pickemFragmentETag(html)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("ETag", etag)
		if pickemETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setPickemFragmentPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func pickemFragmentETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func pickemETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func pickemFragmentRender(data map[string]any, request *http.Request) (string, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return "", err
	}
	return route.RenderProgramComponent(program, "PickemLiveRegion", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": data,
		},
	})
}

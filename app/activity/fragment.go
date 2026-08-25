package activity

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

const activityRegionInterval = "4s"
const activityStateSignal = "$players.state.refresh"

// ActivityFragmentHandler serves the authoritative transaction feed as a
// private, no-store HTML region. It is deliberately read-only; the same
// ActivityDataReadOnly projection is used for every client and query state is
// carried in the request URL.
func ActivityFragmentHandler(service *league.Service) http.Handler {
	return activityFragmentHandler(
		activityFragmentAccess(service),
		func(request *http.Request) map[string]any { return service.ActivityDataReadOnly(request) },
	)
}

func activityFragmentAccess(service *league.Service) func(*http.Request) bool {
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

func activityFragmentHandler(allowed func(*http.Request) bool, load func(*http.Request) map[string]any) http.Handler {
	return activityFragmentHandlerWithRenderer(allowed, load, activityFragmentRender)
}

func activityFragmentHandlerWithRenderer(allowed func(*http.Request) bool, load func(*http.Request) map[string]any, render func(map[string]any) (string, error)) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setActivityFragmentPrivacyHeaders(writer)
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
		if transactions, ok := data["transactions"].([]map[string]any); ok {
			data["transactions"] = activityRows(transactions)
		}
		html, err := render(data)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		etag := activityFragmentETag(html)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("ETag", etag)
		if activityETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setActivityFragmentPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func activityFragmentETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func activityETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func activityFragmentRender(data map[string]any) (string, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return "", err
	}
	return route.RenderProgramComponent(program, "ActivityRegion", route.ProgramRenderEnv{
		Values: map[string]any{"data": data},
	})
}

package locker

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// LockerFragmentHandler serves the board region as a private, no-store
// HTML fragment (activity's ActivityFragmentHandler precedent). Access is
// the same layered rule every league route uses: signed in, or demo mode
// — the finer "admitted" distinction Locker Room posting itself requires
// stays inside LockerData's own admitted/can_post/read_only_reason
// fields, exactly as pickem's own admission check lives only at its
// mutation boundary, not this GET layer.
func LockerFragmentHandler(service *league.Service) http.Handler {
	return lockerFragmentHandler(
		lockerFragmentAccess(service),
		func(request *http.Request) map[string]any {
			data := service.LockerDataReadOnly(request)
			return prepareLockerData(data, request, "", "", session.Token(request))
		},
	)
}

func lockerFragmentAccess(service *league.Service) func(*http.Request) bool {
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

func lockerFragmentHandler(allowed func(*http.Request) bool, load func(*http.Request) map[string]any) http.Handler {
	return lockerFragmentHandlerWithRenderer(allowed, load, lockerFragmentRender)
}

func lockerFragmentHandlerWithRenderer(allowed func(*http.Request) bool, load func(*http.Request) map[string]any, render func(map[string]any) (string, error)) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setLockerFragmentPrivacyHeaders(writer)
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
		html, err := render(data)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		etag := lockerFragmentETag(html)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("ETag", etag)
		if lockerETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setLockerFragmentPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func lockerFragmentETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func lockerETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func lockerFragmentRender(data map[string]any) (string, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return "", err
	}
	return route.RenderProgramComponent(program, "LockerBoard", route.ProgramRenderEnv{
		Values: map[string]any{"data": data},
	})
}

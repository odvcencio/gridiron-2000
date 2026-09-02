package trades

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// Trade Desk convergence uses the same four-second cadence as the league
// heartbeat. Action responses publish the same signal for an immediate
// refresh; the interval remains the recovery path when a client misses it.
const tradesRegionInterval = "4s"
const tradesStateSignal = "$trades.state.refresh"

type tradesFragmentRenderer func(map[string]any, *http.Request) (string, error)

// TradeDeskFragmentHandler serves the full read-only Trade Desk region. The
// GET boundary includes composer/open/history/review/vote state while keeping
// the masthead and notices stable, so a refresh cannot replace the document
// or reset unrelated page state.
func TradeDeskFragmentHandler(service *league.Service) http.Handler {
	return tradesFragmentHandler(
		tradesFragmentAccess(service),
		func(request *http.Request) map[string]any {
			data := service.TradesDataReadOnly(request)
			// The 4-second polled region reads TradeDeskRegion — the same
			// page.gsx component the full page render uses — so it needs
			// the same empty_inbox_message key page.server.go's Load sets
			// (build item 3), or a refresh would silently drop the
			// accepted-trade-in-review nudge on the next poll.
			data["empty_inbox_message"] = emptyInboxMessage(tradesAttentionCount(data))
			return data
		},
		tradesFragmentRender,
	)
}

func tradesFragmentAccess(service *league.Service) func(*http.Request) bool {
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

func tradesFragmentHandler(allowed func(*http.Request) bool, load func(*http.Request) map[string]any, render tradesFragmentRenderer) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setTradesFragmentPrivacyHeaders(writer)
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
		etag := tradesFragmentETag(html)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("ETag", etag)
		if tradesETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setTradesFragmentPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func tradesFragmentETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func tradesETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func tradesFragmentRender(data map[string]any, request *http.Request) (string, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return "", err
	}
	return route.RenderProgramComponent(program, "TradeDeskRegion", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": data,
			"csrf": map[string]any{
				"token": session.Token(request),
				"field": "csrf_token",
			},
		},
		Funcs: map[string]any{
			"actionPath": func(name string) string { return "/trades/__actions/" + name },
		},
	})
}

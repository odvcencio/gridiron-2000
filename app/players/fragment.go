package players

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// These regions deliberately share the league heartbeat cadence. A second
// signed-in client therefore observes a successful waiver or roster change in
// at most one declared poll interval, while GoSX's interval guard protects a
// focused control and an in-progress search/filter submission.
const playersRegionInterval = "4s"
const playersStateSignal = "$players.state.refresh"

type playersFragmentLoader func(*http.Request) map[string]any
type playersFragmentRenderer func(map[string]any, *http.Request) (string, error)

// PlayersPoolFragmentHandler serves only the player-pool region. It is a
// read-only projection: polling can observe the persisted pool and waiver
// state, but cannot provision a member or mutate league state.
func PlayersPoolFragmentHandler(service *league.Service) http.Handler {
	return playersFragmentHandler(
		playersFragmentAccess(service),
		func(request *http.Request) map[string]any { return service.PlayersDataReadOnly(request) },
		"PlayerPoolRegion",
	)
}

// PlayersWaiverFragmentHandler serves the personal claims and public waiver
// order region. The same query values are read by PlayersDataReadOnly, so
// position/search/page state remains authoritative across a swap.
func PlayersWaiverFragmentHandler(service *league.Service) http.Handler {
	return playersFragmentHandler(
		playersFragmentAccess(service),
		func(request *http.Request) map[string]any { return service.PlayersDataReadOnly(request) },
		"WaiverDeskRegion",
	)
}

func playersFragmentAccess(service *league.Service) func(*http.Request) bool {
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

func playersFragmentHandler(allowed func(*http.Request) bool, load playersFragmentLoader, component string) http.Handler {
	return playersFragmentHandlerWithRenderer(allowed, load, func(data map[string]any, request *http.Request) (string, error) {
		return playersFragmentRender(data, request, component)
	})
}

func playersFragmentHandlerWithRenderer(allowed func(*http.Request) bool, load playersFragmentLoader, render playersFragmentRenderer) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setPlayersFragmentPrivacyHeaders(writer)
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
		etag := playersFragmentETag(html)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("ETag", etag)
		if playersETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setPlayersFragmentPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func playersFragmentETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func playersETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

func playersFragmentRender(data map[string]any, request *http.Request, component string) (string, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return "", err
	}
	return route.RenderProgramComponent(program, component, route.ProgramRenderEnv{
		Values: map[string]any{
			"data": data,
			"csrf": map[string]any{
				"token": session.Token(request),
				"field": "csrf_token",
			},
		},
		Funcs: map[string]any{
			"actionPath": func(name string) string { return "/players/__actions/" + name },
		},
	})
}

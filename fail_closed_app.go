package main

import (
	"io"
	"net/http"
	"time"

	"m31labs.dev/gosx/server"
)

// failClosedOperatorMessage is the static page every route serves in the
// BootFailClosed state (design section 3.1): a marker or league data exists
// in the database, but no league.json resolves. This is a mount failure —
// the config volume or ConfigMap went missing after a completed setup — not
// a fresh instance, so setup must never re-arm on it.
const failClosedOperatorMessage = "This instance already completed setup, but its league configuration is missing now. Restore league.json (or its ConfigMap/volume mount) at the expected path and restart. See docs/configuration.md. Setup does not restart automatically: a missing config file after a completed setup is treated as an operator error, not a fresh install."

// BuildFailClosedApp assembles the BootFailClosed state's HTTP application
// (design section 3.1): a static operator-error page on every route, HTTP
// 503, except /api/live (the process is genuinely still running and
// responding — an orchestrator should not restart-loop it on liveness
// alone) and /api/health (which reports the failure truthfully instead of
// a generic 200).
func BuildFailClosedApp(cfg AppConfig) (*server.App, error) {
	app := server.New()
	app.EnableSecurityPolicy(gridironSecurityPolicy())

	app.API("GET /api/live", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return livenessPayload(), nil
	})
	app.API("GET /api/health", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		ctx.SetStatus(http.StatusServiceUnavailable)
		return failClosedHealthPayload(), nil
	})
	app.Mount("/", failClosedCatchAllHandler())
	return app, nil
}

func failClosedHealthPayload() map[string]any {
	return map[string]any{
		"ok":         false,
		"liveness":   true,
		"readiness":  false,
		"state":      "fail_closed",
		"error":      failClosedOperatorMessage,
		"version":    appVersion,
		"appVersion": appVersion,
		"gitSHA":     appGitSHA,
		"buildDate":  appBuildDate,
		"time":       time.Now().UTC().Format(time.RFC3339),
	}
}

func failClosedCatchAllHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>Gridiron: configuration missing</title></head><body><main><h1>Configuration missing</h1><p>`+failClosedOperatorMessage+`</p></main></body></html>`)
	})
}

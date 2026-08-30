package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
	"gridiron-2000/internal/wire"
	"m31labs.dev/gosx/server"
)

func mountOwnedDataAPI(app *server.App, signalFeed *wire.Service, stats *openstats.Service, pool *fantasy.Service, token string) {
	leagueOnly := func(handler http.Handler) http.Handler {
		return requireLeagueAccess(handler)
	}
	app.Mount("GET /api/data/status", leagueOnly(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeDataJSON(writer, http.StatusOK, map[string]any{
			"signal_wire":         signalFeed.Status(),
			"open_stats":          stats.Status(),
			"fantasy_pool":        pool.Status(),
			"protected_api_ready": strings.TrimSpace(token) != "",
		})
	})))
	app.Mount("GET /api/wire/status", leagueOnly(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeDataJSON(writer, http.StatusOK, signalFeed.Status())
	})))
	app.Mount("GET /api/wire/events", leagueOnly(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		limit := queryInt(request, "limit", 50)
		events := signalFeed.Recent(limit, request.URL.Query().Get("category"))
		status := signalFeed.Status()
		if len(events) > 0 {
			etag := fmt.Sprintf("\"wire-%s-%d\"", events[0].ID, status.RelevantSignals)
			writer.Header().Set("ETag", etag)
			if request.Header.Get("If-None-Match") == etag {
				writer.WriteHeader(http.StatusNotModified)
				return
			}
		}
		writeDataJSON(writer, http.StatusOK, map[string]any{
			"schema_version": wire.SchemaVersion,
			"count":          len(events),
			"events":         events,
			"status":         status,
		})
	})))

	protected := func(handler http.Handler) http.Handler {
		return requireDataToken(strings.TrimSpace(token), handler)
	}
	app.Mount("GET /api/data/games", protected(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		games := stats.Games(queryInt(request, "week", 0))
		writeDataJSON(writer, http.StatusOK, map[string]any{
			"schema_version": openstats.SchemaVersion,
			"count":          len(games),
			"games":          games,
		})
	})))
	app.Mount("GET /api/data/player-week", protected(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		rows := stats.PlayerStats(openstats.PlayerQuery{
			Week:       queryInt(request, "week", 0),
			PlayerID:   request.URL.Query().Get("player_id"),
			Team:       request.URL.Query().Get("team"),
			SeasonType: request.URL.Query().Get("season_type"),
			Limit:      queryInt(request, "limit", 100),
		})
		writeDataJSON(writer, http.StatusOK, map[string]any{
			"schema_version": openstats.SchemaVersion,
			"license":        openstats.License,
			"attribution":    openstats.AttributionURL,
			"count":          len(rows),
			"player_week":    rows,
		})
	})))
	app.Mount("GET /api/data/injuries", protected(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		rows := stats.InjuryReports(openstats.InjuryQuery{
			Week:     queryInt(request, "week", 0),
			PlayerID: request.URL.Query().Get("player_id"),
			Team:     request.URL.Query().Get("team"),
			Limit:    queryInt(request, "limit", 100),
		})
		writeDataJSON(writer, http.StatusOK, map[string]any{
			"schema_version": openstats.SchemaVersion,
			"license":        openstats.License,
			"attribution":    openstats.AttributionURL,
			"count":          len(rows),
			"injuries":       rows,
		})
	})))
	app.Mount("GET /api/data/signal-export.ndjson", protected(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", "application/x-ndjson")
		writer.Header().Set("Content-Disposition", `attachment; filename="gridiron-2000-signals.ndjson"`)
		writer.Header().Set("X-Archive-Schema", strconv.Itoa(wire.SchemaVersion))
		if err := signalFeed.Export(writer); err != nil {
			_, _ = writer.Write([]byte("\n"))
		}
	})))
	app.Mount("GET /api/data/fantasy-pool", protected(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		players, version := pool.Players()
		writeDataJSON(writer, http.StatusOK, map[string]any{
			"schema_version": fantasy.SchemaVersion,
			"status":         pool.Status(),
			"version":        version,
			"count":          len(players),
			"players":        players,
		})
	})))
	mountCSVExport(app, protected, stats, "schedules", "/api/data/schedules.csv", "nflverse-schedules.csv", "text/csv; charset=utf-8")
	mountCSVExport(app, protected, stats, "player_stats", "/api/data/player-stats.csv", "nflverse-player-stats.csv", "text/csv; charset=utf-8")
	mountCSVExport(app, protected, stats, "injuries", "/api/data/injuries.csv", "nflverse-injuries.csv", "text/csv; charset=utf-8")
	// WP-R2: the DEFENSE and PUNTING scoring groups' raw sources, exported
	// for the same transparent-reprocessing reason the three above are.
	// play_by_play's cache file is the gzip-compressed original download
	// (ExportCSV streams whatever is on disk, unmodified), so its
	// content-type and filename are honest about that, unlike the three
	// plain-CSV exports above.
	mountCSVExport(app, protected, stats, "team_stats", "/api/data/team-stats.csv", "nflverse-team-stats.csv", "text/csv; charset=utf-8")
	mountCSVExport(app, protected, stats, "play_by_play", "/api/data/play-by-play.csv.gz", "nflverse-play-by-play.csv.gz", "application/gzip")
}

func mountCSVExport(app *server.App, protect func(http.Handler) http.Handler, stats *openstats.Service, dataset, pattern, filename, contentType string) {
	app.Mount("GET "+pattern, protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", contentType)
		writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		writer.Header().Set("X-Data-License", openstats.License)
		writer.Header().Set("X-Data-Attribution", openstats.AttributionURL)
		if err := stats.ExportCSV(writer, dataset); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(writer, "dataset has not been synchronized yet", http.StatusNotFound)
				return
			}
			http.Error(writer, "dataset export failed", http.StatusInternalServerError)
		}
	})))
}

func requireLeagueAccess(next http.Handler) http.Handler {
	return requireLeagueAccessWithPolicy(next,
		func() bool { return league.Default().DemoMode() },
		func(request *http.Request) bool {
			_, ok := league.Default().CurrentUser(request)
			return ok
		},
	)
}

func requireLeagueAccessWithPolicy(next http.Handler, demoMode func() bool, signedIn func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if demoMode() {
			next.ServeHTTP(writer, request)
			return
		}
		if !signedIn(request) {
			writer.Header().Set("Cache-Control", "no-store")
			http.Error(writer, "Google sign-in is required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// liveWeekAPIHandler serves the live matchup view with an ETag so a
// browser poller (default 60 s, or the "checks every 5 s" live cadence
// once wired) can send If-None-Match and get a bare 304 instead of
// re-downloading an unchanged body. Cache-Control stays no-store — the
// ETag is a bandwidth optimization, not a cache directive; every request
// still reaches the handler and reads the current view.
func liveWeekAPIHandler(protect func(http.Handler) http.Handler) http.Handler {
	return protect(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := json.Marshal(league.Default().LiveScoresView(request.Context()))
		if err != nil {
			http.Error(writer, "live view unavailable", http.StatusInternalServerError)
			return
		}
		// The same trailing newline json.NewEncoder(w).Encode(v) writes
		// (and writeDataJSON therefore produces elsewhere): appended here,
		// before hashing, so the ETag matches the exact bytes this handler
		// writes rather than the pre-newline json.Marshal output (round-2
		// review of commit cdeb7f2, finding 6).
		body = append(body, '\n')
		sum := sha256.Sum256(body)
		etag := `"live-` + hex.EncodeToString(sum[:8]) + `"`
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("ETag", etag)
		if ifNoneMatchHasWeak(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		// Same Content-Type value writeDataJSON sets, applied by hand here
		// since the ETag must be computed from the body before any header
		// is written.
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(body)
	}))
}

// ifNoneMatchHasWeak reports whether header — an If-None-Match request
// header value — matches etag under RFC 9110 section 8.8.3.2's weak
// comparison, the form GET must use: the "W/" prefix is stripped (weak
// and strong tags compare equal once stripped), the header may carry a
// comma-separated list of entity tags and any one matching is enough, and
// a bare "*" matches unconditionally (round-2 review of commit cdeb7f2,
// finding 3).
func ifNoneMatchHasWeak(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

func queryInt(request *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(request.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func requireDataToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if token == "" {
			http.NotFound(writer, request)
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
		if !dataTokenEqual(token, provided) {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="gridiron-data"`)
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func dataTokenEqual(expected, provided string) bool {
	expectedHash := sha256.Sum256([]byte(expected))
	providedHash := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1
}

func writeDataJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
	"gridiron-2000/internal/wire"
	_ "gridiron-2000/modules"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/env"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	if err := env.LoadDir(root, ""); err != nil {
		log.Fatal(err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	signalFeed, err := wire.Default()
	if err != nil {
		log.Fatal(err)
	}
	openStats, err := openstats.Default()
	if err != nil {
		log.Fatal(err)
	}
	fantasyPool, err := fantasy.Default()
	if err != nil {
		log.Fatal(err)
	}
	signalFeed.Start(runtimeContext)
	openStats.Start(runtimeContext)
	fantasyPool.Start(runtimeContext)
	league.Default().SetPlayerSource(fantasyPlayerSource(fantasyPool))
	league.Default().SetPoolStatus(fantasyPoolStatus(fantasyPool))
	league.Default().SetScheduleSource(leagueScheduleSource(openStats))

	appName := getenv("APP_NAME", "GRIDIRON 2000")
	port := getenv("PORT", "8080")
	production := strings.EqualFold(os.Getenv("APP_ENV"), "production")
	secret := getenv("SESSION_SECRET", "gridiron-2000-local-session-secret-change-me")
	sessions, err := session.New(secret, session.Options{
		CookieName: "gridiron_session",
		Secure:     production,
		Encrypt:    true,
		MaxAge:     30 * 24 * time.Hour,
		SameSite:   http.SameSiteLaxMode,
	})
	if err != nil {
		log.Fatal(err)
	}

	authManager := auth.New(sessions, auth.Options{LoginPath: "/login"})
	googleConfigured := googleAuthConfigured()
	googleProvider := auth.GoogleProvider(
		os.Getenv("GOOGLE_CLIENT_ID"),
		os.Getenv("GOOGLE_CLIENT_SECRET"),
		getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback"),
	)
	googleProvider.AuthParams = map[string]string{
		"include_granted_scopes": "true",
		"prompt":                 "select_account",
	}
	googleOAuth := authManager.OAuth(auth.OAuthOptions{
		Providers:   []auth.OAuthProvider{googleProvider},
		SuccessPath: "/",
		FailurePath: "/login?error=oauth",
	})

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetMetadata(server.Metadata{
			Links: []server.LinkTag{
				{Rel: "stylesheet", Href: "/styles.css"},
				{Rel: "icon", Href: "/favicon.svg", Type: "image/svg+xml"},
			},
			ThemeColor: []server.ThemeColor{{Color: "#070A16"}},
		})
		ctx.AddHead(
			gosx.El("meta", gosx.Attrs(gosx.Attr("name", "viewport"), gosx.Attr("content", "width=device-width, initial-scale=1"))),
			// The page-navigation runtime soft-swaps links and managed forms,
			// so the site works without full page reloads.
			server.NavigationScript(),
			gosx.El("script", gosx.Attrs(gosx.Attr("src", "/gridiron.js"), gosx.Attr("defer", "defer")), gosx.Text("")),
		)
		return server.HTMLDocument(ctx.Title(appName), ctx.Head(), body)
	})
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}

	app := server.New()
	app.EnableNavigation()
	app.EnableGzip()
	app.Use(sessions.Middleware)
	app.Use(sessions.Protect)
	app.Use(authManager.Middleware)
	app.SetPublicDir(filepath.Join(root, "public"))

	app.API("GET /api/health", func(ctx *server.Context) (any, error) {
		ctx.CachePublic(30 * time.Second)
		ctx.CacheTag("health")
		wireStatus := signalFeed.Status()
		openStatus := openStats.Status()
		poolStatus := fantasyPool.Status()
		return map[string]any{
			"ok":                   true,
			"app":                  appName,
			"version":              gosx.Version,
			"googleOAuthReady":     googleConfigured,
			"signalWireReady":      wireStatus.Configured,
			"signalWireMode":       wireStatus.Mode,
			"ownedSignals":         wireStatus.RelevantSignals,
			"openStatsRunning":     openStatus.Running,
			"openScheduleState":    openStatus.Schedules.State,
			"openPlayerStatsState": openStatus.PlayerStats.State,
			"openInjuryState":      openStatus.Injuries.State,
			"fantasyPoolEnabled":   poolStatus.Enabled,
			"fantasyPoolMode":      poolStatus.Mode,
			"fantasyPoolPlayers":   poolStatus.Players,
			"fantasyPoolScoring":   poolStatus.Scoring,
			"fantasyPoolError":     poolStatus.LastError,
			"draftAt":              league.Default().DraftAt().Format(time.RFC3339),
			"time":                 time.Now().UTC().Format(time.RFC3339),
		}, nil
	})
	app.API("GET /api/live/week", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return league.Default().LiveScores(ctx.Request.Context()), nil
	})
	app.API("GET /api/league/version", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		_, poolVersion := fantasyPool.Players()
		return map[string]any{
			"fingerprint": league.Default().StateFingerprint(poolVersion),
		}, nil
	})
	mountOwnedDataAPI(app, signalFeed, openStats, fantasyPool, os.Getenv("DATA_API_TOKEN"))

	app.Mount("GET /auth/google/start", googleStartHandler(googleOAuth, googleConfigured))
	app.Mount("GET /auth/google/callback", googleCallbackHandler(googleOAuth, authManager, googleConfigured))
	app.Mount("POST /auth/logout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authManager.SignOut(r)
		session.AddFlash(r, "notice", "You are signed out. The demo league is still available.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}))

	rootHandler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	app.Mount("/", requireLeagueSession(rootHandler))

	if !googleConfigured {
		log.Printf("Google OAuth is in setup mode; add GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET to .env")
	}
	wireStatus := signalFeed.Status()
	if !wireStatus.Configured {
		log.Printf("Signal wire is awaiting sources; enable public feeds or add BLUESKY_HANDLES / BLUESKY_DIDS")
	} else if !wireStatus.BlueskyConfigured {
		log.Printf("Free public feed mesh is active; add optional BLUESKY_HANDLES / BLUESKY_DIDS for event-driven social alerts")
	}
	log.Printf("%s listening on http://localhost:%s", appName, port)
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- app.ListenAndServe(":" + port)
	}()
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-runtimeContext.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := app.Shutdown(shutdownContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server shutdown: %v", err)
		}
	}
}

// requireLeagueSession gates the league pages behind sign-in, like a hosted
// service: anonymous visitors see the landing page, the login page, and the
// legal pages only. Demo mode leaves everything open for local rehearsal.
func requireLeagueSession(next http.Handler) http.Handler {
	open := map[string]bool{
		"/": true, "/login": true, "/privacy": true, "/terms": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if league.Default().DemoMode() {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := auth.Current(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		if path == "" {
			path = "/"
		}
		if open[path] {
			next.ServeHTTP(w, r)
			return
		}
		session.AddFlash(r, "notice", "Sign in to enter the league.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

// fantasyPlayerSource adapts the fantasy pool to the league's PlayerSource.
// Conversion runs only when the pool version changes, so page renders reuse
// one converted slice.
func fantasyPlayerSource(pool *fantasy.Service) league.PlayerSource {
	var mu sync.Mutex
	var lastVersion int64
	var lastMode string
	var converted []league.Player
	return func() ([]league.Player, int64, string) {
		players, version := pool.Players()
		mode := pool.Status().Mode
		mu.Lock()
		defer mu.Unlock()
		if converted == nil || version != lastVersion || mode != lastMode {
			converted = make([]league.Player, 0, len(players))
			for _, player := range players {
				converted = append(converted, league.Player{
					ID:         player.ID,
					Name:       player.Name,
					Position:   player.Position,
					NFLTeam:    player.NFLTeam,
					ADP:        player.ADP,
					ADPRank:    player.ADPRank,
					ByeWeek:    player.ByeWeek,
					Injury:     player.Injury,
					Headshot:   player.Headshot,
					Projection: player.Projection,
					News:       player.News,
					Status:     "Available",
				})
			}
			lastVersion = version
			lastMode = mode
		}
		return converted, version, mode
	}
}

// leagueScheduleSource adapts the mirrored nflverse schedule to the pick'em
// engine. nflverse kickoff times are Eastern. The parsed schedule drops the
// result column, so a game counts as final five hours after kickoff; the
// winner resolves once scores land in the mirror.
func leagueScheduleSource(stats *openstats.Service) league.ScheduleSource {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		eastern = time.UTC
	}
	return func() []league.GameInfo {
		games := stats.Games(0)
		now := time.Now()
		out := make([]league.GameInfo, 0, len(games))
		for _, game := range games {
			if game.GameType != "REG" {
				continue
			}
			gameTime := game.GameTime
			if strings.TrimSpace(gameTime) == "" {
				gameTime = "13:00"
			}
			kickoff, err := time.ParseInLocation("2006-01-02 15:04", game.GameDay+" "+gameTime, eastern)
			if err != nil {
				continue
			}
			out = append(out, league.GameInfo{
				ID:        game.GameID,
				Week:      game.Week,
				Kickoff:   kickoff,
				Away:      strings.ToUpper(game.AwayTeam),
				Home:      strings.ToUpper(game.HomeTeam),
				AwayScore: int(game.AwayScore),
				HomeScore: int(game.HomeScore),
				Final:     now.After(kickoff.Add(5 * time.Hour)),
			})
		}
		return out
	}
}

// fantasyPoolStatus renders the fantasy pool diagnostics as the legible map
// the commissioner console displays.
func fantasyPoolStatus(pool *fantasy.Service) league.PoolStatusSource {
	return func() map[string]any {
		status := pool.Status()
		lastSync := "never"
		if !status.LastSync.IsZero() {
			lastSync = status.LastSync.Local().Format("Jan 2 · 3:04 PM MST")
		}
		positions := make([]map[string]any, 0, len(status.Positions))
		for _, position := range []string{"QB", "RB", "WR", "TE", "K", "DST"} {
			if count, ok := status.Positions[position]; ok {
				positions = append(positions, map[string]any{"pos": position, "count": count})
			}
		}
		return map[string]any{
			"mode":           status.Mode,
			"players":        status.Players,
			"with_adp":       status.WithADP,
			"with_proj":      status.WithProj,
			"with_bye":       status.WithBye,
			"requests":       status.Requests,
			"last_sync":      lastSync,
			"error":          status.LastError,
			"positions_list": positions,
		}
	}
}

func googleStartHandler(flow *auth.OAuth, configured bool) http.Handler {
	if configured {
		return flow.BeginHandler("google")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.AddFlash(r, "notice", "Google OAuth needs a client ID and secret. Follow the setup guide, or keep exploring in demo mode.")
		http.Redirect(w, r, "/login?setup=google", http.StatusSeeOther)
	})
}

func googleCallbackHandler(flow *auth.OAuth, manager *auth.Manager, configured bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !configured {
			http.Redirect(w, r, "/login?setup=google", http.StatusSeeOther)
			return
		}
		user, target, err := flow.Callback(r, "google")
		if err != nil {
			session.AddFlash(r, "notice", "Google sign-in could not be completed. Check the redirect URI and try again.")
			http.Redirect(w, r, "/login?error=oauth", http.StatusSeeOther)
			return
		}
		if !league.Default().EmailAllowed(user.Email) {
			manager.SignOut(r)
			session.AddFlash(r, "notice", "That Google account is not on this league's invite list.")
			http.Redirect(w, r, "/login?error=invite", http.StatusSeeOther)
			return
		}
		member, err := league.Default().AssignManager(user.Email, user.Name)
		if err != nil {
			manager.SignOut(r)
			session.AddFlash(r, "notice", "All eight manager seats are currently claimed.")
			http.Redirect(w, r, "/login?error=full", http.StatusSeeOther)
			return
		}
		session.AddFlash(r, "notice", "Welcome to "+member.TeamID+", "+user.Name+".")
		if target == "" {
			target = "/"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

func googleAuthConfigured() bool {
	return strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")) != "" && strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")) != ""
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

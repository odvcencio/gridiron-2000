package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	activitypage "gridiron-2000/app/activity"
	adminpage "gridiron-2000/app/admin"
	commissionerpage "gridiron-2000/app/commissioner"
	draftpage "gridiron-2000/app/draft"
	playerspage "gridiron-2000/app/players"
	teampage "gridiron-2000/app/team"
	wirepage "gridiron-2000/app/wire"
	"gridiron-2000/internal/commissionerhq"
	"gridiron-2000/internal/commissionerhq/v1fleet"
	"gridiron-2000/internal/commissionerhq/v1provider"
	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/mailer"
	"gridiron-2000/internal/navigation"
	"gridiron-2000/internal/notify"
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

// gridironSessionOptions keeps the cookie policy explicit at the one place
// where the application decides whether it is serving local plain HTTP or a
// deployed HTTPS environment. gosx defaults to Secure when AllowInsecure is
// omitted, so only known local/default environments opt in to plain HTTP.
func gridironSessionOptions(appEnv string) session.Options {
	localHTTP := false
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "", "local", "development", "test":
		localHTTP = true
	}
	return session.Options{
		CookieName:    "gridiron_session",
		Secure:        !localHTTP,
		AllowInsecure: localHTTP,
		HTTPOnly:      true,
		Encrypt:       true,
		MaxAge:        30 * 24 * time.Hour,
		SameSite:      http.SameSiteLaxMode,
	}
}

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
	// league.Default() loads league.json (or the neutral built-in default)
	// first, so its team count and roster shape are on hand to scale
	// FANTASY_POOL_LIMIT's own default (owner decision, productization
	// wave: teams × roster spots × headroom, not a flat constant).
	poolLimit := fantasy.ScaledPoolLimit(league.Default().TeamCount(), league.Default().RosterSpots())
	fantasyPool, err := fantasy.Default(poolLimit)
	if err != nil {
		log.Fatal(err)
	}
	signalFeed.Start(runtimeContext)
	openStats.Start(runtimeContext)
	fantasyPool.Start(runtimeContext)
	league.Default().SetPlayerSource(fantasyPlayerSource(fantasyPool))
	league.Default().SetPoolStatus(fantasyPoolStatus(fantasyPool))
	league.Default().SetScheduleSource(leagueScheduleSource(openStats))
	league.Default().StartPickemMarketSync(runtimeContext)
	league.Default().SetStatsUpdatedSource(func() time.Time {
		return openStats.Status().PlayerStats.LastUpdated
	})
	league.Default().SetHistoricalSource(historicalSource(openStats))
	league.Default().SetWeekStatsSource(leagueWeekStatsSource(openStats))
	league.Default().SetInjuryDesignationSource(leagueInjuryDesignationSource(openStats))
	startBlitzPoller(runtimeContext, fantasyPool, league.Default())
	// startBlitzPre1 attaches its loading snapshot before it backgrounds the
	// handful of REST calls against already-final games, so first render can
	// distinguish "checking" from a verified zero-player evidence map.
	startBlitzPre1(runtimeContext, fantasyPool, league.Default())
	// startMatchupRanks computes the matchup-difficulty rank cache (owner
	// ask: "we should see the opponent at a glance" plus a difficulty
	// rank) and keeps it refreshed; it backgrounds itself for the same
	// reason startBlitzPre1 does — see matchup_cache.go.
	go startMatchupRanks(runtimeContext, openStats, league.Default())
	league.Default().StartDraftClock(runtimeContext)
	// StartRosterOps always runs, mail wired or not: waiver processing
	// (and WP-R5's trade execution/expiry) are state mutations, not sends
	// — only the send step at the end of each tick is itself
	// notifyReady-gated (roster-ops spec section 5.4).
	league.Default().StartRosterOps(runtimeContext)
	notifyMailer := mailer.FromEnv()
	notifyQueue := notify.New(notificationSender(notifyMailer), log.Printf)
	league.Default().SetNotifier(notifyQueue, notifyMailer.Enabled())
	// The notify worker gets its own cancellation, separate from
	// runtimeContext: on shutdown the HTTP server stops accepting requests
	// immediately, but the worker keeps draining whatever is already
	// queued (see notifyQueue.Drain below the server's shutdown branch)
	// until it finishes or the drain deadline expires. A message the
	// ledger already marked "sent" must not be silently abandoned
	// (design spec section 6.3; finding m1).
	notifyContext, stopNotify := context.WithCancel(context.Background())
	defer stopNotify()
	// Spec section 6.6: without a transport, notifications are disabled
	// across both the delivery queue and every league-side trigger hook.
	// StartNotifier always runs and re-checks the same enabled flag
	// (via notifyReady), so it is the single source of the spec's exact
	// startup log line — no separate log call is needed here.
	if notifyMailer.Enabled() {
		notifyQueue.Start(notifyContext)
	}
	league.Default().StartNotifier(runtimeContext)

	// league.Default() has already resolved APP_NAME (env) over
	// league.name (file) over the neutral built-in default (spec section
	// 3.3 precedence); read the wordmark through it instead of a second,
	// independent getenv call so the two never disagree.
	appName := league.Default().Config().Name
	hqConfig, err := commissionerhq.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	hqService, err := commissionerhq.New(hqConfig, func() commissionerhq.Summary {
		poolStatus := fantasyPool.Status()
		openData := commissionerOpenData(openStats.Status())
		stateSchema := league.Default().StateSchemaCompatibility()
		return league.Default().CommissionerSummary(hqConfig.InstanceID, commissionerhq.Runtime{
			Ready:      league.Default().PersistenceError() == nil,
			AppVersion: appVersion, FrameworkVersion: gosx.Version,
			GitSHA: appGitSHA, Build: appBuildDate,
			StateSchema: commissionerhq.StateSchema{
				PersistedVersion:         stateSchema.PersistedVersion,
				SupportedVersion:         stateSchema.SupportedVersion,
				PersistedDatabaseVersion: stateSchema.PersistedDatabaseVersion,
				SupportedDatabaseVersion: stateSchema.SupportedDatabaseVersion,
				Compatible:               stateSchema.Compatible,
			},
		}, commissionerhq.Pool{
			Mode: poolStatus.State, Actual: poolStatus.Players, Target: poolStatus.PoolLimit,
			LastSync: poolStatus.LastSync, Error: poolStatus.LastError,
		}, openData)
	})
	if err != nil {
		log.Fatal(err)
	}
	commissionerhq.SetDefault(hqService)
	hqV1Config, err := v1provider.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	hqV1Runtime, err := buildCommissionerHQV1Runtime(hqV1Config, league.Default(), fantasyPool, openStats)
	if err != nil {
		log.Fatal(err)
	}
	hqV1FleetConfig, err := v1fleet.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	hqV1Fleet, err := v1fleet.New(hqV1FleetConfig, v1fleet.Options{})
	if err != nil {
		log.Fatal(err)
	}
	commissionerpage.SetHQV1Fleet(hqV1Fleet)
	port := getenv("PORT", "8080")
	appEnv := strings.TrimSpace(os.Getenv("APP_ENV"))
	secret := getenv("SESSION_SECRET", "gridiron-2000-local-session-secret-change-me")
	sessions, err := session.New(secret, gridironSessionOptions(appEnv))
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
		ctx.SetLanguage("en")
		ctx.SetMetadata(server.Metadata{
			Links: []server.LinkTag{
				{Rel: "stylesheet", Href: "/styles.css"},
				{Rel: "icon", Href: "/favicon.svg", Type: "image/svg+xml"},
			},
			ThemeColor: []server.ThemeColor{{Color: "#070A16"}},
		})
		// data-gosx-heartbeat/-interval (gosx#216) replaces gridiron.js's old
		// sendPresenceHeartbeat loop on every page, not just the ones that
		// also carry data-gosx-revalidate-interval: the heartbeat ping is
		// visibility-aware (it pauses while the tab is hidden) but, unlike
		// revalidation and every other periodic primitive here, it carries
		// no focused-control interaction guard, so it keeps presence current
		// while a manager is typing in a search box — the exact gap the old
		// JS's focusedControlActive() special case existed only to close.
		// PageState.BodyAttrs (v0.50.0) puts the two heartbeat attributes
		// directly on <body>, so no wrapper element is needed. The endpoint is
		// route-aware because the one body marker must not turn an ordinary
		// page's version poll into a draft-room attendance claim. Draft's two
		// fragment regions own live room/version updates; its body heartbeat
		// is presence-only. The native route Document contract carries both
		// through the framework shell and re-reads them after managed navigation.
		heartbeatEndpoint := leagueHeartbeatEndpoint(ctx.Request.URL.Path)
		ctx.BodyAttrs(
			gosx.Attr("data-gosx-heartbeat", heartbeatEndpoint),
			gosx.Attr("data-gosx-heartbeat-interval", "4s"),
		)
		return server.HTMLDocument(ctx.Document(appName, body))
	})
	// Authentication and onboarding redirects belong to the file routes
	// themselves. GoSX applies this middleware only after a page or action
	// route matches, so an unknown URL keeps the normal truthful 404 instead
	// of being turned into a misleading login redirect.
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{
		Middleware: []route.Middleware{
			requireLeagueSession,
			redirectSeatedFromJoin,
		},
	}); err != nil {
		log.Fatal(err)
	}

	app := server.New()
	app.EnableNavigation()
	app.EnableSecurityPolicy(gridironSecurityPolicy())
	app.EnableGzip()
	app.Use(avatarMultipartEnvelopeLimit)
	app.Use(sessions.Middleware)
	app.Use(sessions.Protect)
	app.Use(authManager.Middleware)
	app.SetPublicDir(filepath.Join(root, "public"))

	// Liveness is deliberately independent of league persistence and optional
	// upstream feeds. A process that is still serving requests must not be
	// restarted merely because readiness has withdrawn for an operator repair.
	app.API("GET /api/live", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return livenessPayload(), nil
	})
	app.API("GET /api/health", func(ctx *server.Context) (any, error) {
		// Health is a live readiness signal. A public 30-second cache could
		// keep advertising "ok" after a runtime persistence poison, which is
		// exactly when an orchestrator must stop routing writes here. Optional
		// feed degradation remains diagnostic only and never changes readiness.
		ctx.NoStore()
		ctx.CacheTag("health")
		wireStatus := signalFeed.Status()
		openStatus := openStats.Status()
		poolStatus := fantasyPool.Status()
		draftStarted, draftStartedAt := league.Default().DraftLifecycle()
		draftStartedAtText := ""
		if !draftStartedAt.IsZero() {
			draftStartedAtText = draftStartedAt.Format(time.RFC3339)
		}
		persistenceErr := league.Default().PersistenceError()
		persistenceReady, persistenceStatus, persistenceMessage := persistenceHealth(persistenceErr)
		stateSchema := league.Default().StateSchemaCompatibility()
		blitzHealth := league.Default().BlitzDependencyHealth()
		rosterCapacity := league.Default().TeamCount() * league.CurrentDraftRounds()
		poolCushion := max(0, poolStatus.Players-rosterCapacity)
		poolCoverage := 0.0
		if rosterCapacity > 0 {
			poolCoverage = float64(poolStatus.PoolLimit) / float64(rosterCapacity)
		}
		if persistenceStatus != http.StatusOK {
			ctx.SetStatus(persistenceStatus)
		}
		return map[string]any{
			"ok":               persistenceReady,
			"liveness":         true,
			"readiness":        persistenceReady,
			"persistenceReady": persistenceReady,
			"persistenceError": persistenceMessage,
			// State schema evidence is intentionally numeric and PII-free. It
			// reports the authoritative persisted marker, not the normalized
			// in-memory version, so release rollback decisions can be made
			// before an image-only change.
			"stateSchema": stateSchemaPayload(stateSchema),
			// Blitz is an optional upstream dependency: expose its bounded,
			// safe provenance without making a Tank01 outage look like a
			// process-readiness failure.
			"blitz": blitzHealthPayload(blitzHealth),
			"app":   appName,
			// "version" is the Gridiron release, not the GoSX framework
			// version. Keeping the framework version adjacent makes runtime
			// drift (and an accidentally old image) immediately visible.
			"version":                    appVersion,
			"appVersion":                 appVersion,
			"frameworkVersion":           gosx.Version,
			"gitSHA":                     appGitSHA,
			"buildDate":                  appBuildDate,
			"googleOAuthReady":           googleConfigured,
			"commissionerHQV1Configured": hqV1Config.Enabled,
			"commissionerHQV1Listening":  hqV1Runtime.Listening(),
			"signalWireReady":            wireStatus.Configured,
			"signalWireMode":             wireStatus.Mode,
			"ownedSignals":               wireStatus.RelevantSignals,
			"openStatsRunning":           openStatus.Running,
			"openScheduleState":          openStatus.Schedules.State,
			"openPlayerStatsState":       openStatus.PlayerStats.State,
			"openInjuryState":            openStatus.Injuries.State,
			"fantasyPoolEnabled":         poolStatus.Enabled,
			"fantasyPoolMode":            poolStatus.Mode,
			"fantasyPoolState":           poolStatus.State,
			"fantasyPoolPlayers":         poolStatus.Players,
			"fantasyPoolTarget":          poolStatus.PoolLimit,
			"fantasyRosterCapacity":      rosterCapacity,
			"fantasyPoolCushion":         poolCushion,
			"fantasyPoolCoverage":        poolCoverage,
			"fantasyPoolScoring":         poolStatus.Scoring,
			"fantasyPoolError":           poolStatus.LastError,
			"fantasyPoolLastSuccess": func() string {
				if poolStatus.LastSync.IsZero() {
					return ""
				}
				return poolStatus.LastSync.UTC().Format(time.RFC3339)
			}(),
			"fantasyPoolAgeSeconds":             int64(poolStatus.Age / time.Second),
			"fantasyPoolFreshnessWindowSeconds": int64(poolStatus.FreshFor / time.Second),
			"draftAt":                           league.Default().DraftAt().Format(time.RFC3339),
			"draftStarted":                      draftStarted,
			"draftStartedAt":                    draftStartedAtText,
			// leagueConfig: "defaults" on an unconfigured checkout, or
			// "file:<path>" once a league.json loads (productization spec
			// section 4.3).
			"leagueConfig": league.Default().Config().Source,
			"time":         time.Now().UTC().Format(time.RFC3339),
		}, nil
	})
	app.Mount("GET /api/live/week", liveWeekAPIHandler(requireLeagueAccess))
	registerLeagueHeartbeatAPIs(app, league.Default(), func() string {
		_, poolVersion := fantasyPool.Players()
		return league.Default().StateFingerprint(poolVersion)
	})
	// /wire/fragment answers app/wire/page.gsx's data-gosx-region /
	// data-gosx-region-interval poll (gosx#217): wirepage.FeedFragmentWithError
	// loads that page program once and renders its typed SignalCard /
	// WireEmptyState components, the same components the initial page uses.
	// It is a plain HTML fragment, not a JSON API, so it lives next to the page
	// it serves rather than under mountOwnedDataAPI's external data contract.
	app.Mount("GET /wire/fragment", requireLeagueAccess(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		node, err := wirepage.FeedFragmentWithError(request, signalFeed)
		if err != nil {
			http.Error(writer, "wire fragment unavailable", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(writer, gosx.RenderHTML(node))
	})))
	app.API("GET /api/wire/pulse", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return wirepage.PulseData(signalFeed), nil
	})
	app.Mount("GET /commissioner/fragment", commissionerpage.FragmentHandler(hqService))
	app.Mount("GET /commissioner/switch", adminpage.SwitchHandler(hqService))
	app.Mount("GET /admin/fragment", adminpage.AdminAttentionFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/room", draftpage.RoomFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/workspace", draftpage.WorkspaceFragmentHandler(league.Default()))
	// Player-pool/waiver and transaction regions are read-only projections.
	// Their shared 4-second interval is the declared cross-client convergence
	// bound; managed player mutations signal the same regions immediately while
	// native forms continue through their existing POST-redirect-GET paths.
	app.Mount("GET /players/fragment/pool", playerspage.PlayersPoolFragmentHandler(league.Default()))
	app.Mount("GET /players/fragment/waivers", playerspage.PlayersWaiverFragmentHandler(league.Default()))
	app.Mount("GET /activity/fragment", activitypage.ActivityFragmentHandler(league.Default()))
	app.Mount("GET /team/fragment", teampage.TeamLineupFragmentHandler(league.Default()))
	app.Mount("GET /api/commissioner/v2/summary", hqService.SummaryHandler())
	mountOwnedDataAPI(app, signalFeed, openStats, fantasyPool, os.Getenv("DATA_API_TOKEN"))

	// Team avatars (design decisions 1-3): GoSX v0.50.0 exposes File/Files
	// and MaxActionBodyBytes for managed actions, but this native upload keeps
	// its own complete-multipart envelope cap until a bounded-multipart
	// contract can run before the session/CSRF parser. The serving route emits
	// its own fixed Cache-Control lifetime rather than the public-dir default,
	// since an uploaded avatar lives in the data dir, not public/. Both still
	// pass through the session/CSRF/auth middleware registered above (app.Use
	// wraps every mount, not just page routes).
	app.Mount("POST /avatar/upload", avatarUploadHandler(league.Default()))
	app.Mount("GET /avatars/", avatarServeHandler(league.Default()))

	// Team badges (the badge-picker feature): POST /avatar/badge claims,
	// swaps, or releases a team's badge motif; GET /avatars/badge/ is a
	// more specific subtree than the "GET /avatars/" mount above, so it is
	// preferred for any request under that path — see badge_handlers.go's
	// doc comments for the routing and PathValue details.
	app.Mount("POST /avatar/badge", badgeUploadHandler(league.Default()))
	app.Mount("GET /avatars/badge/", badgeServeHandler(league.Default()))

	app.Mount("GET /auth/google/start", googleStartHandler(googleOAuth, googleConfigured))
	app.Mount("GET /auth/google/callback", googleCallbackHandler(googleOAuth, authManager, googleConfigured))
	app.Mount("POST /auth/logout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authManager.SignOut(r)
		session.AddFlash(r, "notice", "You are signed out. Sign in again to return to your team.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}))

	rootHandler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	app.Mount("/", rootHandler)

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
	if hqV1Runtime != nil {
		log.Printf("Commissioner HQ v1 provider listening on its private listener")
	}
	type runtimeServerError struct {
		name string
		err  error
	}
	serverErrors := make(chan runtimeServerError, 2)
	go func() {
		serverErrors <- runtimeServerError{name: "application", err: app.ListenAndServe(":" + port)}
	}()
	if hqV1Runtime != nil {
		go func() {
			serverErrors <- runtimeServerError{name: "Commissioner HQ provider", err: hqV1Runtime.Serve()}
		}()
	}
	var runtimeFailure *runtimeServerError
	select {
	case result := <-serverErrors:
		if result.err != nil && !errors.Is(result.err, http.ErrServerClosed) {
			runtimeFailure = &result
		}
	case <-runtimeContext.Done():
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	var shutdownGroup sync.WaitGroup
	shutdownGroup.Add(1)
	go func() {
		defer shutdownGroup.Done()
		if err := app.Shutdown(shutdownContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server shutdown: %v", err)
		}
	}()
	if hqV1Runtime != nil {
		shutdownGroup.Add(1)
		go func() {
			defer shutdownGroup.Done()
			if err := hqV1Runtime.Shutdown(shutdownContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("Commissioner HQ provider shutdown: %v", err)
			}
		}()
	}
	shutdownGroup.Wait()
	cancelShutdown()
	// Give the notify worker a bounded window to finish delivering whatever
	// was already queued before the process exits. This applies equally to a
	// signal and either listener failing unexpectedly.
	notifyQueue.Drain(10 * time.Second)
	stopNotify()
	if runtimeFailure != nil {
		log.Fatalf("%s listener failed: %v", runtimeFailure.name, runtimeFailure.err)
	}
}

// blitzHealthPayload keeps the public /api/health shape flat enough for
// operators while reusing the same typed source facts commissioner summary
// consumes. The nested pre1 object is intentionally separate because its
// partial evidence must not make live slate readiness look offline.
func blitzHealthPayload(value league.BlitzDependencyHealth) map[string]any {
	return map[string]any{
		"enabled":              value.Source.Enabled,
		"state":                value.Source.State,
		"lastAttempt":          value.Source.LastAttempt,
		"lastSuccess":          value.Source.LastSuccess,
		"error":                value.Source.SafeError,
		"expectedGames":        value.Source.ExpectedGames,
		"fetchedGames":         value.Source.FetchedGames,
		"finalGames":           value.Source.FinalGames,
		"expectedScoringGames": value.Source.ExpectedScoringGames,
		"fetchedScoringGames":  value.Source.FetchedScoringGames,
		"scoringComplete":      value.Source.ScoringComplete,
		"complete":             value.Source.Complete,
		"final":                value.Source.Final,
		"verifiedZero":         value.Source.VerifiedZero,
		"slates":               value.Source.Slates,
		"pre1":                 value.Pre1,
	}
}

// gridironSecurityPolicy keeps the browser-facing surface on GoSX's native
// nonce contract. The app has a few deliberate inline style attributes (badge
// colors and consensus bars), so style-src allows inline styles while script
// execution remains nonce-gated. Google OAuth is a top-level redirect, not a
// frame or XHR, and the policy explicitly permits its form destination.
func gridironSecurityPolicy() server.SecurityPolicy {
	return server.SecurityPolicy{
		ContentSecurityPolicy: strings.Join([]string{
			"default-src 'self'",
			"base-uri 'self'",
			"object-src 'none'",
			"frame-ancestors 'none'",
			"form-action 'self' https://accounts.google.com",
			"script-src 'self' 'nonce-{nonce}' 'strict-dynamic' 'wasm-unsafe-eval'",
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
			"font-src 'self' https://fonts.gstatic.com",
			"img-src 'self' data: https://a.espncdn.com",
			"connect-src 'self'",
			"manifest-src 'self'",
			"worker-src 'self' blob:",
		}, "; "),
		FrameOptions:      "DENY",
		ReferrerPolicy:    "strict-origin-when-cross-origin",
		PermissionsPolicy: "camera=(), microphone=(), geolocation=()",
	}
}

// requireLeagueSession gates the league pages behind sign-in, like a hosted
// service: anonymous visitors see the landing page, the login page, and the
// legal pages only. Demo mode leaves everything open for local rehearsal.
func requireLeagueSession(next http.Handler) http.Handler {
	return requireLeagueSessionWithDemoMode(next, func() bool {
		return league.Default().DemoMode()
	})
}

func requireLeagueSessionWithDemoMode(next http.Handler, demoMode func() bool) http.Handler {
	open := map[string]bool{
		"/": true, "/guide": true, "/login": true, "/privacy": true, "/terms": true,
		"/open-source": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if demoMode != nil && demoMode() {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := league.Default().CurrentUser(r); ok {
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
		http.Redirect(w, r, navigation.LoginPathForRequest(r), http.StatusSeeOther)
	})
}

// redirectSeatedFromJoin sends an already-seated visitor straight to
// their team terminal instead of the fantasy-signup form (registration
// wave, build item 2: "already-seated visitors get redirected to
// /team"). GoSX applies this middleware only to a matched file page or
// action; the signup action itself posts to /join/__actions/signup-claim,
// a distinct path this leaves untouched. Every other matched route passes
// through unchanged.
func redirectSeatedFromJoin(next http.Handler) http.Handler {
	return redirectSeatedFromJoinWithViewer(next, func(r *http.Request) bool {
		hasSeat, _ := league.Default().Viewer(r)["has_seat"].(bool)
		return hasSeat
	})
}

func redirectSeatedFromJoinWithViewer(next http.Handler, hasSeat func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/join" {
			if hasSeat != nil && hasSeat(r) {
				http.Redirect(w, r, "/team", http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// notificationSender adapts mailer.Config to notify.Sender: it converts one
// rendered notify.Message into a mailer.Message, tagging it with its
// notification category and carrying its ledger key as the Resend
// Idempotency-Key (design spec section 6.5). The league-shortcode tag
// named in the spec is not wired yet — league.json's config surface (the
// productization spec) has not landed, so there is no shortcode to read
// here; it is a documented follow-up, not a silent gap. An SMTP-transport
// failure is wrapped notify.Permanent so Queue.deliver does not retry it:
// smtp.SendMail can error after the server already accepted the message,
// and at-most-once is preferred there. A Resend failure is returned
// unwrapped and stays retryable — its Idempotency-Key header protects a
// retry from becoming a duplicate (finding M3).
func notificationSender(mailCfg mailer.Config) notify.Sender {
	return func(m notify.Message) error {
		err := mailCfg.SendMessage(mailer.Message{
			To:             m.To,
			Subject:        m.Subject,
			Text:           m.Text,
			HTML:           m.HTML,
			Tags:           map[string]string{"category": m.Category},
			IdempotencyKey: m.Key,
		})
		if errors.Is(err, mailer.ErrSMTPTransport) {
			return notify.Permanent(err)
		}
		return err
	}
}

// fantasyPlayerSource adapts the fantasy pool to the league's PlayerSource.
// Conversion runs only when the pool version changes, so page renders reuse
// one converted slice.
func fantasyPlayerSource(pool *fantasy.Service) league.PlayerSource {
	var mu sync.Mutex
	var lastVersion int64
	var lastState string
	var converted []league.Player
	return func() ([]league.Player, int64, string) {
		players, version := pool.Players()
		state := pool.Status().State
		mu.Lock()
		defer mu.Unlock()
		if converted == nil || version != lastVersion || state != lastState {
			converted = make([]league.Player, 0, len(players))
			for _, player := range players {
				converted = append(converted, league.Player{
					ID:           player.ID,
					Name:         player.Name,
					Position:     player.Position,
					NFLTeam:      player.NFLTeam,
					ADP:          player.ADP,
					ADPRank:      player.ADPRank,
					ByeWeek:      player.ByeWeek,
					Injury:       player.Injury,
					Headshot:     player.Headshot,
					Jersey:       player.Jersey,
					ProjStats:    player.ProjStats,
					Projection:   player.Projection,
					News:         player.News,
					Status:       "Available",
					Rookie:       player.IsRookie(),
					DraftCapital: player.DraftCapitalLabel(),
				})
			}
			lastVersion = version
			lastState = state
		}
		return converted, version, state
	}
}

// leagueScheduleSource adapts the mirrored nflverse schedule to the Pick'em
// engine without discarding market or source truth. nflverse kickoff times
// are Eastern. Final follows actual result/score presence, never elapsed time,
// and a missing spread remains distinct from a real pick'em line of zero.
func leagueScheduleSource(stats *openstats.Service) league.ScheduleSource {
	return func() []league.GameInfo {
		return leagueGamesFromScheduleSnapshot(stats.ScheduleSnapshot())
	}
}

// openStatsEastern resolves the league's game-time zone, falling back to
// UTC when the tzdata lookup fails (the same fallback leagueScheduleSource
// used inline before this helper existed).
func openStatsEastern() *time.Location {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return eastern
}

// openStatsKickoff parses one schedule row's authoritative kickoff instant.
// A blank/TBA time is deliberately not fabricated as 1:00 PM: it is neither
// pickable nor eligible to create a missed-pick loss until the source supplies
// a real time.
func openStatsKickoff(game openstats.ScheduleGame, eastern *time.Location) (time.Time, bool) {
	gameTime := strings.TrimSpace(game.GameTime)
	if gameTime == "" {
		return time.Time{}, false
	}
	kickoff, err := time.ParseInLocation("2006-01-02 15:04", game.GameDay+" "+gameTime, eastern)
	if err != nil {
		return time.Time{}, false
	}
	return kickoff, true
}

// openStatsGameFinal is the single source-truth finality rule shared by
// Pick'em and DST points allowed. The location and clock remain in the
// signature for existing callers, but elapsed wall time cannot make a game
// final without an actual result or both source scores.
func openStatsGameFinal(game openstats.ScheduleGame, _ *time.Location, _ time.Time) bool {
	return game.HasResult()
}

// pointsAllowedByTeam maps a team abbreviation to the opponent's score in
// its week game, but only for a game openStatsGameFinal reports final — an
// in-progress or unplayed game's 0-0 placeholder score must never look like
// a shutout. A team absent from the returned map has no final score yet;
// dstWeekStatLines must not compute dstShutout for it.
func pointsAllowedByTeam(games []openstats.ScheduleGame, eastern *time.Location, now time.Time) map[string]float64 {
	allowed := make(map[string]float64, len(games)*2)
	for _, game := range games {
		if game.GameType != "REG" || !openStatsGameFinal(game, eastern, now) {
			continue
		}
		allowed[strings.ToUpper(game.HomeTeam)] = game.AwayScore
		allowed[strings.ToUpper(game.AwayTeam)] = game.HomeScore
	}
	return allowed
}

// dstNicknames maps every nflverse team abbreviation to the "{Nickname}
// D/ST" display name the fantasy pool's demo/fallback data already uses
// for a team-defense player (internal/fantasy/fallback.go), since
// normalizePlayerKey joins on (name, position) and openstats' team-stats
// mirror carries only abbreviations. A team missing from the live pool
// under a differently-formatted DST name is a join miss, reported through
// the existing JoinMiss path — never a crash, never a wrong attribution.
var dstNicknames = map[string]string{
	"ARI": "Cardinals D/ST", "ATL": "Falcons D/ST", "BAL": "Ravens D/ST", "BUF": "Bills D/ST",
	"CAR": "Panthers D/ST", "CHI": "Bears D/ST", "CIN": "Bengals D/ST", "CLE": "Browns D/ST",
	"DAL": "Cowboys D/ST", "DEN": "Broncos D/ST", "DET": "Lions D/ST", "GB": "Packers D/ST",
	"HOU": "Texans D/ST", "IND": "Colts D/ST", "JAX": "Jaguars D/ST", "KC": "Chiefs D/ST",
	"LA": "Rams D/ST", "LAC": "Chargers D/ST", "LV": "Raiders D/ST", "MIA": "Dolphins D/ST",
	"MIN": "Vikings D/ST", "NE": "Patriots D/ST", "NO": "Saints D/ST", "NYG": "Giants D/ST",
	"NYJ": "Jets D/ST", "PHI": "Eagles D/ST", "PIT": "Steelers D/ST", "SEA": "Seahawks D/ST",
	"SF": "49ers D/ST", "TB": "Buccaneers D/ST", "TEN": "Titans D/ST", "WAS": "Commanders D/ST",
}

// dstWeekStatLines builds one WeekStatLine per NFL team's defense/special
// teams unit for week from the team-stats mirror (WP-R2, DEFENSE group):
// dstSack, dstInt, dstFumbleRec (opponent-fumble recoveries only —
// recovering the team's own fumble is not a defensive scoring event), and
// dstTD, dstSafety feed directly from stats_team_week's def_* columns.
// dstShutout derives from the schedule's points-allowed, gated on the same
// finality rule as leagueScheduleSource (openStatsGameFinal) so an
// unplayed or in-progress game never reads as a shutout.
func dstWeekStatLines(stats *openstats.Service, eastern *time.Location, week int) []league.WeekStatLine {
	rows := stats.TeamStats(openstats.TeamStatsQuery{Week: week, Limit: 64})
	allowed := pointsAllowedByTeam(stats.Games(week), eastern, time.Now())
	out := make([]league.WeekStatLine, 0, len(rows))
	for _, row := range rows {
		team := strings.ToUpper(row.Team)
		name, ok := dstNicknames[team]
		if !ok {
			// An unrecognized team abbreviation (a source drift, not a
			// user error) scores nothing rather than guessing a name —
			// the same fail-quiet discipline punterHistLine follows.
			continue
		}
		statLine := map[string]float64{
			"dstSack":      row.DefSacks,
			"dstInt":       row.DefInterceptions,
			"dstFumbleRec": row.FumbleRecoveryOpp,
			"dstTD":        row.DefTDs,
			"dstSafety":    row.DefSafeties,
			"dstShutout":   0,
		}
		if pointsAllowed, ok := allowed[team]; ok && pointsAllowed == 0 {
			statLine["dstShutout"] = 1
		}
		out = append(out, league.WeekStatLine{
			Key:   openstats.NormalizePlayerKey(name, "DST"),
			Stats: statLine,
		})
	}
	return out
}

// puntEventsByPunter groups week's play-by-play punt events by nflverse
// player ID (the same ID scheme stats_player_week uses, so PlayerID joins
// directly — no name-normalization needed for this particular join). A nil
// return (as opposed to a non-nil empty map) is the honest-fallback signal:
// the play-by-play mirror has no punts at all for this week, so every
// punter degrades to the box-score aggregate path — never per-punter
// silence dressed up as "this punter had zero punts."
func puntEventsByPunter(stats *openstats.Service, week int) map[string][]openstats.PuntEvent {
	events := stats.PuntEvents(openstats.PuntQuery{Week: week, Limit: 5000})
	if len(events) == 0 {
		return nil
	}
	byPunter := make(map[string][]openstats.PuntEvent, 32)
	for _, event := range events {
		byPunter[event.PunterID] = append(byPunter[event.PunterID], event)
	}
	return byPunter
}

// addPuntingStatsFromPBP fills every PUNTING scoring key from this week's
// play-by-play punt events (WP-R2): the exact-events source. The puntYards
// 40+-yard gate is applied here, at scoring time, not in openstats'
// ingestion layer (openstats.PuntEvent carries gross per-punt distance
// only). puntLong50 counts every 50+-yard punt ("(each)", scoring.go), not
// a single per-game flag. A blocked punt cannot also count toward
// puntYards/puntLong50 (its recorded distance is 0 in the source anyway)
// and is excluded from those two by the !Blocked guard for clarity.
func addPuntingStatsFromPBP(statLine map[string]float64, events []openstats.PuntEvent) {
	var yards40Plus, in20, coffin, inside5, long50, touchback, blocked float64
	for _, event := range events {
		if !event.Blocked && event.Distance >= 40 {
			yards40Plus += event.Distance
		}
		if event.InsideTwenty {
			in20++
		}
		if event.CoffinCorner {
			coffin++
		}
		if event.Inside5 {
			inside5++
		}
		if !event.Blocked && event.Distance >= 50 {
			long50++
		}
		if event.Touchback {
			touchback++
		}
		if event.Blocked {
			blocked++
		}
	}
	statLine["puntYards"] = yards40Plus
	statLine["puntIn20"] = in20
	statLine["coffinCorner"] = coffin
	statLine["puntDownedInside5"] = inside5
	statLine["puntLong50"] = long50
	statLine["puntTouchback"] = touchback
	statLine["puntBlocked"] = blocked
}

// addPuntingStatsFromBoxScore is the honest fallback (WP-R2, section 3):
// stats_player_week's per-game punting aggregates, used only when the
// play-by-play mirror has no data for the requested week. puntYards cannot
// honor the 40+-yard gate from an aggregate (pt_yards sums every punt in
// the game, with no per-punt breakdown) and stays at the same zero the
// pre-WP-R2 dormant rule used, rather than an unqualified approximation.
// coffinCorner and puntDownedInside5 need a per-punt landing spot no
// aggregate carries and also stay at zero. puntIn20, puntTouchback, and
// puntBlocked have honest box-score equivalents and feed for real.
func addPuntingStatsFromBoxScore(statLine map[string]float64, row openstats.PlayerWeekStat) {
	statLine["puntYards"] = 0
	statLine["puntIn20"] = row.PuntInside20
	statLine["coffinCorner"] = 0
	statLine["puntDownedInside5"] = 0
	statLine["puntLong50"] = 0
	if row.PuntLong >= 50 {
		statLine["puntLong50"] = 1
	}
	statLine["puntTouchback"] = row.PuntTouchback
	statLine["puntBlocked"] = row.PuntBlocked
}

// leagueWeekStatsSource adapts the mirrored nflverse weekly player ledger,
// team-week box scores, and play-by-play mirror to the league's
// WeekStatsSource seam (competition-formats spec section 2.4): one
// WeekStatLine per player (plus one per NFL team's DST unit), keyed by the
// same openstats.NormalizePlayerKey the pick'em and historical-line seams
// already use, with stats mapped onto the league's scoring rule keys.
// PASSING/RUSHING/RECEIVING/MISC feed as before; WP-R2 adds KICKING
// (fgMade, fgMissed, xpMade — defaultScoringRules' KICKING group carries no
// FG distance bands or an xpMissed key, so nothing else is available to
// feed), DEFENSE (dstWeekStatLines), and PUNTING (per-punt from
// play-by-play when it has data for the week, else the honest box-score
// fallback with one log line — never a crash, never a fabricated event).
// openstats.Service.PlayerStats caps results at 1000 rows per call
// regardless of Limit, an existing openstats constraint this adapter does
// not change.
// offenseStatLine maps one openstats weekly player row onto the league's
// scoring-rule keys for PASSING/RUSHING/RECEIVING/MISC plus KICKING —
// every group a single player-week row can feed (DEFENSE and PUNTING's
// per-punt keys need a different source; see dstWeekStatLines and
// addPuntingStatsFromPBP/addPuntingStatsFromBoxScore). Both live weekly
// scoring (leagueWeekStatsSource) and the matchup-rank cache
// (matchup_cache.go) score through this one mapping, so a fantasy-
// points-allowed rank is computed from the exact same fields a live
// score is.
func offenseStatLine(row openstats.PlayerWeekStat) map[string]float64 {
	return map[string]float64{
		"passYards":  row.PassingYards,
		"passTD":     row.PassingTDs,
		"passInt":    row.PassingInterceptions,
		"rushYards":  row.RushingYards,
		"rushTD":     row.RushingTDs,
		"reception":  row.Receptions,
		"recYards":   row.ReceivingYards,
		"recTD":      row.ReceivingTDs,
		"fumbleLost": row.FumblesLost,
		"fgMade":     row.FGMade,
		"fgMissed":   row.FGMissed,
		"xpMade":     row.XPMade,
	}
}

func leagueWeekStatsSource(stats *openstats.Service) league.WeekStatsSource {
	eastern := openStatsEastern()
	return func(week int) []league.WeekStatLine {
		rows := stats.PlayerStats(openstats.PlayerQuery{Week: week, Limit: 1000})
		puntsByPunter := puntEventsByPunter(stats, week)
		if puntsByPunter == nil {
			log.Printf("openstats: no play-by-play punt data for week %d; punting scores from box-score aggregates only (puntYards, coffinCorner, and puntDownedInside5 score zero this week)", week)
		}
		out := make([]league.WeekStatLine, 0, len(rows)+32)
		for _, row := range rows {
			statLine := offenseStatLine(row)
			if row.Position == "P" {
				if puntsByPunter != nil {
					// A non-nil map with no entry for this punter is a
					// genuine zero (no punts recorded for them this week),
					// not a fallback trigger — puntsByPunter[missing key]
					// safely returns a nil, empty slice.
					addPuntingStatsFromPBP(statLine, puntsByPunter[row.PlayerID])
				} else {
					addPuntingStatsFromBoxScore(statLine, row)
				}
			}
			out = append(out, league.WeekStatLine{
				Key:   openstats.NormalizePlayerKey(row.PlayerName, row.Position),
				Stats: statLine,
			})
		}
		out = append(out, dstWeekStatLines(stats, eastern, week)...)
		return out
	}
}

// leagueInjuryDesignationSource adapts the mirrored nflverse weekly injury
// report to league.InjuryDesignationSource (roster-ops SK spec: the IR
// eligibility gate). It is keyed by normalizePlayerKey(name, position) —
// the same join key historicalSource and leagueWeekStatsSource already
// use — because internal/fantasy's Tank01-backed live pool carries no
// GSIS ID (see league.InjuryDesignationSource's doc comment); nflTeam
// scopes the openstats query so one lookup stays cheap (openstats caps
// InjuryReports at 1000 rows per call regardless of Limit). Among that
// team's reports for the matching name+position, the highest-week row's
// ReportStatus wins — "this player's most recently reported weekly
// designation." ok is false when no report matches at all.
func leagueInjuryDesignationSource(stats *openstats.Service) league.InjuryDesignationSource {
	return func(name, position, nflTeam string) (string, bool) {
		key := openstats.NormalizePlayerKey(name, position)
		reports := stats.InjuryReports(openstats.InjuryQuery{Team: nflTeam, Limit: 1000})
		bestWeek := -1
		designation := ""
		found := false
		for _, r := range reports {
			if openstats.NormalizePlayerKey(r.PlayerName, r.Position) != key {
				continue
			}
			if r.Week > bestWeek {
				bestWeek = r.Week
				designation = r.ReportStatus
				found = true
			}
		}
		return designation, found
	}
}

// historicalSource joins previous-season nflverse totals onto pool players by
// normalized name and position. The lookup builds lazily once summaries are
// mirrored; previous-season data is static after the first sync.
func historicalSource(stats *openstats.Service) league.HistoricalSource {
	var mu sync.Mutex
	lookup := map[string]string{}
	return func(name, position string) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(lookup) == 0 {
			for _, summary := range stats.PlayerSeasonSummaries() {
				key := openstats.NormalizePlayerKey(summary.PlayerName, summary.Position)
				lookup[key] = histLine(summary)
			}
		}
		line, ok := lookup[openstats.NormalizePlayerKey(name, position)]
		return line, ok
	}
}

// histLine renders one legible previous-season line, shaped by position.
func histLine(s openstats.PlayerSeasonSummary) string {
	switch s.Position {
	case "QB":
		return fmt.Sprintf("%d · %d G · %s pass yds · %d TD · %d INT · %.1f FPts",
			s.Season, s.Games, thousands(s.PassYds), s.PassTD, s.PassInt, s.FantasyPoints)
	case "RB":
		return fmt.Sprintf("%d · %d G · %s rush yds · %d TD · %d rec · %.1f FPts",
			s.Season, s.Games, thousands(s.RushYds), s.RushTD+s.RecTD, s.Receptions, s.FantasyPoints)
	default:
		return fmt.Sprintf("%d · %d G · %d rec · %s rec yds · %d TD · %.1f FPts",
			s.Season, s.Games, s.Receptions, thousands(s.RecYds), s.RecTD, s.FantasyPoints)
	}
}

// thousands formats an int with a comma separator, US style.
func thousands(value int) string {
	raw := strconv.Itoa(value)
	if len(raw) <= 3 || value < 0 {
		return raw
	}
	var b strings.Builder
	lead := len(raw) % 3
	if lead > 0 {
		b.WriteString(raw[:lead])
	}
	for i := lead; i < len(raw); i += 3 {
		if b.Len() > 0 {
			b.WriteString(",")
		}
		b.WriteString(raw[i : i+3])
	}
	return b.String()
}

// fantasyPoolStatus projects raw fantasy-source facts into the typed league
// seam. Presentation and recovery copy stay in internal/league so Draft,
// Board, Players, and Admin cannot drift into different freshness meanings.
func fantasyPoolStatus(pool *fantasy.Service) league.PoolStatusSource {
	return func() league.PlayerPoolStatus {
		status := pool.Status()
		if status.LastError != "" {
			log.Printf("fantasy pool sync error: %s", status.LastError)
		}
		return league.PlayerPoolStatus{
			Provider:        status.Provider,
			Mode:            status.Mode,
			State:           status.State,
			Players:         status.Players,
			Target:          status.PoolLimit,
			Positions:       status.Positions,
			WithADP:         status.WithADP,
			WithProjection:  status.WithProj,
			WithBye:         status.WithBye,
			Requests:        status.Requests,
			LastSuccess:     status.LastSync,
			FreshnessWindow: status.FreshFor,
			LastError:       status.LastError,
		}
	}
}

func googleStartHandler(flow *auth.OAuth, configured bool) http.Handler {
	if configured {
		begin := flow.BeginHandler("google")
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request := r.Clone(r.Context())
			request.URL = new(url.URL)
			*request.URL = *r.URL
			query := request.URL.Query()
			query.Set("next", navigation.SafeReturnPath(query.Get("next")))
			request.URL.RawQuery = query.Encode()
			begin.ServeHTTP(w, request)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.AddFlash(r, "notice", "Sign-in is not ready yet. Ask the commissioner to finish league setup.")
		http.Redirect(w, r, "/login?setup=google", http.StatusSeeOther)
	})
}

type googleMembership interface {
	EmailAllowed(email string) bool
	CanonicalUser(user auth.User) auth.User
	BindCoManagerOnSignIn(email, name string) (league.Member, bool, error)
	EnsureMember(email, name string) (league.Member, error)
}

func googleCallbackHandler(flow *auth.OAuth, manager *auth.Manager, configured bool) http.Handler {
	return googleCallbackHandlerWithMembership(flow, manager, configured, league.Default())
}

func googleCallbackHandlerWithMembership(flow *auth.OAuth, manager *auth.Manager, configured bool, membership googleMembership) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !configured {
			http.Redirect(w, r, "/login?setup=google", http.StatusSeeOther)
			return
		}
		user, target, err := flow.Callback(r, "google")
		if err != nil {
			session.AddFlash(r, "notice", "Google sign-in did not finish. Try again, or tell the commissioner.")
			http.Redirect(w, r, "/login?error=oauth", http.StatusSeeOther)
			return
		}
		if !membership.EmailAllowed(user.Email) {
			manager.SignOut(r)
			session.AddFlash(r, "notice", "That Google account is not admitted by this league's membership policy.")
			http.Redirect(w, r, "/login?error=invite", http.StatusSeeOther)
			return
		}
		// flow.Callback signs the provider's raw identity before returning.
		// Re-sign the session with the canonical application identity after
		// raw-email admission succeeds, so every subsequent request shares
		// the same principal without allowing aliases to bypass admission.
		user = membership.CanonicalUser(user)
		if !manager.SignIn(r, user) {
			manager.SignOut(r)
			session.AddFlash(r, "notice", "Sign-in could not be completed. Try again.")
			http.Redirect(w, r, "/login?error=oauth", http.StatusSeeOther)
			return
		}
		// Sign-in creates membership only (registration wave, build item 1 —
		// AssignManager's auto-seating at sign-in retires). Every signed-in,
		// allowed email is pick'em-enrolled by definition; claiming a
		// fantasy seat is now a deliberate act at /join (build item 2), not
		// a side effect of the first sign-in a member ever makes.
		// AssignManager itself stays live — /join's atomic claim calls it —
		// this is the only call site that retires.
		//
		// A co-manager invite is checked first: an email a primary invited
		// (Store.InviteCoManager) binds to that seat on this, its first
		// sign-in (BindCoManagerOnSignIn), rather than landing seatless.
		member, bound, err := membership.BindCoManagerOnSignIn(user.Email, user.Name)
		if err != nil {
			manager.SignOut(r)
			session.AddFlash(r, "notice", "Sign-in could not be completed. Try again.")
			http.Redirect(w, r, "/login?error=oauth", http.StatusSeeOther)
			return
		}
		if !bound {
			member, err = membership.EnsureMember(user.Email, user.Name)
			if err != nil {
				manager.SignOut(r)
				session.AddFlash(r, "notice", "Sign-in could not be completed. Try again.")
				http.Redirect(w, r, "/login?error=oauth", http.StatusSeeOther)
				return
			}
		}
		if bound {
			session.AddFlash(r, "notice", "You're co-managing "+league.Default().TeamLabel(member.TeamID)+" alongside its primary manager, "+user.Name+".")
		} else if member.TeamID != "" {
			session.AddFlash(r, "notice", "Welcome back to "+league.Default().TeamLabel(member.TeamID)+", "+user.Name+".")
		} else {
			session.AddFlash(r, "notice", "You're in, "+user.Name+". Claim a fantasy seat any time a spot opens, or head straight to Pick'em.")
		}
		target = navigation.SafeReturnPath(target)
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

func googleAuthConfigured() bool {
	return strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")) != "" && strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")) != ""
}

func mapPersistenceHealthError(err error) string {
	if err == nil {
		return ""
	}
	// Health callers need a truthful failure class, not filesystem paths,
	// SQL text, or other operator-only details that could disclose layout.
	return "persistence unavailable"
}

// persistenceHealth keeps the public readiness contract testable without
// booting the singleton service: persistence poison is a readiness failure,
// while optional feed degradation is reported separately and never reaches
// this decision.
func persistenceHealth(err error) (ready bool, status int, publicError string) {
	if err != nil {
		return false, http.StatusServiceUnavailable, mapPersistenceHealthError(err)
	}
	return true, http.StatusOK, ""
}

func stateSchemaPayload(value league.StateSchemaCompatibility) map[string]any {
	return map[string]any{
		"persistedVersion":         value.PersistedVersion,
		"supportedVersion":         value.SupportedVersion,
		"persistedDatabaseVersion": value.PersistedDatabaseVersion,
		"supportedDatabaseVersion": value.SupportedDatabaseVersion,
		"compatible":               value.Compatible,
	}
}

func livenessPayload() map[string]any {
	return map[string]any{"ok": true, "liveness": true}
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

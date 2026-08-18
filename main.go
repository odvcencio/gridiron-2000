package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
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

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/mailer"
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
	league.Default().SetHistoricalSource(historicalSource(openStats))
	league.Default().SetWeekStatsSource(leagueWeekStatsSource(openStats))
	league.Default().SetInjuryDesignationSource(leagueInjuryDesignationSource(openStats))
	startBlitzPoller(runtimeContext, fantasyPool, league.Default())
	// startBlitzPre1 makes a handful of REST calls against already-final
	// games; it backgrounds itself so a slow or unreachable Tank01 never
	// delays the server accepting requests (see blitz_pre1.go).
	go startBlitzPre1(runtimeContext, fantasyPool, league.Default())
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
			// leagueConfig: "defaults" on an unconfigured checkout, or
			// "file:<path>" once a league.json loads (productization spec
			// section 4.3).
			"leagueConfig": league.Default().Config().Source,
			"time":         time.Now().UTC().Format(time.RFC3339),
		}, nil
	})
	app.API("GET /api/live/week", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return league.Default().LiveScores(ctx.Request.Context()), nil
	})
	app.API("GET /api/league/version", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		league.Default().RecordPresence(ctx.Request, time.Now())
		_, poolVersion := fantasyPool.Players()
		return map[string]any{
			"fingerprint": league.Default().StateFingerprint(poolVersion),
		}, nil
	})
	mountOwnedDataAPI(app, signalFeed, openStats, fantasyPool, os.Getenv("DATA_API_TOKEN"))

	// Team avatars (design decisions 1-3): the upload endpoint sits outside
	// gosx's action registry (its 1MB action-body cap is well under the 2MB
	// avatar limit — see avatar_handlers.go), and the serving route emits
	// its own fixed Cache-Control lifetime rather than the public-dir
	// default, since an uploaded avatar lives in the data dir, not
	// public/. Both still pass through the session/CSRF/auth middleware
	// registered above (app.Use wraps every mount, not just page routes).
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
		// Give the notify worker a bounded window to finish delivering
		// whatever was already queued before the process exits; Drain logs
		// the remaining count itself if the deadline is reached (finding
		// m1). stopNotify then cancels the worker's context so it does not
		// linger up to another 30s in a retry wait the drain has already
		// given up on.
		notifyQueue.Drain(10 * time.Second)
		stopNotify()
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
					Jersey:     player.Jersey,
					ProjStats:  player.ProjStats,
					Projection: player.Projection,
					News:       player.News,
					Status:     "Available",
					Rookie:     player.IsRookie(),
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
	eastern := openStatsEastern()
	return func() []league.GameInfo {
		games := stats.Games(0)
		now := time.Now()
		out := make([]league.GameInfo, 0, len(games))
		for _, game := range games {
			if game.GameType != "REG" {
				continue
			}
			kickoff, ok := openStatsKickoff(game, eastern)
			if !ok {
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

// openStatsKickoff parses one schedule row's kickoff instant, defaulting a
// blank gametime to the early-slate 1:00 PM ET the schedule adapter has
// always used. false means the row's date failed to parse.
func openStatsKickoff(game openstats.ScheduleGame, eastern *time.Location) (time.Time, bool) {
	gameTime := game.GameTime
	if strings.TrimSpace(gameTime) == "" {
		gameTime = "13:00"
	}
	kickoff, err := time.ParseInLocation("2006-01-02 15:04", game.GameDay+" "+gameTime, eastern)
	if err != nil {
		return time.Time{}, false
	}
	return kickoff, true
}

// openStatsGameFinal reuses the schedule adapter's exact finality rule
// (kickoff plus five hours) rather than inventing a second one — the
// week-close/final-detection discipline WP-R2's DST points-allowed band
// (dstShutout) must follow (see leagueScheduleSource).
func openStatsGameFinal(game openstats.ScheduleGame, eastern *time.Location, now time.Time) bool {
	kickoff, ok := openStatsKickoff(game, eastern)
	if !ok {
		return false
	}
	return now.After(kickoff.Add(5 * time.Hour))
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
			statLine := map[string]float64{
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
		for _, position := range []string{"QB", "RB", "WR", "TE", "K", "P", "DST"} {
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
		// AssignManager is the deliberate seat-claim act (league.Service's
		// only caller of assignMember directly): a Google sign-in with an
		// open seat claims it, exactly as before. Once every seat is
		// claimed, the league still has room for pick'em — EnsureMember
		// records membership with no team, and the sign-in proceeds rather
		// than turning the person away (many more people want pick'em than
		// want a fantasy team; see the pick'em HQ task).
		member, err := league.Default().AssignManager(user.Email, user.Name)
		seatless := false
		if err != nil && errors.Is(err, league.ErrLeagueFull) {
			member, err = league.Default().EnsureMember(user.Email, user.Name)
			seatless = true
		}
		if err != nil {
			manager.SignOut(r)
			session.AddFlash(r, "notice", fmt.Sprintf("All %d manager seats are currently claimed.", league.Default().TeamCount()))
			http.Redirect(w, r, "/login?error=full", http.StatusSeeOther)
			return
		}
		if seatless {
			session.AddFlash(r, "notice", "Every manager seat is claimed. You're in for Pick'em, "+user.Name+".")
		} else {
			session.AddFlash(r, "notice", "Welcome to "+member.TeamID+", "+user.Name+".")
		}
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

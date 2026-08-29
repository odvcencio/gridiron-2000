package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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
	"gridiron-2000/internal/navigation"
	"gridiron-2000/internal/notify"
	"gridiron-2000/internal/openstats"
	_ "gridiron-2000/modules"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/env"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// isLocalAppEnv reports whether APP_ENV names a local, non-deployed
// environment. It is an allow-list: every other label, "prod" and "staging"
// included, is a deployment. The cookie policy and AppConfig.validate share
// this one answer so the two can never disagree about where the process runs.
func isLocalAppEnv(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "", "local", "development", "test":
		return true
	}
	return false
}

// gridironSessionOptions keeps the cookie policy explicit at the one place
// where the application decides whether it is serving local plain HTTP or a
// deployed HTTPS environment. gosx defaults to Secure when AllowInsecure is
// omitted, so only known local/default environments opt in to plain HTTP.
func gridironSessionOptions(appEnv string) session.Options {
	localHTTP := isLocalAppEnv(appEnv)
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
	if err := env.LoadDir(server.ResolveAppRoot(thisFile), ""); err != nil {
		log.Fatal(err)
	}
	cfg, err := AppConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	app, rt, err := BuildApp(cfg)
	if err != nil {
		log.Fatal(err)
	}
	// The notify worker keeps its own cancellation, separate from
	// runtimeContext: on shutdown the HTTP server stops accepting requests
	// immediately, but the worker keeps draining whatever is already queued
	// (see rt.Drain below the server's shutdown branch) until it finishes or
	// the drain deadline expires. A message the ledger already marked "sent"
	// must not be silently abandoned (design spec section 6.3; finding m1).
	if rt.StopNotify != nil {
		defer rt.StopNotify()
	}
	rt.Start(runtimeContext)
	log.Printf("%s listening on http://localhost:%s", rt.AppName, rt.Port)
	if rt.HQV1 != nil {
		log.Printf("Commissioner HQ v1 provider listening on its private listener")
	}
	type runtimeServerError struct {
		name string
		err  error
	}
	serverErrors := make(chan runtimeServerError, 2)
	go func() {
		serverErrors <- runtimeServerError{name: "application", err: app.ListenAndServe(":" + rt.Port)}
	}()
	if rt.HQV1 != nil {
		go func() {
			serverErrors <- runtimeServerError{name: "Commissioner HQ provider", err: rt.HQV1.Serve()}
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
	if rt.HQV1 != nil {
		shutdownGroup.Add(1)
		go func() {
			defer shutdownGroup.Done()
			if err := rt.HQV1.Shutdown(shutdownContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("Commissioner HQ provider shutdown: %v", err)
			}
		}()
	}
	shutdownGroup.Wait()
	cancelShutdown()
	// Give the notify worker a bounded window to finish delivering whatever
	// was already queued before the process exits. This applies equally to a
	// signal and either listener failing unexpectedly.
	if rt.Drain != nil {
		rt.Drain(10 * time.Second)
	}
	if rt.StopNotify != nil {
		rt.StopNotify()
	}
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

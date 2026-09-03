// Command gridiron-sim drives a rehearsal draft against a running Gridiron
// instance that was started with GRIDIRON_TEST_AUTH=1.
//
//	gridiron-sim draft --url http://127.0.0.1:8099 --managers 8 --delay 3s
//
// It is a rehearsal tool and never a production tool. Every request it
// makes rides the harness surface: the read-only /test/draft endpoint and
// a set of @sim.test identities that a server only accepts while
// GRIDIRON_TEST_AUTH=1 is set. A production instance mounts neither, and
// the loopback guard in front of the harness routes rejects a remote
// caller with 403. The preflight below turns both answers into a non-zero
// exit, so pointing this command at a real league stops it instead of
// seating bots in it.
//
// The default seat source, --seats claim, seats generated @sim.test
// identities into whatever seats are still open — the shape a brand-new
// league needs. A league whose seats are already held by real members (a
// faithful copy of a live league, for example) has none open, so
// --seats existing signs in as each seat's CURRENT holder instead,
// reading the seat -> email map the harness's /test/draft teams rows
// carry. --members restricts that to a named subset of those emails,
// letting a rehearsal drive some seats while leaving the rest for a human
// or the server's own autopick.
//
// The command seats managers and marks them ready. It never starts the
// draft: a commissioner does that from the console, or with a POST to
// /draft/__actions/draft-start. Once the draft is live, each bot picks
// after its own think time.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gridiron-2000/internal/sim/draft"
)

// simTeamNames names the seats a rehearsal claims, in claim order. The
// list caps --managers: a league seats at most this many rehearsal
// managers.
var simTeamNames = []string{
	"Kernel Panic",
	"Segfault City",
	"Null Pointers",
	"Garbage Collectors",
	"Race Condition",
	"Big Endians",
	"Stack Overflow",
	"Hot Path",
	"Byte Me",
	"Cache Money",
	"Fork Bomb",
	"Heap Sort",
	"Idle Loop",
	"Null Route",
}

const (
	// defaultURL is the loopback address the harness instructions use.
	defaultURL = "http://127.0.0.1:8099"
	// defaultManagers seats a standard eight-team league.
	defaultManagers = 8
	// seatSourceClaim is the default --seats value: claim whatever seats
	// are still open with generated @sim.test identities.
	seatSourceClaim = "claim"
	// seatSourceExisting is the --seats value that signs in as each
	// seat's CURRENT holder instead of claiming a seat.
	seatSourceExisting = "existing"
	// defaultDelay is one bot's think time before it submits a pick.
	defaultDelay = 3 * time.Second
	// commissionerEmail names the identity this command primes to read the
	// draft state. It performs no commissioner action: it never starts,
	// pauses, extends, or forces a pick. A primed seatless session is only
	// the cheapest way to read /test/draft without holding a seat, so the
	// address needs a session, not the target's COMMISSIONER_EMAILS.
	commissionerEmail = "commish@sim.test"
	// waitForStart is the pause between polls while the draft is closed.
	waitForStart = 2 * time.Second
	// waitForSeat is the pause while a seat this run does not drive holds
	// the clock. It is shorter than waitForStart because a human manager
	// can pick at any moment and the next bot must not lose its turn.
	waitForSeat = time.Second
	// preflightTimeout bounds the one request that proves the target is a
	// harness build.
	preflightTimeout = 10 * time.Second
	// pickBackoff is the minimum pause after a pick attempt that failed. It
	// is independent of --delay on purpose: --delay 0 asks for no think
	// time, not for a hot loop against a server that keeps rejecting the
	// same pick.
	pickBackoff = 500 * time.Millisecond
)

func main() {
	log.SetFlags(log.Ltime)
	log.SetPrefix("gridiron-sim: ")
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `gridiron-sim drives a rehearsal draft against a local harness instance.

usage:
  gridiron-sim draft [--url URL] [--managers N] [--delay D]
  gridiron-sim draft [--url URL] [--seats existing] [--delay D]
  gridiron-sim draft [--url URL] [--members a@x,b@y] [--delay D]

--seats claim (the default) seats generated @sim.test identities into
whatever seats are open. --seats existing signs in as each seat's
CURRENT holder instead, for a league whose seats are already real
members. --members restricts --seats existing to a comma-separated list
of those members' emails, driving only that subset.

Start the target with GRIDIRON_TEST_AUTH=1 first. Never point this
command at production.
`)
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("no subcommand; the only subcommand is draft")
	}
	switch args[0] {
	case "draft":
		return runDraft(args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q; the only subcommand is draft", args[0])
	}
}

func runDraft(args []string) error {
	flags := flag.NewFlagSet("draft", flag.ContinueOnError)
	flags.Usage = usage
	base := flags.String("url", defaultURL, "base URL of the running harness instance")
	managerCount := flags.Int("managers", defaultManagers, "how many seats to claim (1 to 14); --seats claim only")
	seats := flags.String("seats", seatSourceClaim, `seat source: "claim" seats new @sim.test identities, "existing" signs in as each seat's current holder`)
	members := flags.String("members", "", "comma-separated emails to drive (each must already hold a seat); implies --seats existing")
	delay := flags.Duration("delay", defaultDelay, "think time before each bot submits its pick")
	if err := flags.Parse(args); err != nil {
		// flag.ContinueOnError already printed the usage for -h; asking for
		// help is not a failure, so exit 0.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	seatSource := strings.ToLower(strings.TrimSpace(*seats))
	if seatSource == "" {
		seatSource = seatSourceClaim
	}
	memberList := splitMembers(*members)
	if len(memberList) > 0 {
		// --members names specific seat holders; that only makes sense
		// against the existing seats a real league already has, so it
		// selects that seat source on its own rather than requiring both
		// flags spelled out together.
		seatSource = seatSourceExisting
	}
	switch seatSource {
	case seatSourceClaim, seatSourceExisting:
	default:
		return fmt.Errorf("--seats is %q; it must be %q or %q", *seats, seatSourceClaim, seatSourceExisting)
	}
	if seatSource == seatSourceClaim && (*managerCount < 1 || *managerCount > len(simTeamNames)) {
		return fmt.Errorf("--managers is %d; it must be between 1 and %d", *managerCount, len(simTeamNames))
	}
	if *delay < 0 {
		return fmt.Errorf("--delay is %s; it must not be negative", *delay)
	}
	target := strings.TrimRight(strings.TrimSpace(*base), "/")
	if target == "" {
		return errors.New("--url is empty")
	}
	if err := preflight(target); err != nil {
		return err
	}

	// One Ctrl-C stops the loop between actions; a second one kills the
	// process outright, because signal.NotifyContext restores the default
	// handler after the first signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var commish *draft.Bot
	var managers []*manager
	var err error
	switch seatSource {
	case seatSourceExisting:
		commish, managers, err = seatExisting(target, memberList)
	default:
		commish, managers, err = seat(target, *managerCount)
	}
	if err != nil {
		return err
	}
	if len(managers) == 0 {
		if seatSource == seatSourceExisting {
			return errors.New("no seat holder was signed in; check --members, or that /test/draft reports member_email for the league's seats")
		}
		return errors.New("no seat was claimed; the league may already be full of other managers")
	}
	ready, err := prepareSeats(commish, managers)
	if err != nil {
		return err
	}
	if ready == 0 {
		return errors.New("no seat is ready; nothing to rehearse")
	}
	if ready < len(managers) {
		log.Printf("%d of %d claimed seats are ready; the commissioner cannot start the draft until every claimed seat is",
			ready, len(managers))
	}
	log.Printf("%d managers seated and ready; start the draft from the commissioner console (or curl /draft/__actions/draft-start), bots pick every %s",
		ready, *delay)
	return drive(ctx, commish, managers, *delay)
}

// preflight proves the target is reachable and mounts the harness surface.
// Its three failures are the three ways a rehearsal is pointed at the
// wrong thing: nothing listening, a remote address the loopback guard
// refuses, and a build that never mounted the routes at all.
func preflight(target string) error {
	client := &http.Client{Timeout: preflightTimeout}
	probe := target + "/test/draft"
	response, err := client.Get(probe)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w\nStart a harness instance first: GRIDIRON_TEST_AUTH=1 ... gridiron-2000", probe, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	switch response.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusForbidden:
		return fmt.Errorf("%s answered 403: the harness routes are loopback-only.\nRun this command on the same machine as the server and point --url at 127.0.0.1", probe)
	case http.StatusNotFound:
		return fmt.Errorf("%s answered 404: the target mounts no harness routes.\nRestart it with GRIDIRON_TEST_AUTH=1, or point --url at a rehearsal instance instead of production", probe)
	default:
		return fmt.Errorf("%s answered %d: %s", probe, response.StatusCode, strings.TrimSpace(string(body)))
	}
}

// manager is one seated rehearsal bot and the seat it holds. team is the
// name the server reports for that seat, not the name this run asked for:
// a bot that already held a different seat keeps it, and every log line
// must name the seat that actually picks. The bot carries the identity, so
// a log line reads it from bot.Email.
type manager struct {
	bot  *draft.Bot
	team string
}

// seat primes the commissioner, then claims one seat per manager. It does
// not mark anything ready; prepareSeats does that, because readiness needs
// the server's own view of every seat first.
//
// The commissioner primes first on purpose. Once the last seat is claimed,
// /join renders its closed state with no form and therefore no CSRF token,
// so a commissioner that primed later would never get one.
//
// A seat that is already claimed is not fatal. The bot re-reads its own
// state: an identity that claimed the seat on an earlier run keeps it and
// drives on, and only an identity with no seat at all is dropped.
func seat(target string, count int) (*draft.Bot, []*manager, error) {
	commish := draft.New(target, commissionerEmail, "Commissioner")
	if err := commish.Prime(); err != nil {
		return nil, nil, fmt.Errorf("prime the commissioner (%s): %w", commissionerEmail, err)
	}
	managers := make([]*manager, 0, count)
	for index := 0; index < count; index++ {
		email := fmt.Sprintf("manager%d@sim.test", index+1)
		team := simTeamNames[index]
		bot := draft.New(target, email, team+" Manager")
		if err := bot.Prime(); err != nil {
			log.Printf("skip %s: prime failed: %v", email, err)
			continue
		}
		if err := bot.Join(team); err != nil {
			if _, stateErr := bot.State(); stateErr != nil || bot.TeamID == "" {
				log.Printf("skip %s: seat %q is not available: %v", email, team, err)
				continue
			}
			log.Printf("%s already holds seat %s; keeping it", email, bot.TeamID)
		}
		// Join reads viewer_team_id back from the server, so an empty seat
		// here means the claim did not stick. Driving such a bot would post
		// a blank team_id at every later action.
		if bot.TeamID == "" {
			log.Printf("skip %s: the claim on seat %q left no seat id", email, team)
			continue
		}
		managers = append(managers, &manager{bot: bot, team: team})
	}
	return commish, managers, nil
}

// splitMembers parses --members's comma-separated email list, trimming
// whitespace and dropping empty entries so "a@x, ,b@y," reads as two
// emails, not three.
func splitMembers(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if email := strings.TrimSpace(part); email != "" {
			out = append(out, email)
		}
	}
	return out
}

// stringField reads a string value out of a /test/draft team row (or any
// other JSON-decoded map), returning "" for a missing or non-string key.
func stringField(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return value
}

// seatExisting signs in as seats' CURRENT holders instead of claiming a
// seat: the mode a league whose seats are already real members needs,
// where every seat is already taken and "claim" would find nothing open.
// It reads the seat -> email map the harness's /test/draft teams rows
// carry (member_email, added for exactly this) and, for each seat that
// has a member and (when members is non-empty) is in that allow-list,
// signs in as that email — the same bot.Prime that an already-seated
// viewer redirects to /team for its CSRF token, per Prime's doc comment.
//
// A seat with no member (member_email == "") is skipped: there is no one
// to sign in as, and this mode never claims a seat itself. When members
// names an email this run never signs in — a typo, or a seat that traded
// or is a co-manager-only address — that email is logged so the gap is
// visible instead of silently driving fewer seats than asked for.
func seatExisting(target string, members []string) (*draft.Bot, []*manager, error) {
	commish := draft.New(target, commissionerEmail, "Commissioner")
	if err := commish.Prime(); err != nil {
		return nil, nil, fmt.Errorf("prime the commissioner (%s): %w", commissionerEmail, err)
	}
	state, err := commish.State()
	if err != nil {
		return nil, nil, fmt.Errorf("read the seat grid: %w", err)
	}
	want := make(map[string]bool, len(members))
	for _, email := range members {
		want[strings.ToLower(email)] = true
	}
	seen := make(map[string]bool, len(members))
	managers := make([]*manager, 0, len(state.Teams))
	for _, row := range state.Teams {
		id := stringField(row, "id")
		email := strings.TrimSpace(stringField(row, "member_email"))
		if email == "" {
			continue
		}
		if len(want) > 0 {
			if !want[strings.ToLower(email)] {
				continue
			}
			seen[strings.ToLower(email)] = true
		}
		name := stringField(row, "manager")
		bot := draft.New(target, email, name)
		if err := bot.Prime(); err != nil {
			log.Printf("skip %s: prime failed: %v", email, err)
			continue
		}
		if _, err := bot.State(); err != nil {
			log.Printf("skip %s: read own seat: %v", email, err)
			continue
		}
		if bot.TeamID == "" {
			log.Printf("skip %s: signed in but the server reports no seat", email)
			continue
		}
		if id != "" && bot.TeamID != id {
			log.Printf("skip %s: signed in but holds seat %q, not the %q /test/draft named", email, bot.TeamID, id)
			continue
		}
		managers = append(managers, &manager{bot: bot, team: stringField(row, "name")})
	}
	for _, email := range members {
		if !seen[strings.ToLower(email)] {
			log.Printf("--members named %s but no claimed seat's member_email matched it", email)
		}
	}
	return commish, managers, nil
}

// prepareSeats names every claimed seat from the server's own team grid and
// marks the seats that are not ready yet. It returns how many of the driven
// seats the server reports ready afterwards.
//
// The read is the whole point. Service.ToggleReady FLIPS the flag
// (internal/league/service.go), so calling it for every seat would un-ready
// a league that a previous run already readied — and the "seated and ready"
// line would then be a lie. Reading first makes a rerun idempotent.
func prepareSeats(commish *draft.Bot, managers []*manager) (int, error) {
	before, err := commish.State()
	if err != nil {
		return 0, fmt.Errorf("read the seat grid: %w", err)
	}
	for _, m := range managers {
		if name := seatName(before, m.bot.TeamID); name != "" {
			m.team = name
		}
		if seatReady(before, m.bot.TeamID) {
			continue
		}
		if err := m.bot.ToggleReady(); err != nil {
			log.Printf("%s did not mark seat %q ready: %v", m.bot.Email, m.team, err)
		}
	}
	after, err := commish.State()
	if err != nil {
		return 0, fmt.Errorf("re-read the seat grid: %w", err)
	}
	ready := 0
	for _, m := range managers {
		if seatReady(after, m.bot.TeamID) {
			ready++
			continue
		}
		log.Printf("seat %q (%s) is still not ready", m.team, m.bot.Email)
	}
	return ready, nil
}

// seatRow returns the draft-room team grid row for seat id, or nil.
// draftTeamMaps (internal/league/service.go) builds each row, so "id",
// "name", and "ready" are the keys this file reads.
func seatRow(state draft.DraftState, id string) map[string]any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	for _, row := range state.Teams {
		if rowID, _ := row["id"].(string); rowID == id {
			return row
		}
	}
	return nil
}

// seatName returns the name the server gives seat id, or an empty string.
func seatName(state draft.DraftState, id string) string {
	name, _ := seatRow(state, id)["name"].(string)
	return strings.TrimSpace(name)
}

// seatReady reports whether the server has seat id marked ready.
func seatReady(state draft.DraftState, id string) bool {
	ready, _ := seatRow(state, id)["ready"].(bool)
	return ready
}

// drive runs the rehearsal until the draft completes or the caller stops
// it. It never starts the draft itself.
func drive(ctx context.Context, commish *draft.Bot, managers []*manager, delay time.Duration) error {
	announcedWait := false
	for {
		if ctx.Err() != nil {
			log.Print("stopped; the draft keeps its state on the server")
			return nil
		}
		state, err := commish.State()
		if err != nil {
			log.Printf("read the draft state: %v", err)
			if !pause(ctx, waitForStart) {
				return nil
			}
			continue
		}
		if state.Complete {
			log.Printf("draft complete: %d picks over %d rounds", len(state.Picks), state.Rounds)
			return nil
		}
		if !state.Started {
			if !announcedWait {
				log.Print("waiting for the commissioner to start the draft")
				announcedWait = true
			}
			heartbeat(managers)
			if !pause(ctx, waitForStart) {
				return nil
			}
			continue
		}
		announcedWait = false
		onClock := seatHolder(managers, state.OnClockID)
		if onClock == nil {
			// A seat this run does not drive holds the clock: a human
			// manager, or a seat another rehearsal claimed.
			heartbeat(managers)
			if !pause(ctx, waitForSeat) {
				return nil
			}
			continue
		}
		if !pause(ctx, delay) { // think time
			return nil
		}
		// The clock can move while a bot thinks: an expiry auto-pick or a
		// commissioner force-pick resolves the turn without it. Read again
		// so the pick number and the player name in the log line describe
		// the pick the server is actually waiting on.
		current, err := commish.State()
		if err != nil {
			log.Printf("read the draft state: %v", err)
			if !pause(ctx, pickBackoff) {
				return nil
			}
			continue
		}
		if seatHolder(managers, current.OnClockID) != onClock {
			// Somebody else took the turn. Let the next round read the
			// board fresh instead of submitting a pick that must fail.
			heartbeat(managers)
			continue
		}
		if !makePick(current, onClock) {
			if !pause(ctx, pickBackoff) {
				return nil
			}
		}
		heartbeat(managers)
	}
}

// makePick submits one pick for the seat on the clock and reports whether
// the server took it. A rejection is logged and never fatal, so the caller
// backs off and reads the board again rather than stopping the rehearsal.
func makePick(state draft.DraftState, onClock *manager) bool {
	playerID, err := onClock.bot.NextPick()
	if err != nil {
		log.Printf("pick %d for %s: no eligible player: %v", state.PickNumber, onClock.team, err)
		return false
	}
	result, err := onClock.bot.MakePick(playerID)
	if err != nil {
		log.Printf("pick %d for %s: %v", state.PickNumber, onClock.team, err)
		return false
	}
	if !result.OK {
		log.Printf("pick %d for %s rejected %s: %s", state.PickNumber, onClock.team, playerLabel(state, playerID), result.Message)
		return false
	}
	log.Printf("pick %d: %s takes %s", state.PickNumber, onClock.team, playerLabel(state, playerID))
	return true
}

// seatHolder returns the manager that holds seat id, or nil.
func seatHolder(managers []*manager, id string) *manager {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	for _, m := range managers {
		if m.bot.TeamID == id {
			return m
		}
	}
	return nil
}

// playerLabel names a player from the pool page the caller already read.
// It falls back to the raw id for a late-round kicker, punter, or defense:
// NextPick reaches those through its own pos=K, pos=P, and pos=DST pages,
// which this page does not carry. The id still identifies the pick, and
// the room's own pick tape carries the name.
func playerLabel(state draft.DraftState, playerID string) string {
	for _, row := range state.Available {
		if id, _ := row["id"].(string); id != playerID {
			continue
		}
		name, _ := row["name"].(string)
		position, _ := row["position"].(string)
		switch {
		case name != "" && position != "":
			return name + " (" + position + ")"
		case name != "":
			return name
		}
		break
	}
	return playerID
}

// heartbeat reports every seat as an open tab would, so the room's
// presence view matches the rehearsal.
func heartbeat(managers []*manager) {
	for _, m := range managers {
		if err := m.bot.Presence(); err != nil {
			log.Printf("presence for %s: %v", m.team, err)
		}
	}
}

// pause waits for d, and reports false when the caller was stopped first.
func pause(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

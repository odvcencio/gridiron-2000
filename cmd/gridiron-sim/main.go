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
	// defaultDelay is one bot's think time before it submits a pick.
	defaultDelay = 3 * time.Second
	// commissionerEmail must match the target's COMMISSIONER_EMAILS.
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
	managerCount := flags.Int("managers", defaultManagers, "how many seats to claim (1 to 14)")
	delay := flags.Duration("delay", defaultDelay, "think time before each bot submits its pick")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *managerCount < 1 || *managerCount > len(simTeamNames) {
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

	commish, managers, err := seat(target, *managerCount)
	if err != nil {
		return err
	}
	if len(managers) == 0 {
		return errors.New("no seat was claimed; the league may already be full of other managers")
	}
	log.Printf("%d managers seated and ready; start the draft from the commissioner console (or curl /draft/__actions/draft-start), bots pick every %s",
		len(managers), *delay)
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

// manager is one seated rehearsal bot and the team name it claimed.
type manager struct {
	bot  *draft.Bot
	team string
}

// seat primes the commissioner, then claims one seat per manager and marks
// each ready.
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
		if err := bot.ToggleReady(); err != nil {
			// A seat that is already ready reports an error here; the seat
			// is still usable, so keep it and say what happened.
			log.Printf("%s did not toggle ready: %v", email, err)
		}
		managers = append(managers, &manager{bot: bot, team: team})
	}
	return commish, managers, nil
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
		makePick(state, onClock)
		heartbeat(managers)
	}
}

// makePick submits one pick for the seat on the clock. A rejection is
// logged and never fatal: the clock can expire during a bot's think time,
// which resolves the pick without it and leaves the next seat on the
// clock.
func makePick(state draft.DraftState, onClock *manager) {
	playerID, err := onClock.bot.NextPick()
	if err != nil {
		log.Printf("pick %d for %s: no eligible player: %v", state.PickNumber, onClock.team, err)
		return
	}
	result, err := onClock.bot.MakePick(playerID)
	if err != nil {
		log.Printf("pick %d for %s: %v", state.PickNumber, onClock.team, err)
		return
	}
	if !result.OK {
		log.Printf("pick %d for %s rejected %s: %s", state.PickNumber, onClock.team, playerLabel(state, playerID), result.Message)
		return
	}
	log.Printf("pick %d: %s takes %s", state.PickNumber, onClock.team, playerLabel(state, playerID))
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
// It falls back to the raw id for a late-round kicker or defense: NextPick
// reaches those through its own pos=K and pos=DST pages, which this page
// does not carry. The id still identifies the pick, and the room's own
// pick tape carries the name.
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

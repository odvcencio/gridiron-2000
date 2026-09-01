// Command seasonprobe is a read-only, operator-run upstream probe
// (spec.gridiron.gap-closure GC-2b item 1). Through statrelay, it fetches
// the season's regular-season week-1 games list and diffs team pairs and
// Eastern dates against the mirrored nflverse games.csv — validating the
// same date-plus-team-pair assumption internal/livescore's match.go
// relies on for the whole season — then fetches one completed preseason
// box score and runs it through the same parser the live poller and the
// fantasy pool use. It prints a PASS/FAIL verdict per check and exits
// non-zero the moment any check fails.
//
// It costs a handful of real upstream calls (about six) every time it
// runs and MUST NOT be run against production or scheduled automatically;
// an operator runs it by hand, through the port-forward pattern
// scripts/gameday-preflight.sh already uses for other read-only checks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("seasonprobe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tank01BaseURL := flags.String("tank01-base-url", os.Getenv("TANK01_BASE_URL"), "statrelay base URL (env TANK01_BASE_URL)")
	openStatsRoot := flags.String("open-stats-root", envOr("OPEN_STATS_ROOT", "data/open-stats"), "mirrored nflverse root (env OPEN_STATS_ROOT)")
	season := flags.Int("season", envIntOr("NFL_SEASON", defaultSeason), "NFL season both checks target")
	week := flags.Int("week", 1, "regular-season week to diff against the mirror")
	preseasonWeek := flags.String("preseason-week", "1", `Tank01 "week" query param to search for a completed preseason game`)
	captureDir := flags.String("capture-dir", "", "directory to save raw payloads for the release receipt (optional)")
	timeout := flags.Duration("timeout", 30*time.Second, "overall timeout for every check")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: seasonprobe --tank01-base-url <url> --open-stats-root <dir> [--season N] [--week N] [--preseason-week N] [--capture-dir <dir>]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Read-only. Costs about five real Tank01 calls through statrelay. Operator-run only; never scheduled, never run against production by an agent.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	if strings.TrimSpace(*tank01BaseURL) == "" {
		fmt.Fprintln(stderr, "seasonprobe: --tank01-base-url (or TANK01_BASE_URL) is required")
		flags.Usage()
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cfg := ProbeConfig{
		Tank01BaseURL: *tank01BaseURL,
		OpenStatsRoot: *openStatsRoot,
		Season:        *season,
		Week:          *week,
		PreseasonWeek: *preseasonWeek,
		CaptureDir:    *captureDir,
	}
	results, err := Run(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "seasonprobe: %v\n", err)
		return 1
	}

	allPass := true
	for _, result := range results {
		status := "PASS"
		if !result.Pass {
			status, allPass = "FAIL", false
		}
		fmt.Fprintf(stdout, "%-4s %s: %s\n", status, result.Name, result.Detail)
	}
	if allPass {
		fmt.Fprintln(stdout, "seasonprobe: all checks passed")
		return 0
	}
	fmt.Fprintln(stdout, "seasonprobe: one or more checks failed")
	return 1
}

// defaultSeason is the spec-pinned target season (GC-2b, the 2026
// season); --season or NFL_SEASON overrides it.
const defaultSeason = 2026

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

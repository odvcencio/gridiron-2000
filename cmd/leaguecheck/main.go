// Command leaguecheck validates and summarizes one league.json without
// starting Gridiron or opening its state database.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gridiron-2000/internal/league"
)

type configSummary struct {
	Status     string            `json:"status"`
	Source     string            `json:"source"`
	League     leagueSummary     `json:"league"`
	Draft      draftSummary      `json:"draft"`
	Roster     rosterSummary     `json:"roster"`
	Membership membershipSummary `json:"membership"`
	Waivers    waiverSummary     `json:"waivers"`
	Trades     tradeSummary      `json:"trades"`
}

type leagueSummary struct {
	Name          string `json:"name"`
	ShortCode     string `json:"shortCode"`
	Mode          string `json:"mode"`
	Season        int    `json:"season"`
	Teams         int    `json:"teams"`
	Timezone      string `json:"timezone"`
	PublicURL     string `json:"publicUrl"`
	ScoringFormat string `json:"scoringFormat"`
	// ReceptionPoints (GC-1 fix 2) is the reception rule value
	// scoring_format implies — the value a fresh league seeds at first
	// boot (league.ReceptionPointsForScoringFormat). leaguecheck never
	// opens the state database (see this command's own doc comment), so
	// it cannot see whether a commissioner already edited scoring; a
	// mismatch against the shipped default (see the warnings below)
	// still means "check Scoring Settings."
	ReceptionPoints float64 `json:"receptionPoints"`
}

type draftSummary struct {
	Meeting string `json:"meeting"`
	Rounds  int    `json:"rounds"`
	Format  string `json:"format,omitempty"`
}

type rosterSummary struct {
	Preset         string `json:"preset,omitempty"`
	Starters       int    `json:"starters"`
	Bench          int    `json:"bench"`
	Reserve        int    `json:"reserve"`
	IR             int    `json:"ir"`
	Draftable      int    `json:"draftablePerTeam"`
	LeagueCapacity int    `json:"leagueCapacity"`
}

type membershipSummary struct {
	AllowedDomain string `json:"allowedDomain,omitempty"`
	Admission     string `json:"admission"`
}

type waiverSummary struct {
	Mode            string `json:"mode"`
	ProcessTime     string `json:"processTime"`
	ClearDays       int    `json:"clearDays"`
	SeasonWeightPct int    `json:"seasonWeightPct"`
	FAABBudget      int    `json:"faabBudget"`
}

type tradeSummary struct {
	Deadline    string `json:"deadline,omitempty"`
	Veto        string `json:"veto"`
	ReviewHours int    `json:"reviewHours"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("leaguecheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "path to the league.json to validate")
	format := flags.String("format", "text", "output format: text or json")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: leaguecheck --file <league.json> [--format text|json]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*file) == "" {
		flags.Usage()
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "leaguecheck: --format must be text or json, got %q\n", *format)
		return 2
	}

	cfg, warnings, err := league.LoadConfigFileWithEnvOverrides(*file)
	if err != nil {
		fmt.Fprintf(stderr, "leaguecheck: invalid: %v\n", err)
		return 1
	}
	for _, warning := range warnings {
		fmt.Fprintf(stderr, "warning: league config: %s\n", warning)
	}
	summary := summarize(cfg)
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summary); err != nil {
			fmt.Fprintf(stderr, "leaguecheck: encode result: %v\n", err)
			return 1
		}
		return 0
	}
	writeText(stdout, summary)
	return 0
}

func summarize(cfg league.Config) configSummary {
	domain := strings.TrimSpace(cfg.Membership.AllowedDomain)
	admission := "runtime invitations / allowlist (or open setup)"
	if domain != "" {
		admission = "domain @" + domain + " plus runtime invitations / allowlist"
	}
	deadline := strings.TrimSpace(cfg.Trades.Deadline)
	meeting := cfg.DraftAt.Format(time.RFC3339)
	return configSummary{
		Status: "ok",
		Source: cfg.Source,
		League: leagueSummary{
			Name: cfg.Name, ShortCode: cfg.ShortCode, Mode: cfg.ModeLabel,
			Season: cfg.Season, Teams: len(cfg.Teams), Timezone: cfg.Timezone,
			PublicURL: cfg.URL, ScoringFormat: cfg.ScoringFormat,
			ReceptionPoints: league.ReceptionPointsForScoringFormat(cfg.ScoringFormat),
		},
		Draft: draftSummary{Meeting: meeting, Rounds: cfg.Rounds, Format: cfg.FormatLabel},
		Roster: rosterSummary{
			Preset: cfg.RosterPresetName, Starters: cfg.Roster.Starters(),
			Bench: cfg.Roster.Bench, Reserve: cfg.Roster.ReserveTotal(), IR: cfg.Roster.IR,
			Draftable: cfg.Roster.Total(), LeagueCapacity: len(cfg.Teams) * cfg.Roster.Total(),
		},
		Membership: membershipSummary{AllowedDomain: domain, Admission: admission},
		Waivers: waiverSummary{
			Mode: cfg.Waivers.Mode, ProcessTime: cfg.Waivers.ProcessTime,
			ClearDays: cfg.Waivers.ClearDays, SeasonWeightPct: cfg.Waivers.SeasonWeightPct,
			FAABBudget: cfg.Waivers.FAABBudget,
		},
		Trades: tradeSummary{Deadline: deadline, Veto: cfg.Trades.Veto, ReviewHours: cfg.Trades.ReviewHours},
	}
}

func writeText(output io.Writer, summary configSummary) {
	fmt.Fprintf(output, "OK %s (%s)\n", summary.League.Name, summary.League.ShortCode)
	fmt.Fprintf(output, "source: %s\n", summary.Source)
	fmt.Fprintf(output, "league: %d teams · %s · %d · %s (reception %g)\n", summary.League.Teams, summary.League.Mode, summary.League.Season, summary.League.ScoringFormat, summary.League.ReceptionPoints)
	fmt.Fprintf(output, "public URL: %s\n", summary.League.PublicURL)
	fmt.Fprintf(output, "draft meeting: %s · %s · %d rounds\n", summary.Draft.Meeting, summary.League.Timezone, summary.Draft.Rounds)
	fmt.Fprintf(output, "roster: %d starters + %d bench + %d reserve = %d draftable per team; %d league slots; %d IR\n", summary.Roster.Starters, summary.Roster.Bench, summary.Roster.Reserve, summary.Roster.Draftable, summary.Roster.LeagueCapacity, summary.Roster.IR)
	fmt.Fprintf(output, "membership: %s\n", summary.Membership.Admission)
	fmt.Fprintf(output, "waivers: %s · %s local · %d clear days · FAAB %d\n", summary.Waivers.Mode, summary.Waivers.ProcessTime, summary.Waivers.ClearDays, summary.Waivers.FAABBudget)
	deadline := summary.Trades.Deadline
	if deadline == "" {
		deadline = "none configured"
	}
	fmt.Fprintf(output, "trades: veto %s · %dh review · deadline %s\n", summary.Trades.Veto, summary.Trades.ReviewHours, deadline)
}

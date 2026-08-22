// Command identitydoctor projects IDENTITY_ALIASES reconciliation against an
// offline SQLite or legacy JSON state snapshot. It never opens a Store or
// writes state, and its JSON output contains only counts and a bounded category.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"gridiron-2000/internal/identity"
	"gridiron-2000/internal/league"
)

const (
	exitReady    = 0
	exitConflict = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("identitydoctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonSnapshot := flags.String("snapshot", "", "offline legacy JSON state snapshot")
	sqliteSnapshot := flags.String("sqlite-snapshot", "", "offline SQLite state snapshot")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 ||
		(*jsonSnapshot == "") == (*sqliteSnapshot == "") {
		writeReport(stdout, league.IdentityPreflightReport{ConflictCategory: "usage"})
		return exitConflict
	}

	resolver, err := identity.FromEnv()
	if err != nil {
		writeReport(stdout, league.IdentityPreflightReport{ConflictCategory: "configuration"})
		return exitConflict
	}

	if *sqliteSnapshot != "" {
		report := league.PreflightIdentityAliasesFromSQLiteSnapshot(*sqliteSnapshot, resolver)
		writeReport(stdout, report)
		if !report.Ready {
			return exitConflict
		}
		return exitReady
	}

	file, err := os.Open(*jsonSnapshot)
	if err != nil {
		writeReport(stdout, league.IdentityPreflightReport{ConflictCategory: "snapshot_read"})
		return exitConflict
	}
	defer file.Close()

	var snapshot league.PersistedState
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		writeReport(stdout, league.IdentityPreflightReport{ConflictCategory: "snapshot_decode"})
		return exitConflict
	}
	if err := requireEOF(decoder); err != nil {
		writeReport(stdout, league.IdentityPreflightReport{ConflictCategory: "snapshot_decode"})
		return exitConflict
	}

	report := league.PreflightIdentityAliases(snapshot, resolver)
	writeReport(stdout, report)
	if !report.Ready {
		return exitConflict
	}
	return exitReady
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("snapshot contains multiple JSON values")
}

func writeReport(writer io.Writer, report league.IdentityPreflightReport) {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(report)
}

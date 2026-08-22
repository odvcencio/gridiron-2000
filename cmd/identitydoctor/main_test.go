package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

const (
	doctorAlias     = "commissioner.alias@example.org"
	doctorCanonical = "commissioner@example.com"
)

func TestIdentityDoctorReportsOnlyAggregateProjection(t *testing.T) {
	t.Setenv("IDENTITY_ALIASES", doctorAlias+"="+doctorCanonical)
	path := writeSnapshot(t, league.PersistedState{
		Members: map[string]league.Member{
			doctorAlias: {TeamID: "private-team", Email: doctorAlias},
		},
	})
	var stdout bytes.Buffer
	if code := run([]string{"-snapshot", path}, &stdout); code != exitReady {
		t.Fatalf("exit = %d, output = %s", code, stdout.String())
	}
	var report league.IdentityPreflightReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Ready || !report.WouldChange || report.Before.Members != 1 || report.After.Members != 1 {
		t.Fatalf("report = %+v", report)
	}
	for _, private := range []string{doctorAlias, doctorCanonical, "private-team", path} {
		if strings.Contains(stdout.String(), private) {
			t.Fatalf("doctor output leaked %q: %s", private, stdout.String())
		}
	}
}

func TestIdentityDoctorFailsClosedWithCategoryOnly(t *testing.T) {
	t.Setenv("IDENTITY_ALIASES", doctorAlias+"="+doctorCanonical)
	path := writeSnapshot(t, league.PersistedState{
		Members: map[string]league.Member{
			doctorAlias:     {TeamID: "private-a", Email: doctorAlias},
			doctorCanonical: {TeamID: "private-b", Email: doctorCanonical},
		},
	})
	var stdout bytes.Buffer
	if code := run([]string{"-snapshot", path}, &stdout); code != exitConflict {
		t.Fatalf("exit = %d, output = %s", code, stdout.String())
	}
	var report league.IdentityPreflightReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Ready || report.ConflictCategory != "seat" {
		t.Fatalf("report = %+v", report)
	}
	for _, private := range []string{doctorAlias, doctorCanonical, "private-a", "private-b"} {
		if strings.Contains(stdout.String(), private) {
			t.Fatalf("doctor output leaked %q: %s", private, stdout.String())
		}
	}
}

func TestIdentityDoctorHidesMalformedConfigurationDetails(t *testing.T) {
	t.Setenv("IDENTITY_ALIASES", doctorAlias+"="+doctorCanonical+"="+doctorAlias)
	path := writeSnapshot(t, league.PersistedState{})
	var stdout bytes.Buffer
	if code := run([]string{"-snapshot", path}, &stdout); code != exitConflict {
		t.Fatalf("exit = %d, output = %s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), doctorAlias) || strings.Contains(stdout.String(), doctorCanonical) {
		t.Fatalf("configuration failure leaked identities: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"conflictCategory": "configuration"`) {
		t.Fatalf("output = %s", stdout.String())
	}
}

func TestIdentityDoctorHidesSQLiteSnapshotReadDetails(t *testing.T) {
	t.Setenv("IDENTITY_ALIASES", doctorAlias+"="+doctorCanonical)
	missing := filepath.Join(t.TempDir(), doctorAlias+".db")
	var stdout bytes.Buffer
	if code := run([]string{"-sqlite-snapshot", missing}, &stdout); code != exitConflict {
		t.Fatalf("exit = %d, output = %s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), doctorAlias) || strings.Contains(stdout.String(), missing) {
		t.Fatalf("snapshot failure leaked path or identity: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"conflictCategory": "snapshot_read"`) {
		t.Fatalf("output = %s", stdout.String())
	}
}

func writeSnapshot(t *testing.T, state league.PersistedState) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.json")
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

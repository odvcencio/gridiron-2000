package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearConfigOverrides(t *testing.T) {
	t.Helper()
	for _, key := range []string{"APP_NAME", "DRAFT_TZ", "LEAGUE_URL", "SCORING_FORMAT", "DRAFT_AT", "SEASON_START_AT", "NFL_SEASON"} {
		t.Setenv(key, "")
	}
}

func TestRunValidatesAndSummarizesEffectiveConfig(t *testing.T) {
	clearConfigOverrides(t)
	file := filepath.Join("..", "..", "config", "league.json.example")
	t.Setenv("LEAGUE_FILE", filepath.Join(t.TempDir(), "hostile-league.json"))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--file", file}, &stdout, &stderr); code != 0 {
		t.Fatalf("run code = %d; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{
		"OK THE LEAGUE (TL)",
		"8 teams · DYNASTY · 2026 · half_ppr",
		"15 draftable per team; 120 league slots",
		"runtime invitations / allowlist (or open setup)",
		"waivers: perf-priority · 09:00 local",
		"trades: veto commissioner · 24h review · deadline none configured",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("text output omitted %q:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
}

func TestRunJSONIsMachineReadableAndAppliesRuntimeOverrides(t *testing.T) {
	clearConfigOverrides(t)
	t.Setenv("APP_NAME", "Preflight League")
	t.Setenv("NFL_SEASON", "2027")
	file := filepath.Join("..", "..", "config", "league.json.example")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--file", file, "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run code = %d; stderr: %s", code, stderr.String())
	}
	var summary configSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	if summary.Status != "ok" || summary.League.Name != "Preflight League" || summary.League.Season != 2027 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Roster.Draftable != 15 || summary.Roster.LeagueCapacity != 120 {
		t.Fatalf("roster summary = %#v", summary.Roster)
	}
}

// TestRunReportsReceptionValueAndWarnsOnMismatch pins GC-1 fix 2:
// leaguecheck reports the effective reception value in both text and JSON
// output, and warns on stderr when scoring_format's implied reception
// value disagrees with the shipped default reception rule.
func TestRunReportsReceptionValueAndWarnsOnMismatch(t *testing.T) {
	clearConfigOverrides(t)
	t.Setenv("SCORING_FORMAT", "standard")
	file := filepath.Join("..", "..", "config", "league.json.example")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--file", file}, &stdout, &stderr); code != 0 {
		t.Fatalf("run code = %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reception 0") {
		t.Errorf("text output omitted the effective reception value:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "reception") {
		t.Errorf("stderr missing the scoring_format/reception mismatch warning: %q", stderr.String())
	}

	var stdoutJSON, stderrJSON bytes.Buffer
	if code := run([]string{"--file", file, "--format", "json"}, &stdoutJSON, &stderrJSON); code != 0 {
		t.Fatalf("run code = %d; stderr: %s", code, stderrJSON.String())
	}
	var summary configSummary
	if err := json.Unmarshal(stdoutJSON.Bytes(), &summary); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdoutJSON.String())
	}
	if summary.League.ReceptionPoints != 0 {
		t.Fatalf("ReceptionPoints = %v, want 0 for standard scoring", summary.League.ReceptionPoints)
	}
	if !strings.Contains(stderrJSON.String(), "reception") {
		t.Errorf("json-mode stderr missing the mismatch warning: %q", stderrJSON.String())
	}
}

func TestRunFailsClosedOnInvalidConfig(t *testing.T) {
	clearConfigOverrides(t)
	file := filepath.Join(t.TempDir(), "league.json")
	valid, err := os.ReadFile(filepath.Join("..", "..", "config", "league.json.example"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := bytes.Replace(valid, []byte(`"version": 1`), []byte(`"version": 1, "mystery": true`), 1)
	if err := os.WriteFile(file, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--file", file}, &stdout, &stderr); code != 1 {
		t.Fatalf("run code = %d, want 1", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), `invalid: league config: unknown field "mystery"`) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRequiresExplicitFileAndKnownFormat(t *testing.T) {
	for _, args := range [][]string{nil, {"--file", "league.json", "--format", "yaml"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%q) code = %d, want 2", args, code)
		}
	}
}

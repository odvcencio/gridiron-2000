package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gridiron-2000/internal/fleetconfig"
)

func TestHelpAndUsageExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"top-level help", []string{"--help"}, 0},
		{"command help", []string{"render", "--help"}, 0},
		{"missing command", nil, 2},
		{"unknown command", []string{"publish"}, 2},
		{"missing file", []string{"render", "--out", "bundle"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, &stdout, &stderr); got != tc.code {
				t.Fatalf("exit = %d, want %d (stdout=%q stderr=%q)", got, tc.code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String()+stderr.String(), "fleetgen") {
				t.Fatalf("usage output missing fleetgen: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRenderCheckAndDriftContract(t *testing.T) {
	dir := t.TempDir()
	leaguePath := filepath.Join(dir, "league.json")
	if err := os.WriteFile(leaguePath, []byte(testCLiLeague+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fleet := fleetconfig.Fleet{
		Version:           fleetconfig.SchemaVersion,
		Image:             "registry.example.test/gridiron/app@sha256:0000000000000000000000000000000000000000000000000000000000000000",
		StatrelayOrigin:   "http://statrelay.example.test",
		IngressClass:      "traefik",
		CertificateIssuer: "example-issuer",
		Instances: []fleetconfig.Instance{{
			ID: "alpha", Namespace: "alpha", ResourcePrefix: "alpha-app",
			PublicOrigin: "https://alpha.example.test", LeagueConfigPath: "league.json",
			PVCStorage: "1Gi",
		}},
	}
	fleetPath := filepath.Join(dir, "fleet.json")
	raw, err := json.MarshalIndent(fleet, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fleetPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "bundle")

	var stdout, stderr bytes.Buffer
	if got := run([]string{"render", "--file", fleetPath, "--out", out}, &stdout, &stderr); got != 0 {
		t.Fatalf("render exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"check", "--file", fleetPath, "--out", out}, &stdout, &stderr); got != 0 {
		t.Fatalf("clean check exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "clean") {
		t.Fatalf("clean output = %q", stdout.String())
	}

	if err := os.WriteFile(filepath.Join(out, "instances", "alpha", "deployment.yaml"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "unexpected.txt"), []byte("unexpected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"check", "--file", fleetPath, "--out", out}, &stdout, &stderr); got != 1 {
		t.Fatalf("drift check exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	driftOutput := stdout.String()
	if !strings.Contains(driftOutput, "instances/alpha/deployment.yaml") || !strings.Contains(driftOutput, "unexpected.txt") {
		t.Fatalf("drift output = %q", driftOutput)
	}
}

func TestPrivacySafeFleetExampleCompiles(t *testing.T) {
	path := filepath.Join("..", "..", "config", "fleet.json.example")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "TANK01_API_KEY") {
		t.Fatal("fleet example contains a Tank01 credential name")
	}
	email := regexp.MustCompile("[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}")
	if email.Match(raw) {
		t.Fatal("fleet example contains an email identity")
	}
	fleet, _, err := fleetconfig.Load(path)
	if err != nil {
		t.Fatalf("fleet example does not preflight: %v", err)
	}
	if _, err := fleetconfig.CompileFleet(fleet); err != nil {
		t.Fatalf("fleet example does not compile: %v", err)
	}
}

func TestAdoptProducesReadOnlyDeterministicPlan(t *testing.T) {
	dir := t.TempDir()
	leaguePath := filepath.Join(dir, "league.json")
	if err := os.WriteFile(leaguePath, []byte(testCLiLeague+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fleet := fleetconfig.Fleet{
		Version:           fleetconfig.SchemaVersion,
		Image:             "registry.example.test/gridiron/app@sha256:0000000000000000000000000000000000000000000000000000000000000000",
		StatrelayOrigin:   "http://statrelay.example.test",
		IngressClass:      "traefik",
		CertificateIssuer: "example-issuer",
		Instances: []fleetconfig.Instance{{
			ID: "alpha", Namespace: "alpha", ResourcePrefix: "alpha-app",
			PublicOrigin: "https://alpha.example.test", LeagueConfigPath: "league.json",
			PVCStorage: "1Gi", CommissionerHQ: &fleetconfig.CommissionerHQ{
				LeagueID: "alpha-league", Order: 0, Accent: "cyan", KeyID: "hq-alpha", Host: true,
			},
		}},
	}
	fleetRaw, err := json.MarshalIndent(fleet, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fleetPath := filepath.Join(dir, "fleet.json")
	if err := os.WriteFile(fleetPath, append(fleetRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	trueValue, falseValue := true, false
	inventory := fleetconfig.AdoptionInventory{
		Version: fleetconfig.AdoptionSchemaVersion, Mode: "existing",
		Instances: []fleetconfig.AdoptionInstance{{
			ID: "alpha", Namespace: "alpha", ResourcePrefix: "alpha-app",
			PublicOrigin: "https://alpha.example.test", Image: fleet.Image,
			Resources: fleetconfig.AdoptionResources{
				Deployment: "alpha-app", Service: "alpha-app", PVC: "alpha-app-data",
				LeagueConfig: "alpha-app-league-config", Secret: "alpha-app-secrets",
			},
			Legacy:   fleetconfig.AdoptionLegacyState{PeerMeshConfigured: &trueValue, HQV1Configured: &falseValue},
			Preserve: fleetconfig.AdoptionPreservationState{PVC: &trueValue, LeagueConfig: &trueValue, Secret: &trueValue},
		}},
	}
	inventoryRaw, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	inventoryPath := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(inventoryPath, append(inventoryRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if got := run([]string{"adopt", "--file", fleetPath, "--inventory", inventoryPath}, &stdout, &stderr); got != 0 {
		t.Fatalf("adopt exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ready") || !strings.Contains(stdout.String(), "read-only") {
		t.Fatalf("adoption output = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "bundle")); !os.IsNotExist(err) {
		t.Fatalf("adopt unexpectedly created bundle: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"preflight", "--file", fleetPath, "--inventory", inventoryPath, "--format", "json"}, &stdout, &stderr); got != 0 {
		t.Fatalf("preflight exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	var plan fleetconfig.AdoptionPlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("plan JSON: %v; output=%q", err, stdout.String())
	}
	if !plan.Ready || plan.SecretValuesRead || plan.PIIRead {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestAdoptReturnsOneForIdentityDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "league.json"), []byte(testCLiLeague+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fleet := fleetconfig.Fleet{
		Version: fleetconfig.SchemaVersion, Image: "registry.example.test/gridiron/app@sha256:0000000000000000000000000000000000000000000000000000000000000000",
		StatrelayOrigin: "http://statrelay.example.test", IngressClass: "traefik", CertificateIssuer: "example-issuer",
		Instances: []fleetconfig.Instance{{ID: "alpha", Namespace: "alpha", ResourcePrefix: "alpha-app", PublicOrigin: "https://alpha.example.test", LeagueConfigPath: "league.json", PVCStorage: "1Gi"}},
	}
	fleetRaw, err := json.MarshalIndent(fleet, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fleetPath := filepath.Join(dir, "fleet.json")
	if err := os.WriteFile(fleetPath, append(fleetRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	trueValue := true
	inventory := fleetconfig.AdoptionInventory{
		Version: fleetconfig.AdoptionSchemaVersion, Mode: "existing",
		Instances: []fleetconfig.AdoptionInstance{{
			ID: "alpha", Namespace: "alpha", ResourcePrefix: "alpha-app", PublicOrigin: "https://alpha.example.test", Image: "registry.example.test/gridiron/app@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Resources: fleetconfig.AdoptionResources{Deployment: "alpha-app", Service: "alpha-app", PVC: "alpha-app-data", LeagueConfig: "alpha-app-league-config", Secret: "alpha-app-secrets"},
			Legacy:    fleetconfig.AdoptionLegacyState{PeerMeshConfigured: &trueValue, HQV1Configured: &trueValue},
			Preserve:  fleetconfig.AdoptionPreservationState{PVC: &trueValue, LeagueConfig: &trueValue, Secret: &trueValue},
		}},
	}
	inventoryRaw, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	inventoryPath := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(inventoryPath, append(inventoryRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"adopt", "--file", fleetPath, "--inventory", inventoryPath}, &stdout, &stderr); got != 1 {
		t.Fatalf("adopt drift exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "blocked") || !strings.Contains(stdout.String(), "image differs") {
		t.Fatalf("adopt drift output = %q", stdout.String())
	}
}

const testCLiLeague = `{
  "version": 1,
  "league": {"name":"Example","short_code":"EX","tagline":"","mode_label":"DYNASTY","url":"http://localhost:8080","timezone":"America/New_York","season":2026},
  "teams": [
    {"id":"team-1","name":"East 1","abbreviation":"E1","division":"East","tone":"cyan"},
    {"id":"team-2","name":"East 2","abbreviation":"E2","division":"East","tone":"blue"},
    {"id":"team-3","name":"East 3","abbreviation":"E3","division":"East","tone":"violet"},
    {"id":"team-4","name":"East 4","abbreviation":"E4","division":"East","tone":"lime"},
    {"id":"team-5","name":"West 1","abbreviation":"W1","division":"West","tone":"orange"},
    {"id":"team-6","name":"West 2","abbreviation":"W2","division":"West","tone":"gold"},
    {"id":"team-7","name":"West 3","abbreviation":"W3","division":"West","tone":"magenta"},
    {"id":"team-8","name":"West 4","abbreviation":"W4","division":"West","tone":"pink"}
  ],
  "draft":{"at":"2099-01-01T00:00:00Z","rounds":15,"format_label":""},
  "season_start_at":"2099-01-08T00:00:00Z",
  "scoring_format":"half_ppr",
  "copy":{"hero_kicker":"","footer_line":"","venue_line":"","invite_blurb":""},
  "membership":{"allowed_domain":""},
  "roster":{"preset":"standard","reserve":{},"ir":0,"limits":{}},
  "waivers":{"mode":"perf-priority","season_weight_pct":60,"faab_budget":100,"clear_days":2,"process_time":"09:00"},
  "trades":{"deadline":"","veto":"commissioner","review_hours":24}
}`

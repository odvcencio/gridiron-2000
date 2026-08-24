package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

const currentGoSXVersion = "v0.53.7"
const currentGoSXSum = "h1:MlyUmhejomqMyNHJr27yQvChWzFs3ymZwC4rr7GauQo="

const prohibitedReversedIdentityAlias = "IDENTITY_ALIASES=commissioner@example.com=" +
	"commissioner.alias@example.org"

func readDocumentationFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func TestDocumentationPinsCurrentFrameworkAndScoringTruth(t *testing.T) {
	readme := readDocumentationFile(t, "README.md")
	for _, want := range []string{
		"GoSX " + currentGoSXVersion,
		"schedule-backed fantasy matchups",
		"pins every team's effective starters",
		"cannot rewrite that closed result",
		"docs/configuration.md",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README omitted current contract %q", want)
		}
	}
	for _, obsolete := range []string{
		"GoSX v0.50.0",
		"current matchup cards are clearly labeled local fixtures",
		"wiring draft rosters into a scoring engine is the next application layer",
		"`2026-08-22T16:00:00-04:00` | Draft start",
		prohibitedReversedIdentityAlias,
	} {
		if strings.Contains(readme, obsolete) {
			t.Errorf("README retained obsolete claim %q", obsolete)
		}
	}
	for _, want := range []string{
		"`2099-01-01T00:00:00Z`",
		"only the commissioner’s **Start draft** action begins pick one",
		"COMMISSIONER_EMAILS=commissioner@example.com",
		"IDENTITY_ALIASES=commissioner.alias@example.org=commissioner@example.com",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README omitted corrected contract %q", want)
		}
	}
}

func TestFrameworkReleasePinsStayExact(t *testing.T) {
	moduleVersion := "m31labs.dev/gosx " + currentGoSXVersion
	moduleSum := moduleVersion + " " + currentGoSXSum
	cliVersion := "m31labs.dev/gosx/cmd/gosx@" + currentGoSXVersion

	goMod := readDocumentationFile(t, "go.mod")
	if !strings.Contains(goMod, moduleVersion) {
		t.Fatalf("go.mod omitted exact framework pin %q", moduleVersion)
	}
	goSum := readDocumentationFile(t, "go.sum")
	if !strings.Contains(goSum, moduleSum) {
		t.Fatalf("go.sum omitted exact public module checksum %q", moduleSum)
	}
	dockerfile := readDocumentationFile(t, "Dockerfile")
	for _, want := range []string{
		cliVersion,
		"# " + currentGoSXVersion + " includes",
		"last good declarative-region DOM across HTTP failures",
		"GOSX_SKIP_VERSION_CHECK=1 /go/bin/gosx build --dev .",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("Dockerfile omitted release contract %q", want)
		}
	}
	for _, obsolete := range []string{
		"m31labs.dev/gosx v0.53.0",
		"m31labs.dev/gosx/cmd/gosx@v0.53.0",
		"GoSX v0.53.0",
	} {
		if strings.Contains(goMod+goSum+dockerfile+readDocumentationFile(t, "README.md"), obsolete) {
			t.Errorf("release pins retained obsolete contract %q", obsolete)
		}
	}
}

func TestExampleLeagueConfigIsStrictValidJSONAndDocumentsMembership(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("config", "league.json.example"))
	if err != nil {
		t.Fatal(err)
	}
	body := readDocumentationFile(t, path)
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("example config is not valid JSON: %v", err)
	}
	membership, ok := raw["membership"].(map[string]any)
	if !ok {
		t.Fatal("example config omitted membership object")
	}
	if _, ok := membership["allowed_domain"]; !ok {
		t.Fatal("example config omitted membership.allowed_domain")
	}

	for _, key := range []string{"APP_NAME", "DRAFT_TZ", "LEAGUE_URL", "SCORING_FORMAT", "DRAFT_AT", "SEASON_START_AT", "NFL_SEASON"} {
		t.Setenv(key, "")
	}
	t.Setenv("LEAGUE_FILE", path)
	cfg, err := league.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(example): %v", err)
	}
	if cfg.Roster.Total() != cfg.Rounds {
		t.Fatalf("example roster total %d does not match %d draft rounds", cfg.Roster.Total(), cfg.Rounds)
	}
}

func TestConfigurationReferenceCoversEveryPublicField(t *testing.T) {
	doc := readDocumentationFile(t, filepath.Join("docs", "configuration.md"))
	for _, field := range []string{
		"`version`", "`league`", "`name`", "`short_code`", "`tagline`", "`mode_label`", "`url`", "`timezone`", "`season`",
		"`teams`", "`id`", "`abbreviation`", "`division`", "`tone`",
		"`draft`", "`at`", "`rounds`", "`format_label`", "`season_start_at`", "`scoring_format`",
		"`copy`", "`hero_kicker`", "`footer_line`", "`venue_line`", "`invite_blurb`",
		"`membership`", "`membership.allowed_domain`",
		"`roster`", "`preset`", "`slots`", "`bench`", "`reserve`", "`ir`", "`limits`",
		"`waivers`", "`mode`", "`season_weight_pct`", "`faab_budget`", "`clear_days`", "`process_time`",
		"`trades`", "`deadline`", "`veto`", "`review_hours`",
	} {
		if !strings.Contains(doc, field) {
			t.Errorf("configuration reference omitted %s", field)
		}
	}
}

func TestHistoricalDocsDoNotClaimCurrentLaunchFacts(t *testing.T) {
	combined := readDocumentationFile(t, filepath.Join("docs", "design-spec.md")) + readDocumentationFile(t, filepath.Join("docs", "launch-checklist.md"))
	for _, obsolete := range []string{
		"Release status (",
		"Future release-pin step",
		"The inaugural draft is scheduled",
		"This release enables",
		prohibitedReversedIdentityAlias,
	} {
		if strings.Contains(combined, obsolete) {
			t.Errorf("historical/runbook docs retained time-bound claim %q", obsolete)
		}
	}
	for _, want := range []string{
		"COMMISSIONER_EMAILS=commissioner@example.com",
		"IDENTITY_ALIASES=commissioner.alias@example.org=commissioner@example.com",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("release runbook omitted canonical identity contract %q", want)
		}
	}
}

func TestTrackedIdentityExamplesKeepCanonicalDirection(t *testing.T) {
	const canonical = "IDENTITY_ALIASES=commissioner.alias@example.org=commissioner@example.com"
	paths := []string{
		".env.example",
		"README.md",
		filepath.Join("deploy", "README.md"),
		filepath.Join("deploy", "k8s", "secret.example.yaml"),
		filepath.Join("deploy", "k8s", "sk", "secret.example.yaml"),
		filepath.Join("docs", "launch-checklist.md"),
		filepath.Join("internal", "identity", "identity.go"),
	}
	for _, path := range paths {
		body := readDocumentationFile(t, path)
		if strings.Contains(body, prohibitedReversedIdentityAlias) {
			t.Errorf("%s retained reversed identity mapping %q", path, prohibitedReversedIdentityAlias)
		}
	}

	environment := readDocumentationFile(t, ".env.example")
	if !strings.Contains(environment, "# Example: "+canonical) {
		t.Fatalf(".env.example omitted canonical alias=identity example %q", canonical)
	}
}

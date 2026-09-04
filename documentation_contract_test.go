package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

const currentGoSXVersion = "v0.55.1"
const currentGoSXSum = "h1:kCu3aFEnKIqMsgg3tO2OUpvN8aweyH5btJgKl5nk5us="

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

// gosxVersionCitationPattern extracts a "gosx@<version>" token from a
// comment line. It accepts dots and dashes so it also captures full
// pseudo-versions such as v0.53.11-0.20260903011141-48af3189fe1f, not
// only semantic-version tags (currentGoSXVersion itself is now a plain
// tag, v0.55.1).
var gosxVersionCitationPattern = regexp.MustCompile(`gosx@(v[\w.\-]+)`)

// TestGoSXInstallCitationsInTestFilesMatchGoModPin is the drift gate for
// *_test.go comments that tell a developer what to install, or assert
// which GoSX CLI build is currently pinned: sim_browser_test.go and
// sim_room_browser_test.go both still named v0.53.10 after go.mod moved
// to currentGoSXVersion, and nothing caught it. A comment line that names
// a past milestone only (for example "Task 8 (target mode,
// gosx@v0.53.10)") records history and is exempt; only a line that also
// says "go install" or "is pinned" describes the CURRENT required build,
// so only those lines must match currentGoSXVersion.
func TestGoSXInstallCitationsInTestFilesMatchGoModPin(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "dist", "vendor", "node_modules", ".gosx", ".canopy", ".analyses", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for i, line := range strings.Split(string(body), "\n") {
			lower := strings.ToLower(line)
			citesCurrentBuild := strings.Contains(lower, "go install") || strings.Contains(lower, "is pinned")
			if !citesCurrentBuild {
				continue
			}
			match := gosxVersionCitationPattern.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			if match[1] != currentGoSXVersion {
				t.Errorf("%s:%d cites gosx@%s, go.mod pins %s", rel, i+1, match[1], currentGoSXVersion)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk repository for *_test.go files: %v", walkErr)
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

func TestReleaseChecklistRequiresAuthenticatedPromotionGates(t *testing.T) {
	doc := readDocumentationFile(t, filepath.Join("docs", "launch-checklist.md"))
	normalized := strings.Join(strings.Fields(doc), " ")
	for _, required := range []string{
		"### 11.1 SK canary acceptance before flagship",
		"### 11.2 Bilateral post-flagship acceptance",
		"allowed manager",
		"read-only Team, Board, and Draft",
		"exact candidate release metadata",
		"four visible build fields",
		"read-only Deployment metadata",
		"HQ card is not a digest source",
		"old flagship peer may be unavailable",
		"both HQ peer cards must be available",
		"Do not POST or submit any production mutation",
		"claim or release a seat",
		"the exact candidate release record",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("release checklist omitted authenticated gate contract %q", required)
		}
	}

	ordered := []string{
		"apply the new digest-pinned SK Deployment manifest",
		"complete the authenticated SK canary gate",
		"apply the new digest-pinned flagship Deployment manifest",
		"complete the bilateral post-flagship gate",
	}
	previous := -1
	for _, marker := range ordered {
		at := strings.Index(doc, marker)
		if at < 0 {
			t.Fatalf("release checklist omitted ordered promotion marker %q", marker)
		}
		if at <= previous {
			t.Fatalf("release checklist reordered promotion marker %q", marker)
		}
		previous = at
	}
}

func TestReleaseChecklistRequiresSchemaAwareRollbackAdjudication(t *testing.T) {
	launch := readDocumentationFile(t, filepath.Join("docs", "launch-checklist.md"))
	deploy := readDocumentationFile(t, filepath.Join("deploy", "README.md"))
	season := readDocumentationFile(t, filepath.Join("docs", "season-operations.md"))
	normalized := strings.Join(strings.Fields(launch), " ")
	for _, required := range []string{
		"stateSchema",
		"persistedVersion",
		"supportedVersion",
		"persistedDatabaseVersion",
		"supportedDatabaseVersion",
		"schema-aware adjudication",
		"both persisted versions",
		"separately tested, schema-compatible fallback digest",
		"Do not run `kubectl rollout undo` to an incompatible binary",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("release checklist omitted schema rollback contract %q", required)
		}
	}
	for _, body := range []struct {
		name string
		text string
	}{
		{name: "deploy README", text: deploy},
		{name: "season operations", text: season},
	} {
		if !strings.Contains(body.text, "stateSchema") {
			t.Errorf("%s omitted state schema release evidence", body.name)
		}
	}
	if !strings.Contains(deploy, "does not require off-node backups") {
		t.Error("deploy README omitted the no-off-node-backup boundary")
	}
}

// TestSeasonOperationsDocPinsPunterGamesFloor is finding 4's own
// regression test: docs/season-operations.md states the punter rank
// games floor as a literal number, for readers, rather than a computed
// value — so this test ties that literal to
// league.MinPunterGamesForRank directly. A future change to the constant
// that leaves the doc's number untouched fails here instead of silently
// misleading an operator.
func TestSeasonOperationsDocPinsPunterGamesFloor(t *testing.T) {
	doc := readDocumentationFile(t, filepath.Join("docs", "season-operations.md"))
	want := fmt.Sprintf("fewer than %d games shows no rank", league.MinPunterGamesForRank)
	if !strings.Contains(doc, want) {
		t.Errorf("season-operations.md omitted the current punter games floor %q", want)
	}
}

// envExampleKeys reads a dotenv-style file and returns the set of variable
// names it assigns, ignoring blank lines, comments, and any line without
// an "=". It does not validate values, only the key names on the left.
func envExampleKeys(t *testing.T, path string) map[string]bool {
	t.Helper()
	body := readDocumentationFile(t, path)
	keys := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		keys[strings.TrimSpace(key)] = true
	}
	return keys
}

// TestComposeEnvExampleStaysInSyncWithAppContract is the one-command-deploy
// lane's drift gate: deploy/compose/.env.example must not silently diverge
// from the app's real environment contract in the repository root's
// .env.example. Every variable name it forwards into the app container
// must exist in the canonical file. The named exceptions are Docker
// Compose orchestration inputs (the published image reference, the
// public domain, and the local-trial host port) that Compose itself
// consumes for variable substitution and never forwards into the app
// container's process environment, so they have no entry in the app's own
// contract.
func TestComposeEnvExampleStaysInSyncWithAppContract(t *testing.T) {
	canonical := envExampleKeys(t, ".env.example")
	compose := envExampleKeys(t, filepath.Join("deploy", "compose", ".env.example"))

	composeOnlyOrchestrationKeys := map[string]bool{
		"GRIDIRON_IMAGE": true,
		"DOMAIN":         true,
		"LOCALHOST_PORT": true,
	}

	if len(compose) == 0 {
		t.Fatal("deploy/compose/.env.example named no variables")
	}
	for key := range compose {
		if composeOnlyOrchestrationKeys[key] {
			continue
		}
		if !canonical[key] {
			t.Errorf("deploy/compose/.env.example names %q, which the canonical .env.example does not contain", key)
		}
	}
}

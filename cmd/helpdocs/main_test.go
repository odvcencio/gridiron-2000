package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	help "gridiron-2000/app/help"
)

func TestHelpAndUsageExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{name: "top-level help", args: []string{"--help"}, code: 0},
		{name: "command help", args: []string{"render", "--help"}, code: 0},
		{name: "missing command", code: 2},
		{name: "unknown command", args: []string{"publish"}, code: 2},
		{name: "missing output", args: []string{"render"}, code: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, &stdout, &stderr); got != tc.code {
				t.Fatalf("exit = %d, want %d (stdout=%q stderr=%q)", got, tc.code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String()+stderr.String(), "helpdocs") {
				t.Fatalf("usage output missing helpdocs: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRenderWritesOnlyOwnedFilesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if got := run([]string{"render", "--out", dir}, &stdout, &stderr); got != 0 {
		t.Fatalf("first render exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	first := readOwnedFiles(t, dir)
	if got, want := len(first), 2; got != want {
		t.Fatalf("rendered file count = %d, want %d", got, want)
	}
	for _, name := range []string{commissionerDocument, operatorDocument} {
		if len(first[name]) == 0 {
			t.Fatalf("rendered %s is empty", name)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"render", "--out", dir}, &stdout, &stderr); got != 0 {
		t.Fatalf("second render exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	second := readOwnedFiles(t, dir)
	for name, want := range first {
		if !bytes.Equal(second[name], want) {
			t.Fatalf("second render changed %s", name)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"check", "--out", dir}, &stdout, &stderr); got != 0 {
		t.Fatalf("clean check exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "clean (2 files)") {
		t.Fatalf("clean check output = %q", stdout.String())
	}
}

func TestCheckDetectsOneByteDriftWithoutRepairingIt(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if got := run([]string{"render", "--out", dir}, &stdout, &stderr); got != 0 {
		t.Fatalf("render exit = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	path := filepath.Join(dir, commissionerDocument)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) < 2 {
		t.Fatal("commissioner projection too short to drift")
	}
	mutated := append([]byte(nil), original...)
	mutated[1] ^= 1
	if err := os.WriteFile(path, mutated, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"check", "--out", dir}, &stdout, &stderr); got != 1 {
		t.Fatalf("drift check exit = %d, want 1 (stdout=%q stderr=%q)", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), commissionerDocument) || !strings.Contains(stdout.String(), "drift detected") {
		t.Fatalf("drift check output = %q", stdout.String())
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, mutated) {
		t.Fatal("check rewrote the drifted document")
	}
}

func TestGeneratedDocsDeriveCorpusReceiptAndOverlay(t *testing.T) {
	documents, err := renderDocuments()
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]string, len(documents))
	for _, doc := range documents {
		byName[doc.Name] = string(doc.Contents)
	}
	topic, ok := help.FindTopic("commissioner-operations")
	if !ok {
		t.Fatal("commissioner topic missing")
	}
	overlay := commissionerOverlay(t)
	commissioner := byName[commissionerDocument]
	operator := byName[operatorDocument]
	for name, content := range byName {
		for _, want := range []string{
			"Corpus version: `" + help.CorpusVersion + "`",
			"Verified source SHA: `" + help.VerifiedSourceSHA + "`",
			"Corpus source owner: `app/help` (`app/help/content.go`).",
			"Topic ID: `" + topic.ID + "`.",
			"Introduced version: `" + topic.IntroducedVersion + "`.",
			"Last verified topic SHA: `" + topic.LastVerifiedSHA + "`.",
			"Evidence status: source-only",
			topic.RuntimeSource,
			topic.SourceRefs[0],
			"Identity states: `" + strings.Join(topic.IdentityStates, "`, `") + "`",
			"Admission states: `" + strings.Join(topic.AdmissionStates, "`, `") + "`",
			"Team associations: `" + strings.Join(topic.TeamAssociations, "`, `") + "`",
			"Team roles: `" + strings.Join(topic.TeamRoles, "`, `") + "`",
			"Commissioner capability: `" + strings.Join(topic.CommissionerCapability, "`, `") + "`",
			"Modes: `" + strings.Join(topic.Modes, "`, `") + "`",
			"Phases: `" + strings.Join(topic.Phases, "`, `") + "`",
			"Required capabilities: `" + strings.Join(topic.RequiredCapabilities, "`, `") + "`",
			"Data states: `" + strings.Join(topic.DataStates, "`, `") + "`",
		} {
			if !strings.Contains(content, want) {
				t.Errorf("%s omitted %q", name, want)
			}
		}
	}
	for _, want := range []string{
		topic.ID,
		topic.Title,
		topic.Actor,
		topic.Prerequisites,
		topic.ActionRoute,
		"Owner: the `commissioner-operations` topic",
		"Audience: the commissioner with configured capability",
		"Safe operating loop",
		"season-operations.md",
	} {
		if !strings.Contains(commissioner, want) {
			t.Errorf("commissioner projection omitted %q", want)
		}
	}
	for _, item := range overlay {
		for _, want := range []string{item.ID, item.Title, item.Detail, item.Predicate, item.ActionRoute} {
			if !strings.Contains(commissioner, want) {
				t.Errorf("commissioner projection omitted overlay %s field %q", item.ID, want)
			}
		}
		if !strings.Contains(operator, "`"+item.ID+"`") {
			t.Errorf("operator projection omitted overlay ID %q", item.ID)
		}
	}
	for _, candidate := range help.TopicCorpus() {
		if !strings.Contains(operator, "`"+candidate.ID+"`") {
			t.Errorf("operator projection omitted topic ID %q", candidate.ID)
		}
	}
	for _, want := range []string{
		"TestHelpDocsInventoryMatchesExecutableCorpus",
		"TestVerifiedSourceSHARecordsReviewedOriginSnapshot",
		"TestChecklistProjectionUsesViewerRoleAndOrthogonalCommissioner",
		"TestTopicRouteRendersStateSemanticsValidationAndOwningTopic",
		"go test ./app/help/_topic_id",
		"px1_help_corpus.md",
		"app routes above remain explicit route strings",
	} {
		if !strings.Contains(operator, want) {
			t.Errorf("operator projection omitted %q", want)
		}
	}
}

func TestAxisValuesMakesEmptyApplicabilityExplicit(t *testing.T) {
	if got := axisValues(nil); got != "unrestricted/not-applicable in the corpus" {
		t.Fatalf("empty axis = %q, want explicit unrestricted/not-applicable wording", got)
	}
	if got := axisValues([]string{"alpha", "beta"}); got != "`alpha`, `beta`" {
		t.Fatalf("populated axis = %q, want stable backtick list", got)
	}
}

func TestGeneratedDocsHaveValidLocalLinksAndNoPrivateValues(t *testing.T) {
	documents, err := renderDocuments()
	if err != nil {
		t.Fatal(err)
	}
	linkPattern := regexp.MustCompile(`\]\(([^)]+)\)`)
	emailPattern := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	secretAssignmentPattern := regexp.MustCompile(`(?i)(password|secret|token|api[_ -]?key)\s*[:=]`)
	for _, doc := range documents {
		content := string(doc.Contents)
		if strings.Contains(content, "../help/") {
			t.Errorf("%s uses a repository-relative link for an app route", doc.Name)
		}
		if emailPattern.MatchString(content) || secretAssignmentPattern.MatchString(content) {
			t.Errorf("%s contains a PII/secret-looking value", doc.Name)
		}
		for _, match := range linkPattern.FindAllStringSubmatch(content, -1) {
			target := match[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "#") {
				continue
			}
			if _, err := os.Stat(filepath.Join("..", "..", "docs", filepath.Clean(target))); err != nil {
				t.Errorf("%s has missing local link target %q: %v", doc.Name, target, err)
			}
		}
	}
}

func TestTrackedProjectionsMatchGenerator(t *testing.T) {
	documents, err := renderDocuments()
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range documents {
		path := filepath.Join("..", "..", "docs", doc.Name)
		actual, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read tracked %s: %v", doc.Name, err)
		}
		if !bytes.Equal(actual, doc.Contents) {
			t.Errorf("tracked %s differs from helpdocs render; run helpdocs render --out docs", doc.Name)
		}
	}
}

func commissionerOverlay(t *testing.T) []help.ChecklistItem {
	t.Helper()
	all := help.ChecklistFor("seatless", "", "", true)
	var overlay []help.ChecklistItem
	seen := map[string]bool{}
	for _, item := range all {
		if item.Role != "commissioner-overlay" {
			continue
		}
		if seen[item.ID] {
			t.Fatalf("duplicate commissioner overlay ID %q", item.ID)
		}
		seen[item.ID] = true
		overlay = append(overlay, item)
	}
	sort.Slice(overlay, func(i, j int) bool { return overlay[i].ID < overlay[j].ID })
	if len(overlay) == 0 {
		t.Fatal("commissioner overlay is empty")
	}
	return overlay
}

func readOwnedFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("render created unexpected directory %q", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = data
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{commissionerDocument, operatorDocument}
	sort.Strings(want)
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("render created files %v, want only %v", names, want)
	}
	return files
}

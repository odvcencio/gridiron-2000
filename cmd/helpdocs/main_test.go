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
	if got, want := len(first), 3; got != want {
		t.Fatalf("rendered file count = %d, want %d", got, want)
	}
	for _, name := range []string{commissionerDocument, managerDocument, operatorDocument} {
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
	if !strings.Contains(stdout.String(), "clean (3 files)") {
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
	managerTopic, ok := help.FindTopic("getting-started")
	if !ok {
		t.Fatal("manager root topic missing")
	}
	commissioner := byName[commissionerDocument]
	operator := byName[operatorDocument]
	for name, content := range byName {
		expectedTopic := topic
		if name == managerDocument {
			expectedTopic = managerTopic
		}
		for _, want := range []string{
			"Corpus version: `" + help.CorpusVersion + "`",
			"Verified source SHA: `" + help.VerifiedSourceSHA + "`",
			"Corpus source owner: `app/help` (`app/help/content.go`).",
			"Topic ID: `" + expectedTopic.ID + "`.",
			"Introduced version: `" + expectedTopic.IntroducedVersion + "`.",
			"Last verified topic SHA: `" + expectedTopic.LastVerifiedSHA + "`.",
			"Evidence status: source-only",
			expectedTopic.RuntimeSource,
			expectedTopic.SourceRefs[0],
			"Identity states: `" + strings.Join(expectedTopic.IdentityStates, "`, `") + "`",
			"Admission states: `" + strings.Join(expectedTopic.AdmissionStates, "`, `") + "`",
			"Team associations: `" + strings.Join(expectedTopic.TeamAssociations, "`, `") + "`",
			"Team roles: `" + strings.Join(expectedTopic.TeamRoles, "`, `") + "`",
			"Commissioner capability: `" + strings.Join(expectedTopic.CommissionerCapability, "`, `") + "`",
			"Modes: `" + strings.Join(expectedTopic.Modes, "`, `") + "`",
			"Phases: `" + strings.Join(expectedTopic.Phases, "`, `") + "`",
			"Required capabilities: `" + strings.Join(expectedTopic.RequiredCapabilities, "`, `") + "`",
			"Data states: `" + strings.Join(expectedTopic.DataStates, "`, `") + "`",
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
		if strings.Contains(content, "../help") || strings.Contains(content, "../app") {
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
	want := []string{commissionerDocument, managerDocument, operatorDocument}
	sort.Strings(want)
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("render created files %v, want only %v", names, want)
	}
	return files
}
func TestManagerProjectionDerivesRolesPhasesAndReferences(t *testing.T) {
	documents, err := renderDocuments()
	if err != nil {
		t.Fatal(err)
	}
	var manager string
	for _, doc := range documents {
		if doc.Name == managerDocument {
			manager = string(doc.Contents)
			break
		}
	}
	if manager == "" {
		t.Fatal("manager projection is missing")
	}
	for _, want := range []string{
		"# Manager handbook",
		"## Source and route references",
		"## Five-minute manager path",
		"## Role checklists",
		"### Shared admitted-member checks",
		"### Primary manager",
		"### Co-manager",
		"### Seatless member",
		"### Commissioner overlay",
		"## Manager workflow index",
		"## State recovery reference",
		"## Manager glossary",
		"## Concept transition reference",
		"## Technical appendix: role, mode, and phase coverage",
		"## Technical appendix: corpus provenance and selection axes",
		"### Projection receipt",
		"not a universal entitlement",
		"When this applies:",
		"Anonymous and pending viewers",
		"primary-manager and manager",
		"comanager to the co-manager key",
	} {
		if !strings.Contains(manager, want) {
			t.Errorf("manager projection omitted %q", want)
		}
	}
	pathAt := strings.Index(manager, "## Five-minute manager path")
	receiptAt := strings.Index(manager, "### Projection receipt")
	matrixAt := strings.Index(manager, "## Technical appendix: role, mode, and phase coverage")
	provenanceAt := strings.Index(manager, "## Technical appendix: corpus provenance and selection axes")
	if pathAt < 0 || receiptAt < 0 || matrixAt < 0 || provenanceAt < 0 {
		t.Fatal("manager projection is missing the expected reading-order anchors")
	}
	if pathAt >= receiptAt {
		t.Fatal("five-minute manager path must precede the projection receipt")
	}
	if matrixAt >= provenanceAt || provenanceAt >= receiptAt {
		t.Fatal("projection receipt must remain inside the technical appendix")
	}
	if strings.Contains(manager, "../help") || strings.Contains(manager, "../app") {
		t.Fatal("manager projection contains repository-relative app links")
	}
	for _, topic := range managerWorkflowTopics(help.TopicCorpus()) {
		for _, want := range []string{topic.ID, topic.Title, topic.ActionRoute, topic.RuntimeSource, topic.Recovery} {
			if !strings.Contains(manager, want) {
				t.Errorf("manager projection omitted workflow %s field %q", topic.ID, want)
			}
		}
	}
	modes := corpusAxisValues(managerWorkflowTopics(help.TopicCorpus()), func(topic help.Topic) []string { return topic.Modes }, preferredManagerModes)
	phases := corpusAxisValues(managerWorkflowTopics(help.TopicCorpus()), func(topic help.Topic) []string { return topic.Phases }, preferredManagerPhases)
	for _, mode := range modes {
		if !strings.Contains(manager, codeSpan(mode)) {
			t.Errorf("manager projection omitted mode %q", mode)
		}
	}
	for _, phase := range phases {
		if !strings.Contains(manager, codeSpan(phase)) {
			t.Errorf("manager projection omitted phase %q", phase)
		}
	}
	for _, role := range managerRoleSpecs {
		items := help.ChecklistFor(role.Key, "", "", false)
		if len(items) == 0 {
			t.Fatalf("checklist for %s is empty", role.Key)
		}
		for _, item := range items {
			if !strings.Contains(manager, item.Title) || !strings.Contains(manager, item.Predicate) || !strings.Contains(manager, item.ActionRoute) {
				t.Errorf("manager projection omitted %s checklist item %q", role.Key, item.ID)
			}
		}
	}
	for _, entry := range help.Glossary() {
		if !strings.Contains(manager, entry.Term) || !strings.Contains(manager, entry.TopicID) {
			t.Errorf("manager projection omitted glossary entry %q", entry.Term)
		}
	}
	for _, mapping := range help.MigrationMappings() {
		if !strings.Contains(manager, mapping.Canonical) || !strings.Contains(manager, mapping.Difference) {
			t.Errorf("manager projection omitted concept mapping %q", mapping.Canonical)
		}
	}
	for _, link := range []string{
		"[px1_help_corpus.md](px1_help_corpus.md)",
		"[px1_glossary.md](px1_glossary.md)",
		"[px1_concept-transition.md](px1_concept-transition.md)",
		"[px1_commissioner-handbook.md](px1_commissioner-handbook.md)",
		"[season-operations.md](season-operations.md)",
	} {
		if !strings.Contains(manager, link) {
			t.Errorf("manager projection omitted local reference %q", link)
		}
	}
}

// Command helpdocs renders the read-only commissioner and operator projections
// from the executable help corpus. It never reads league state or performs a
// deployment/runtime action.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	help "gridiron-2000/app/help"
)

const (
	commissionerDocument = "px1_commissioner-handbook.md"
	managerDocument      = "px1_manager-handbook.md"
	operatorDocument     = "px1_operator-help-projection.md"
)

const usage = `Usage:
  helpdocs render --out <directory>
  helpdocs check  --out <directory>

Commands:
  render  write the deterministic commissioner, manager, and operator projections
  check   compare those three projections read-only and report named drift

Options:
  --out <dir>  explicit directory owning the three projection files
  -h, --help   show this help
`

const evidenceStatus = "source-only; this projection records corpus provenance, not production or runtime acceptance."

type document struct {
	Name     string
	Contents []byte
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		if len(args) != 1 {
			fmt.Fprintln(stderr, "helpdocs: usage error: help cannot be combined with other options")
			writeUsage(stderr)
			return 2
		}
		writeUsage(stdout)
		return 0
	}
	if args[0] != "render" && args[0] != "check" {
		fmt.Fprintf(stderr, "helpdocs: usage error: unknown command %q\n", args[0])
		writeUsage(stderr)
		return 2
	}
	out, err := parseOptions(args[1:])
	if err != nil {
		if errors.Is(err, errHelp) {
			writeUsage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "helpdocs: usage error: %v\n", err)
		writeUsage(stderr)
		return 2
	}
	if args[0] == "render" {
		count, err := renderTo(out)
		if err != nil {
			fmt.Fprintf(stderr, "helpdocs render: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "helpdocs render: wrote %d files to %s\n", count, out)
		return 0
	}
	return checkAt(out, stdout, stderr)
}

var errHelp = errors.New("help requested")

func parseOptions(args []string) (string, error) {
	var out string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-h", "--help":
			if len(args) != 1 {
				return "", fmt.Errorf("help cannot be combined with other options")
			}
			return "", errHelp
		case "--out":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return "", fmt.Errorf("--out requires a directory")
			}
			if out != "" {
				return "", fmt.Errorf("--out specified more than once")
			}
			out = args[index+1]
			index++
		default:
			return "", fmt.Errorf("unknown option %q", args[index])
		}
	}
	if out == "" {
		return "", fmt.Errorf("--out is required")
	}
	return out, nil
}

func writeUsage(writer io.Writer) {
	_, _ = io.WriteString(writer, usage)
}

func renderTo(out string) (int, error) {
	documents, err := renderDocuments()
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return 0, fmt.Errorf("create output directory: %w", err)
	}
	for _, doc := range documents {
		path := filepath.Join(out, doc.Name)
		if err := os.WriteFile(path, doc.Contents, 0o644); err != nil {
			return 0, fmt.Errorf("write %s: %w", doc.Name, err)
		}
	}
	return len(documents), nil
}

func checkAt(out string, stdout, stderr io.Writer) int {
	documents, err := renderDocuments()
	if err != nil {
		fmt.Fprintf(stderr, "helpdocs check: %v\n", err)
		return 1
	}
	var drift []string
	for _, doc := range documents {
		path := filepath.Join(out, doc.Name)
		actual, readErr := os.ReadFile(path)
		if readErr != nil {
			drift = append(drift, fmt.Sprintf("%s (missing or unreadable: %v)", doc.Name, readErr))
			continue
		}
		if !bytes.Equal(actual, doc.Contents) {
			drift = append(drift, fmt.Sprintf("%s (content differs)", doc.Name))
		}
	}
	if len(drift) != 0 {
		fmt.Fprintln(stdout, "helpdocs check: drift detected")
		for _, name := range drift {
			fmt.Fprintf(stdout, "- %s\n", name)
		}
		return 1
	}
	fmt.Fprintf(stdout, "helpdocs check: clean (%d files)\n", len(documents))
	return 0
}

func renderDocuments() ([]document, error) {
	topic, topics, overlay, err := projectionInputs()
	if err != nil {
		return nil, err
	}
	commissioner := renderCommissionerHandbook(topic, overlay)
	manager, err := renderManagerHandbook(topics, overlay)
	if err != nil {
		return nil, err
	}
	operator := renderOperatorProjection(topic, topics, overlay)
	return []document{
		{Name: commissionerDocument, Contents: []byte(commissioner)},
		{Name: managerDocument, Contents: []byte(manager)},
		{Name: operatorDocument, Contents: []byte(operator)},
	}, nil
}

func projectionInputs() (help.Topic, []help.Topic, []help.ChecklistItem, error) {
	topic, ok := help.FindTopic("commissioner-operations")
	if !ok {
		return help.Topic{}, nil, nil, fmt.Errorf("corpus is missing commissioner-operations")
	}
	topics := help.TopicCorpus()
	if len(topics) == 0 {
		return help.Topic{}, nil, nil, fmt.Errorf("corpus is empty")
	}
	seenTopics := make(map[string]bool, len(topics))
	for _, candidate := range topics {
		if strings.TrimSpace(candidate.ID) == "" || seenTopics[candidate.ID] {
			return help.Topic{}, nil, nil, fmt.Errorf("corpus has an empty or duplicate topic ID")
		}
		seenTopics[candidate.ID] = true
	}
	// A neutral phase/mode keeps every overlay entry in the projection. The
	// predicate remains visible so the runtime, not this document, decides
	// applicability for a specific league.
	all := help.ChecklistFor("seatless", "", "", true)
	var overlay []help.ChecklistItem
	seenOverlay := make(map[string]bool)
	for _, item := range all {
		if item.Role != "commissioner-overlay" {
			continue
		}
		if strings.TrimSpace(item.ID) == "" || seenOverlay[item.ID] {
			return help.Topic{}, nil, nil, fmt.Errorf("commissioner overlay has an empty or duplicate ID")
		}
		seenOverlay[item.ID] = true
		overlay = append(overlay, item)
	}
	if len(overlay) == 0 {
		return help.Topic{}, nil, nil, fmt.Errorf("commissioner overlay is empty")
	}
	sort.Slice(overlay, func(i, j int) bool { return overlay[i].ID < overlay[j].ID })
	return topic, topics, overlay, nil
}

func renderReceipt(b *strings.Builder, owner, audience string, topic help.Topic) {
	renderReceiptWithHeading(b, "## Projection receipt", owner, audience, topic)
}

func renderManagerReceipt(b *strings.Builder, owner, audience string, topic help.Topic) {
	renderReceiptWithHeading(b, "### Projection receipt", owner, audience, topic)
}

func renderReceiptWithHeading(b *strings.Builder, heading, owner, audience string, topic help.Topic) {
	b.WriteString(heading + "\n\n")
	fmt.Fprintf(b, "- Corpus version: `%s`.\n", help.CorpusVersion)
	fmt.Fprintf(b, "- Verified source SHA: `%s`. This is a reviewed source snapshot, not a deployment identity.\n", help.VerifiedSourceSHA)
	fmt.Fprintf(b, "- Corpus source owner: `app/help` (`app/help/content.go`).\n")
	fmt.Fprintf(b, "- Owner: %s\n", owner)
	fmt.Fprintf(b, "- Audience: %s\n", audience)
	fmt.Fprintf(b, "- Topic ID: `%s`.\n", topic.ID)
	fmt.Fprintf(b, "- Topic audiences: %s\n", axisValues(topic.Audiences))
	fmt.Fprintf(b, "- Introduced version: `%s`.\n", topic.IntroducedVersion)
	fmt.Fprintf(b, "- Last verified topic SHA: `%s`.\n", topic.LastVerifiedSHA)
	fmt.Fprintf(b, "- Runtime source: %s\n", topic.RuntimeSource)
	fmt.Fprintf(b, "- Source refs: `%s`\n", strings.Join(topic.SourceRefs, "`, `"))
	b.WriteString("- Selection axes (runtime filters these; this projection freezes no league value):\n")
	fmt.Fprintf(b, "  - Identity states: %s\n", axisValues(topic.IdentityStates))
	fmt.Fprintf(b, "  - Admission states: %s\n", axisValues(topic.AdmissionStates))
	fmt.Fprintf(b, "  - Team associations: %s\n", axisValues(topic.TeamAssociations))
	fmt.Fprintf(b, "  - Team roles: %s\n", axisValues(topic.TeamRoles))
	fmt.Fprintf(b, "  - Commissioner capability: %s\n", axisValues(topic.CommissionerCapability))
	fmt.Fprintf(b, "  - Modes: %s\n", axisValues(topic.Modes))
	fmt.Fprintf(b, "  - Phases: %s\n", axisValues(topic.Phases))
	fmt.Fprintf(b, "  - Required capabilities: %s\n", axisValues(topic.RequiredCapabilities))
	fmt.Fprintf(b, "  - Data states: %s\n", axisValues(topic.DataStates))
	fmt.Fprintf(b, "- Evidence status: %s\n\n", evidenceStatus)
}

func axisValues(values []string) string {
	if len(values) == 0 {
		return "unrestricted/not-applicable in the corpus"
	}
	return "`" + strings.Join(values, "`, `") + "`"
}

func renderCommissionerHandbook(topic help.Topic, overlay []help.ChecklistItem) string {
	var b strings.Builder
	b.WriteString("# Commissioner help projection\n\n")
	b.WriteString("<!-- Generated by cmd/helpdocs from app/help/content.go; do not edit by hand. -->\n\n")
	b.WriteString("Use the `commissioner-operations` topic as the short decision card, then follow the detailed [`season-operations.md`](season-operations.md) runbook. Commissioner HQ is a read-only cross-league projection; mutations belong to the owning league's `/admin` or `/draft` route.\n\n")
	renderReceipt(&b, fmt.Sprintf("the `%s` topic and owning `%s` route.", topic.ID, topic.ActionRoute), "the commissioner with configured capability; the corpus also serves admitted members, managers, and seatless viewers when the runtime makes the topic applicable.", topic)

	b.WriteString("## Canonical commissioner operations topic\n\n")
	fmt.Fprintf(&b, "**%s.** %s\n\n", topic.Title, topic.Summary)
	fmt.Fprintf(&b, "- Actor: %s\n", topic.Actor)
	fmt.Fprintf(&b, "- Prerequisites: %s\n", topic.Prerequisites)
	fmt.Fprintf(&b, "- Supported mode/phase: %s\n", topic.Supported)
	fmt.Fprintf(&b, "- States: %s\n", topic.States)
	fmt.Fprintf(&b, "- Deadline source: %s\n", topic.Deadline)
	fmt.Fprintf(&b, "- Owning action route: `%s`\n", topic.ActionRoute)
	fmt.Fprintf(&b, "- Topic audiences: `%s`\n", strings.Join(topic.Audiences, "`, `"))

	b.WriteString("\n### Commissioner overlay checklist\n\n")
	b.WriteString("These are all `commissioner-overlay` checklist entries returned by `ChecklistFor`; predicates stay visible so runtime mode, phase, and capability remain authoritative.\n\n")
	for _, item := range overlay {
		fmt.Fprintf(&b, "- **`%s` — %s.** %s\n", item.ID, item.Title, item.Detail)
		fmt.Fprintf(&b, "  - Predicate: `%s`.\n", item.Predicate)
		fmt.Fprintf(&b, "  - Owning action: `%s`.\n", item.ActionRoute)
	}
	b.WriteString("\n## Safe operating loop\n\n")
	b.WriteString("1. Read the owning league, mode, normalized phase, current source state, and last-success label.\n2. Confirm the actor and capability on the rendered control. Commissioner authority does not replace membership or team-seat identity.\n3. Read the exact current object and deadline immediately before submitting.\n4. Use the product's typed confirmation for consequential actions; submit once and reread the persisted result.\n5. Record a concise commissioner note for a correction, force, or exception.\n6. If the result is stale or conflicting, stop and recover from the owning route; never edit SQLite or infer state from a browser toast.\n\n")

	b.WriteString("## Recovery links\n\n")
	b.WriteString("App routes are shown as routes, not repository-relative Markdown links:\n\n")
	b.WriteString("- Draft readiness/order/clock: `/help/draft-order-readiness-and-clock`\n- Commissioner operations: `/help/commissioner-operations`\n- Activity and notes: `/help/activity-and-commissioner-notes`\n- Data/freshness: `/help/data-state-and-freshness`\n- Detailed restart and week-close procedure: [`season-operations.md`](season-operations.md)\n\n")
	b.WriteString("Start, pause, force, undo, close, waiver processing, and trade boundaries are runtime-owned. This document intentionally describes the safety contract and navigation, not a calendar or an unchangeable rule value.\n\n")

	b.WriteString("## Runtime-owned boundaries\n\n")
	fmt.Fprintf(&b, "- Consequence: %s\n", topic.Consequence)
	fmt.Fprintf(&b, "- Reversibility: %s\n", topic.Reversibility)
	fmt.Fprintf(&b, "- Result: %s\n", topic.Result)
	fmt.Fprintf(&b, "- Failure: %s\n", topic.Failure)
	fmt.Fprintf(&b, "- Recovery: %s\n", topic.Recovery)
	b.WriteString("\nThe running league page owns mutable dates, rules, permissions, locks, source freshness, and persisted results. This source-only projection never supplies a current league date, identity, score, health fact, or secret value.\n")
	return b.String()
}

func renderOperatorProjection(topic help.Topic, topics []help.Topic, overlay []help.ChecklistItem) string {
	var b strings.Builder
	b.WriteString("# Operator help projection\n\n")
	b.WriteString("<!-- Generated by cmd/helpdocs from app/help/content.go; do not edit by hand. -->\n\n")
	b.WriteString("The public help corpus is safe to publish because it carries product contracts, not league secrets. This operator view verifies the deterministic projection and keeps runtime-owned values out of static docs.\n\n")
	renderReceipt(&b, "the `helpdocs` projection over the versioned help corpus.", "operators and support reviewers validating the public help projection; the runtime remains authoritative for managers and commissioners.", topic)

	b.WriteString("## Corpus inventory\n\n")
	b.WriteString("The inventory below is derived from `TopicCorpus()` in stable topic-ID order. Each route is an app route; it is not a local file path.\n\n")
	b.WriteString("| Topic ID | Category | Owning route |\n| --- | --- | --- |\n")
	ordered := append([]help.Topic(nil), topics...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, candidate := range ordered {
		fmt.Fprintf(&b, "| `%s` | %s | `%s` |\n", candidate.ID, candidate.Category, candidate.ActionRoute)
	}

	b.WriteString("\n## Commissioner overlay coverage\n\n")
	fmt.Fprintf(&b, "The commissioner projection contains all %d `commissioner-overlay` entries from `ChecklistFor`; each entry preserves its predicate and owning action.\n\n", len(overlay))
	for _, item := range overlay {
		fmt.Fprintf(&b, "- `%s`: predicate `%s`; action `%s`.\n", item.ID, item.Predicate, item.ActionRoute)
	}

	b.WriteString("\n## Projection checks\n\n")
	b.WriteString("Run executable checks rather than copying search-query fixtures into an operator document:\n\n")
	b.WriteString("1. `go test ./app/help -run TestHelpDocsInventoryMatchesExecutableCorpus -count=1` checks the corpus inventory and docs inventory contract.\n2. `go test ./app/help -run TestVerifiedSourceSHARecordsReviewedOriginSnapshot -count=1` checks reviewed-source provenance.\n3. `go test ./app/help -run TestChecklistProjectionUsesViewerRoleAndOrthogonalCommissioner -count=1` checks role and commissioner-overlay composition.\n4. `go test ./app/help/_topic_id -run TestTopicRouteRendersStateSemanticsValidationAndOwningTopic -count=1` checks the underscore route package explicitly; `go test ./app/help/...` does not include it.\n5. `go test ./cmd/helpdocs -count=1` checks deterministic rendering, one-byte drift detection, local links, privacy, and complete overlay coverage.\n6. `go vet ./cmd/helpdocs` and `go build ./cmd/helpdocs` check the generator itself.\n7. `go run ./cmd/helpdocs check --out docs` performs the read-only byte comparison against the two tracked projections.\n\n")
	b.WriteString("## Authority boundary\n\n")
	b.WriteString("Help may explain a state and name an owning action; it must not decide the current date, deadline, score, phase, lock, capability, source freshness, permission, league identity, or deployment health. Those values are projected from the running service on request. When a topic and runtime page disagree, the runtime page wins and the operator records the discrepancy for the next corpus review.\n\n")
	b.WriteString("The detailed corpus contract is [`px1_help_corpus.md`](px1_help_corpus.md). The links in this document are local documentation links only; app routes above remain explicit route strings.\n\n")
	b.WriteString("## Evidence status\n\n")
	b.WriteString("This is a source-only projection. It records the exact corpus version, reviewed source SHA, topic source references, and executable checks; it does not claim that a production instance, current league, member identity, or runtime health state was inspected.\n")
	return b.String()
}

const (
	managerGlossaryDocument = "px1_glossary.md"
	managerConceptDocument  = "px1_concept-transition.md"
	managerCorpusDocument   = "px1_help_corpus.md"
	managerSeasonDocument   = "season-operations.md"
)
const markdownCode = "\x60"

type managerRoleSpec struct {
	Key   string
	Label string
}

var managerRoleSpecs = []managerRoleSpec{
	{Key: "primary", Label: "Primary manager"},
	{Key: "co-manager", Label: "Co-manager"},
	{Key: "seatless", Label: "Seatless member"},
}

var managerPathTopicIDs = []string{
	"identity-admission-and-membership",
	"teams-team-seats-and-rosters",
	"draft-order-readiness-and-clock",
	"big-board-and-autopick",
	"lineups-locks-matchups-and-scoring",
	"players-free-agents-waivers-and-faab",
	"trades-review-and-processing",
}

var preferredManagerModes = []string{"dynasty", "redraft", "configured"}
var preferredManagerPhases = []string{"pre-draft", "draft", "preseason", "regular-season", "post-season", "complete", "unknown"}

func renderManagerHandbook(topics []help.Topic, overlay []help.ChecklistItem) (string, error) {
	root, ok := topicFrom(topics, "getting-started")
	if !ok {
		return "", fmt.Errorf("corpus is missing getting-started for manager projection")
	}
	stateTopic, ok := topicFrom(topics, "data-state-and-freshness")
	if !ok {
		return "", fmt.Errorf("corpus is missing data-state-and-freshness for manager projection")
	}
	workflows := managerWorkflowTopics(topics)
	if len(workflows) == 0 {
		return "", fmt.Errorf("manager workflow projection is empty")
	}
	modes := corpusAxisValues(workflows, func(topic help.Topic) []string { return topic.Modes }, preferredManagerModes)
	phases := corpusAxisValues(workflows, func(topic help.Topic) []string { return topic.Phases }, preferredManagerPhases)
	if len(modes) == 0 || len(phases) == 0 {
		return "", fmt.Errorf("manager workflow projection has no mode or phase axis")
	}

	var b strings.Builder
	b.WriteString("# Manager handbook\n\n")
	b.WriteString("<!-- Generated by cmd/helpdocs from app/help/content.go; do not edit by hand. -->\n\n")
	b.WriteString("This manager-facing reference is generated from the executable help corpus. It keeps the first-session path, role predicates, workflow contracts, state recovery, glossary, and platform vocabulary in one reviewed source. Runtime pages own current dates, rules, permissions, source freshness, and persisted results.\n\n")
	if err := renderManagerPath(&b, topics); err != nil {
		return "", err
	}
	renderManagerReferences(&b)
	renderManagerChecklists(&b, overlay)
	renderManagerTopicDirectory(&b, workflows)
	for _, topic := range workflows {
		renderManagerWorkflow(&b, topic)
	}
	renderManagerStateRecovery(&b, stateTopic)
	if err := renderManagerGlossary(&b, topics); err != nil {
		return "", err
	}
	renderManagerConcepts(&b)
	renderManagerApplicabilityMatrix(&b, workflows, modes, phases)
	b.WriteString("## Technical appendix: corpus provenance and selection axes\n\n")
	renderManagerReceipt(&b, "the `getting-started` topic and the manager workflow topics.", "managers and support reviewers validating the manager projection; runtime pages remain authoritative for current league state.", root)
	b.WriteString("## Runtime precedence\n\n")
	b.WriteString("The live league page owns mutable values: dates, rules, phase, clocks, lineup/waiver/trade boundaries, feature capability, source freshness, and last-success time. This handbook supplies navigation and safe interpretation; it does not freeze those values. When a guide and a live state disagree, reread the owning route and treat its persisted result as authoritative.\n\n")
	b.WriteString("The generated references are source-only evidence. They do not claim that a production instance, current league, member identity, or runtime health state was inspected.\n")
	return b.String(), nil
}

func topicFrom(topics []help.Topic, id string) (help.Topic, bool) {
	for _, topic := range topics {
		if topic.ID == id {
			return topic, true
		}
	}
	return help.Topic{}, false
}

func managerWorkflowTopics(topics []help.Topic) []help.Topic {
	out := make([]help.Topic, 0, len(topics))
	for _, topic := range topics {
		if topic.Category == "commissioner" || topic.Category == "glossary" {
			continue
		}
		if !containsString(topic.Audiences, "primary manager") &&
			!containsString(topic.Audiences, "co-manager") &&
			!containsString(topic.Audiences, "admitted member") &&
			!containsString(topic.Audiences, "seatless member") {
			continue
		}
		out = append(out, topic)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := managerCategoryOrder(out[i].Category), managerCategoryOrder(out[j].Category)
		if ai != aj {
			return ai < aj
		}
		ti, tj := strings.ToLower(out[i].Title), strings.ToLower(out[j].Title)
		if ti != tj {
			return ti < tj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func managerCategoryOrder(id string) int {
	for _, category := range help.Categories() {
		if category.ID == id {
			return category.Order
		}
	}
	return 999
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func corpusAxisValues(topics []help.Topic, selectValues func(help.Topic) []string, preferred []string) []string {
	seen := map[string]bool{}
	for _, topic := range topics {
		for _, value := range selectValues(topic) {
			value = strings.TrimSpace(value)
			if value != "" {
				seen[value] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for _, value := range preferred {
		if seen[value] {
			out = append(out, value)
			delete(seen, value)
		}
	}
	extras := make([]string, 0, len(seen))
	for value := range seen {
		extras = append(extras, value)
	}
	sort.Strings(extras)
	return append(out, extras...)
}

func codeSpan(value string) string {
	return markdownCode + strings.TrimSpace(value) + markdownCode
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func renderManagerReferences(b *strings.Builder) {
	b.WriteString("## Source and route references\n\n")
	b.WriteString("The interactive help center is the short path: ")
	b.WriteString(codeSpan("/help"))
	b.WriteString(". Topic actions below are app routes, not repository-relative file paths.\n\n")
	fmt.Fprintf(b, "- Executable corpus contract: [px1_help_corpus.md](%s)\n", managerCorpusDocument)
	fmt.Fprintf(b, "- Generated glossary projection: [px1_glossary.md](%s)\n", managerGlossaryDocument)
	fmt.Fprintf(b, "- Concept-transition projection: [px1_concept-transition.md](%s)\n", managerConceptDocument)
	fmt.Fprintf(b, "- Commissioner boundary and operator path: [px1_commissioner-handbook.md](%s)\n", commissionerDocument)
	fmt.Fprintf(b, "- Detailed commissioner runbook: [season-operations.md](%s)\n\n", managerSeasonDocument)
}

func renderManagerPath(b *strings.Builder, topics []help.Topic) error {
	b.WriteString("## Five-minute manager path\n\n")
	b.WriteString("Start with the active league context, then follow the owning route for the task. Each step below uses the topic title, summary, and action route from the corpus.\n\n")
	for index, id := range managerPathTopicIDs {
		topic, ok := topicFrom(topics, id)
		if !ok {
			return fmt.Errorf("corpus is missing manager path topic %s", id)
		}
		fmt.Fprintf(b, "%d. **%s** (%s) — %s Read the current owning route %s before acting.\n", index+1, topic.Title, codeSpan(topic.ID), topic.Summary, codeSpan(topic.ActionRoute))
	}
	b.WriteString("\n")
	return nil
}

func renderManagerChecklists(b *strings.Builder, overlay []help.ChecklistItem) {
	b.WriteString("## Role checklists\n\n")
	b.WriteString("These sections are the shared and role-specific checklist entries from the corpus with no mode or phase filter. The When this applies text on every item is the access and capability gate; it is not a universal entitlement. Anonymous and pending viewers do not receive an admitted-member checklist until the live Help entry resolves their admission state.\n\n")

	common := help.ChecklistFor("primary", "", "", false)
	b.WriteString("### Shared admitted-member checks\n\n")
	b.WriteString("These common checks are rendered once and reused by each admitted role.\n\n")
	for _, item := range common {
		if item.Role != "admitted-member" {
			continue
		}
		renderManagerChecklistItem(b, item)
	}
	b.WriteString("\n")

	for _, role := range managerRoleSpecs {
		fmt.Fprintf(b, "### %s\n\n", role.Label)
		fmt.Fprintf(b, "The live Help entry resolves this section only after the %s condition is true. The source keeps the When this applies condition and owning route visible.\n\n", codeSpan(role.Key))
		items := help.ChecklistFor(role.Key, "", "", false)
		for _, item := range items {
			if item.Role == "admitted-member" {
				continue
			}
			renderManagerChecklistItem(b, item)
			b.WriteString("\n")
		}
	}

	b.WriteString("### Commissioner overlay\n\n")
	b.WriteString("Commissioner capability is orthogonal to the manager role. These entries are shown separately and never grant a team seat.\n\n")
	for _, item := range overlay {
		renderManagerChecklistItem(b, item)
		b.WriteString("\n")
	}
	b.WriteString("The corpus normalizes primary-manager and manager to the primary key, and comanager to the co-manager key. The runtime still resolves sign-in, admission, seat association, and capability before any checklist appears.\n\n")
}

func renderManagerChecklistItem(b *strings.Builder, item help.ChecklistItem) {
	fmt.Fprintf(b, "- **%s** — %s\n", item.Title, item.Detail)
	fmt.Fprintf(b, "  - When this applies: %s; owning route: %s.\n", codeSpan(item.Predicate), codeSpan(item.ActionRoute))
}

func renderManagerApplicabilityMatrix(b *strings.Builder, workflows []help.Topic, modes, phases []string) {
	b.WriteString("## Technical appendix: role, mode, and phase coverage\n\n")
	b.WriteString("This appendix is executable projection evidence for reviewers, not manager instructions. It records checklist IDs that remain applicable for each normalized role/mode/phase combination; the live route supplies the actual admission, seat, capability, and data state.\n\n")
	b.WriteString("| Role | Mode | Phase | Applicable checklist IDs |\n| --- | --- | --- | --- |\n")
	for _, role := range managerRoleSpecs {
		for _, mode := range modes {
			for _, phase := range phases {
				items := help.ChecklistFor(role.Key, mode, phase, false)
				ids := make([]string, 0, len(items))
				for _, item := range items {
					if item.Applicable {
						ids = append(ids, item.ID)
					}
				}
				sort.Strings(ids)
				if len(ids) == 0 {
					ids = []string{"none"}
				}
				fmt.Fprintf(b, "| %s | %s | %s | %s |\n", role.Label, codeSpan(mode), codeSpan(phase), markdownCell(strings.Join(ids, ", ")))
			}
		}
	}
	fmt.Fprintf(b, "\nThe matrix covers %d manager workflow topics from the corpus; each topic's own modes and phases remain listed below.\n\n", len(workflows))
}

func renderManagerTopicDirectory(b *strings.Builder, workflows []help.Topic) {
	b.WriteString("## Manager workflow index\n\n")
	b.WriteString("| Topic ID | Category | Owning route | Modes | Phases |\n| --- | --- | --- | --- | --- |\n")
	for _, topic := range workflows {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
			codeSpan(topic.ID), markdownCell(topic.Category), codeSpan(topic.ActionRoute),
			markdownCell(strings.Join(topic.Modes, ", ")), markdownCell(strings.Join(topic.Phases, ", ")))
	}
	b.WriteString("\nThe detailed entries that follow are rendered from the same topic records; they do not introduce a second manager guide.\n\n")
}

func renderManagerWorkflow(b *strings.Builder, topic help.Topic) {
	fmt.Fprintf(b, "## %s\n\n", topic.Title)
	fmt.Fprintf(b, "%s · category %s · owning route %s\n\n", codeSpan(topic.ID), codeSpan(topic.Category), codeSpan(topic.ActionRoute))
	fmt.Fprintf(b, "%s\n\n", topic.Summary)
	fmt.Fprintf(b, "- Audiences: %s\n", axisValues(topic.Audiences))
	fmt.Fprintf(b, "- Actor: %s\n", topic.Actor)
	fmt.Fprintf(b, "- Prerequisites: %s\n", topic.Prerequisites)
	fmt.Fprintf(b, "- Supported mode/phase: %s\n", topic.Supported)
	fmt.Fprintf(b, "- States: %s\n", topic.States)
	fmt.Fprintf(b, "- Deadline source: %s\n", topic.Deadline)
	fmt.Fprintf(b, "- Modes: %s\n", axisValues(topic.Modes))
	fmt.Fprintf(b, "- Phases: %s\n", axisValues(topic.Phases))
	fmt.Fprintf(b, "- Data states: %s\n", axisValues(topic.DataStates))
	fmt.Fprintf(b, "- Privacy: %s\n", topic.Privacy)
	fmt.Fprintf(b, "- Consequence: %s\n", topic.Consequence)
	fmt.Fprintf(b, "- Reversibility: %s\n", topic.Reversibility)
	fmt.Fprintf(b, "- Result: %s\n", topic.Result)
	fmt.Fprintf(b, "- Failure: %s\n", topic.Failure)
	fmt.Fprintf(b, "- Recovery: %s\n", topic.Recovery)
	fmt.Fprintf(b, "- Runtime source: %s\n", topic.RuntimeSource)
	fmt.Fprintf(b, "- Example: %s\n", topic.Example)
	fmt.Fprintf(b, "- Source refs: %s\n", axisValues(topic.SourceRefs))
	b.WriteString("\n### Steps\n\n")
	for index, step := range topic.Steps {
		fmt.Fprintf(b, "%d. %s\n", index+1, step)
	}
	b.WriteString("\n")
}

func renderManagerStateRecovery(b *strings.Builder, topic help.Topic) {
	b.WriteString("## State recovery reference\n\n")
	b.WriteString("Every state entry below comes from the corpus state guidance. Read the owning route's exact value and last-success label; this projection never invents a timestamp, score, zero, or live result.\n\n")
	for _, state := range help.StateNames() {
		guidance := help.Guidance(topic.ID, state)
		fmt.Fprintf(b, "### %s\n\n", codeSpan(guidance.State))
		fmt.Fprintf(b, "- Why: %s\n", guidance.Why)
		fmt.Fprintf(b, "- Impact: %s\n", guidance.Impact)
		fmt.Fprintf(b, "- Remaining: %s\n", guidance.Remaining)
		fmt.Fprintf(b, "- Preserve: %s\n", guidance.PreservedContext)
		fmt.Fprintf(b, "- Next action: %s\n", guidance.NextAction)
		fmt.Fprintf(b, "- Retry: %s\n", guidance.Retry)
		fmt.Fprintf(b, "- Last success: %s\n", guidance.LastSuccess)
		fmt.Fprintf(b, "- Owning topic ID: %s\n\n", codeSpan(guidance.TopicID))
	}
}

func renderManagerGlossary(b *strings.Builder, topics []help.Topic) error {
	entries := help.Glossary()
	sort.SliceStable(entries, func(i, j int) bool {
		ai, aj := strings.ToLower(entries[i].Term), strings.ToLower(entries[j].Term)
		if ai != aj {
			return ai < aj
		}
		return entries[i].Term < entries[j].Term
	})
	b.WriteString("## Manager glossary\n\n")
	b.WriteString("The table is derived from the executable glossary entries and keeps each related topic's current app route visible. The full generated projection is [px1_glossary.md](px1_glossary.md).\n\n")
	b.WriteString("| Term | Aliases | Definition | Topic route |\n| --- | --- | --- | --- |\n")
	for _, entry := range entries {
		topic, ok := topicFrom(topics, entry.TopicID)
		if !ok {
			return fmt.Errorf("glossary %q references missing topic %s", entry.Term, entry.TopicID)
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n",
			markdownCell(entry.Term), markdownCell(strings.Join(entry.Aliases, ", ")),
			markdownCell(entry.Definition), codeSpan(topic.ActionRoute))
	}
	b.WriteString("\n")
	return nil
}

func renderManagerConcepts(b *strings.Builder) {
	mappings := help.MigrationMappings()
	sort.SliceStable(mappings, func(i, j int) bool {
		ai, aj := strings.ToLower(mappings[i].Canonical), strings.ToLower(mappings[j].Canonical)
		if ai != aj {
			return ai < aj
		}
		return mappings[i].Canonical < mappings[j].Canonical
	})
	b.WriteString("## Concept transition reference\n\n")
	b.WriteString("Use canonical Gridiron terms first. The derived mapping below is a vocabulary aid, not an import promise; the full projection is [px1_concept-transition.md](px1_concept-transition.md).\n\n")
	b.WriteString("| Canonical concept | Incoming aliases | Equivalent | Difference | Next action |\n| --- | --- | --- | --- | --- |\n")
	for _, mapping := range mappings {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
			markdownCell(mapping.Canonical), markdownCell(strings.Join(mapping.IncomingAliases, ", ")),
			markdownCell(mapping.Equivalent), markdownCell(mapping.Difference), markdownCell(mapping.NextAction))
	}
	b.WriteString("\n")
}

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
	operatorDocument     = "px1_operator-help-projection.md"
)

const usage = `Usage:
  helpdocs render --out <directory>
  helpdocs check  --out <directory>

Commands:
  render  write the deterministic commissioner and operator projections
  check   compare those two projections read-only and report named drift

Options:
  --out <dir>  explicit directory owning the two projection files
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
		if err := renderTo(out); err != nil {
			fmt.Fprintf(stderr, "helpdocs render: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "helpdocs render: wrote %d files to %s\n", 2, out)
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

func renderTo(out string) error {
	documents, err := renderDocuments()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	for _, doc := range documents {
		path := filepath.Join(out, doc.Name)
		if err := os.WriteFile(path, doc.Contents, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", doc.Name, err)
		}
	}
	return nil
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
	operator := renderOperatorProjection(topic, topics, overlay)
	return []document{
		{Name: commissionerDocument, Contents: []byte(commissioner)},
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
	b.WriteString("## Projection receipt\n\n")
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

package help

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpDocsInventoryMatchesExecutableCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "px1_help_corpus.md"))
	if err != nil {
		t.Fatal(err)
	}
	var documented []string
	for _, line := range strings.Split(string(raw), "\n") {
		columns := strings.Split(line, "|")
		if len(columns) < 2 {
			continue
		}
		cell := strings.TrimSpace(columns[1])
		if !strings.HasPrefix(cell, "`") || !strings.HasSuffix(cell, "`") {
			continue
		}
		documented = append(documented, strings.Trim(cell, "`"))
	}
	want := RequiredTopicIDs()
	if len(documented) != len(want) {
		t.Fatalf("help docs list %d topic IDs, executable corpus has %d: docs=%v corpus=%v", len(documented), len(want), documented, want)
	}
	seen := make(map[string]bool, len(documented))
	for i, id := range want {
		if documented[i] != id {
			t.Errorf("documented topic[%d] = %q, want executable topic %q", i, documented[i], id)
		}
		if seen[documented[i]] {
			t.Errorf("help docs duplicate topic ID %q", documented[i])
		}
		seen[documented[i]] = true
	}
}

func TestVerifiedSourceSHARecordsReviewedOriginSnapshot(t *testing.T) {
	const reviewedOrigin = "e666554818c82410fb651ac88236441dc9ac275c"
	if VerifiedSourceSHA != reviewedOrigin {
		t.Fatalf("VerifiedSourceSHA = %q, want reviewed origin/main snapshot %q", VerifiedSourceSHA, reviewedOrigin)
	}
	if len(VerifiedSourceSHA) != 40 {
		t.Fatalf("VerifiedSourceSHA length = %d, want 40", len(VerifiedSourceSHA))
	}
	if strings.Contains(VerifiedSourceSHA, "6561962") {
		t.Fatal("VerifiedSourceSHA retained the stale source receipt")
	}
}

func TestFailedGuidanceNamesUnknownOutcomeAndReread(t *testing.T) {
	guidance := Guidance("big-board-and-autopick", "failed")
	for name, text := range map[string]string{
		"impact":      guidance.Impact,
		"next action": guidance.NextAction,
		"retry":       guidance.Retry,
	} {
		lower := strings.ToLower(text)
		if !strings.Contains(lower, "unknown") && name != "next action" {
			t.Errorf("failed guidance %s omitted unknown outcome: %q", name, text)
		}
		if !strings.Contains(lower, "reread") && name != "impact" {
			t.Errorf("failed guidance %s omitted reread instruction: %q", name, text)
		}
	}
	topic, ok := FindTopic("big-board-and-autopick")
	if !ok {
		t.Fatal("Big Board topic missing")
	}
	for name, text := range map[string]string{"failure": topic.Failure, "recovery": topic.Recovery} {
		lower := strings.ToLower(text)
		if !strings.Contains(lower, "unknown") || !strings.Contains(lower, "reread") {
			t.Errorf("Big Board %s omitted unknown-outcome reread rule: %q", name, text)
		}
	}
}

func TestChecklistProjectionUsesViewerRoleAndOrthogonalCommissioner(t *testing.T) {
	tests := []struct {
		name            string
		mode            string
		role            string
		roleLabel       string
		admitted        bool
		hasSeat         bool
		coManager       bool
		commissioner    bool
		hasChecklist    bool
		hasCommissioner bool
	}{
		{name: "dynasty primary", mode: "dynasty", role: "primary", roleLabel: "PRIMARY MANAGER", admitted: true, hasSeat: true, hasChecklist: true},
		{name: "redraft co-manager", mode: "redraft", role: "co-manager", roleLabel: "CO-MANAGER", admitted: true, hasSeat: true, coManager: true, hasChecklist: true},
		{name: "dynasty seatless commissioner", mode: "dynasty", role: "seatless", roleLabel: "SEATLESS MEMBER", admitted: true, commissioner: true, hasChecklist: true, hasCommissioner: true},
		{name: "redraft unadmitted commissioner", mode: "redraft", role: "admission-required", roleLabel: "ADMISSION REQUIRED", commissioner: true, hasCommissioner: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := map[string]any{"signed_in": true}
			entry := map[string]any{
				"signed_in":       tt.admitted || tt.role == "admission-required",
				"admitted":        tt.admitted,
				"has_seat":        tt.hasSeat,
				"is_co_manager":   tt.coManager,
				"is_commissioner": tt.commissioner,
			}
			projection := checklistProjectionForViewer(viewer, entry, tt.mode, "pre-draft")
			if projection.Role != tt.role || projection.RoleLabel != tt.roleLabel {
				t.Fatalf("projection role = %q/%q, want %q/%q", projection.Role, projection.RoleLabel, tt.role, tt.roleLabel)
			}
			if projection.HasChecklist != tt.hasChecklist || projection.HasCommissioner != tt.hasCommissioner {
				t.Fatalf("projection capabilities = checklist:%v commissioner:%v, want checklist:%v commissioner:%v", projection.HasChecklist, projection.HasCommissioner, tt.hasChecklist, tt.hasCommissioner)
			}
			if tt.hasChecklist && len(projection.Items) == 0 {
				t.Fatal("admitted role received no checklist items")
			}
			if tt.hasCommissioner && len(projection.CommissionerItems) == 0 {
				t.Fatal("commissioner capability received no overlay items")
			}
			for _, item := range projection.Items {
				if item.Role == "commissioner-overlay" {
					t.Fatal("base role card leaked commissioner overlay item")
				}
			}
		})
	}
}

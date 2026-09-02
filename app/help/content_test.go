package help

import (
	"strings"
	"testing"
)

func TestCorpusValidatesAndContainsStableTopicInventory(t *testing.T) {
	if err := ValidateCorpus(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"getting-started",
		"identity-admission-and-membership",
		"teams-team-seats-and-rosters",
		"roles-primary-co-manager-and-commissioner",
		"draft-order-readiness-and-clock",
		"big-board-and-autopick",
		"lineups-locks-matchups-and-scoring",
		"players-free-agents-waivers-and-faab",
		"trades-review-and-processing",
		"pickem",
		"preseason-blitz",
		"activity-and-commissioner-notes",
		"data-state-and-freshness",
		"commissioner-operations",
		"concept-transition",
		"glossary",
	}
	got := RequiredTopicIDs()
	if len(got) != len(want) {
		t.Fatalf("topic count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("topic[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSearchGoldensAreDeterministic(t *testing.T) {
	tests := map[string]string{
		"big board":                "big-board-and-autopick",
		"draft queue":              "big-board-and-autopick",
		"when do lineups lock":     "lineups-locks-matchups-and-scoring",
		"waiver budget":            "players-free-agents-waivers-and-faab",
		"FAAB":                     "players-free-agents-waivers-and-faab",
		"why can't I add a player": "players-free-agents-waivers-and-faab",
		"trade veto":               "trades-review-and-processing",
		"where did my pick go":     "pickem",
		"blitz":                    "preseason-blitz",
		"owner":                    "roles-primary-co-manager-and-commissioner",
		"live data":                "data-state-and-freshness",
	}
	for query, want := range tests {
		got, ok := SearchTop(query)
		if !ok || got.ID != want {
			t.Errorf("SearchTop(%q) = %q, %v; want %q", query, got.ID, ok, want)
		}
		first := Search(query)
		second := Search(query)
		if len(first) != len(second) {
			t.Fatalf("Search(%q) changed result count", query)
		}
		for i := range first {
			if first[i].Topic.ID != second[i].Topic.ID || first[i].Score != second[i].Score {
				t.Fatalf("Search(%q) changed order at %d: %#v vs %#v", query, i, first[i], second[i])
			}
		}
	}
}

func TestSearchNormalizesPunctuationAndWhitespace(t *testing.T) {
	for _, query := range []string{"  PICK’EM  ", "pick-em", "PICK'EM"} {
		got, ok := SearchTop(query)
		if !ok || got.ID != "pickem" {
			t.Errorf("SearchTop(%q) = %q, %v; want pickem", query, got.ID, ok)
		}
	}
}

func TestRoleChecklistComposesCommissionerOverlay(t *testing.T) {
	items := ChecklistFor("co-manager", "redraft", "pre-draft", true)
	var coManager, overlay bool
	for _, item := range items {
		if item.Role == "co-manager" {
			coManager = true
			if item.ID == "board-truth" && !strings.Contains(strings.ToLower(item.Detail), "per-account") {
				t.Errorf("co-manager checklist omitted per-account board truth: %q", item.Detail)
			}
		}
		if item.Role == "commissioner-overlay" {
			overlay = true
		}
	}
	if !coManager || !overlay {
		t.Fatalf("checklist did not compose co-manager=%v commissioner=%v", coManager, overlay)
	}
}

// TestRulesStateChecklistUsesTheCanonicalSevenTermFreshnessVocabulary
// guards wave-6 item 8's cross-worker decision (matchups owns the
// vocabulary): one seven-term list — LIVE, CACHED, STALE, DEGRADED,
// OFFLINE, UNAVAILABLE, AWAITING RELEASE — everywhere, including here.
// "provisional" is a scoring-finality term (provisional vs. final score),
// a different axis from data-source freshness; the rules-state checklist
// item previously listed it as if it were an eighth freshness state.
func TestRulesStateChecklistUsesTheCanonicalSevenTermFreshnessVocabulary(t *testing.T) {
	var detail string
	for _, item := range baseChecklist {
		if item.ID == "rules-state" {
			detail = item.Detail
		}
	}
	if detail == "" {
		t.Fatal("baseChecklist has no rules-state item")
	}
	for _, term := range []string{"LIVE", "CACHED", "STALE", "DEGRADED", "OFFLINE", "UNAVAILABLE", "AWAITING RELEASE"} {
		if !strings.Contains(detail, term) {
			t.Errorf("rules-state checklist detail omitted %q: %q", term, detail)
		}
	}
	if strings.Contains(strings.ToLower(detail), "provisional") {
		t.Errorf("rules-state checklist detail still conflates provisional (a scoring-finality term) with data freshness: %q", detail)
	}
}

func TestStateGuidanceCarriesEveryRecoveryField(t *testing.T) {
	for _, state := range StateNames() {
		got := Guidance("data-state-and-freshness", state)
		for name, value := range map[string]string{
			"why": got.Why, "impact": got.Impact, "remaining": got.Remaining,
			"context": got.PreservedContext, "next": got.NextAction, "retry": got.Retry, "topic": got.TopicID,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("state %q omitted %s", state, name)
			}
		}
	}
}

func TestMigrationAndGlossaryStayPIIFreeAndRuntimeOwned(t *testing.T) {
	for _, mapping := range MigrationMappings() {
		joined := strings.ToLower(mapping.Canonical + " " + strings.Join(mapping.IncomingAliases, " ") + " " + mapping.Difference + " " + mapping.NextAction)
		for _, prohibited := range []string{"@", "password" + "=", "secret" + "="} {
			if strings.Contains(joined, prohibited) {
				t.Errorf("migration mapping %q contains prohibited PII marker %q", mapping.Canonical, prohibited)
			}
		}
	}
	for _, topic := range TopicCorpus() {
		if strings.Contains(topic.Deadline, "2026-") || strings.Contains(topic.Deadline, "2025-") {
			t.Errorf("topic %q freezes a mutable date: %q", topic.ID, topic.Deadline)
		}
	}
}

func TestGlossaryIncludesStateAndRuntimeTerms(t *testing.T) {
	want := []string{"live", "loading", "empty", "no-results", "pending", "locked", "disabled", "stale", "degraded", "offline", "unavailable", "failed", "permission-denied", "not-applicable", "league mode", "normalized phase", "feature capability", "runtime source"}
	got := make(map[string]bool)
	for _, entry := range Glossary() {
		got[strings.ToLower(entry.Term)] = true
	}
	for _, term := range want {
		if !got[term] {
			t.Errorf("glossary omitted %q", term)
		}
	}
}

// TestGlossaryExpandsCommonJargon is P1-7/P3-20's own render test (UI
// pass 2026-08-30): ADP, snake, FLEX, SUPERFLEX, PF, and IR were entirely
// absent, and FAAB (already present as "FAAB units") was never spelled
// out. A newbie drafting on a phone should not have to guess any of them.
func TestGlossaryExpandsCommonJargon(t *testing.T) {
	entries := Glossary()
	byLowerTerm := make(map[string]GlossaryEntry, len(entries))
	for _, entry := range entries {
		byLowerTerm[strings.ToLower(entry.Term)] = entry
	}
	for _, term := range []string{"adp", "snake draft", "flex", "superflex", "pf", "ir"} {
		if _, ok := byLowerTerm[term]; !ok {
			t.Errorf("glossary omitted %q", term)
		}
	}
	faab, ok := byLowerTerm["faab units"]
	if !ok {
		t.Fatal("glossary omitted \"FAAB units\"")
	}
	if !strings.Contains(faab.Definition, "Free Agent Acquisition Budget") {
		t.Errorf("FAAB units definition never spells out Free Agent Acquisition Budget: %q", faab.Definition)
	}
}

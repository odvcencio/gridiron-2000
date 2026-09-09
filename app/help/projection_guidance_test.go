package help

import (
	"strings"
	"testing"
)

// TestMatchupGuidanceExplainsProjectionScopeAndLineupMovement keeps the
// owning topic useful for the projection status link: the corpus must explain
// the matching-week gate, starters-only scoring, unknown/partial coverage, and
// the native movement controls without copying a mutable league value.
func TestMatchupGuidanceExplainsProjectionScopeAndLineupMovement(t *testing.T) {
	topic, ok := FindTopic("lineups-locks-matchups-and-scoring")
	if !ok {
		t.Fatal("lineups/matchups topic missing")
	}
	text := strings.ToLower(strings.Join([]string{
		topic.Summary,
		topic.Prerequisites,
		topic.Supported,
		topic.States,
		strings.Join(topic.Steps, " "),
		topic.Consequence,
		topic.Reversibility,
		topic.Result,
		topic.Failure,
		topic.Recovery,
		topic.RuntimeSource,
		topic.Example,
	}, "\n"))
	for _, want := range []string{
		"starter-only projections",
		"projection source/week label",
		"selected week",
		"bench disclosure is comparison context",
		"never contributes to team score",
		"em dash",
		"unknown, not zero",
		"move / change",
		"start / replace",
		"transfer affordance",
		"matching-week projection pool",
		"source-week mismatch",
		"legal edit can still be saved",
		"missing forecast",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lineups/matchups guidance omitted %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"week 1", "week 2", "2026-", "4:25 pm"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("lineups/matchups guidance copied mutable/example fact %q: %s", forbidden, text)
		}
	}
}

func TestMatchupProjectionGuidanceIsSearchable(t *testing.T) {
	for _, query := range []string{"projection week", "bench projection", "move starter"} {
		topic, ok := SearchTop(query)
		if !ok || topic.ID != "lineups-locks-matchups-and-scoring" {
			t.Fatalf("SearchTop(%q) = %q, %v; want lineups-locks-matchups-and-scoring", query, topic.ID, ok)
		}
	}
}

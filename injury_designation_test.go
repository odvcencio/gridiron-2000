package main

import (
	"testing"

	"gridiron-2000/internal/openstats"
)

// TestCurrentInjuryDesignationUsesTheTeamsLatestReport pins the stale-Out
// fix (2026-09-29 IR review): a player who was Out in week 1 and has not
// been on his team's report since is not Out in week 3.
func TestCurrentInjuryDesignationUsesTheTeamsLatestReport(t *testing.T) {
	reports := []openstats.InjuryReport{
		{Week: 1, Team: "PIT", PlayerName: "Kaleb Johnson", Position: "RB", ReportStatus: "Out"},
		{Week: 3, Team: "PIT", PlayerName: "Other Player", Position: "WR", ReportStatus: "Questionable"},
		{Week: 3, Team: "PIT", PlayerName: "Hurt Receiver", Position: "WR", ReportStatus: "Doubtful"},
		{Week: 2, Team: "PIT", PlayerName: "Hurt Receiver", Position: "WR", ReportStatus: "Out"},
	}
	if status, ok := currentInjuryDesignation(reports, openstats.NormalizePlayerKey("Kaleb Johnson", "RB")); ok {
		t.Fatalf("a week-1 Out survived to week 3: %q", status)
	}
	status, ok := currentInjuryDesignation(reports, openstats.NormalizePlayerKey("Hurt Receiver", "WR"))
	if !ok || status != "Doubtful" {
		t.Fatalf("latest-week status = %q, %v; want Doubtful, true", status, ok)
	}
	if _, ok := currentInjuryDesignation(nil, openstats.NormalizePlayerKey("Anyone", "QB")); ok {
		t.Fatal("no reports must mean no designation")
	}
}

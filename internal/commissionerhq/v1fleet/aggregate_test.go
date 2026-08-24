package v1fleet

import (
	"reflect"
	"testing"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

func stringPointer(value string) *string { return &value }

func TestAggregateRowsUsesCanonicalCrossLeagueOrdering(t *testing.T) {
	first := fleetTestSummary(t, "first-id")
	second := fleetTestSummary(t, "second-id")
	first.Calendar.NextDeadline = &hqv1.Deadline{Code: "late", At: stringPointer("2026-08-26T00:00:00Z")}
	first.Calendar.Deadlines = []hqv1.Deadline{{Code: "none", At: nil}}
	second.Calendar.NextDeadline = &hqv1.Deadline{Code: "early", At: stringPointer("2026-08-25T00:00:00Z")}
	second.Calendar.Deadlines = nil
	first.AttentionItems = []hqv1.AttentionItem{{Code: "info", Severity: "info", DueAt: nil}}
	second.AttentionItems = []hqv1.AttentionItem{{Code: "block", Severity: "blocking", DueAt: stringPointer("2026-08-27T00:00:00Z")}}
	first.Warnings = []hqv1.Warning{{Code: "warn", Severity: "warning"}}
	second.Warnings = []hqv1.Warning{{Code: "critical", Severity: "critical"}}
	first.RecentActivity.Items = []hqv1.ActivityItem{{ID: "older", OccurredAt: "2026-08-24T20:00:00Z"}}
	second.RecentActivity.Items = []hqv1.ActivityItem{{ID: "newer", OccurredAt: "2026-08-24T21:00:00Z"}}
	portfolio := aggregateRows([]Row{
		{ConnectionKey: "first", Order: 10, Snapshot: &first},
		{ConnectionKey: "second", Order: 20, Snapshot: &second},
	})
	if got := []string{portfolio.Deadlines[0].Item.Code, portfolio.Deadlines[1].Item.Code, portfolio.Deadlines[2].Item.Code}; !reflect.DeepEqual(got, []string{"early", "late", "none"}) {
		t.Fatalf("deadlines = %v", got)
	}
	if portfolio.Attention[0].Item.Code != "block" || portfolio.Warnings[0].Item.Code != "critical" || portfolio.Activity[0].Item.ID != "newer" {
		t.Fatalf("aggregate order = attention:%+v warnings:%+v activity:%+v", portfolio.Attention, portfolio.Warnings, portfolio.Activity)
	}
}

func TestAggregateRowsUsesConnectionOrderThenStableIdentityTies(t *testing.T) {
	at := stringPointer("2026-08-25T00:00:00Z")
	first := fleetTestSummary(t, "first-id")
	second := fleetTestSummary(t, "second-id")
	first.Calendar.NextDeadline = &hqv1.Deadline{Code: "z", At: at}
	second.Calendar.NextDeadline = &hqv1.Deadline{Code: "a", At: at}
	portfolio := aggregateRows([]Row{
		{ConnectionKey: "later", Order: 20, Snapshot: &first},
		{ConnectionKey: "earlier", Order: 10, Snapshot: &second},
	})
	if portfolio.Deadlines[0].ConnectionKey != "earlier" {
		t.Fatalf("tie order = %+v", portfolio.Deadlines)
	}
}

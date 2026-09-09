package commissioner

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1fleet"
	"m31labs.dev/gosx/route"
)

func TestHQV1PortfolioViewRendersOperationsAndSafeOwningLinks(t *testing.T) {
	deadlineAt := "2026-08-29T17:00:00Z"
	lockAt := "2026-08-30T16:00:00Z"
	asOf := "2026-08-25T15:00:00Z"
	rowLastSuccess := time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)
	publicOrigin := "https://alpha.example"
	summary := hqv1.Summary{
		Competition: hqv1.Competition{Phase: "regular-season", Teams: hqv1.TeamCounts{Total: intPtr(8), Occupied: intPtr(7), Vacant: intPtr(1)}},
		Draft:       &hqv1.Draft{ReadyTeams: intPtr(6), BoardGapCount: intPtr(1)},
		Readiness:   &hqv1.Readiness{Severity: "warning", Items: []hqv1.ReadinessItem{{Code: "board_gap", Severity: "warning", Count: 1, Label: "team has a board gap"}}},
		Membership:  hqv1.Membership{ClaimedTeams: intPtr(7), OpenTeams: intPtr(1), PendingInvites: intPtr(2)},
		Lineup:      hqv1.Lineup{IssueCount: intPtr(2), NextLockAt: &lockAt},
		Waivers:     hqv1.Waivers{Mode: stringPtr("faab"), OpenClaims: intPtr(3), NextRunAt: &deadlineAt},
		Trades:      hqv1.Trades{PendingCount: intPtr(2), CommissionerDecisions: intPtr(1)},
		Pickem:      hqv1.Pickem{Week: intPtr(4), Unpicked: intPtr(5), NextDeadlineAt: &deadlineAt},
		Calendar:    hqv1.Calendar{NextDeadline: &hqv1.Deadline{Code: "lineup_lock", Category: "lineup", Title: "Lineup lock", At: &lockAt, RelativeText: "in 1 day", State: "open", Href: stringPtr("/team")}},
		DataHealth:  hqv1.DataHealth{Quality: "healthy", SourceState: stringPtr("live"), AsOf: &asOf},
		Release:     hqv1.Release{GitSHA: "0123456789abcdef0123456789abcdef01234567", BuiltAt: asOf},
		ProducedAt:  asOf,
		Links:       hqv1.Links{League: stringPtr("/"), Commissioner: stringPtr("/admin")},
	}
	portfolio := v1fleet.Portfolio{
		Rows: []v1fleet.Row{{
			ConnectionKey: "alpha", Order: 1, LeagueID: "alpha-league", DisplayName: "Alpha League", ShortCode: "ALP", PublicOrigin: publicOrigin,
			ConnectionResult: v1fleet.Connected, SnapshotFreshness: v1fleet.Live, ProviderDataQuality: v1fleet.Healthy,
			Snapshot: &summary, LastSuccessAt: &rowLastSuccess,
		}},
		Attention: []v1fleet.FleetAttention{{ConnectionKey: "alpha", Order: 1, Item: hqv1.AttentionItem{Severity: "warning", Title: "Lineups need attention", Summary: "Two lineups remain incomplete.", DueAt: &lockAt, Href: stringPtr("/team"), LeagueID: "alpha-league"}}},
		Deadlines: []v1fleet.FleetDeadline{{ConnectionKey: "alpha", Order: 1, Item: hqv1.Deadline{Category: "lineup", Title: "Lineup lock", At: &lockAt, RelativeText: "in 1 day", Href: stringPtr("/team")}}},
		Activity:  []v1fleet.FleetActivity{{ConnectionKey: "alpha", Order: 1, Item: hqv1.ActivityItem{Category: "draft", Summary: "Draft order published", OccurredAt: asOf, Href: stringPtr("/activity")}}},
	}
	props := hqV1PortfolioView(portfolio, rowLastSuccess)
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := route.RenderProgramComponent(program, "HQV1Portfolio", route.ProgramRenderEnv{Values: map[string]any{"props": props}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PRIVATE HQ V1",
		"regular-season",
		"Lineup lock",
		"lineup issues",
		"faab",
		"pending trades",
		"unpicked",
		"Last successful collection",
		"https://alpha.example/team",
		"Recent activity",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("HQ v1 render missing %q: %s", want, rendered)
		}
	}
	for _, forbidden := range []string{"service.internal", "bearer", "secret", "operator@example.com"} {
		if strings.Contains(strings.ToLower(rendered), strings.ToLower(forbidden)) {
			t.Errorf("HQ v1 render leaked %q: %s", forbidden, rendered)
		}
	}
}

func TestHQV1RowProjectsMissingOptionalFactsWithoutFabricatingZero(t *testing.T) {
	data, err := os.ReadFile("../../internal/commissionerhq/v1/testdata/minimal_capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := hqv1.Decode(data)
	if err != nil {
		t.Fatalf("minimal capabilities fixture must remain valid: %v", err)
	}
	view := hqV1Row(v1fleet.Row{Snapshot: &summary}, "Minimal League")
	const missing = "Not reported by this league"
	for _, test := range []struct {
		name string
		got  any
	}{
		{"seats", view.Seats},
		{"claimed seats", view.ClaimedSeats},
		{"open seats", view.OpenSeats},
		{"pending invites", view.PendingInvites},
		{"ready teams", view.ReadyTeams},
		{"board gaps", view.BoardGaps},
		{"lineup issues", view.LineupIssues},
		{"open claims", view.OpenClaims},
		{"pending trades", view.TradePending},
		{"trade decisions", view.TradeDecisions},
		{"pick'em week", view.PickemWeek},
		{"pick'em unpicked", view.PickemUnpicked},
		{"lineup lock", view.LineupLock},
		{"waiver mode", view.WaiverMode},
		{"waiver run", view.WaiverRun},
		{"pick'em deadline", view.PickemDeadline},
		{"data as of", view.DataAsOf},
		{"source state", view.SourceState},
		{"readiness", view.Readiness},
	} {
		if got := fmt.Sprint(test.got); got != missing {
			t.Errorf("%s = %q, want %q", test.name, got, missing)
		}
	}
}

func TestHQV1RowPreservesGenuineZeroAndLiveSourceState(t *testing.T) {
	zero := 0
	zeroAt := "2026-08-30T16:00:00Z"
	summary := hqv1.Summary{
		Competition: hqv1.Competition{Teams: hqv1.TeamCounts{Total: &zero}},
		Draft:       &hqv1.Draft{ReadyTeams: &zero, BoardGapCount: &zero},
		Readiness:   &hqv1.Readiness{},
		Membership:  hqv1.Membership{ClaimedTeams: &zero, OpenTeams: &zero, PendingInvites: &zero},
		Lineup:      hqv1.Lineup{IssueCount: &zero, NextLockAt: &zeroAt},
		Waivers:     hqv1.Waivers{OpenClaims: &zero},
		Trades:      hqv1.Trades{PendingCount: &zero, CommissionerDecisions: &zero},
		Pickem:      hqv1.Pickem{Week: &zero, Unpicked: &zero},
		DataHealth:  hqv1.DataHealth{SourceState: stringPtr("live")},
	}
	view := hqV1Row(v1fleet.Row{Snapshot: &summary}, "Zero League")
	for _, test := range []struct {
		name string
		got  any
	}{
		{"seats", view.Seats},
		{"claimed seats", view.ClaimedSeats},
		{"open seats", view.OpenSeats},
		{"pending invites", view.PendingInvites},
		{"ready teams", view.ReadyTeams},
		{"board gaps", view.BoardGaps},
		{"lineup issues", view.LineupIssues},
		{"open claims", view.OpenClaims},
		{"pending trades", view.TradePending},
		{"trade decisions", view.TradeDecisions},
		{"pick'em week", view.PickemWeek},
		{"pick'em unpicked", view.PickemUnpicked},
	} {
		if got := fmt.Sprint(test.got); got != "0" {
			t.Errorf("%s = %q, want genuine zero", test.name, got)
		}
	}
	if view.Readiness != "CLEAR" {
		t.Errorf("empty readiness = %q, want CLEAR", view.Readiness)
	}
	if view.SourceState != "live" {
		t.Errorf("source state = %q, want live", view.SourceState)
	}
}

func TestHQV1UnavailableRowHidesDetailFacts(t *testing.T) {
	props := hqV1PortfolioView(v1fleet.Portfolio{Rows: []v1fleet.Row{{
		ConnectionKey: "offline", LeagueID: "offline-league", ShortCode: "OFF", DisplayName: "Offline League",
		PublicOrigin: "https://offline.example", ConnectionResult: v1fleet.Unreachable,
		SnapshotFreshness: v1fleet.Unavailable, ProviderDataQuality: v1fleet.NotReported,
		DiagnosticCode: v1fleet.DiagnosticUnreachable,
	}}}, time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC))
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := route.RenderProgramComponent(program, "HQV1Portfolio", route.ProgramRenderEnv{Values: map[string]any{"props": props}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "This league snapshot is unavailable") {
		t.Fatalf("unavailable row missing availability message: %s", rendered)
	}
	for _, detail := range []string{"PHASE / DEADLINE", "SEATS / READINESS", "LINEUP / WAIVERS", "TRADES / PICK'EM", "RELEASE / HEALTH"} {
		if strings.Contains(rendered, detail) {
			t.Errorf("unavailable row rendered detail section %q: %s", detail, rendered)
		}
	}
}

func intPtr(value int) *int          { return &value }
func stringPtr(value string) *string { return &value }

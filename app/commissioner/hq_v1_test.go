package commissioner

import (
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

func intPtr(value int) *int          { return &value }
func stringPtr(value string) *string { return &value }

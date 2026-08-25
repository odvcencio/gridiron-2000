package commissioner

import (
	"fmt"
	"strings"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1fleet"
)

type hqV1PortfolioProps struct {
	GeneratedAt string
	Total       int
	Live        int
	Stale       int
	Warnings    int
	Rows        []hqV1RowView
	Deadlines   []hqV1DeadlineView
	Attention   []hqV1AttentionView
	Activity    []hqV1ActivityView
}

type hqV1RowView struct {
	Available bool
	Stale     bool
	Name      string
	ShortCode string
	LeagueID  string
	PublicURL string

	ConnectionResult string
	Freshness        string
	Quality          string
	SourceState      string
	Diagnostic       string
	LastAttempt      string
	LastSuccess      string

	Phase          string
	Deadline       string
	DeadlineAt     string
	DeadlineHref   string
	HasDeadline    bool
	Seats          int
	ClaimedSeats   int
	OpenSeats      int
	PendingInvites int
	ReadyTeams     int
	BoardGaps      int
	Readiness      string

	LineupIssues   int
	LineupLock     string
	WaiverMode     string
	OpenClaims     int
	WaiverRun      string
	TradePending   int
	TradeDecisions int
	PickemWeek     int
	PickemUnpicked int
	PickemDeadline string

	ReleaseSHA       string
	ReleaseBuiltAt   string
	DataAsOf         string
	ProviderProduced string
	LeagueURL        string
	CommissionerURL  string
}

type hqV1DeadlineView struct {
	League   string
	Title    string
	Category string
	When     string
	Relative string
	State    string
	Href     string
	HasHref  bool
}

type hqV1AttentionView struct {
	League   string
	Severity string
	Title    string
	Summary  string
	Due      string
	Href     string
	HasHref  bool
}

type hqV1ActivityView struct {
	League   string
	When     string
	Category string
	Summary  string
	Href     string
	HasHref  bool
}

func emptyHQV1Portfolio() hqV1PortfolioProps {
	return hqV1PortfolioProps{
		Rows:      []hqV1RowView{},
		Deadlines: []hqV1DeadlineView{},
		Attention: []hqV1AttentionView{},
		Activity:  []hqV1ActivityView{},
	}
}

func hqV1PortfolioView(portfolio v1fleet.Portfolio, generatedAt time.Time) hqV1PortfolioProps {
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	view := emptyHQV1Portfolio()
	view.GeneratedAt = generatedAt.UTC().Format(time.RFC3339)
	view.Total = len(portfolio.Rows)

	nameByKey := make(map[string]string, len(portfolio.Rows))
	originByKey := make(map[string]string, len(portfolio.Rows))
	for _, row := range portfolio.Rows {
		name := strings.TrimSpace(row.DisplayName)
		if name == "" {
			name = strings.TrimSpace(row.ShortCode)
		}
		if name == "" {
			name = row.ConnectionKey
		}
		nameByKey[row.ConnectionKey] = name
		originByKey[row.ConnectionKey] = strings.TrimRight(row.PublicOrigin, "/")
		view.Rows = append(view.Rows, hqV1Row(row, name))
		switch row.SnapshotFreshness {
		case v1fleet.Live:
			view.Live++
		case v1fleet.Stale:
			view.Stale++
		}
		if row.ProviderDataQuality == v1fleet.Degraded || row.SnapshotFreshness == v1fleet.Stale || row.Snapshot == nil {
			view.Warnings++
		}
	}
	for _, item := range portfolio.Deadlines {
		view.Deadlines = append(view.Deadlines, hqV1Deadline(item, nameByKey, originByKey))
	}
	for _, item := range portfolio.Attention {
		view.Attention = append(view.Attention, hqV1Attention(item, nameByKey, originByKey))
	}
	for _, item := range portfolio.Activity {
		view.Activity = append(view.Activity, hqV1Activity(item, nameByKey, originByKey))
	}
	return view
}

func hqV1Row(row v1fleet.Row, name string) hqV1RowView {
	view := hqV1RowView{
		Available:        row.Snapshot != nil,
		Stale:            row.SnapshotFreshness == v1fleet.Stale,
		Name:             name,
		ShortCode:        row.ShortCode,
		LeagueID:         row.LeagueID,
		PublicURL:        strings.TrimRight(row.PublicOrigin, "/"),
		ConnectionResult: string(row.ConnectionResult),
		Freshness:        string(row.SnapshotFreshness),
		Quality:          string(row.ProviderDataQuality),
		Diagnostic:       string(row.DiagnosticCode),
		LastAttempt:      hqV1Time(row.LastAttemptAt),
		LastSuccess:      hqV1Time(row.LastSuccessAt),
	}
	if row.Snapshot == nil {
		return view
	}
	summary := row.Snapshot
	view.Phase = summary.Competition.Phase
	view.Seats = intPointer(summary.Competition.Teams.Total)
	view.ClaimedSeats = intPointer(summary.Membership.ClaimedTeams)
	view.OpenSeats = intPointer(summary.Membership.OpenTeams)
	view.PendingInvites = intPointer(summary.Membership.PendingInvites)
	view.ReadyTeams = intPointer(summary.Draft.ReadyTeams)
	view.BoardGaps = intPointer(summary.Draft.BoardGapCount)
	view.Readiness = readinessText(summary.Readiness)
	view.LineupIssues = intPointer(summary.Lineup.IssueCount)
	view.LineupLock = stringPointer(summary.Lineup.NextLockAt)
	view.WaiverMode = stringPointer(summary.Waivers.Mode)
	view.OpenClaims = intPointer(summary.Waivers.OpenClaims)
	view.WaiverRun = stringPointer(summary.Waivers.NextRunAt)
	view.TradePending = intPointer(summary.Trades.PendingCount)
	view.TradeDecisions = intPointer(summary.Trades.CommissionerDecisions)
	view.PickemWeek = intPointer(summary.Pickem.Week)
	view.PickemUnpicked = intPointer(summary.Pickem.Unpicked)
	view.PickemDeadline = stringPointer(summary.Pickem.NextDeadlineAt)
	view.ReleaseSHA = summary.Release.GitSHA
	view.ReleaseBuiltAt = summary.Release.BuiltAt
	view.DataAsOf = stringPointer(summary.DataHealth.AsOf)
	view.ProviderProduced = summary.ProducedAt
	view.LeagueURL = qualifiedHQV1URL(view.PublicURL, summary.Links.League)
	view.CommissionerURL = qualifiedHQV1URL(view.PublicURL, summary.Links.Commissioner)
	if deadline := summary.Calendar.NextDeadline; deadline != nil {
		view.HasDeadline = true
		view.Deadline = deadline.Title
		view.DeadlineAt = stringPointer(deadline.At)
		view.DeadlineHref = qualifiedHQV1URL(view.PublicURL, deadline.Href)
	}
	return view
}

func hqV1Deadline(item v1fleet.FleetDeadline, names, origins map[string]string) hqV1DeadlineView {
	return hqV1DeadlineView{
		League:   names[item.ConnectionKey],
		Title:    item.Item.Title,
		Category: item.Item.Category,
		When:     stringPointer(item.Item.At),
		Relative: item.Item.RelativeText,
		State:    item.Item.State,
		Href:     qualifiedHQV1URL(origins[item.ConnectionKey], item.Item.Href),
		HasHref:  item.Item.Href != nil,
	}
}

func hqV1Attention(item v1fleet.FleetAttention, names, origins map[string]string) hqV1AttentionView {
	return hqV1AttentionView{
		League:   names[item.ConnectionKey],
		Severity: item.Item.Severity,
		Title:    item.Item.Title,
		Summary:  item.Item.Summary,
		Due:      stringPointer(item.Item.DueAt),
		Href:     qualifiedHQV1URL(origins[item.ConnectionKey], item.Item.Href),
		HasHref:  item.Item.Href != nil,
	}
}

func hqV1Activity(item v1fleet.FleetActivity, names, origins map[string]string) hqV1ActivityView {
	return hqV1ActivityView{
		League:   names[item.ConnectionKey],
		When:     item.Item.OccurredAt,
		Category: item.Item.Category,
		Summary:  item.Item.Summary,
		Href:     qualifiedHQV1URL(origins[item.ConnectionKey], item.Item.Href),
		HasHref:  item.Item.Href != nil,
	}
}

func readinessText(readiness *hqv1.Readiness) string {
	if readiness == nil || len(readiness.Items) == 0 {
		return "CLEAR"
	}
	parts := make([]string, 0, len(readiness.Items))
	for _, item := range readiness.Items {
		parts = append(parts, fmt.Sprintf("%d %s", item.Count, item.Label))
	}
	return strings.Join(parts, " · ")
}

func qualifiedHQV1URL(origin string, path *string) string {
	if path == nil || strings.TrimSpace(*path) == "" {
		return ""
	}
	return strings.TrimRight(origin, "/") + "/" + strings.TrimLeft(strings.TrimSpace(*path), "/")
}

func hqV1Time(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "UNKNOWN"
	}
	return value.UTC().Format(time.RFC3339)
}

func intPointer(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func stringPointer(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "UNKNOWN"
	}
	return *value
}

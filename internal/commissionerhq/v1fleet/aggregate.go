package v1fleet

import (
	"sort"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

func (service *Service) Rows() []Row {
	if service == nil {
		return nil
	}
	return service.rowsAt(service.clock().UTC())
}

func (service *Service) rowsAt(now time.Time) []Row {
	rows := make([]Row, 0, len(service.config.Connections))
	for _, connection := range service.config.Connections {
		rows = append(rows, service.rowAt(connection, now))
	}
	return rows
}

func (service *Service) rowAt(connection Connection, now time.Time) Row {
	service.mu.RLock()
	state := *service.states[connection.Key]
	var cached hqv1.Summary
	if state.cache != nil {
		cached = *state.cache
	}
	service.mu.RUnlock()
	row := Row{
		ConnectionKey: connection.Key, Order: connection.Order, LeagueID: connection.LeagueID,
		DisplayName: connection.DisplayName, ShortCode: connection.ShortCode, Accent: connection.Accent,
		PublicOrigin: connection.PublicOrigin, Capabilities: append([]string(nil), connection.Capabilities...),
		Links: cloneLinks(connection.Links), ConnectionResult: state.result, DiagnosticCode: state.diagnostic,
		SnapshotFreshness: Unavailable, ProviderDataQuality: NotReported,
		LastAttemptAt: copyTime(state.lastAttempt), LastSuccessAt: copyTime(state.lastSuccess),
	}
	showCache := false
	if state.cache != nil && state.result == Connected && !now.Before(state.cachedAt) {
		row.SnapshotFreshness = Live
		showCache = true
	} else if state.cache != nil && (state.result == Unreachable || state.result == Unauthorized || state.result == Incompatible) &&
		!now.Before(state.cachedAt) && now.Sub(state.cachedAt) <= service.staleFor {
		row.SnapshotFreshness = Stale
		showCache = true
	}
	if !showCache {
		return row
	}
	copyValue, err := cloneSummary(cached)
	if err != nil {
		row.SnapshotFreshness = Unavailable
		return row
	}
	row.Snapshot = &copyValue
	switch copyValue.DataHealth.Quality {
	case string(Healthy):
		row.ProviderDataQuality = Healthy
	case string(Degraded):
		row.ProviderDataQuality = Degraded
	default:
		row.ProviderDataQuality = NotReported
	}
	row.ProviderProducedAt = parseProviderTime(copyValue.ProducedAt)
	if copyValue.DataHealth.AsOf != nil {
		row.ProviderAsOf = parseProviderTime(*copyValue.DataHealth.AsOf)
	}
	return row
}

func parseProviderTime(raw string) *time.Time {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil
	}
	value = value.UTC()
	return &value
}

func aggregateRows(rows []Row) Portfolio {
	portfolio := Portfolio{Rows: append([]Row(nil), rows...)}
	for _, row := range rows {
		if row.Snapshot == nil {
			continue
		}
		deadlines := row.Snapshot.Calendar.Deadlines
		if row.Snapshot.Calendar.NextDeadline != nil {
			deadlines = append([]hqv1.Deadline{*row.Snapshot.Calendar.NextDeadline}, deadlines...)
		}
		for _, item := range deadlines {
			portfolio.Deadlines = append(portfolio.Deadlines, FleetDeadline{ConnectionKey: row.ConnectionKey, Order: row.Order, Item: item})
		}
		for _, item := range row.Snapshot.AttentionItems {
			portfolio.Attention = append(portfolio.Attention, FleetAttention{ConnectionKey: row.ConnectionKey, Order: row.Order, Item: item})
		}
		for _, item := range row.Snapshot.Warnings {
			portfolio.Warnings = append(portfolio.Warnings, FleetWarning{ConnectionKey: row.ConnectionKey, Order: row.Order, Item: item})
		}
		for _, item := range row.Snapshot.RecentActivity.Items {
			portfolio.Activity = append(portfolio.Activity, FleetActivity{ConnectionKey: row.ConnectionKey, Order: row.Order, Item: item})
		}
	}
	sort.SliceStable(portfolio.Deadlines, func(i, j int) bool {
		left, right := portfolio.Deadlines[i], portfolio.Deadlines[j]
		if comparison := compareOptionalTime(left.Item.At, right.Item.At, false); comparison != 0 {
			return comparison < 0
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.Item.Code < right.Item.Code
	})
	sort.SliceStable(portfolio.Attention, func(i, j int) bool {
		left, right := portfolio.Attention[i], portfolio.Attention[j]
		if severityRank(left.Item.Severity) != severityRank(right.Item.Severity) {
			return severityRank(left.Item.Severity) < severityRank(right.Item.Severity)
		}
		if comparison := compareOptionalTime(left.Item.DueAt, right.Item.DueAt, false); comparison != 0 {
			return comparison < 0
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.Item.Code < right.Item.Code
	})
	sort.SliceStable(portfolio.Warnings, func(i, j int) bool {
		left, right := portfolio.Warnings[i], portfolio.Warnings[j]
		if severityRank(left.Item.Severity) != severityRank(right.Item.Severity) {
			return severityRank(left.Item.Severity) < severityRank(right.Item.Severity)
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.Item.Code < right.Item.Code
	})
	sort.SliceStable(portfolio.Activity, func(i, j int) bool {
		left, right := portfolio.Activity[i], portfolio.Activity[j]
		if left.Item.OccurredAt != right.Item.OccurredAt {
			return left.Item.OccurredAt > right.Item.OccurredAt
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.Item.ID < right.Item.ID
	})
	return portfolio
}

func compareOptionalTime(left, right *string, descending bool) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	leftTime, leftErr := time.Parse(time.RFC3339Nano, *left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, *right)
	if leftErr != nil || rightErr != nil || leftTime.Equal(rightTime) {
		return 0
	}
	if descending {
		if leftTime.After(rightTime) {
			return -1
		}
		return 1
	}
	if leftTime.Before(rightTime) {
		return -1
	}
	return 1
}

func severityRank(value string) int {
	switch value {
	case "blocking", "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

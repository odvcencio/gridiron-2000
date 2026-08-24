package v1fleet

import (
	"encoding/json"
	"errors"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

type ConnectionResult string

const (
	Connected     ConnectionResult = "connected"
	Unreachable   ConnectionResult = "unreachable"
	Unauthorized  ConnectionResult = "unauthorized"
	Incompatible  ConnectionResult = "incompatible"
	Misconfigured ConnectionResult = "misconfigured"
	Disabled      ConnectionResult = "disabled"
)

type SnapshotFreshness string

const (
	Live        SnapshotFreshness = "live"
	Stale       SnapshotFreshness = "stale"
	Unavailable SnapshotFreshness = "unavailable"
)

type ProviderDataQuality string

const (
	Healthy     ProviderDataQuality = "healthy"
	Degraded    ProviderDataQuality = "degraded"
	NotReported ProviderDataQuality = "not_reported"
)

type DiagnosticCode string

const (
	DiagnosticNone          DiagnosticCode = "none"
	DiagnosticUnreachable   DiagnosticCode = "unreachable"
	DiagnosticUnauthorized  DiagnosticCode = "unauthorized"
	DiagnosticIncompatible  DiagnosticCode = "incompatible"
	DiagnosticMisconfigured DiagnosticCode = "misconfigured"
	DiagnosticDisabled      DiagnosticCode = "disabled"
)

type Row struct {
	ConnectionKey       string
	Order               int
	LeagueID            string
	DisplayName         string
	ShortCode           string
	Accent              string
	PublicOrigin        string
	Capabilities        []string
	Links               hqv1.Links
	ConnectionResult    ConnectionResult
	SnapshotFreshness   SnapshotFreshness
	ProviderDataQuality ProviderDataQuality
	Snapshot            *hqv1.Summary
	LastAttemptAt       *time.Time
	LastSuccessAt       *time.Time
	ProviderProducedAt  *time.Time
	ProviderAsOf        *time.Time
	DiagnosticCode      DiagnosticCode
}

type FleetDeadline struct {
	ConnectionKey string
	Order         int
	Item          hqv1.Deadline
}

type FleetAttention struct {
	ConnectionKey string
	Order         int
	Item          hqv1.AttentionItem
}

type FleetWarning struct {
	ConnectionKey string
	Order         int
	Item          hqv1.Warning
}

type FleetActivity struct {
	ConnectionKey string
	Order         int
	Item          hqv1.ActivityItem
}

type Portfolio struct {
	Rows      []Row
	Deadlines []FleetDeadline
	Attention []FleetAttention
	Warnings  []FleetWarning
	Activity  []FleetActivity
}

type CallerError struct{ code string }

func (err *CallerError) Error() string { return "Commissioner HQ connection key is invalid" }
func (err *CallerError) Code() string  { return err.code }

func cloneSummary(summary hqv1.Summary) (hqv1.Summary, error) {
	payload, err := json.Marshal(summary)
	if err != nil {
		return hqv1.Summary{}, errors.New("Commissioner HQ summary cannot be copied")
	}
	copyValue, err := hqv1.Decode(payload)
	if err != nil {
		return hqv1.Summary{}, errors.New("Commissioner HQ summary cannot be copied")
	}
	return copyValue, nil
}

func cloneLinks(links hqv1.Links) hqv1.Links {
	return hqv1.Links{
		League: copyString(links.League), Overview: copyString(links.Overview), Join: copyString(links.Join),
		Draft: copyString(links.Draft), Board: copyString(links.Board), Team: copyString(links.Team),
		Players: copyString(links.Players), Trades: copyString(links.Trades), Pickem: copyString(links.Pickem),
		Blitz: copyString(links.Blitz), Activity: copyString(links.Activity), Commissioner: copyString(links.Commissioner),
	}
}

func copyTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copyValue := value
	return &copyValue
}

package league

import (
	"database/sql"
	"net/url"
	"strings"

	"gridiron-2000/internal/identity"
)

// IdentityPreflightCounts is a PII-free shape summary of identity-owned
// state. It intentionally reports no keys, team IDs, player IDs, categories,
// addresses, or values.
type IdentityPreflightCounts struct {
	Members                 int `json:"members"`
	TeamSeats               int `json:"teamSeats"`
	PrimaryRoles            int `json:"primaryRoles"`
	CoManagerRoles          int `json:"coManagerRoles"`
	OtherRoles              int `json:"otherRoles"`
	CoManagerInvites        int `json:"coManagerInvites"`
	BoardOwners             int `json:"boardOwners"`
	BoardPlayers            int `json:"boardPlayers"`
	PickemOwners            int `json:"pickemOwners"`
	PickemPicks             int `json:"pickemPicks"`
	BlitzOwners             int `json:"blitzOwners"`
	BlitzEntries            int `json:"blitzEntries"`
	NotificationOwners      int `json:"notificationOwners"`
	NotificationPreferences int `json:"notificationPreferences"`
	Announcements           int `json:"announcements"`
	SentNotifications       int `json:"sentNotifications"`
}

// IdentityPreflightReport describes what the authoritative startup identity
// reconciliation would do. ConflictCategory is a bounded operator-facing
// category; the underlying error can contain identity-bearing state and is
// deliberately never returned here.
type IdentityPreflightReport struct {
	Ready            bool                    `json:"ready"`
	WouldChange      bool                    `json:"wouldChange"`
	AliasMappings    int                     `json:"aliasMappings"`
	ConflictCategory string                  `json:"conflictCategory,omitempty"`
	Before           IdentityPreflightCounts `json:"before"`
	After            IdentityPreflightCounts `json:"after"`
}

// PreflightIdentityAliases runs the same reconciliation used at startup on a
// deep clone. The caller's snapshot is never changed and no Store write path
// is reachable from this function.
func PreflightIdentityAliases(snapshot PersistedState, resolver identity.Resolver) IdentityPreflightReport {
	candidate := cloneState(snapshot)
	report := IdentityPreflightReport{
		AliasMappings: len(resolver.Pairs()),
		Before:        identityPreflightCounts(snapshot),
	}
	changed, err := reconcileIdentityState(&candidate, resolver)
	if err != nil {
		report.ConflictCategory = identityConflictCategory(err)
		return report
	}
	report.Ready = true
	report.WouldChange = changed
	report.After = identityPreflightCounts(candidate)
	return report
}

// PreflightIdentityAliasesFromSQLiteSnapshot opens an offline SQLite snapshot
// in immutable, query-only mode, loads its state without repair writes, then
// delegates to the cloned in-memory preflight. Read/decode failures are
// deliberately collapsed to a bounded category so this operator surface never
// emits database contents.
func PreflightIdentityAliasesFromSQLiteSnapshot(path string, resolver identity.Resolver) IdentityPreflightReport {
	query := url.Values{}
	query.Set("mode", "ro")
	query.Set("immutable", "1")
	query.Add("_pragma", "query_only(1)")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return IdentityPreflightReport{ConflictCategory: "snapshot_read"}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		return IdentityPreflightReport{ConflictCategory: "snapshot_read"}
	}
	snapshot, err := loadStateFromDBUnrepaired(db)
	if err != nil {
		return IdentityPreflightReport{ConflictCategory: "snapshot_read"}
	}
	return PreflightIdentityAliases(snapshot, resolver)
}

func identityPreflightCounts(state PersistedState) IdentityPreflightCounts {
	counts := IdentityPreflightCounts{
		Members:            len(state.Members),
		CoManagerInvites:   len(state.CoInvites),
		BoardOwners:        len(state.Boards),
		PickemOwners:       len(state.Pickems),
		BlitzOwners:        len(state.BlitzEntries),
		NotificationOwners: len(state.NotifyPrefs),
		Announcements:      len(state.Announcements),
		SentNotifications:  len(state.SentLog),
	}
	for _, member := range state.Members {
		if strings.TrimSpace(member.TeamID) != "" {
			counts.TeamSeats++
		}
		switch strings.TrimSpace(member.Role) {
		case "":
			if strings.TrimSpace(member.TeamID) != "" {
				counts.PrimaryRoles++
			}
		case "co":
			counts.CoManagerRoles++
		default:
			counts.OtherRoles++
		}
	}
	for _, board := range state.Boards {
		counts.BoardPlayers += len(board)
	}
	for _, picks := range state.Pickems {
		counts.PickemPicks += len(picks)
	}
	for _, entries := range state.BlitzEntries {
		counts.BlitzEntries += len(entries)
	}
	for _, preferences := range state.NotifyPrefs {
		counts.NotificationPreferences += len(preferences)
	}
	return counts
}

func identityConflictCategory(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "co-manager"), strings.Contains(message, "already owns team"):
		return "co_manager"
	case strings.Contains(message, "team seats"):
		return "seat"
	case strings.Contains(message, "member roles"):
		return "role"
	case strings.Contains(message, "board"):
		return "board"
	case strings.Contains(message, "pick'em"):
		return "pickem"
	case strings.Contains(message, "Blitz"):
		return "blitz"
	case strings.Contains(message, "notification preference"):
		return "notification"
	default:
		return "identity_state"
	}
}

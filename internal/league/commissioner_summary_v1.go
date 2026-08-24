package league

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

// CommissionerSummaryV1ConfigSnapshot is the immutable league identity and
// topology read used by one v1 provider response. Teams is the active runtime
// topology, after any durable seat trim; Config retains the resolved rules.
type CommissionerSummaryV1ConfigSnapshot struct {
	InstanceID string
	LeagueID   string
	Config     Config
	Teams      []Team
	// BlitzEnabled is immutable feature configuration for the response. It
	// deliberately lives outside the fallible data-health capture so a source
	// outage cannot fabricate a disabled feature or remove its native link.
	BlitzEnabled bool
}

// CommissionerSummaryV1DataSnapshot is one already-copied source generation.
// The projection never invokes a live provider. Schedule data is included so
// Pick'em gaps can be derived against the same Store snapshot. Lineup issue
// facts are supplied by the source capture because they also depend on the
// player-pool generation; nil means that fact is not reportable.
type CommissionerSummaryV1DataSnapshot struct {
	Quality          string
	SourceMode       string
	SourceState      string
	PlayerCount      *int
	LastDegradation  string
	LastSuccessAt    time.Time
	AsOf             time.Time
	Games            []GameInfo
	LineupIssueCount *int
	NextLineupLockAt time.Time
	// DegradationCode is a closed, non-sensitive reason code. The projection
	// never emits arbitrary provider diagnostics, credentials, or local paths.
	DegradationCode string
}

// CommissionerSummaryV1ReleaseSnapshot is captured once per response rather
// than reading mutable build globals while the projection is in progress.
type CommissionerSummaryV1ReleaseSnapshot struct {
	GitSHA      string
	BuiltAt     time.Time
	ImageDigest string
}

// CommissionerSummaryV1Captures makes the coherent tuple observable in tests:
// every callback is invoked exactly once, in declaration order, for one
// response. A data-source failure is represented honestly as not_reported;
// failures of config, Store, release, or clock prevent a coherent response.
type CommissionerSummaryV1Captures struct {
	Config  func(context.Context) (CommissionerSummaryV1ConfigSnapshot, error)
	Store   func(context.Context) (PersistedState, error)
	Data    func(context.Context) (CommissionerSummaryV1DataSnapshot, error)
	Release func(context.Context) (CommissionerSummaryV1ReleaseSnapshot, error)
	Clock   func() time.Time
}

type commissionerSummaryV1Tuple struct {
	config  CommissionerSummaryV1ConfigSnapshot
	state   PersistedState
	data    CommissionerSummaryV1DataSnapshot
	release CommissionerSummaryV1ReleaseSnapshot
	now     time.Time
}

// NewCommissionerSummaryV1Source composes the pure projection with the signed
// transport's source contract. It performs no writes and starts no listener.
func NewCommissionerSummaryV1Source(c CommissionerSummaryV1Captures) (v1transport.SnapshotSource, error) {
	if c.Config == nil || c.Store == nil || c.Data == nil || c.Release == nil || c.Clock == nil {
		return nil, errors.New("commissioner summary v1 requires config, Store, data, release, and clock captures")
	}
	return func(ctx context.Context) (hqv1.Summary, error) {
		cfg, err := c.Config(ctx)
		if err != nil {
			return hqv1.Summary{}, v1transport.ErrTemporarilyUnavailable
		}
		state, err := c.Store(ctx)
		if err != nil {
			return hqv1.Summary{}, v1transport.ErrTemporarilyUnavailable
		}
		data, dataErr := c.Data(ctx)
		release, err := c.Release(ctx)
		if err != nil {
			return hqv1.Summary{}, v1transport.ErrTemporarilyUnavailable
		}
		now := c.Clock().UTC()
		if now.IsZero() {
			return hqv1.Summary{}, v1transport.ErrTemporarilyUnavailable
		}
		if dataErr != nil {
			data = CommissionerSummaryV1DataSnapshot{Quality: "not_reported"}
		}
		summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{
			config: cfg, state: state, data: data, release: release, now: now,
		})
		if err != nil {
			return hqv1.Summary{}, fmt.Errorf("project commissioner summary v1: %w", err)
		}
		return summary, nil
	}, nil
}

// CommissionerSummaryV1Config captures this Service's immutable config and a
// defensive copy of its active topology. It does not touch the Store.
func (s *Service) CommissionerSummaryV1Config(instanceID, leagueID string, blitzEnabled bool) (CommissionerSummaryV1ConfigSnapshot, error) {
	if s == nil {
		return CommissionerSummaryV1ConfigSnapshot{}, errors.New("league service is unavailable")
	}
	return CommissionerSummaryV1ConfigSnapshot{
		InstanceID:   strings.TrimSpace(instanceID),
		LeagueID:     strings.TrimSpace(leagueID),
		Config:       cloneCommissionerV1Config(s.cfg),
		Teams:        append([]Team(nil), s.Teams()...),
		BlitzEnabled: blitzEnabled,
	}, nil
}

// CommissionerSummaryV1State obtains state and persistence-read health under
// one Store read lock. It never calls PersistenceError and Snapshot separately.
func (s *Service) CommissionerSummaryV1State() (PersistedState, error) {
	if s == nil || s.store == nil {
		return PersistedState{}, errors.New("league persistence is unavailable")
	}
	return s.store.ReadableSnapshot()
}

func projectCommissionerSummaryV1(t commissionerSummaryV1Tuple) (hqv1.Summary, error) {
	if strings.TrimSpace(t.config.InstanceID) == "" || strings.TrimSpace(t.config.LeagueID) == "" {
		return hqv1.Summary{}, errors.New("instance and league IDs are required")
	}
	if t.now.IsZero() || t.release.BuiltAt.IsZero() {
		return hqv1.Summary{}, errors.New("clock and release build time are required")
	}
	t.data = commissionerV1NormalizeData(t.data)
	activeTeams := commissionerV1ActiveTeams(t.config.Teams, t.state.TrimmedTeamIDs)
	teamByID := make(map[string]Team, len(activeTeams))
	eligible := make(map[string]bool, len(activeTeams))
	for _, team := range activeTeams {
		teamByID[team.ID] = team
		eligible[team.ID] = true
	}
	claimed, primaryManagers, coManagers := commissionerV1Membership(t.state, teamByID)
	total, occupied := len(activeTeams), len(claimed)
	vacant := total - occupied
	ready := 0
	boardGaps := 0
	for teamID := range claimed {
		if t.state.Ready[teamID] {
			ready++
		}
		if len(t.state.Boards[commissionerV1BoardOwnerKey(t.state, teamID)]) == 0 {
			boardGaps++
		}
	}

	draft := commissionerV1Draft(t, eligible, ready, boardGaps)
	phase := commissionerV1Phase(t.state.Phase, draft.State, t.state.Schedule != nil, t.config.Config.SeasonStartAt, t.now)
	readiness := commissionerV1Readiness(vacant, occupied-ready, boardGaps, draft, t.data)
	lineup := commissionerV1Lineup(t.data)
	waivers := commissionerV1Waivers(t.config.Config, t.state, t.now)
	trades := commissionerV1Trades(t.state)
	pickem := commissionerV1Pickem(t.state, t.data.Games, claimed, t.now)
	configuration := commissionerV1Configuration(t.config.Config, total, t.config.BlitzEnabled, t.now)
	links := commissionerV1Links(configuration)
	warnings := commissionerV1Warnings(t.data)
	attention := commissionerV1Attention(t.config.LeagueID, vacant, occupied-ready, boardGaps, draft, lineup, trades, pickem, t.data)
	calendar := commissionerV1Calendar(t.config.Config, t.state, draft, lineup, waivers, pickem, t.now)
	activity := commissionerV1Activity(t.state, teamByID, t.now)
	dataHealth := commissionerV1DataHealth(t.data, t.now)

	digest := (*string)(nil)
	if strings.TrimSpace(t.release.ImageDigest) != "" {
		digest = v1String(strings.TrimSpace(t.release.ImageDigest))
	}
	format := commissionerV1Format(t.config.Config.ModeLabel)
	summary := hqv1.Summary{
		Contract:      hqv1.ContractName,
		SchemaVersion: hqv1.SchemaVersion,
		Instance: hqv1.Instance{
			ID: strings.TrimSpace(t.config.InstanceID), LeagueID: strings.TrimSpace(t.config.LeagueID),
		},
		League: hqv1.League{
			Name:      commissionerV1Text(t.config.Config.Name, "Gridiron league"),
			ShortName: commissionerV1Text(t.config.Config.ShortCode, "GRIDIRON"),
			Format:    format, Season: t.config.Config.Season, Timezone: t.config.Config.Timezone,
		},
		Capabilities: []string{"draft.v1", "readiness.v1", "data-health.v1"},
		Competition: hqv1.Competition{
			Phase: phase,
			Teams: hqv1.TeamCounts{Occupied: v1Int(occupied), Vacant: v1Int(vacant), Total: v1Int(total)},
		},
		Draft: draft, Readiness: readiness,
		Membership: hqv1.Membership{
			ClaimedTeams: v1Int(occupied), OpenTeams: v1Int(vacant),
			PendingInvites:  v1Int(len(t.state.Invites) + len(t.state.CoInvites)),
			PrimaryManagers: v1Int(primaryManagers), CoManagers: v1Int(coManagers),
		},
		Lineup: lineup, Waivers: waivers, Trades: trades, Pickem: pickem,
		Warnings: warnings, Calendar: calendar, AttentionItems: attention,
		RecentActivity: activity, Configuration: configuration, DataHealth: dataHealth,
		Release: hqv1.Release{
			GitSHA:  strings.TrimSpace(t.release.GitSHA),
			BuiltAt: commissionerV1Time(t.release.BuiltAt), ImageDigest: digest,
		},
		Links: links, ProducedAt: commissionerV1Time(t.now),
	}
	if err := summary.Validate(); err != nil {
		return hqv1.Summary{}, err
	}
	return summary, nil
}

// commissionerV1BoardOwnerKey mirrors the persisted Big Board ownership
// contract used by the Draft Room: a claimed seat's board belongs to its
// normalized primary-manager identity, not to the team ID. Commissioner HQ
// consumes only the resulting count/gap signal; it never exposes the private
// player order.
func commissionerV1BoardOwnerKey(state PersistedState, teamID string) string {
	return normalizeEmail(memberForTeam(state.Members, teamID).Email)
}

func cloneCommissionerV1Config(cfg Config) Config {
	out := cfg
	out.Teams = append([]TeamSeed(nil), cfg.Teams...)
	out.Roster.Slots = cloneIntMap(cfg.Roster.Slots)
	out.Roster.Reserve = cloneIntMap(cfg.Roster.Reserve)
	out.Roster.Limits = cloneIntMap(cfg.Roster.Limits)
	return out
}

func cloneIntMap(values map[string]int) map[string]int {
	if values == nil {
		return nil
	}
	out := make(map[string]int, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func commissionerV1ActiveTeams(configured []Team, trimmed []string) []Team {
	removed := make(map[string]bool, len(trimmed))
	for _, id := range trimmed {
		removed[id] = true
	}
	out := make([]Team, 0, len(configured))
	seen := make(map[string]bool, len(configured))
	for _, team := range configured {
		if team.ID == "" || removed[team.ID] || seen[team.ID] {
			continue
		}
		seen[team.ID] = true
		out = append(out, team)
	}
	return out
}

func commissionerV1Membership(state PersistedState, teams map[string]Team) (map[string]bool, int, int) {
	claimed := make(map[string]bool, len(teams))
	primary, co := 0, 0
	for _, member := range state.Members {
		if _, ok := teams[member.TeamID]; !ok {
			continue
		}
		claimed[member.TeamID] = true
		if member.Role == "co" {
			co++
		} else {
			primary++
		}
	}
	return claimed, primary, co
}

func commissionerV1Time(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func v1Int(value int) *int          { return &value }
func v1String(value string) *string { return &value }

func commissionerV1Text(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || strings.Contains(value, "@") || strings.Contains(strings.ToLower(value), "://") {
		value = fallback
	}
	if len(value) > 256 {
		value = string([]byte(value)[:256])
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return strings.TrimSpace(value)
}

func commissionerV1Format(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dynasty":
		return "dynasty"
	case "redraft":
		return "redraft"
	case "keeper":
		return "keeper"
	default:
		return "other"
	}
}

func commissionerV1SortSeverity(value string) int {
	switch value {
	case "blocking":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func commissionerV1SortAttention(items []hqv1.AttentionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if commissionerV1SortSeverity(left.Severity) != commissionerV1SortSeverity(right.Severity) {
			return commissionerV1SortSeverity(left.Severity) < commissionerV1SortSeverity(right.Severity)
		}
		if left.DueAt == nil != (right.DueAt == nil) {
			return left.DueAt != nil
		}
		if left.DueAt != nil && *left.DueAt != *right.DueAt {
			return commissionerV1TimeBefore(*left.DueAt, *right.DueAt)
		}
		return left.Code < right.Code
	})
}

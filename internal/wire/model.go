package wire

import "time"

const (
	SchemaVersion  = 1
	SourceBluesky  = "bluesky"
	SourceFeed     = "syndication"
	SourceLeague   = "league_member"
	PostCollection = "app.bsky.feed.post"
)

// Category values a Signal can carry after classification
// (signal_rules.arb). These four are the ones GC-2's live-scoring
// box-fetch trigger seam (see Service.OnSignal, live_scoring.go) treats
// as fast enough, and fantasy-relevant enough, to justify an
// out-of-band, freshness-only box fetch ahead of the next scoreboard
// tick. Every other category (injury, role, weather, market, ...) stays
// provisional-only and never triggers one.
const (
	CategoryTouchdown   = "touchdown"
	CategoryTurnover    = "turnover"
	CategoryBigPlay     = "big_play"
	CategoryKickingPlay = "kicking"
)

// Signal is a small, current-state record derived from a public post. It is
// deliberately not a fantasy scoring event: every signal stays provisional
// until a structured dataset confirms it.
type Signal struct {
	SchemaVersion  int       `json:"schema_version"`
	ID             string    `json:"id"`
	Source         string    `json:"source"`
	SourceDID      string    `json:"source_did,omitempty"`
	SourceHandle   string    `json:"source_handle,omitempty"`
	SourceName     string    `json:"source_name,omitempty"`
	ReportedBy     string    `json:"reported_by,omitempty"`
	SourceURI      string    `json:"source_uri"`
	SourceURL      string    `json:"source_url"`
	EvidenceType   string    `json:"evidence_type,omitempty"`
	TrustTier      string    `json:"trust_tier,omitempty"`
	ClusterID      string    `json:"cluster_id,omitempty"`
	Corroborations int       `json:"corroborations,omitempty"`
	CID            string    `json:"cid,omitempty"`
	Category       string    `json:"category"`
	Label          string    `json:"label"`
	Text           string    `json:"text,omitempty"`
	TextHash       string    `json:"text_hash,omitempty"`
	Rule           string    `json:"rule"`
	TrustRule      string    `json:"trust_rule,omitempty"`
	Confidence     float64   `json:"confidence"`
	Provisional    bool      `json:"provisional"`
	OccurredAt     time.Time `json:"occurred_at"`
	ObservedAt     time.Time `json:"observed_at"`
	Deleted        bool      `json:"deleted,omitempty"`
}

// JournalEvent contains only derived metadata. Post text lives in the
// rewriteable current-state file so a source deletion can actually redact it.
type JournalEvent struct {
	SchemaVersion int       `json:"schema_version"`
	Operation     string    `json:"operation"`
	SignalID      string    `json:"signal_id"`
	Source        string    `json:"source"`
	SourceDID     string    `json:"source_did,omitempty"`
	SourceName    string    `json:"source_name,omitempty"`
	ReportedBy    string    `json:"reported_by,omitempty"`
	SourceURI     string    `json:"source_uri"`
	EvidenceType  string    `json:"evidence_type,omitempty"`
	TrustTier     string    `json:"trust_tier,omitempty"`
	ClusterID     string    `json:"cluster_id,omitempty"`
	Category      string    `json:"category,omitempty"`
	TextHash      string    `json:"text_hash,omitempty"`
	Rule          string    `json:"rule,omitempty"`
	TrustRule     string    `json:"trust_rule,omitempty"`
	Confidence    float64   `json:"confidence,omitempty"`
	ObservedAt    time.Time `json:"observed_at"`
}

type SourceStatus struct {
	Handle string `json:"handle,omitempty"`
	DID    string `json:"did"`
}

// FeedSource is an explicit, commissioner-controlled RSS or Atom input.
// EvidenceType must be news, community_feed, or social.
type FeedSource struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	EvidenceType string `json:"evidence_type"`
	Enabled      bool   `json:"enabled"`
}

type FeedStatus struct {
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	EvidenceType  string    `json:"evidence_type"`
	State         string    `json:"state"`
	Accepted      int64     `json:"accepted"`
	Ignored       int64     `json:"ignored"`
	LastChecked   time.Time `json:"last_checked,omitzero"`
	LastPublished time.Time `json:"last_published,omitzero"`
	LastError     string    `json:"last_error,omitempty"`
}

type CommunitySubmission struct {
	ReporterID   string
	ReporterName string
	EvidenceType string
	SourceName   string
	SourceURL    string
	Summary      string
}

// Status is safe for the league UI and contains no filesystem paths.
type Status struct {
	SchemaVersion      int              `json:"schema_version"`
	Configured         bool             `json:"configured"`
	Running            bool             `json:"running"`
	Mode               string           `json:"mode"`
	ConfigurationIssue string           `json:"configuration_issue,omitempty"`
	SourceIssue        string           `json:"source_issue,omitempty"`
	SourcesPartial     bool             `json:"sources_partial"`
	BlueskyConfigured  bool             `json:"bluesky_configured"`
	Sources            []SourceStatus   `json:"sources"`
	Feeds              []FeedStatus     `json:"feeds"`
	FeedStaleAfter     time.Duration    `json:"feed_stale_after"`
	SourceCounts       map[string]int64 `json:"source_counts"`
	RelevantSignals    int64            `json:"relevant_signals"`
	IgnoredPosts       int64            `json:"ignored_posts"`
	DeletedSignals     int64            `json:"deleted_signals"`
	LastCursor         int64            `json:"last_cursor,omitempty"`
	LastEventAt        time.Time        `json:"last_event_at,omitzero"`
	ReconnectAt        time.Time        `json:"reconnect_at,omitzero"`
	LastError          string           `json:"last_error,omitempty"`
}

type Classification struct {
	Category   string
	Label      string
	Rule       string
	Confidence float64
	Relevant   bool
}

type TrustAssessment struct {
	Tier   string
	Rule   string
	Weight float64
}

type jetstreamEvent struct {
	DID    string           `json:"did"`
	TimeUS int64            `json:"time_us"`
	Cursor int64            `json:"cursor,omitempty"`
	Kind   string           `json:"kind"`
	Commit *jetstreamCommit `json:"commit,omitempty"`
}

type jetstreamCommit struct {
	Rev        string       `json:"rev,omitempty"`
	Operation  string       `json:"operation"`
	Collection string       `json:"collection"`
	RKey       string       `json:"rkey"`
	Record     *blueskyPost `json:"record,omitempty"`
	CID        string       `json:"cid,omitempty"`
}

type blueskyPost struct {
	Type      string `json:"$type,omitempty"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt,omitempty"`
}

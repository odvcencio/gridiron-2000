package commissionerhq

import "time"

const (
	SchemaVersion = 2
	SummaryPath   = "/api/commissioner/v2/summary"
)

// Summary is the deliberately small, PII-free contract one league exposes to
// another commissioner dashboard. It contains counts, state, and operator
// guidance only: never identities, invites, boards, sessions, or raw errors.
type Summary struct {
	SchemaVersion int         `json:"schemaVersion"`
	GeneratedAt   time.Time   `json:"generatedAt"`
	Instance      Instance    `json:"instance"`
	Runtime       Runtime     `json:"runtime"`
	Membership    Membership  `json:"membership"`
	Draft         Draft       `json:"draft"`
	Season        Season      `json:"season"`
	Pool          Pool        `json:"pool"`
	OpenData      OpenData    `json:"openData"`
	Blitz         BlitzHealth `json:"blitz"`
	Attention     []Attention `json:"attention"`
}

type Instance struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortCode string `json:"shortCode"`
	PublicURL string `json:"publicURL"`
	Mode      string `json:"mode"`
	Season    int    `json:"season"`
}

// StateSchema is the PII-free persistence compatibility evidence carried in
// release metadata. It is intentionally separate from the federation
// protocol's own Summary.SchemaVersion: this describes the league state a
// candidate binary must be able to read before a rollout or rollback.
type StateSchema struct {
	PersistedVersion         int  `json:"persistedVersion"`
	SupportedVersion         int  `json:"supportedVersion"`
	PersistedDatabaseVersion int  `json:"persistedDatabaseVersion"`
	SupportedDatabaseVersion int  `json:"supportedDatabaseVersion"`
	Compatible               bool `json:"compatible"`
}

type Runtime struct {
	Ready            bool        `json:"ready"`
	AppVersion       string      `json:"appVersion"`
	FrameworkVersion string      `json:"frameworkVersion"`
	GitSHA           string      `json:"gitSHA"`
	Build            string      `json:"build"`
	StateSchema      StateSchema `json:"stateSchema"`
}

type SeatLedgerEntry struct {
	Seat    int  `json:"seat"`
	Claimed bool `json:"claimed"`
	Ready   bool `json:"ready"`
}

type Membership struct {
	Seats        int               `json:"seats"`
	ClaimedSeats int               `json:"claimedSeats"`
	ReadySeats   int               `json:"readySeats"`
	Members      int               `json:"members"`
	SeatLedger   []SeatLedgerEntry `json:"seatLedger"`
}

type Draft struct {
	ScheduledAt       time.Time `json:"scheduledAt"`
	Status            string    `json:"status"`
	Started           bool      `json:"started"`
	StartedAt         time.Time `json:"startedAt,omitzero"`
	Rounds            int       `json:"rounds"`
	Picks             int       `json:"picks"`
	Order             []int     `json:"order,omitempty"`
	OrderSet          bool      `json:"orderSet"`
	ClockArmed        bool      `json:"clockArmed"`
	ClockPaused       bool      `json:"clockPaused"`
	ClockRemainingSec int       `json:"clockRemainingSec,omitempty"`
	Deadline          time.Time `json:"clockDeadline,omitzero"`
}

type Schedule struct {
	Published        bool   `json:"published"`
	Season           int    `json:"season"`
	WeekCount        int    `json:"weekCount"`
	StartWeek        int    `json:"startWeek"`
	EndWeek          int    `json:"endWeek"`
	CurrentWeek      int    `json:"currentWeek"`
	TotalMatchups    int    `json:"totalMatchups"`
	FinalWeeks       int    `json:"finalWeeks"`
	FinalMatchups    int    `json:"finalMatchups"`
	RedrawLocked     bool   `json:"redrawLocked"`
	RedrawLockReason string `json:"redrawLockReason,omitempty"`
}

type WeekClose struct {
	Week           int       `json:"week"`
	Final          bool      `json:"final"`
	Ready          bool      `json:"ready"`
	GamesKnown     bool      `json:"gamesKnown"`
	GamesTotal     int       `json:"gamesTotal"`
	GamesFinal     int       `json:"gamesFinal"`
	StatsFresh     bool      `json:"statsFresh"`
	StatsUpdatedAt time.Time `json:"statsUpdatedAt,omitzero"`
	Reason         string    `json:"reason,omitempty"`
}

type Playoffs struct {
	Seeded         bool   `json:"seeded"`
	Available      bool   `json:"available"`
	Status         string `json:"status,omitempty"`
	StatusLabel    string `json:"statusLabel,omitempty"`
	Source         string `json:"source,omitempty"`
	SourceState    string `json:"sourceState,omitempty"`
	Authoritative  bool   `json:"authoritative"`
	FinalWeek      int    `json:"finalWeek,omitempty"`
	CurrentRound   int    `json:"currentRound,omitempty"`
	NextMatchups   int    `json:"nextMatchups,omitempty"`
	ChampionTeamID string `json:"championTeamId,omitempty"`
	Note           string `json:"note"`
}

type Season struct {
	Season      int       `json:"season"`
	Phase       string    `json:"phase"`
	CurrentWeek int       `json:"currentWeek"`
	Schedule    Schedule  `json:"schedule"`
	WeekClose   WeekClose `json:"weekClose"`
	Playoffs    Playoffs  `json:"playoffs"`
}

type Pool struct {
	Mode           string    `json:"mode"`
	Actual         int       `json:"actual"`
	Target         int       `json:"target"`
	RosterCapacity int       `json:"rosterCapacity"`
	Cushion        int       `json:"cushion"`
	Shortfall      int       `json:"shortfall"`
	ActualCoverage float64   `json:"actualCoverage"`
	TargetCoverage float64   `json:"targetCoverage"`
	RosterCoverage float64   `json:"rosterCoverage"`
	LastSync       time.Time `json:"lastSync,omitzero"`
	// Players and Coverage are internal compatibility aliases used while
	// assembling the summary; neither is exposed on the v2 wire contract.
	Players  int     `json:"-"`
	Coverage float64 `json:"-"`
	Error    string  `json:"-"`
}

type DatasetStatus struct {
	State       string    `json:"state"`
	LastChecked time.Time `json:"lastChecked,omitzero"`
	LastUpdated time.Time `json:"lastUpdated,omitzero"`
}

type OpenData struct {
	Season          int           `json:"season"`
	Running         bool          `json:"running"`
	Schedules       DatasetStatus `json:"schedules"`
	PlayerStats     DatasetStatus `json:"playerStats"`
	PlayerStatsPrev DatasetStatus `json:"playerStatsPrev"`
	Injuries        DatasetStatus `json:"injuries"`
	TeamStats       DatasetStatus `json:"teamStats"`
	PlayByPlay      DatasetStatus `json:"playByPlay"`
}

// BlitzSlateHealth and BlitzHealth are a PII-free, provider-safe projection
// of the optional preseason source. Error is bounded operator copy only;
// URLs, credentials, and raw transport failures never cross this contract.
type BlitzSlateHealth struct {
	State                string    `json:"state"`
	LastAttempt          time.Time `json:"lastAttempt,omitzero"`
	LastSuccess          time.Time `json:"lastSuccess,omitzero"`
	Error                string    `json:"error,omitempty"`
	ExpectedGames        int       `json:"expectedGames"`
	FetchedGames         int       `json:"fetchedGames"`
	FinalGames           int       `json:"finalGames"`
	ExpectedScoringGames int       `json:"expectedScoringGames"`
	FetchedScoringGames  int       `json:"fetchedScoringGames"`
	ScoringComplete      bool      `json:"scoringComplete"`
	Complete             bool      `json:"complete"`
	Final                bool      `json:"final"`
	VerifiedZero         bool      `json:"verifiedZero"`
}

type BlitzPre1Health struct {
	State         string    `json:"state"`
	LastAttempt   time.Time `json:"lastAttempt,omitzero"`
	LastSuccess   time.Time `json:"lastSuccess,omitzero"`
	Error         string    `json:"error,omitempty"`
	ExpectedGames int       `json:"expectedGames"`
	FetchedGames  int       `json:"fetchedGames"`
	Complete      bool      `json:"complete"`
}

type BlitzHealth struct {
	Enabled              bool                        `json:"enabled"`
	State                string                      `json:"state"`
	LastAttempt          time.Time                   `json:"lastAttempt,omitzero"`
	LastSuccess          time.Time                   `json:"lastSuccess,omitzero"`
	Error                string                      `json:"error,omitempty"`
	ExpectedGames        int                         `json:"expectedGames"`
	FetchedGames         int                         `json:"fetchedGames"`
	FinalGames           int                         `json:"finalGames"`
	ExpectedScoringGames int                         `json:"expectedScoringGames"`
	FetchedScoringGames  int                         `json:"fetchedScoringGames"`
	ScoringComplete      bool                        `json:"scoringComplete"`
	Complete             bool                        `json:"complete"`
	Final                bool                        `json:"final"`
	VerifiedZero         bool                        `json:"verifiedZero"`
	Slates               map[string]BlitzSlateHealth `json:"slates,omitempty"`
	Pre1                 BlitzPre1Health             `json:"pre1"`
}

type Attention struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Count    int    `json:"count,omitempty"`
	Message  string `json:"message"`
	Area     string `json:"area"`
}

type FleetEntry struct {
	PeerID    string
	PublicURL string
	Summary   Summary
	Error     string
}

func (e FleetEntry) Available() bool { return e.Error == "" }

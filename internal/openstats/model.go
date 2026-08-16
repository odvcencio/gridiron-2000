package openstats

import "time"

const (
	SchemaVersion  = 1
	License        = "CC-BY-4.0"
	Attribution    = "nflverse"
	AttributionURL = "https://github.com/nflverse/nflverse-data"
)

type DatasetStatus struct {
	Name         string    `json:"name"`
	State        string    `json:"state"`
	SourceURL    string    `json:"source_url"`
	License      string    `json:"license"`
	Rows         int       `json:"rows"`
	Bytes        int64     `json:"bytes"`
	SHA256       string    `json:"sha256,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	LastChecked  time.Time `json:"last_checked,omitzero"`
	LastUpdated  time.Time `json:"last_updated,omitzero"`
	LastError    string    `json:"last_error,omitempty"`
}

type Status struct {
	SchemaVersion  int           `json:"schema_version"`
	Provider       string        `json:"provider"`
	License        string        `json:"license"`
	Attribution    string        `json:"attribution"`
	AttributionURL string        `json:"attribution_url"`
	Season         int           `json:"season"`
	Running        bool          `json:"running"`
	Schedules      DatasetStatus `json:"schedules"`
	PlayerStats    DatasetStatus `json:"player_stats"`
	Injuries       DatasetStatus `json:"injuries"`
}

type ScheduleGame struct {
	GameID    string  `json:"game_id"`
	Season    int     `json:"season"`
	GameType  string  `json:"game_type"`
	Week      int     `json:"week"`
	GameDay   string  `json:"gameday"`
	GameTime  string  `json:"gametime,omitempty"`
	AwayTeam  string  `json:"away_team"`
	AwayScore float64 `json:"away_score,omitempty"`
	HomeTeam  string  `json:"home_team"`
	HomeScore float64 `json:"home_score,omitempty"`
}

// PlayerWeekStat is the compact, provider-neutral fantasy ledger retained by
// the league. The raw CC-BY CSV remains cached alongside it for future models.
type PlayerWeekStat struct {
	PlayerID             string  `json:"player_id"`
	PlayerName           string  `json:"player_name"`
	Position             string  `json:"position"`
	Season               int     `json:"season"`
	Week                 int     `json:"week"`
	SeasonType           string  `json:"season_type"`
	GameID               string  `json:"game_id"`
	Team                 string  `json:"team"`
	OpponentTeam         string  `json:"opponent_team"`
	PassingYards         float64 `json:"passing_yards"`
	PassingTDs           float64 `json:"passing_tds"`
	PassingInterceptions float64 `json:"passing_interceptions"`
	RushingYards         float64 `json:"rushing_yards"`
	RushingTDs           float64 `json:"rushing_tds"`
	Receptions           float64 `json:"receptions"`
	ReceivingYards       float64 `json:"receiving_yards"`
	ReceivingTDs         float64 `json:"receiving_tds"`
	FumblesLost          float64 `json:"fumbles_lost"`
	FantasyPoints        float64 `json:"fantasy_points"`
	FantasyPointsPPR     float64 `json:"fantasy_points_ppr"`
}

type PlayerQuery struct {
	Week       int
	PlayerID   string
	Team       string
	SeasonType string
	Limit      int
}

type InjuryReport struct {
	Season                  int    `json:"season"`
	SeasonType              string `json:"season_type"`
	Team                    string `json:"team"`
	Week                    int    `json:"week"`
	PlayerID                string `json:"player_id"`
	Position                string `json:"position"`
	PlayerName              string `json:"player_name"`
	ReportPrimaryInjury     string `json:"report_primary_injury,omitempty"`
	ReportSecondaryInjury   string `json:"report_secondary_injury,omitempty"`
	ReportStatus            string `json:"report_status,omitempty"`
	PracticePrimaryInjury   string `json:"practice_primary_injury,omitempty"`
	PracticeSecondaryInjury string `json:"practice_secondary_injury,omitempty"`
	PracticeStatus          string `json:"practice_status,omitempty"`
	DateModified            string `json:"date_modified,omitempty"`
}

type InjuryQuery struct {
	Week     int
	PlayerID string
	Team     string
	Limit    int
}

type manifest struct {
	SchemaVersion int           `json:"schema_version"`
	Season        int           `json:"season"`
	Schedules     DatasetStatus `json:"schedules"`
	PlayerStats   DatasetStatus `json:"player_stats"`
	Injuries      DatasetStatus `json:"injuries"`
}

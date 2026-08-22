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
	SchemaVersion   int           `json:"schema_version"`
	Provider        string        `json:"provider"`
	License         string        `json:"license"`
	Attribution     string        `json:"attribution"`
	AttributionURL  string        `json:"attribution_url"`
	Season          int           `json:"season"`
	Running         bool          `json:"running"`
	Schedules       DatasetStatus `json:"schedules"`
	PlayerStats     DatasetStatus `json:"player_stats"`
	PlayerStatsPrev DatasetStatus `json:"player_stats_prev"`
	Injuries        DatasetStatus `json:"injuries"`
	// TeamStats mirrors nflverse's "stats_team" release (team-week box
	// scores): the source for DEFENSE-group scoring (WP-R2).
	TeamStats DatasetStatus `json:"team_stats"`
	// PlayByPlay mirrors nflverse's "pbp" release: the source for the
	// per-punt PUNTING keys that a box-score aggregate cannot supply
	// (coffinCorner, puntDownedInside5, and the puntYards 40+-yard gate;
	// WP-R2, scoring.go's TODO(WP-R2)).
	PlayByPlay DatasetStatus `json:"play_by_play"`
}

type ScheduleGame struct {
	GameID            string   `json:"game_id"`
	Season            int      `json:"season"`
	GameType          string   `json:"game_type"`
	Week              int      `json:"week"`
	GameDay           string   `json:"gameday"`
	GameTime          string   `json:"gametime,omitempty"`
	AwayTeam          string   `json:"away_team"`
	AwayScore         float64  `json:"away_score,omitempty"`
	AwayScorePresent  bool     `json:"away_score_present,omitempty"`
	HomeTeam          string   `json:"home_team"`
	HomeScore         float64  `json:"home_score,omitempty"`
	HomeScorePresent  bool     `json:"home_score_present,omitempty"`
	Result            string   `json:"result,omitempty"`
	ResultPresent     bool     `json:"result_present,omitempty"`
	SpreadLine        *float64 `json:"spread_line,omitempty"`
}

// HasFinalScore reports whether the source supplied both scores. The
// presence bits are deliberately separate from the numeric values: a blank
// nflverse score is not the same thing as an actual zero.
func (game ScheduleGame) HasFinalScore() bool {
	return game.AwayScorePresent && game.HomeScorePresent
}

// HasResult reports whether the schedule source supplied an actual result or
// both final scores. Pick'em finality must follow source truth, never elapsed
// time since kickoff.
func (game ScheduleGame) HasResult() bool {
	return game.ResultPresent || game.HasFinalScore()
}

// ScheduleSnapshot is the small source envelope consumed at the adapter
// boundary. Games remain provider-normalized while the source observation,
// update, and provenance stay available to callers without coupling the
// league package to openstats.
type ScheduleSnapshot struct {
	Games       []ScheduleGame `json:"games"`
	ObservedAt  time.Time      `json:"observed_at,omitzero"`
	UpdatedAt   time.Time      `json:"updated_at,omitzero"`
	SourceURL   string         `json:"source_url,omitempty"`
	Provenance  string         `json:"provenance,omitempty"`
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
	// Kicking fields (WP-R2): sourced from the same stats_player_week
	// release's fg_made/fg_missed/pat_made columns, present on K-position
	// rows. defaultScoringRules' KICKING group carries no FG distance
	// bands or an xpMissed key, so only these three feed live scoring
	// today; see main.go's leagueWeekStatsSource.
	FGMade   float64 `json:"fg_made"`
	FGMissed float64 `json:"fg_missed"`
	XPMade   float64 `json:"xp_made"`
	// Punting box-score aggregates (WP-R2): present on P-position rows in
	// the same stats_player_week release. These are the honest fallback
	// when the play-by-play mirror (PuntEvents) has no data for the
	// requested week: per-game aggregates only, no location-qualified
	// events (no coffin-corner/inside-5 breakdown, no per-punt 40+-yard
	// gate — pt_yards is gross yards over every punt in the game). See
	// main.go's leagueWeekStatsSource and its punting fallback path.
	Punts          float64 `json:"punts"`
	PuntYardsGross float64 `json:"punt_yards_gross"`
	PuntLong       float64 `json:"punt_long"`
	PuntInside20   float64 `json:"punt_inside_20"`
	PuntDowned     float64 `json:"punt_downed"`
	PuntTouchback  float64 `json:"punt_touchback"`
	PuntBlocked    float64 `json:"punt_blocked"`
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
	SchemaVersion   int           `json:"schema_version"`
	Season          int           `json:"season"`
	Schedules       DatasetStatus `json:"schedules"`
	PlayerStats     DatasetStatus `json:"player_stats"`
	PlayerStatsPrev DatasetStatus `json:"player_stats_prev"`
	Injuries        DatasetStatus `json:"injuries"`
	TeamStats       DatasetStatus `json:"team_stats"`
	PlayByPlay      DatasetStatus `json:"play_by_play"`
}

// TeamWeekStat is one team's defensive/special-teams box score for one NFL
// week, mirrored from nflverse's "stats_team" release. It is the source for
// defaultScoringRules' DEFENSE group (WP-R2): dstSack, dstInt, dstFumbleRec,
// dstTD, and dstSafety feed directly; dstShutout derives from the schedule's
// points-allowed, not from this dataset (see main.go's leagueWeekStatsSource).
type TeamWeekStat struct {
	Season           int     `json:"season"`
	Week             int     `json:"week"`
	SeasonType       string  `json:"season_type"`
	GameID           string  `json:"game_id"`
	Team             string  `json:"team"`
	OpponentTeam     string  `json:"opponent_team"`
	DefSacks         float64 `json:"def_sacks"`
	DefInterceptions float64 `json:"def_interceptions"`
	DefTDs           float64 `json:"def_tds"`
	DefSafeties      float64 `json:"def_safeties"`
	// FumbleRecoveryOpp counts only recoveries of the OPPONENT's fumbles
	// (a takeaway); recovering the team's own fumble (fumble_recovery_own
	// in the source CSV) is not a defensive scoring event and is
	// deliberately excluded.
	FumbleRecoveryOpp float64 `json:"fumble_recovery_opp"`
}

type TeamStatsQuery struct {
	Week  int
	Team  string
	Limit int
}

// PuntEvent is one punt play, parsed from the play-by-play mirror
// (nflverse's "pbp" release). It carries exactly the fields
// defaultScoringRules' PUNTING group needs for the keys a box-score
// aggregate cannot supply (WP-R2): coffinCorner, puntDownedInside5, and the
// puntYards 40+-yard gate (applied by the caller, not here — see main.go's
// leagueWeekStatsSource).
type PuntEvent struct {
	Season   int    `json:"season"`
	Week     int    `json:"week"`
	GameID   string `json:"game_id"`
	Team     string `json:"team"`
	PunterID string `json:"punter_id"`
	Punter   string `json:"punter_name"`
	// Distance is the gross kick distance in yards (kick_distance).
	// Blocked punts carry distance 0.
	Distance float64 `json:"distance"`
	Blocked  bool    `json:"blocked"`
	// InsideTwenty mirrors punt_inside_twenty: the receiving team's
	// possession started inside its own 20 (downed, fair catch, or a
	// short return that never left the 20) — nflverse's definition, not a
	// landing-spot-only measure.
	InsideTwenty bool `json:"inside_twenty"`
	Touchback    bool `json:"touchback"`
	Downed       bool `json:"downed"`
	OutOfBounds  bool `json:"out_of_bounds"`
	FairCatch    bool `json:"fair_catch"`
	// CoffinCorner: the punt went out of bounds with an (estimated,
	// pre-return) landing spot inside the receiving team's 10-yard line.
	CoffinCorner bool `json:"coffin_corner"`
	// Inside5: the punt was downed (not returned) with an (estimated)
	// landing spot inside the receiving team's 5-yard line.
	Inside5 bool `json:"inside5"`
}

type PuntQuery struct {
	Week     int
	PunterID string
	Team     string
	Limit    int
}

// PlayerSeasonSummary is one player's aggregated previous-season stat line,
// summed across every week in the "player_stats_prev" dataset.
type PlayerSeasonSummary struct {
	PlayerName    string  `json:"player_name"`
	Position      string  `json:"position"`
	Team          string  `json:"team"`
	Season        int     `json:"season"`
	Games         int     `json:"games"`
	PassYds       int     `json:"pass_yds"`
	PassTD        int     `json:"pass_td"`
	PassInt       int     `json:"pass_int"`
	RushYds       int     `json:"rush_yds"`
	RushTD        int     `json:"rush_td"`
	Receptions    int     `json:"receptions"`
	RecYds        int     `json:"rec_yds"`
	RecTD         int     `json:"rec_td"`
	FantasyPoints float64 `json:"fantasy_points"`
}

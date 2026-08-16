package league

import "time"

const (
	// DraftRounds caps the snake draft; the room completes when every team
	// holds this many picks.
	DraftRounds = 15

	DefaultDraftAt       = "2026-08-22T16:00:00-04:00"
	DefaultDraftTZ       = "America/New_York"
	DefaultRefreshPeriod = 60 * time.Second
	DefaultSeasonStartAt = "2026-09-10T20:20:00-04:00"
)

// Team is one franchise in the private league.
type Team struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Abbreviation string  `json:"abbreviation"`
	Division     string  `json:"division"`
	Manager      string  `json:"manager"`
	Record       string  `json:"record"`
	PointsFor    float64 `json:"pointsFor"`
	Rank         int     `json:"rank"`
	Streak       string  `json:"streak"`
	Tone         string  `json:"tone"`
}

// Player is the compact player shape shared by the roster and draft room.
type Player struct {
	ID         string  `json:"id"`
	GSISID     string  `json:"gsisId,omitempty"`
	Name       string  `json:"name"`
	Position   string  `json:"position"`
	NFLTeam    string  `json:"nflTeam"`
	Jersey     string  `json:"jersey,omitempty"`
	Opponent   string  `json:"opponent"`
	ADP        float64 `json:"adp,omitempty"`
	ADPRank    int     `json:"adpRank,omitempty"`
	ByeWeek    int     `json:"byeWeek,omitempty"`
	Injury     string  `json:"injury,omitempty"`
	Projection float64 `json:"projection"`
	// ProjStats holds the projected-stat line behind Projection, keyed by
	// stat name (for example "passYds", "rushTD"). scoreBreakdown resolves
	// it against the league's live scoring settings; see breakdown.go.
	ProjStats map[string]float64 `json:"projStats,omitempty"`
	Points    float64            `json:"points"`
	Status    string             `json:"status"`
	News      string             `json:"news"`
	Headshot  string             `json:"headshot,omitempty"`
	// Hist is a prebuilt, legible previous-season line (for example
	// "2025 · 17 G · 1,206 rush yds · 12 TD · 4.8 FPts"). An empty string
	// means no historical line is available. main.go builds the text; the
	// league package only carries and serves it. See HistoricalSource.
	Hist string `json:"hist,omitempty"`
}

// GameInfo is one real NFL game supplied by the schedule source.
type GameInfo struct {
	ID        string
	Week      int
	Kickoff   time.Time
	Away      string // team abbreviation
	Home      string
	AwayScore int
	HomeScore int
	Final     bool
}

// DraftPick is persisted when a mock or live pick is made.
type DraftPick struct {
	Number   int       `json:"number"`
	Round    int       `json:"round"`
	TeamID   string    `json:"teamId"`
	PlayerID string    `json:"playerId"`
	MadeAt   time.Time `json:"madeAt"`
	// MadeBy is the pick's provenance: "manager", "auto", or "commissioner".
	// Old state files decode with MadeBy == ""; pickMaps normalizes the
	// empty value to "manager" for display, leaving stored data untouched.
	MadeBy string `json:"madeBy,omitempty"`
}

// Member binds a Google identity to a league seat.
type Member struct {
	TeamID string `json:"teamId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

// PersistedState is intentionally small and can later be replaced by a DB adapter.
type PersistedState struct {
	Ready      map[string]bool     `json:"ready"`
	Picks      []DraftPick         `json:"picks"`
	Members    map[string]Member   `json:"members"`
	Invites    []string            `json:"invites"`
	Boards     map[string][]string `json:"boards"`
	TeamNames  map[string]string   `json:"teamNames"`
	DraftOrder []string            `json:"draftOrder"`
	Scoring    map[string]float64  `json:"scoring"`
	// Pickems maps owner email to game ID to the picked team abbreviation.
	Pickems map[string]map[string]string `json:"pickems"`

	// Clock state. Zero values mean: no clock armed, not paused, env
	// default duration. Old state files decode to exactly that, so the
	// change is additive and needs no migration.
	ClockDeadline time.Time `json:"clockDeadline"` // zero = unarmed
	ClockPaused   bool      `json:"clockPaused,omitempty"`
	// ClockRemainingSec holds the frozen countdown while paused.
	ClockRemainingSec int `json:"clockRemainingSec,omitempty"`
	// ClockDurationSec overrides PICK_CLOCK when nonzero; the commissioner
	// sets it mid-draft. It applies from the next arm.
	ClockDurationSec int `json:"clockDurationSec,omitempty"`
	// Autopick maps team ID to its away-mode auto-pick toggle.
	Autopick map[string]bool `json:"autopick"`
}

// ScoreTeam is the live score representation returned to browsers.
type ScoreTeam struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Abbreviation string  `json:"abbreviation"`
	Score        float64 `json:"score"`
}

// ScoreMatchup is a paired fantasy matchup.
type ScoreMatchup struct {
	ID     string    `json:"id"`
	Away   ScoreTeam `json:"away"`
	Home   ScoreTeam `json:"home"`
	Status string    `json:"status"`
	Clock  string    `json:"clock"`
}

// LiveSnapshot is the stable JSON contract consumed by the score enhancer.
type LiveSnapshot struct {
	OK                  bool           `json:"ok"`
	Source              string         `json:"source"`
	SourceLabel         string         `json:"sourceLabel"`
	Week                int            `json:"week"`
	WeekLabel           string         `json:"weekLabel"`
	Status              string         `json:"status"`
	LastUpdated         time.Time      `json:"lastUpdated"`
	RefreshAfterSeconds int            `json:"refreshAfterSeconds"`
	Matchups            []ScoreMatchup `json:"matchups"`
	Warning             string         `json:"warning,omitempty"`
}

// defaultTeams returns the eight franchise identities. Manager, record, and
// streak stay empty until a real member claims the seat and games are played;
// the UI renders unclaimed seats explicitly. Order matters: the snake draft
// and matchup pairing index into this slice. team-1..team-4 sit in the Aqua
// division; team-5..team-8 sit in the Orange division. A commissioner may
// rename any seat; see Store.SetTeamName.
func defaultTeams() []Team {
	return []Team{
		{ID: "team-1", Name: "Aqua 1", Abbreviation: "AQ1", Division: "Aqua", Record: "0–0", Rank: 1, Streak: "—", Tone: "cyan"},
		{ID: "team-2", Name: "Aqua 2", Abbreviation: "AQ2", Division: "Aqua", Record: "0–0", Rank: 2, Streak: "—", Tone: "blue"},
		{ID: "team-3", Name: "Aqua 3", Abbreviation: "AQ3", Division: "Aqua", Record: "0–0", Rank: 3, Streak: "—", Tone: "violet"},
		{ID: "team-4", Name: "Aqua 4", Abbreviation: "AQ4", Division: "Aqua", Record: "0–0", Rank: 4, Streak: "—", Tone: "lime"},
		{ID: "team-5", Name: "Orange 1", Abbreviation: "OR1", Division: "Orange", Record: "0–0", Rank: 5, Streak: "—", Tone: "orange"},
		{ID: "team-6", Name: "Orange 2", Abbreviation: "OR2", Division: "Orange", Record: "0–0", Rank: 6, Streak: "—", Tone: "gold"},
		{ID: "team-7", Name: "Orange 3", Abbreviation: "OR3", Division: "Orange", Record: "0–0", Rank: 7, Streak: "—", Tone: "magenta"},
		{ID: "team-8", Name: "Orange 4", Abbreviation: "OR4", Division: "Orange", Record: "0–0", Rank: 8, Streak: "—", Tone: "pink"},
	}
}

// defaultTeamIDs returns the eight team IDs in their default (team-1..team-8)
// order. It is the fallback draft order and the permutation set that
// Store.SetDraftOrder validates against.
func defaultTeamIDs() []string {
	teams := defaultTeams()
	ids := make([]string, len(teams))
	for index, team := range teams {
		ids[index] = team.ID
	}
	return ids
}

func defaultPlayers() []Player {
	return []Player{
		{ID: "p-01", GSISID: "00-0036900", Name: "Ja'Marr Chase", Position: "WR", NFLTeam: "CIN", Opponent: "vs CLE", Projection: 20.8, Points: 0, Status: "Available", News: "Demo player pool — sync the open ledger for current status."},
		{ID: "p-02", GSISID: "00-0038542", Name: "Bijan Robinson", Position: "RB", NFLTeam: "ATL", Opponent: "vs TB", Projection: 19.7, Points: 0, Status: "Available", News: "Three-down workload profile."},
		{ID: "p-03", GSISID: "00-0036322", Name: "Justin Jefferson", Position: "WR", NFLTeam: "MIN", Opponent: "@ GB", Projection: 19.5, Points: 0, Status: "Available", News: "Elite target-share profile."},
		{ID: "p-04", GSISID: "00-0039139", Name: "Jahmyr Gibbs", Position: "RB", NFLTeam: "DET", Opponent: "vs CHI", Projection: 18.9, Points: 0, Status: "Available", News: "High-value touches in the demo projection."},
		{ID: "p-05", GSISID: "00-0036358", Name: "CeeDee Lamb", Position: "WR", NFLTeam: "DAL", Opponent: "@ PHI", Projection: 18.4, Points: 0, Status: "Available", News: "Volume-driven WR1 profile."},
		{ID: "p-06", GSISID: "00-0034796", Name: "Lamar Jackson", Position: "QB", NFLTeam: "BAL", Opponent: "vs PIT", Projection: 22.6, Points: 0, Status: "Available", News: "Dual-threat ceiling in the sample pool."},
		{ID: "p-07", GSISID: "00-0036963", Name: "Amon-Ra St. Brown", Position: "WR", NFLTeam: "DET", Opponent: "vs CHI", Projection: 17.8, Points: 0, Status: "Available", News: "Reliable interior target profile."},
		{ID: "p-08", GSISID: "00-0039338", Name: "Brock Bowers", Position: "TE", NFLTeam: "LV", Opponent: "@ DEN", Projection: 15.9, Points: 0, Status: "Available", News: "Premium tight-end advantage."},
		{ID: "p-09", GSISID: "00-0034857", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF", Opponent: "vs MIA", Projection: 22.1, Points: 0, Status: "Available", News: "Top-tier weekly scoring ceiling."},
		{ID: "p-10", GSISID: "00-0039075", Name: "Puka Nacua", Position: "WR", NFLTeam: "LAR", Opponent: "@ SF", Projection: 17.2, Points: 0, Status: "Available", News: "High-volume demo projection."},
		{ID: "p-11", GSISID: "00-0034844", Name: "Saquon Barkley", Position: "RB", NFLTeam: "PHI", Opponent: "vs DAL", Projection: 17.0, Points: 0, Status: "Available", News: "Explosive rushing profile."},
		{ID: "p-12", GSISID: "00-0037744", Name: "Trey McBride", Position: "TE", NFLTeam: "ARI", Opponent: "vs SEA", Projection: 14.8, Points: 0, Status: "Available", News: "Target-volume edge at tight end."},
	}
}

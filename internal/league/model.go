package league

import "time"

const (
	DefaultDraftAt       = "2026-08-22T16:00:00-04:00"
	DefaultDraftTZ       = "America/New_York"
	DefaultRefreshPeriod = 60 * time.Second
)

// Team is one franchise in the private league.
type Team struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Abbreviation string  `json:"abbreviation"`
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
	Opponent   string  `json:"opponent"`
	ADP        float64 `json:"adp,omitempty"`
	ADPRank    int     `json:"adpRank,omitempty"`
	ByeWeek    int     `json:"byeWeek,omitempty"`
	Injury     string  `json:"injury,omitempty"`
	Projection float64 `json:"projection"`
	Points     float64 `json:"points"`
	Status     string  `json:"status"`
	News       string  `json:"news"`
}

// DraftPick is persisted when a mock or live pick is made.
type DraftPick struct {
	Number   int       `json:"number"`
	Round    int       `json:"round"`
	TeamID   string    `json:"teamId"`
	PlayerID string    `json:"playerId"`
	MadeAt   time.Time `json:"madeAt"`
}

// Member binds a Google identity to a league seat.
type Member struct {
	TeamID string `json:"teamId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

// PersistedState is intentionally small and can later be replaced by a DB adapter.
type PersistedState struct {
	Ready   map[string]bool     `json:"ready"`
	Picks   []DraftPick         `json:"picks"`
	Members map[string]Member   `json:"members"`
	Invites []string            `json:"invites"`
	Boards  map[string][]string `json:"boards"`
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
// the UI renders unclaimed seats explicitly.
func defaultTeams() []Team {
	return []Team{
		{ID: "team-1", Name: "Fourth & Longing", Abbreviation: "F&L", Record: "0–0", Rank: 1, Streak: "—", Tone: "lime"},
		{ID: "team-2", Name: "Dial-Up Defense", Abbreviation: "DUD", Record: "0–0", Rank: 2, Streak: "—", Tone: "magenta"},
		{ID: "team-3", Name: "End Zone Empire", Abbreviation: "EZE", Record: "0–0", Rank: 3, Streak: "—", Tone: "cyan"},
		{ID: "team-4", Name: "Blitz Protocol", Abbreviation: "BLZ", Record: "0–0", Rank: 4, Streak: "—", Tone: "orange"},
		{ID: "team-5", Name: "VHS Victory", Abbreviation: "VHS", Record: "0–0", Rank: 5, Streak: "—", Tone: "violet"},
		{ID: "team-6", Name: "Pixel Punters", Abbreviation: "PXL", Record: "0–0", Rank: 6, Streak: "—", Tone: "blue"},
		{ID: "team-7", Name: "Neon Audible", Abbreviation: "NEO", Record: "0–0", Rank: 7, Streak: "—", Tone: "pink"},
		{ID: "team-8", Name: "Sunday Service", Abbreviation: "SUN", Record: "0–0", Rank: 8, Streak: "—", Tone: "gold"},
	}
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

func defaultRoster() []Player {
	return []Player{
		{ID: "r-01", GSISID: "00-0034796", Name: "Lamar Jackson", Position: "QB", NFLTeam: "BAL", Opponent: "vs PIT", Projection: 22.6, Points: 24.1, Status: "Final", News: "Demo roster fixture."},
		{ID: "r-02", GSISID: "00-0038542", Name: "Bijan Robinson", Position: "RB", NFLTeam: "ATL", Opponent: "vs TB", Projection: 19.7, Points: 16.8, Status: "Q3 08:14", News: "Nine touches since halftime."},
		{ID: "r-03", GSISID: "00-0039139", Name: "Jahmyr Gibbs", Position: "RB", NFLTeam: "DET", Opponent: "vs CHI", Projection: 18.9, Points: 21.2, Status: "Q4 11:02", News: "Touchdown on the last drive."},
		{ID: "r-04", GSISID: "00-0036900", Name: "Ja'Marr Chase", Position: "WR", NFLTeam: "CIN", Opponent: "vs CLE", Projection: 20.8, Points: 13.4, Status: "Q3 02:30", News: "Seven targets through three quarters."},
		{ID: "r-05", GSISID: "00-0039075", Name: "Puka Nacua", Position: "WR", NFLTeam: "LAR", Opponent: "@ SF", Projection: 17.2, Points: 18.6, Status: "Final", News: "Demo roster fixture."},
		{ID: "r-06", GSISID: "00-0039338", Name: "Brock Bowers", Position: "TE", NFLTeam: "LV", Opponent: "@ DEN", Projection: 15.9, Points: 9.7, Status: "Q2 00:42", News: "Four catches on five targets."},
		{ID: "r-07", GSISID: "00-0036358", Name: "CeeDee Lamb", Position: "FLEX", NFLTeam: "DAL", Opponent: "@ PHI", Projection: 18.4, Points: 0, Status: "SNF", News: "Kickoff tonight."},
		{ID: "r-08", Name: "Steelers", Position: "D/ST", NFLTeam: "PIT", Opponent: "@ BAL", Projection: 7.8, Points: 8.0, Status: "Q4 04:19", News: "Two sacks and one takeaway."},
	}
}

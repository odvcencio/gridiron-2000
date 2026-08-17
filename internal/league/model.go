package league

import "time"

// DraftRounds, DefaultDraftAt, DefaultDraftTZ, and DefaultSeasonStartAt are
// config-derived (productization spec section 3.4). Each starts at
// DefaultConfig()'s neutral value and is mutated exactly once, at boot, by
// applyActiveConfig — the same "package var set once from config" shape
// lineup.go's ActiveRosterPreset already uses. Every existing bare-name
// call site (DraftRounds, DefaultDraftAt, ...) keeps compiling unchanged;
// only the value now tracks the active league.json (or the neutral
// default when none loads). Tests that never call Default() never trigger
// the mutation, so they keep seeing the neutral defaults these vars start
// at, package-wide, for the whole test binary.
var (
	// DraftRounds caps the snake draft; the room completes when every team
	// holds this many picks. It must equal the active roster shape's total
	// (roster-ops spec section 10's slot-count/draft-round equality rule);
	// LoadConfig's validator enforces that at boot. The neutral default
	// (15) matches the standard preset; the reference deployment's
	// league.json sets 17 to match gridiron-house.
	DraftRounds = DefaultConfig().Rounds

	DefaultDraftAt       = placeholderDraftAt
	DefaultDraftTZ       = defaultConfigTimezone
	DefaultRefreshPeriod = 60 * time.Second
	DefaultSeasonStartAt = placeholderSeasonStartAt
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
	// Rookie marks a player Tank01's raw player list flags as a rookie
	// (fantasy.Player.Exp == "R", verified live 2026-08-16). main.go's
	// fantasyPlayerSource sets it; Preseason Blitz uses it to keep
	// first-year players near the top of its zero-pre1 group instead of
	// buried behind established names (owner directive, 2026-08-16 —
	// blitz.go's blitzEligiblePlayers). Every other page ignores it today.
	Rookie bool `json:"rookie,omitempty"`
}

// BlitzEntry is one member's 5-player slate entry for Preseason Blitz
// (WP-B1). Players is add order, not display order; lock and reveal state
// resolve at read time in the service layer against the live schedule (see
// blitz.go), never stored here.
type BlitzEntry struct {
	Players   []string  `json:"players"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	// SchemaVersion is the state file's schema generation (section 6.3 of
	// the competition-formats spec). A missing field decodes as 0 and means
	// "version 1" (every file written before this field existed). This
	// spec's additions — Schedule and Playoffs so far — land as version 2.
	// Store.load migrates forward only: new fields fill with their zero
	// value (nil maps and pointers included), exactly the existing
	// nil-map-guard pattern below, and the version is stamped current on
	// load. A file whose version exceeds currentSchemaVersion refuses to
	// load rather than silently drop data; see Store.load.
	SchemaVersion int                 `json:"schemaVersion"`
	Ready         map[string]bool     `json:"ready"`
	Picks         []DraftPick         `json:"picks"`
	Members       map[string]Member   `json:"members"`
	Invites       []string            `json:"invites"`
	Boards        map[string][]string `json:"boards"`
	TeamNames     map[string]string   `json:"teamNames"`
	DraftOrder    []string            `json:"draftOrder"`
	Scoring       map[string]float64  `json:"scoring"`
	// Pickems maps owner email to game ID to the picked team abbreviation.
	Pickems map[string]map[string]string `json:"pickems"`
	// BlitzEntries maps owner email to slate ID ("pre2" | "pre3") to that
	// owner's Preseason Blitz entry (WP-B1). Additive under schema version
	// 2 — the Pickems precedent (F10): a nil map decodes safely on an old
	// file, and the store normalizes it in load/NewStore/cloneState.
	BlitzEntries map[string]map[string]BlitzEntry `json:"blitzEntries,omitempty"`

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

	// SentLog maps a notification idempotency key (see the notification
	// catalog) to the UTC instant the send was committed. It is written
	// before the transport call, so a restart never double-sends. Old
	// state files decode with a nil map; the store normalizes it to an
	// empty one, matching the clock-field precedent above.
	SentLog map[string]time.Time `json:"sentLog,omitempty"`
	// NotifyPrefs maps member email to category to enabled. An absent
	// entry means the category default applies (league.json, then the
	// catalog default); only overrides are stored.
	NotifyPrefs map[string]map[string]bool `json:"notifyPrefs,omitempty"`
	// ScoringChangedAt is the instant of the last scoring edit. The
	// notifier ticker coalesces edits into one rules-change email 15
	// minutes after the edits go quiet. Zero means no edit is pending.
	// finding nit 5: "omitempty" never omits a time.Time (it is a struct,
	// never the empty value omitempty checks for), so every state file
	// carried a zero ScoringChangedAt regardless. go.mod's Go directive is
	// 1.26 (>= 1.24), so "omitzero" — which calls time.Time's IsZero()
	// method — actually omits it.
	ScoringChangedAt time.Time `json:"scoringChangedAt,omitzero"`

	// Schedule is the persisted regular-season plan plus results (see
	// schedule.go). nil means no schedule has been generated yet; the live
	// feed falls back to the honest preseason snapshot until it exists
	// (feed.go's scheduleProvider).
	Schedule *SeasonSchedule `json:"schedule,omitempty"`
	// Playoffs is the persisted bracket state (see playoffs.go). nil means
	// the bracket has not been seeded yet.
	Playoffs *PlayoffState `json:"playoffs,omitempty"`
	// Phase is the season lifecycle marker (see season.go). Empty means
	// "before the regular season" — no schedule yet, or the schedule
	// exists but SEASON_START_AT has not passed. Section 5.2 of the
	// competition-formats spec defines the full multi-season lifecycle;
	// this field only carries the phases season.go actually drives in this
	// work package: PhaseRegularSeason, PhasePlayoffs, PhaseSeasonComplete.
	Phase string `json:"phase,omitempty"`

	// BadgeClaims maps team ID to the badge motif slug it currently holds
	// (see badge.go's BadgeMotifs catalog). A motif claimed by one team is
	// blocked for every other team; an absent entry means the team shows
	// its tone default badge (or the text mark) instead. Additive under
	// schema version 2 — the BlitzEntries precedent above: a nil map
	// decodes safely on an old file, and the store normalizes it in
	// load/NewStore/cloneState.
	BadgeClaims map[string]string `json:"badgeClaims,omitempty"`
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

// activeTeams backs defaultTeams(): the currently active league's team
// list, seeded from DefaultConfig()'s neutral teams and mutated exactly
// once, at boot, by applyActiveConfig (see model.go's var block doc
// comment). Order matters for the snake draft's default order (see
// defaultTeamIDs) and for divisionMaps' first-seen division order.
var activeTeams = teamsFromSeeds(DefaultConfig().Teams)

// teamsFromSeeds converts config.TeamSeed entries into the runtime Team
// shape: Manager, Record, and Streak stay empty until a real member claims
// the seat and games are played (the UI renders unclaimed seats
// explicitly); Rank is the slice position + 1, exactly as the old literal
// table encoded it (config.go's field-rules doc, spec section 3.2). A seed
// with an empty Tone auto-assigns from the eight-color palette cycle in
// slice order.
func teamsFromSeeds(seeds []TeamSeed) []Team {
	palette := []string{"cyan", "blue", "violet", "lime", "orange", "gold", "magenta", "pink"}
	out := make([]Team, 0, len(seeds))
	for index, seed := range seeds {
		tone := seed.Tone
		if tone == "" {
			tone = palette[index%len(palette)]
		}
		out = append(out, Team{
			ID:           seed.ID,
			Name:         seed.Name,
			Abbreviation: seed.Abbreviation,
			Division:     seed.Division,
			Record:       "0–0",
			Rank:         index + 1,
			Streak:       "—",
			Tone:         tone,
		})
	}
	return out
}

// defaultTeams returns the active league's team list: every subsystem that
// needs "the league's teams" (seat claiming, division split, round math,
// schedule generation) reads a []Team / []string derived from this slice,
// never a bare literal team count or division name (competition-formats
// spec section 1.3). Matchup pairing does NOT index into this slice; it
// reads the persisted SeasonSchedule (schedule.go).
//
// The name is historical (it once returned one hardcoded reference
// league); it now returns whichever config is active — see activeTeams
// and applyActiveConfig in config.go.
func defaultTeams() []Team {
	return activeTeams
}

// defaultTeamIDs returns the active league's team IDs in their configured
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

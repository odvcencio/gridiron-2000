package league

import (
	"sync"
	"time"
)

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

// DefaultMatchupClockLabel is the broadcast-delay disclaimer every matchup
// card's clock field shows: ScoreMatchup.Clock carries no real per-game
// game-clock source (design note: the league provider integration section
// 2.5 wires scores, not play clocks), so both the initial server render
// (matchupMaps) and the live-bind poll payload (LiveScoresView) show this
// fixed label rather than an empty cell.
const DefaultMatchupClockLabel = "60 SEC"

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
	// blitz.go's blitzEligiblePlayers).
	Rookie bool `json:"rookie,omitempty"`
	// DraftCapital is the rookie's real NFL draft slot, pre-rendered as the
	// compact chip text a pool row shows (for example "R1 · P8") —
	// main.go's fantasyPlayerSource sets it from
	// fantasy.Player.DraftCapitalLabel(). Empty for anyone who is not this
	// year's rookie class, or for an undrafted rookie Tank01 reports no
	// draft slot for. Display-only: the default pool ORDER a rookie with
	// draft capital receives is decided upstream, in internal/fantasy's
	// mergePool, before this field is ever read — this field never
	// reorders a row on its own (owner directive, 2026-08-18 — "show the
	// reasoning," the same principle Preseason Blitz's pre1 evidence line
	// already follows).
	DraftCapital string `json:"draftCapital,omitempty"`
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
	ID                string
	Week              int
	Kickoff           time.Time
	Away              string // team abbreviation
	Home              string
	AwayScore         int
	HomeScore         int
	Final             bool
	ScoresPresent     bool      // both authoritative scores exist; ATS never grades placeholder zeroes
	SpreadLineTenths  int       // nflverse/PFR convention: positive means home favored
	SpreadLinePresent bool      // distinguishes a true pick'em line of 0 from no line
	SourceObservedAt  time.Time // when this source snapshot was checked
	SourceUpdatedAt   time.Time // when the cached snapshot last changed
	SourceURL         string
	SourceProvenance  string // provider plus immutable content identity when available
}

// PickemMarket is the durable contest line for one NFL game. Before LockAt,
// LineTenths tracks the newest eligible market observation. Once Frozen or
// Void is set the record is immutable, so a restart or a late feed refresh
// cannot reprice a contest that has already locked.
type PickemMarket struct {
	Week             int       `json:"week"`
	Kickoff          time.Time `json:"kickoff"`
	Away             string    `json:"away"`
	Home             string    `json:"home"`
	LockAt           time.Time `json:"lockAt"`
	LineTenths       int       `json:"lineTenths"`
	LinePresent      bool      `json:"linePresent"`
	ObservedAt       time.Time `json:"observedAt"`
	SourceUpdatedAt  time.Time `json:"sourceUpdatedAt"`
	SourceURL        string    `json:"sourceUrl,omitempty"`
	SourceProvenance string    `json:"sourceProvenance,omitempty"`
	Frozen           bool      `json:"frozen,omitempty"`
	FrozenAt         time.Time `json:"frozenAt,omitempty"`
	Void             bool      `json:"void,omitempty"`
	VoidReason       string    `json:"voidReason,omitempty"`
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
	// Role marks a co-manager: "" (the zero value) means the seat's
	// primary manager, "co" means a co-manager sharing the same TeamID
	// (registration wave, build item 4). A seat holds at most one primary
	// and one co Member at a time — Store.InviteCoManager enforces the
	// one-co-per-seat limit. Every place that reads "the" manager of a
	// team (teamView's Manager display, memberForTeam) prefers Role == ""
	// so a co-manager never displaces the primary's name; teamMembers
	// (service.go) is the one place that reads both.
	Role string `json:"role,omitempty"`
}

// PersistedState is intentionally small and can later be replaced by a DB adapter.
type PersistedState struct {
	// persistenceAuthority is a SQLite-only boot handoff marker. It is kept
	// out of the legacy JSON representation deliberately: the marker is
	// committed with an imported state in the database, and its presence is
	// what lets a restart distinguish an authoritative import from a database
	// that was only created before the import transaction committed.
	persistenceAuthority string
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
	// DraftStarted is the authoritative lifecycle gate. DraftAt is only the
	// announced window; no pick or clock transition is legal until the
	// commissioner explicitly opens the room.
	DraftStarted   bool      `json:"draftStarted,omitempty"`
	DraftStartedAt time.Time `json:"draftStartedAt,omitempty"`
	// Pickems maps owner email to game ID to the picked team abbreviation.
	Pickems map[string]map[string]string `json:"pickems"`
	// PickemMarkets maps game ID to its moving candidate or immutable frozen
	// against-the-spread line. It is league schedule state, not member state.
	PickemMarkets map[string]PickemMarket `json:"pickemMarkets,omitempty"`
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

	// AvatarRefs maps team IDs to immutable content-addressed PNG references.
	// The map is the only authoritative custom-image identity: the serving
	// route resolves a team to this reference and never trusts a team-named
	// file on disk. A missing entry means the team uses its badge/default/text
	// fallback. Additive under schema version 3; old state files decode with a
	// nil map and normalize to an empty map on load.
	AvatarRefs map[string]string `json:"avatarRefs,omitempty"`

	// RosterOverride is the commissioner's runtime roster-shape override
	// (roster-shape-editor spec), or nil when the league runs the
	// config-resolved default (ActiveRosterPreset/DraftRounds) unmodified.
	// Additive under schema version 2 — the BadgeClaims precedent above: a
	// nil pointer decodes safely on an old file and needs no migration.
	// Store.SetRosterOverride/ClearRosterOverride are the only writers, and
	// both reject once len(Picks) > 0 (mirrors SetDraftOrder's lock).
	RosterOverride *RosterOverride `json:"rosterOverride,omitempty"`

	// Announcements is the league's commissioner-posted announcement feed,
	// newest first, capped at 20 entries (Store.PostAnnouncement drops the
	// oldest past the cap). Additive under schema version 2 — the
	// BadgeClaims precedent above: a nil slice decodes safely on an old
	// file, and the store normalizes it in load/NewStore/cloneState.
	Announcements []Announcement `json:"announcements,omitempty"`

	// Lineups maps team ID -> NFL week -> slot ID -> player ID (roster-ops
	// spec section 3.1). Only explicit starter assignments are stored; the
	// bench is the roster remainder, and an unassigned slot derives at read
	// time (lineup.go's effectiveLineup — see that function's doc comment
	// for how this work package reconciles the spec's per-week base with
	// its own worked example). Additive under schema version 2 — the
	// BadgeClaims precedent above: a nil map decodes safely on an old file,
	// and the store normalizes it in load/NewStore/cloneState.
	Lineups map[string]map[int]map[string]string `json:"lineups,omitempty"`

	// Transactions is the append-only roster-mutation log (roster-ops spec
	// section 7.1): one entry per add/drop move (WP-R3), with claim
	// (WP-R4) and trade (WP-R5) entry types slotting into the same slice
	// later. Rosters derive by replaying Picks then Transactions in order
	// (see roster.go's currentRosters) — the log is the only roster-effect
	// write; there is no separate stored roster to fall out of sync
	// (section 3's derive-never-store rule). Additive under schema version
	// 2 — the BadgeClaims precedent above: a nil slice decodes safely on
	// an old file, and the store normalizes it in load/NewStore/cloneState.
	Transactions []Transaction `json:"transactions,omitempty"`

	// WaiverClaims holds every open, unprocessed waiver claim (roster-ops
	// spec section 3.1, 5.3). Store.ProcessWaivers removes a claim the
	// instant it resolves — won, beaten, or failed — so this slice only
	// ever names claims still waiting on a future run. Additive under
	// schema version 2 — the Transactions precedent above: a nil slice
	// decodes safely on an old file, and the store normalizes it in
	// load/NewStore/cloneState.
	WaiverClaims []WaiverClaim `json:"waiverClaims,omitempty"`

	// WaiversProcessedThrough is the instant of the last completed waiver
	// run (roster-ops spec section 3.1, 5.4). Zero means no run has ever
	// completed; Service.rosterOpsTick's first tick sets it to now without
	// processing anything, so a fresh or migrated state never replays the
	// past. Additive under schema version 2, "omitzero" (not
	// "omitempty" — see ScoringChangedAt's doc comment above for why
	// omitempty never omits a time.Time).
	WaiversProcessedThrough time.Time `json:"waiversProcessedThrough,omitzero"`

	// TradeOffers holds every trade offer ever proposed, with its section
	// 6.1 lifecycle status — open, terminal (declined/withdrawn/countered/
	// vetoed/expired/failed), or executed. Nothing is removed: a resolved
	// offer stays for the /trades INBOX/OUTBOX history and, for an
	// executed offer, as the OfferID a Transaction's provenance points
	// back to. Additive under schema version 2 — the WaiverClaims
	// precedent above: a nil slice decodes safely on an old file, and the
	// store normalizes it in load/NewStore/cloneState.
	TradeOffers []TradeOffer `json:"tradeOffers,omitempty"`

	// RosterZones maps team ID to player ID to that player's roster-zone
	// placement (RESERVE or IR — roster-ops SK spec, zones.go). It is an
	// overlay atop the derived roster (currentRosters/Picks+Transactions):
	// placing or activating a zone occupant never touches ownership, only
	// where the player sits. Additive under schema version 2 — the
	// WaiverClaims precedent above: a nil map decodes safely on an old
	// file, and the store normalizes it in load/NewStore/cloneState.
	RosterZones map[string]map[string]ZoneAssignment `json:"rosterZones,omitempty"`
	// CoInvites maps a pending co-manager's email to the team seat they
	// will bind to on their first sign-in (registration wave, build item
	// 4): Store.InviteCoManager records the entry, and the sign-in path
	// (BindCoManager) consumes it, exactly once, into a Role "co" Member.
	// Additive under schema version 2 — the WaiverClaims precedent above:
	// a nil map decodes safely on an old file, and the store normalizes
	// it in load/NewStore/cloneState.
	CoInvites map[string]string `json:"coInvites,omitempty"`

	// TrimmedTeamIDs holds the team IDs a commissioner's seat trim (SK
	// unclaimed-seat spec, Store.TrimUnclaimedSeats) removed as unclaimed.
	// Empty means no trim has run: defaultTeams() returns the full
	// config-seeded list. Once set, Default()'s boot restore recomputes the
	// kept list from activeTeams minus this set and calls applySeatTrim, so
	// a redeploy never un-trims the league — but a *kept* team's own
	// cosmetic fields (name, tone) still track a league.json edit, since
	// only team-ID membership is durable here, not a frozen team snapshot.
	// Additive under schema version 2 — the WaiverClaims precedent above: a
	// nil slice decodes safely on an old file, and the store normalizes it
	// in load/NewStore/cloneState.
	TrimmedTeamIDs []string `json:"trimmedTeamIds,omitempty"`
}

// Announcement is one commissioner-posted league announcement (league-
// announcements spec). ID is a short content hash, stable for delete and
// for the N11 email's idempotency key (keyBroadcast, notifications.go).
type Announcement struct {
	ID       string    `json:"id"`
	Body     string    `json:"body"`
	PostedAt time.Time `json:"postedAt"`
	PostedBy string    `json:"postedBy"`
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
	State  string    `json:"state"`
	Status string    `json:"status"`
	Clock  string    `json:"clock"`
}

const (
	MatchupStatePreseason  = "preseason"
	MatchupStateScheduled  = "scheduled"
	MatchupStateInProgress = "in_progress"
	MatchupStateFinal      = "final"
	MatchupStateDegraded   = "degraded"
)

// LiveSnapshot is the stable JSON contract consumed by the score enhancer.
type LiveSnapshot struct {
	OK                  bool           `json:"ok"`
	Source              string         `json:"source"`
	SourceLabel         string         `json:"sourceLabel"`
	Week                int            `json:"week"`
	WeekLabel           string         `json:"weekLabel"`
	State               string         `json:"state"`
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

// teamRuntimeMu guards the commissioner's seat trim (SK unclaimed-seat
// spec: "basically whatever seats are filled by an hour before draft
// time"). Mirrors rosterRuntimeMu/runtimeRoster's "config baseline, runtime
// override" shape (lineup.go): activeTeams stays the full config-seeded
// list — the trim never rewrites it — and defaultTeams() prefers the
// trimmed list once one is applied. Set by applySeatTrim (Default()'s
// boot-restore of a persisted trim, and Service.TrimUnclaimedSeats after
// Store.TrimUnclaimedSeats succeeds); no test calls it, so every test
// Service literal keeps seeing the full config-seeded list for the life of
// the test binary — the same guarantee applyActiveConfig's doc comment
// pins for the roster override.
var (
	teamRuntimeMu   sync.RWMutex
	runtimeTeams    []Team
	runtimeTeamsSet bool
)

// applySeatTrim installs kept as the runtime team-list override: every
// defaultTeams() read — package-level call sites (draftclock.go, roster.go,
// store.go, notifications.go) and every Service method, which all read
// defaultTeams() directly rather than a per-instance field — reflects it
// immediately.
func applySeatTrim(kept []Team) {
	teamRuntimeMu.Lock()
	runtimeTeams = append([]Team(nil), kept...)
	runtimeTeamsSet = true
	teamRuntimeMu.Unlock()
}

// clearSeatTrim reverts defaultTeams() to the config baseline (activeTeams).
// Production never calls this — a seat trim is one-way, matching the
// owner's "lock to whatever is filled" model — kept for test cleanup
// symmetry with clearRosterShape (lineup.go).
func clearSeatTrim() {
	teamRuntimeMu.Lock()
	runtimeTeamsSet = false
	teamRuntimeMu.Unlock()
}

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
// and applyActiveConfig in config.go — or, once a commissioner's seat trim
// has run (SK unclaimed-seat spec, applySeatTrim above), the trimmed list.
func defaultTeams() []Team {
	teamRuntimeMu.RLock()
	defer teamRuntimeMu.RUnlock()
	if runtimeTeamsSet {
		return runtimeTeams
	}
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

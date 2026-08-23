package fantasy

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// SchemaVersion guards the on-disk pool cache format.
	SchemaVersion = 3
	// DefaultHost is the Tank01 NFL API host on RapidAPI. Point TANK01_HOST at
	// another Tank01 sport host (NBA, MLB, NHL) to reuse this client later.
	DefaultHost = "tank01-nfl-live-in-game-real-time-statistics-nfl.p.rapidapi.com"
)

// Player is one draftable entry in the normalized fantasy pool.
type Player struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Position   string             `json:"position"`
	NFLTeam    string             `json:"nflTeam"`
	Jersey     string             `json:"jersey,omitempty"`
	ByeWeek    int                `json:"byeWeek,omitempty"`
	ADP        float64            `json:"adp,omitempty"`
	ADPRank    int                `json:"adpRank,omitempty"`
	Projection float64            `json:"projection,omitempty"`
	ProjStats  map[string]float64 `json:"projStats,omitempty"`
	Injury     string             `json:"injury,omitempty"`
	News       string             `json:"news,omitempty"`
	Headshot   string             `json:"headshot,omitempty"`
	// Exp is Tank01's raw career-experience field from getNFLPlayerList:
	// "R" for a rookie, or the season count as a string ("1", "12", ...)
	// for a veteran. An empty string means Tank01 did not report it for
	// this player. Verified live 2026-08-16: 976 of 4271 listed players
	// carried "R" that day. See IsRookie.
	Exp string `json:"exp,omitempty"`
	// DraftRound and DraftPick are the player's NFL draft slot, read
	// straight off Tank01's own getNFLPlayerList response
	// (parsePlayerList's draftInfo.round / draftInfo.pick in tank01.go) —
	// not a second data source, and not a computed value. Pick is the
	// OVERALL pick number, not a within-round index: verified live
	// 2026-08-18 against getNFLPlayerList and cross-checked against
	// nflverse's draft_picks release (round 2 begins at pick 33, not
	// pick 1). Both are present for most drafted players, rookie and
	// veteran alike, since Tank01 keeps a player's original draft slot on
	// his record for his whole career — DraftCapital is the one place
	// that decides when this is a meaningful "presumed usage" signal for
	// THIS season versus a stale artifact of a past draft class.
	DraftRound int `json:"draftRound,omitempty"`
	DraftPick  int `json:"draftPick,omitempty"`
}

// IsRookie reports whether Tank01's raw player list marked p a rookie:
// Exp == "R" (verified live via getNFLPlayerList, 2026-08-16). A blank Exp
// means Tank01 did not report it — this reports false for that case too,
// honestly, rather than guessing.
func (p Player) IsRookie() bool {
	return strings.EqualFold(strings.TrimSpace(p.Exp), "R")
}

// DraftCapital reports p's NFL draft slot as an overall pick number, but
// only when it is a meaningful "presumed usage" signal for the CURRENT
// season: p must be this year's rookie class (IsRookie) and Tank01 must
// have reported a real pick (DraftPick > 0). A veteran's DraftRound /
// DraftPick, when Tank01 reports one, reflects a past draft class and is
// not usage evidence for this season, so ok is false for every non-rookie.
// An undrafted rookie (DraftPick == 0 — Tank01 reports no draftInfo for a
// UDFA) also reports ok == false, honestly, rather than guessing a slot
// that does not exist. mergePool's tiebreak and DraftCapitalLabel both
// call this so ranking and display can never disagree.
func (p Player) DraftCapital() (overallPick int, ok bool) {
	if !p.IsRookie() || p.DraftPick <= 0 {
		return 0, false
	}
	return p.DraftPick, true
}

// DraftCapitalLabel renders p's draft slot as the compact chip a pool row
// shows (for example "R1 · P8") so a manager can see, at a glance, why a
// stats-less rookie ranks where he does — the same "show the reasoning"
// principle Preseason Blitz's pre1 evidence line already follows
// (internal/league/blitz.go). It returns "" whenever DraftCapital reports
// no usable slot; every caller (playerMap, and every row template) treats
// an empty label as "render nothing," never a placeholder dash.
func (p Player) DraftCapitalLabel() string {
	pick, ok := p.DraftCapital()
	if !ok {
		return ""
	}
	if p.DraftRound <= 0 {
		return fmt.Sprintf("P%d", pick)
	}
	return fmt.Sprintf("R%d · P%d", p.DraftRound, pick)
}

// Status reports pool health for /api/health, the admin console, and the
// wire page.
type Status struct {
	Enabled   bool           `json:"enabled"`
	Provider  string         `json:"provider"`
	Mode      string         `json:"mode"`  // source provenance: live | cache | stale | offline
	State     string         `json:"state"` // manager truth: live | cached | stale | degraded | offline | unavailable
	Scoring   string         `json:"scoring"`
	Players   int            `json:"players"`
	PoolLimit int            `json:"poolLimit"`
	Positions map[string]int `json:"positions,omitempty"`
	WithADP   int            `json:"withAdp"`
	WithProj  int            `json:"withProjection"`
	WithBye   int            `json:"withBye"`
	Requests  int            `json:"requestsUsed"`
	LastSync  time.Time      `json:"lastSync,omitzero"`
	Age       time.Duration  `json:"-"`
	FreshFor  time.Duration  `json:"-"`
	LastError string         `json:"lastError,omitempty"`
}

// Config controls the sync service. Zero values fall back to safe defaults.
type Config struct {
	APIKey        string
	Host          string
	BaseURL       string
	Root          string
	Season        int
	SyncInterval  time.Duration
	ScoringFormat string // half_ppr | ppr | standard
	PoolLimit     int
	MaxBodyBytes  int64
	HTTPClient    *http.Client
	Now           func() time.Time
}

// DefaultPoolLimitFallback is FANTASY_POOL_LIMIT's fallback of last
// resort: used only when the caller has no league size to scale from (for
// example a direct NewService call in a test). main.go's normal boot path
// always passes a size-scaled default through ConfigFromEnv instead.
const DefaultPoolLimitFallback = 400

// ConfigFromEnv reads the TANK01_* and fantasy pool environment keys.
// defaultPoolLimit is FANTASY_POOL_LIMIT's value when the env var is
// unset: main.go computes it from the active league's team count and
// roster size (productization wave, owner decision) instead of a flat
// constant, so a 12-team deep-league config gets a deeper pool than an
// 8-team one without an operator having to tune FANTASY_POOL_LIMIT by
// hand. Pass <= 0 to fall back to DefaultPoolLimitFallback (tests, or a
// caller with no league size on hand yet).
func ConfigFromEnv(defaultPoolLimit int) Config {
	if defaultPoolLimit <= 0 {
		defaultPoolLimit = DefaultPoolLimitFallback
	}
	season := envInt("NFL_SEASON", time.Now().Year())
	return Config{
		APIKey: strings.TrimSpace(os.Getenv("TANK01_API_KEY")),
		Host:   envString("TANK01_HOST", DefaultHost),
		// BaseURL, when set, points every Tank01 request at a shared
		// caching relay (cmd/statrelay) instead of RapidAPI directly —
		// see tank01.go's get() for how it takes precedence over Host.
		// A trailing slash is trimmed so "base + \"/\" + endpoint" never
		// doubles up, matching how a Kubernetes Service DNS name is
		// typically written with no trailing slash but an operator could
		// paste one in by habit.
		BaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("TANK01_BASE_URL")), "/"),
		Root:          envString("FANTASY_ROOT", "data/fantasy"),
		Season:        season,
		SyncInterval:  envDuration("FANTASY_SYNC_INTERVAL", 6*time.Hour),
		ScoringFormat: normalizeScoring(envString("SCORING_FORMAT", "half_ppr")),
		PoolLimit:     envInt("FANTASY_POOL_LIMIT", defaultPoolLimit),
		MaxBodyBytes:  int64(envInt("FANTASY_MAX_DOWNLOAD_MB", 32)) << 20,
	}
}

// ScaledPoolLimit derives FANTASY_POOL_LIMIT's default from league shape
// instead of a flat constant (owner decision, productization wave): teams
// × rosterSpots is the number of players a full league could ever roster;
// a 2.5x headroom factor keeps free agency and waivers meaningful (the
// roster-ops spec's waiver/FA design assumes a pool several times deeper
// than what is actually rostered, competition-formats' honest-coverage
// precedent for "give the wire something to search"). Clamped to
// [200, 800]: 200 keeps even a 4-team, 15-round league (60 rostered) well
// stocked without a silly-small pool; 800 caps memory/sync cost for large
// configs (14 teams × 17 rounds × 2.5 ≈ 595, comfortably under the cap) —
// Tank01's real-world pool tops out under 800 startable-relevant players
// regardless, so the cap is a safety rail, not an expected ceiling.
func ScaledPoolLimit(teams, rosterSpots int) int {
	if teams <= 0 || rosterSpots <= 0 {
		return DefaultPoolLimitFallback
	}
	const headroom = 2.5
	limit := int(float64(teams*rosterSpots) * headroom)
	switch {
	case limit < 200:
		return 200
	case limit > 800:
		return 800
	default:
		return limit
	}
}

func normalizeScoring(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ppr", "full_ppr":
		return "ppr"
	case "standard", "std":
		return "standard"
	default:
		return "half_ppr"
	}
}

// adpType maps the scoring format to the Tank01 adpType parameter.
func adpType(scoring string) string {
	switch scoring {
	case "ppr":
		return "PPR"
	case "standard":
		return "standard"
	default:
		return "halfPPR"
	}
}

// pointsPerReception maps the scoring format to the projection knob.
func pointsPerReception(scoring string) string {
	switch scoring {
	case "ppr":
		return "1.0"
	case "standard":
		return "0.0"
	default:
		return "0.5"
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

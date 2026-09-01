// Config, the loader, and validation for league.json (oss-productization
// spec section 3). This file is the identity surface the rest of the
// package reads instead of hardcoding one league's flavor: wordmark,
// teams/divisions, draft date, scoring format, and the roster-ops config
// blocks (roster/waivers/trades, roster-ops spec section 10).
//
// Shipped defaults are deliberately neutral (owner decision, productization
// wave): a fresh checkout with no league.json boots a generic, clearly
// fake reference league, never this project's own reference deployment.
// The reference deployment's real identity (name, divisions, draft date,
// invite copy) lives only in a deployment-side league.json — see
// config/league.json.example and deploy/README for the split.
package league

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ConfigSchemaVersion is the only league.json "version" this binary
// accepts. A file naming a higher version fails to load with a named
// migration message (spec section 6.2); this binary reads version 1
// forever.
const ConfigSchemaVersion = 1

// placeholderDraftAt and placeholderSeasonStartAt are the neutral shipped
// config's "clearly fake" instants (owner decision): far enough in the
// future that nobody mistakes them for a real schedule, but still valid
// RFC3339 so every date-math path (countdowns, locks) keeps working on an
// unconfigured checkout.
const (
	placeholderDraftAt       = "2099-01-01T00:00:00Z"
	placeholderSeasonStartAt = "2099-01-08T00:00:00Z"
	defaultConfigTimezone    = "America/New_York"
	defaultConfigURL         = "http://localhost:8080"
	minTeams, maxTeams       = 4, 14 // owner decision: the engine's validated range (do not confuse with the spec's draft 4-16)
	teamCountWarnAbove       = 12
)

// TeamSeed is one operator-configured team identity: league.json's
// "teams[]" entries (spec section 3.2). ID is permanent once state
// references it (spec section 7.1); Name/Abbreviation/Division/Tone are
// display-only and may change freely.
type TeamSeed struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
	Division     string `json:"division"`
	Tone         string `json:"tone"`
}

// CopyBlock carries the operator-authored copy fragments that used to be
// hardcoded flavor text: the landing-page kicker, the site/email footer
// joke, the invite email's venue line, and its opening blurb. Every field
// empty means "derive a generic line from the league shape" except
// VenueLine, whose empty value means "omit the venue row/clause entirely"
// (spec section 3.2 field rules — no invented venue for other leagues).
type CopyBlock struct {
	HeroKicker  string `json:"hero_kicker"`
	FooterLine  string `json:"footer_line"`
	VenueLine   string `json:"venue_line"`
	InviteBlurb string `json:"invite_blurb"`
}

// RosterBlock is the raw "roster" config block before preset resolution
// (roster-ops spec section 10): either "preset" names one of rosterPresets,
// or "slots"/"bench" spell an explicit shape. Setting both is a conflict
// (validateConfig catches it before resolution).
type RosterBlock struct {
	Preset string         `json:"preset,omitempty"`
	Slots  map[string]int `json:"slots,omitempty"`
	Bench  int            `json:"bench,omitempty"`
	// Reserve, IR, and Limits are the SK roster-zones spec's optional
	// additions: a position-gated reserve zone, an injury-gated IR zone,
	// and an optional per-position roster cap. They ride along with
	// either a preset or an explicit slots/bench shape (resolveRosterBlock
	// merges them onto whichever base resolves); absent means none of the
	// three, the pre-existing behavior for every config that never
	// mentions them.
	Reserve map[string]int `json:"reserve,omitempty"`
	IR      int            `json:"ir,omitempty"`
	Limits  map[string]int `json:"limits,omitempty"`
}

// WaiversBlock is the "waivers" config block (roster-ops spec section 10).
type WaiversBlock struct {
	Mode            string `json:"mode"`
	SeasonWeightPct int    `json:"season_weight_pct"`
	FAABBudget      int    `json:"faab_budget"`
	ClearDays       int    `json:"clear_days"`
	ProcessTime     string `json:"process_time"`
}

// TradesBlock is the "trades" config block (roster-ops spec section 10).
type TradesBlock struct {
	Deadline    string `json:"deadline"`
	Veto        string `json:"veto"`
	ReviewHours int    `json:"review_hours"`
}

// MembershipBlock is the "membership" config block (registration wave,
// build item 5 — the platform thesis proof: "separate setups (scoring
// rules, membership rules, etc) with the same underlying platform").
// AllowedDomain is a bare domain (no "@"); when set, a matching raw Google
// provider email is admitted and explicit invitation sources still work
// alongside it. A configured domain closes the no-source setup fallback:
// outsiders are rejected until explicitly invited. An empty AllowedDomain
// means there is no domain path; with no environment or stored invitations,
// setup is OPEN AFTER SIGN-IN, otherwise admission is invite-only.
type MembershipBlock struct {
	AllowedDomain string `json:"allowed_domain,omitempty"`
}

// Config is the resolved, in-memory shape every package reads (spec
// section 3.4). It is a flattened view of league.json's nested wire
// format (ConfigFile, below): callers never see the JSON nesting.
type Config struct {
	Version int

	Name      string
	ShortCode string
	Tagline   string
	ModeLabel string
	URL       string
	Timezone  string
	Season    int

	Teams []TeamSeed

	DraftAt time.Time
	Rounds  int
	// PickClockSeconds is draft.pick_clock_seconds: 0 means unset (the
	// caller falls back to DefaultPickClock); a nonzero value validates to
	// 10-600 (validateConfig) and is resolvePickClockDefault's config-tier
	// input, itself outranked by the PICK_CLOCK environment override.
	PickClockSeconds int
	FormatLabel      string

	SeasonStartAt time.Time
	ScoringFormat string

	Copy CopyBlock

	// RosterPresetName is the raw "roster.preset" value, "" when the
	// config spelled an explicit roster.slots/roster.bench shape instead.
	// Validation messages name it; nothing downstream needs it once Roster
	// is resolved.
	RosterPresetName string
	// RosterConflict is true when the raw file set roster.preset together
	// with roster.slots or roster.bench (roster-ops spec section 10: this
	// combination fails closed). Set during load, before resolution
	// substitutes the preset's shape into Roster.
	RosterConflict bool
	// Roster is the resolved roster shape: a preset's expansion, or the
	// config's explicit slots/bench. Every reader (lineup.go, the draft
	// engine) uses this and never re-reads RosterPresetName.
	Roster RosterPreset

	Waivers WaiversBlock
	Trades  TradesBlock
	// Postseason carries the operator's bracket rules. A zero TeamCount keeps
	// postseason disabled until a deployment opts in explicitly.
	Postseason PlayoffConfig
	// Membership is the "membership" config block's resolved form (build
	// item 5): the domain-gate rule. Absent from the flagship reference
	// config; a deployment that wants it (for example a company league
	// gating on its own email domain) sets it explicitly.
	Membership MembershipBlock

	// Auth is the "auth" config block's resolved form (owner decision,
	// setup-wizard design: auth tier toggles live in an additive
	// league.json block, not a runtime-env-only switch, so leaguecheck can
	// lint them). Absent from the version-1 schema forever — every field
	// defaults to enabled, so an existing league.json with no "auth" key
	// keeps working unchanged.
	Auth ResolvedAuth

	// Source records where this config came from: "defaults", or
	// "file:<path>". AdminData and /api/health surface it.
	Source string
}

// AuthBlock is the raw "auth" config block (owner decision, setup-wizard
// design: additive to the version-1-forever schema). Each field is a
// pointer so the loader can distinguish "not set" (defaults to enabled)
// from an operator's explicit "false". Two tiers are actionable in this
// slice: Tier 0 invite-link auth (default, zero external dependency) and
// Tier 3 Google OAuth (existing wiring, still additionally gated by
// GOOGLE_CLIENT_ID/GOOGLE_CLIENT_SECRET being present). Tier 1 (passkeys)
// and Tier 2 (email/magic-link) toggles are a later slice's addition — the
// block only ever grows new optional fields, never removes or repurposes
// one.
type AuthBlock struct {
	InviteLinks *bool `json:"invite_links,omitempty"`
	Google      *bool `json:"google,omitempty"`
}

// ResolvedAuth is AuthBlock's resolved, always-concrete form: every field
// defaults to true (enabled) when the raw block, or one of its fields, is
// absent.
type ResolvedAuth struct {
	InviteLinks bool
	Google      bool
}

// resolveAuthBlock defaults every field to true (enabled): an absent "auth"
// block, or an absent field within it, must never silently disable a tier
// nobody asked to turn off.
func resolveAuthBlock(block AuthBlock) ResolvedAuth {
	resolved := ResolvedAuth{InviteLinks: true, Google: true}
	if block.InviteLinks != nil {
		resolved.InviteLinks = *block.InviteLinks
	}
	if block.Google != nil {
		resolved.Google = *block.Google
	}
	return resolved
}

// ConfigFileLeague is ConfigFile's "league" block.
type ConfigFileLeague struct {
	Name      string `json:"name"`
	ShortCode string `json:"short_code"`
	Tagline   string `json:"tagline"`
	ModeLabel string `json:"mode_label"`
	URL       string `json:"url"`
	Timezone  string `json:"timezone"`
	Season    int    `json:"season"`
}

// ConfigFileDraft is ConfigFile's "draft" block.
type ConfigFileDraft struct {
	At               string `json:"at"`
	Rounds           int    `json:"rounds"`
	PickClockSeconds int    `json:"pick_clock_seconds"`
	FormatLabel      string `json:"format_label"`
}

// ConfigFile is league.json's wire format (spec section 3.2): nested
// blocks, JSON tags matching the schema exactly. json.Decoder's
// DisallowUnknownFields runs against this shape, so an operator's typo
// fails loudly instead of silently doing nothing (spec section 3.1).
//
// Exported (setup-wizard design section 4.2) so the first-boot wizard can
// build the exact same wire shape leaguecheck and LoadConfigBytes read —
// one canonical struct, marshaled to bytes and validated through
// LoadConfigBytes, never a second hand-maintained mirror that could drift
// from this one as fields are added.
type ConfigFile struct {
	Version int              `json:"version"`
	League  ConfigFileLeague `json:"league"`
	Teams   []TeamSeed       `json:"teams"`
	Draft   ConfigFileDraft  `json:"draft"`

	SeasonStartAt string          `json:"season_start_at"`
	ScoringFormat string          `json:"scoring_format"`
	Copy          CopyBlock       `json:"copy"`
	Roster        RosterBlock     `json:"roster"`
	Waivers       WaiversBlock    `json:"waivers"`
	Trades        TradesBlock     `json:"trades"`
	Postseason    PlayoffConfig   `json:"postseason"`
	Membership    MembershipBlock `json:"membership"`
	Auth          AuthBlock       `json:"auth,omitempty"`
}

// neutralTeams is the shipped, unconfigured checkout's team list: 8 teams,
// divisions East/West, tones cycling the existing eight-color palette. IDs
// stay the plain "team-N" shape regardless of flavor — team IDs are
// permanent once state references them (spec section 7.1) and carry no
// identity of their own.
func neutralTeams() []TeamSeed {
	tones := []string{"cyan", "blue", "violet", "lime", "orange", "gold", "magenta", "pink"}
	out := make([]TeamSeed, 0, 8)
	for i := 0; i < 8; i++ {
		division := "East"
		label := i + 1
		if i >= 4 {
			division = "West"
			label = i - 3
		}
		out = append(out, TeamSeed{
			ID:           fmt.Sprintf("team-%d", i+1),
			Name:         fmt.Sprintf("%s %d", division, label),
			Abbreviation: fmt.Sprintf("%s%d", strings.ToUpper(division[:1]), label),
			Division:     division,
			Tone:         tones[i],
		})
	}
	return out
}

// DefaultConfig returns the built-in, neutral reference league: the config
// an unconfigured checkout runs (spec section 4.3). Every value here is
// deliberately generic — no real league's name, dates, or copy — so the
// public tree ships as a clean platform. A deployment's own flavor lives
// only in its own league.json (config/league.json.example is the shipped
// template).
func DefaultConfig() Config {
	draftAt, _ := time.Parse(time.RFC3339, placeholderDraftAt)
	seasonStartAt, _ := time.Parse(time.RFC3339, placeholderSeasonStartAt)
	return Config{
		Version:          ConfigSchemaVersion,
		Name:             "THE LEAGUE",
		ShortCode:        "TL",
		Tagline:          "Fantasy Football League",
		ModeLabel:        "DYNASTY",
		URL:              defaultConfigURL,
		Timezone:         defaultConfigTimezone,
		Season:           time.Now().Year(),
		Teams:            neutralTeams(),
		DraftAt:          draftAt,
		Rounds:           rosterPresets["standard"].Total(),
		FormatLabel:      "",
		SeasonStartAt:    seasonStartAt,
		ScoringFormat:    "half_ppr",
		Copy:             CopyBlock{},
		RosterPresetName: "standard",
		Roster:           rosterPresets["standard"],
		Waivers: WaiversBlock{
			Mode: "perf-priority", SeasonWeightPct: 60, FAABBudget: 100,
			ClearDays: 2, ProcessTime: "09:00",
		},
		Trades: TradesBlock{Deadline: "", Veto: "commissioner", ReviewHours: 24},
		Auth:   ResolvedAuth{InviteLinks: true, Google: true},
		Source: "defaults",
	}
}

// LoadConfig resolves league.json per the lookup order in resolveConfigPath,
// applies the seven env overrides that have a file twin (spec section 3.3),
// and validates the result. No file found at any location returns
// DefaultConfig() with a nil error — that is the supported "unconfigured"
// state, not a failure. A file found but invalid, or an explicit
// $LEAGUE_FILE that does not exist, is a fatal error: the caller (Default)
// logs and exits rather than silently running under a config the operator
// did not intend (spec section 3.3).
func LoadConfig() (Config, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return Config{}, err
	}
	cfg := DefaultConfig()
	if path != "" {
		fileCfg, err := loadConfigFile(path)
		if err != nil {
			return Config{}, err
		}
		cfg = fileCfg
		cfg.Source = "file:" + path
	}
	cfg, warnings, err := finishConfigLoad(cfg, true)
	for _, w := range warnings {
		log.Printf("league config: %s", w)
	}
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadConfigFile reads and validates exactly path. It never performs the
// runtime config-path lookup and never reads any process environment, so an
// explicit file cannot be redirected by LEAGUE_FILE, GOSX_APP_ROOT, DATA_FILE,
// or one of the runtime's seven supported field overrides. Warnings are
// returned to the caller rather than logged.
func LoadConfigFile(path string) (Config, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, nil, fmt.Errorf("league config: %s: %w", path, err)
	}
	cfg, err := loadConfigBytes(path, raw)
	if err != nil {
		return Config{}, nil, err
	}
	cfg.Source = "file:" + path
	return finishConfigLoad(cfg, false)
}

// LoadConfigBytes reads no files and consults no process environment. It
// decodes, resolves, and validates exactly raw as an explicit league config;
// callers that obtained the bytes from a regular file can therefore validate
// and publish one immutable snapshot without a second read.
func LoadConfigBytes(path string, raw []byte) (Config, []string, error) {
	cfg, err := loadConfigBytes(path, raw)
	if err != nil {
		return Config{}, nil, err
	}
	cfg.Source = "file:" + path
	return finishConfigLoad(cfg, false)
}

// LoadConfigFileBytes is a descriptive alias for LoadConfigBytes.
func LoadConfigFileBytes(path string, raw []byte) (Config, []string, error) {
	return LoadConfigBytes(path, raw)
}

// LoadConfigFileWithEnvOverrides reads exactly path and applies the same
// seven documented field overrides as LoadConfig: APP_NAME, DRAFT_TZ,
// LEAGUE_URL, SCORING_FORMAT, DRAFT_AT, SEASON_START_AT, and NFL_SEASON.
// It deliberately does not consult the runtime path-lookup variables
// LEAGUE_FILE, GOSX_APP_ROOT, or DATA_FILE. This is used by leaguecheck, whose
// documented contract includes the field overrides while keeping --file
// authoritative.
func LoadConfigFileWithEnvOverrides(path string) (Config, []string, error) {
	cfg, err := loadConfigFile(path)
	if err != nil {
		return Config{}, nil, err
	}
	cfg.Source = "file:" + path
	return finishConfigLoad(cfg, true)
}

// finishConfigLoad applies the optional runtime field overrides and runs the
// one shared validation path. Keeping this order preserves LoadConfig's
// built-in < file < environment precedence while allowing explicit callers
// to opt out of all environment access.
func finishConfigLoad(cfg Config, withEnvOverrides bool) (Config, []string, error) {
	if withEnvOverrides {
		if err := applyEnvOverrides(&cfg); err != nil {
			return Config{}, nil, err
		}
	}
	warnings, err := validateConfig(&cfg)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, warnings, nil
}

// ConfigFileResolves reports whether a league.json resolves via the normal
// lookup order (resolveConfigPath), without reading or validating its
// contents. The setup-wizard boot state machine (DetermineBootState) uses
// this to decide SETUP vs CONFIGURED before constructing the process-wide
// Service singleton or its Store.
func ConfigFileResolves() (path string, found bool, err error) {
	path, err = resolveConfigPath()
	return path, path != "", err
}

// DataFilePath is the exported form of dataFilePath: the state-file path
// Default() resolves from DATA_FILE (or its own built-in default). The
// setup-wizard boot state machine opens its Store at this same path so a
// wizard-written database is exactly the one Default() finds after restart.
func DataFilePath() string {
	return dataFilePath()
}

// resolveConfigPath implements the spec section 3.3 lookup order: an
// explicit $LEAGUE_FILE (fatal if missing), then the app root, then
// <app root>/config, then the directory holding DATA_FILE. "" with a nil
// error means no file exists anywhere in the search — DefaultConfig()
// applies.
func resolveConfigPath() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("LEAGUE_FILE")); explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("league config: LEAGUE_FILE %q does not exist", explicit)
		}
		return explicit, nil
	}
	root := appRootDir()
	candidates := []string{
		filepath.Join(root, "league.json"),
		filepath.Join(root, "config", "league.json"),
		filepath.Join(filepath.Dir(dataFilePath()), "league.json"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", nil
}

// appRootDir mirrors main.go's server.ResolveAppRoot resolution without
// importing the gosx server package here: GOSX_APP_ROOT when set (the same
// variable server.ResolveAppRoot honors), otherwise the working directory.
func appRootDir() string {
	if root := strings.TrimSpace(os.Getenv("GOSX_APP_ROOT")); root != "" {
		return root
	}
	return "."
}

// dataFilePath mirrors Default()'s DATA_FILE resolution (service.go) so
// lookup rule 4 (spec section 3.3) can find a league.json dropped beside
// the state file without a second Docker volume mount.
func dataFilePath() string {
	if path := strings.TrimSpace(os.Getenv("DATA_FILE")); path != "" {
		return path
	}
	return filepath.Join("data", "league-state.json")
}

// loadConfigFile decodes and resolves one league.json file: strict JSON
// (DisallowUnknownFields), the wire format flattened into Config, and the
// roster block's preset resolved into a concrete RosterPreset. Validation
// happens later, in LoadConfig, after env overrides apply.
func loadConfigFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("league config: %s: %w", path, err)
	}
	return loadConfigBytes(path, raw)
}

func loadConfigBytes(path string, raw []byte) (Config, error) {
	var file ConfigFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		if field, ok := unknownFieldName(err); ok {
			return Config{}, fmt.Errorf("league config: unknown field %q", field)
		}
		return Config{}, fmt.Errorf("league config: %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("league config: %s: trailing JSON data", path)
		}
		return Config{}, fmt.Errorf("league config: %s: trailing JSON: %w", path, err)
	}

	cfg := Config{
		Version:          file.Version,
		Name:             file.League.Name,
		ShortCode:        file.League.ShortCode,
		Tagline:          file.League.Tagline,
		ModeLabel:        file.League.ModeLabel,
		URL:              file.League.URL,
		Timezone:         file.League.Timezone,
		Season:           file.League.Season,
		Teams:            file.Teams,
		Rounds:           file.Draft.Rounds,
		PickClockSeconds: file.Draft.PickClockSeconds,
		FormatLabel:      file.Draft.FormatLabel,
		ScoringFormat:    file.ScoringFormat,
		Copy:             file.Copy,
		Waivers:          file.Waivers,
		Trades:           file.Trades,
		Postseason:       file.Postseason,
		Membership:       file.Membership,
		Auth:             resolveAuthBlock(file.Auth),
	}
	// Absent waivers/trades blocks resolve to their defaults (roster-ops
	// spec section 10: "Absent blocks resolve to the defaults below"). A
	// present-but-empty Mode/Veto is how an omitted JSON object decodes,
	// since WaiversBlock/TradesBlock carry no "was this key present" bit.
	if strings.TrimSpace(file.Waivers.Mode) == "" {
		cfg.Waivers = DefaultConfig().Waivers
	}
	if strings.TrimSpace(file.Trades.Veto) == "" {
		cfg.Trades = DefaultConfig().Trades
	}
	if strings.TrimSpace(file.Draft.At) != "" {
		if parsed, err := time.Parse(time.RFC3339, file.Draft.At); err == nil {
			cfg.DraftAt = parsed
		}
	}
	if strings.TrimSpace(file.SeasonStartAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, file.SeasonStartAt); err == nil {
			cfg.SeasonStartAt = parsed
		}
	}
	cfg.RosterPresetName = file.Roster.Preset
	cfg.RosterConflict = file.Roster.Preset != "" && (len(file.Roster.Slots) > 0 || file.Roster.Bench > 0)
	rosterAbsent := file.Roster.Preset == "" && len(file.Roster.Slots) == 0 && file.Roster.Bench == 0
	if rosterAbsent {
		// spec (roster-ops section 10): "an absent roster block resolves
		// to gridiron-house" — validate it exactly as if the operator had
		// named the preset, so a mismatched draft.rounds still fails with
		// the preset-shaped message instead of the generic explicit-shape
		// one.
		cfg.RosterPresetName = "gridiron-house"
	}
	cfg.Roster = resolveRosterBlock(file.Roster, file.Draft.Rounds)
	return cfg, nil
}

// unknownFieldName extracts the offending key from json.Decoder's
// DisallowUnknownFields error text ("json: unknown field \"foo\"") so
// LoadConfig can report it in the spec section 3.5 exact-message shape.
var unknownFieldPattern = regexp.MustCompile(`unknown field "([^"]+)"`)

func unknownFieldName(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	match := unknownFieldPattern.FindStringSubmatch(err.Error())
	if match == nil {
		return "", false
	}
	return match[1], true
}

// ResolveRosterTotal computes the draftable roster total (starters + bench
// + reserve; IR excluded) that block implies, using the exact same preset/
// explicit-shape resolution the config loader applies. It is a read-only
// preview, not a validator: an invalid or preset-vs-explicit-conflicting
// block still returns some total (call LoadConfigBytes for the
// authoritative validated result). The setup wizard (design section 4.1,
// step 5) uses this to auto-derive draft.rounds from the roster shape
// chosen at step 4, instead of asking an operator to compute the same sum
// by hand a second time.
func ResolveRosterTotal(block RosterBlock) int {
	return resolveRosterBlock(block, 0).Total()
}

// resolveRosterBlock expands a raw roster.preset name into its RosterPreset,
// or builds one from explicit roster.slots/roster.bench. An absent block
// (both Preset and Slots empty) resolves to gridiron-house per the
// roster-ops spec section 10 fallback rule — but note DefaultConfig never
// takes this path; it always sets RosterPresetName explicitly, so this
// fallback only fires for a config *file* that omits the roster block
// entirely, in which case the file inherits the reference behavior the
// spec pins ("an absent roster block resolves to gridiron-house").
// Malformed input (both set, unknown preset, unknown slot) is left for
// validateConfig to catch with its exact messages; this function never
// itself errors.
func resolveRosterBlock(block RosterBlock, draftRounds int) RosterPreset {
	var preset RosterPreset
	switch {
	case block.Preset != "":
		if p, ok := rosterPresets[block.Preset]; ok {
			preset = p
		} else {
			preset = RosterPreset{Name: block.Preset}
		}
	case len(block.Slots) == 0 && block.Bench == 0:
		preset = rosterPresets["gridiron-house"]
	default:
		preset = RosterPreset{Name: "", Slots: block.Slots, Bench: block.Bench}
	}
	// Reserve/IR/Limits merge onto whichever base resolved above (SK
	// spec): they ride along with either a preset or an explicit shape.
	// Absent (nil/zero) leaves the base untouched — every existing preset
	// and every config that never mentions them is unaffected.
	if len(block.Reserve) > 0 {
		preset.Reserve = block.Reserve
	}
	if block.IR > 0 {
		preset.IR = block.IR
	}
	if len(block.Limits) > 0 {
		preset.Limits = block.Limits
	}
	return preset
}

// envOverride applies value to *target when value is non-empty (spec
// section 3.3: built-in defaults < league.json < env, for the seven keys
// with an env twin).
func envOverride(target *string, key string) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		*target = value
	}
}

// applyEnvOverrides implements the spec section 3.3 precedence table: the
// seven env keys that may override a file (or default) value. A non-empty
// malformed override fails closed: an operator who set an invalid value must
// never get a successful boot that silently retained a value they intended to
// replace. Empty/unset values remain no-ops.
func applyEnvOverrides(cfg *Config) error {
	envOverride(&cfg.Name, "APP_NAME")
	envOverride(&cfg.Timezone, "DRAFT_TZ")
	envOverride(&cfg.URL, "LEAGUE_URL")
	envOverride(&cfg.ScoringFormat, "SCORING_FORMAT")
	if value := strings.TrimSpace(os.Getenv("DRAFT_AT")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil || parsed.IsZero() {
			return fmt.Errorf("league config: DRAFT_AT must be an RFC3339 timestamp")
		}
		cfg.DraftAt = parsed
	}
	if value := strings.TrimSpace(os.Getenv("SEASON_START_AT")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil || parsed.IsZero() {
			return fmt.Errorf("league config: SEASON_START_AT must be an RFC3339 timestamp")
		}
		cfg.SeasonStartAt = parsed
	}
	if value := strings.TrimSpace(os.Getenv("NFL_SEASON")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 2020 || parsed > 2100 {
			return fmt.Errorf("league config: NFL_SEASON must be an integer from 2020 to 2100")
		}
		cfg.Season = parsed
	}
	return nil
}

var teamIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

var validTones = []string{"blue", "cyan", "gold", "lime", "magenta", "orange", "pink", "violet"}

func validTone(tone string) bool {
	if tone == "" {
		return true // spec section 3.2: empty tones auto-assign from the palette cycle
	}
	for _, t := range validTones {
		if t == tone {
			return true
		}
	}
	return false
}

// validateConfig runs every rule in the spec section 3.5 / roster-ops
// section 10 tables, in the documented order, and returns the first
// violation as a fatal error with its exact message. Non-blocking findings
// (today: the team-count-above-12 guidance, owner decision) come back
// separately as warnings and never fail the boot.
func validateConfig(cfg *Config) (warnings []string, err error) {
	if cfg.Version != ConfigSchemaVersion {
		return nil, fmt.Errorf("league config: version must be %d", ConfigSchemaVersion)
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, fmt.Errorf("league config: league.name is required")
	}
	if l := len(cfg.ShortCode); l < 1 || l > 5 {
		return nil, fmt.Errorf("league config: league.short_code must be 1 to 5 characters")
	}

	n := len(cfg.Teams)
	if n < minTeams || n > maxTeams {
		return nil, fmt.Errorf("league config: teams must number %d to %d; got %d", minTeams, maxTeams, n)
	}
	if n%2 != 0 {
		return nil, fmt.Errorf("league config: the team count must be even; got %d", n)
	}

	seenIDs := map[string]bool{}
	seenAbbrevs := map[string]bool{}
	divisionOf := map[string]string{}
	divisionCounts := map[string]int{}
	teamsWithDivision := 0
	for _, team := range cfg.Teams {
		if !teamIDPattern.MatchString(team.ID) {
			return nil, fmt.Errorf("league config: team id %q must match [a-z0-9-]+", team.ID)
		}
		if seenIDs[team.ID] {
			return nil, fmt.Errorf("league config: duplicate team id %q", team.ID)
		}
		seenIDs[team.ID] = true
		if l := len(team.Name); l < 1 || l > 40 {
			return nil, fmt.Errorf("league config: team %q: name must be 1 to 40 characters", team.ID)
		}
		if l := len(team.Abbreviation); l < 2 || l > 4 {
			return nil, fmt.Errorf("league config: team %q: abbreviation must be 2 to 4 characters", team.ID)
		}
		abbrevKey := strings.ToUpper(team.Abbreviation)
		if seenAbbrevs[abbrevKey] {
			return nil, fmt.Errorf("league config: duplicate team abbreviation %q", team.Abbreviation)
		}
		seenAbbrevs[abbrevKey] = true
		if !validTone(team.Tone) {
			return nil, fmt.Errorf("league config: team %q: unknown tone %q; valid tones: %s",
				team.ID, team.Tone, strings.Join(validTones, ", "))
		}
		divisionOf[team.ID] = team.Division
		if team.Division != "" {
			teamsWithDivision++
			divisionCounts[team.Division]++
		}
	}
	if teamsWithDivision != 0 && teamsWithDivision != n {
		return nil, fmt.Errorf("league config: set a division on every team or on none")
	}
	if teamsWithDivision == n {
		divisionNames := make([]string, 0, len(divisionCounts))
		for name := range divisionCounts {
			divisionNames = append(divisionNames, name)
		}
		sort.Strings(divisionNames)
		for _, name := range divisionNames {
			if divisionCounts[name] < 2 {
				return nil, fmt.Errorf("league config: division %q has %d teams; each division needs at least 2", name, divisionCounts[name])
			}
		}
	}

	if cfg.Rounds < 1 || cfg.Rounds > 30 {
		return nil, fmt.Errorf("league config: draft.rounds must be 1 to 30")
	}
	if cfg.PickClockSeconds != 0 && (cfg.PickClockSeconds < 10 || cfg.PickClockSeconds > 600) {
		return nil, fmt.Errorf("league config: draft.pick_clock_seconds must be 10 to 600")
	}
	if cfg.DraftAt.IsZero() {
		return nil, fmt.Errorf("league config: draft.at must be an RFC3339 timestamp")
	}
	if cfg.SeasonStartAt.IsZero() {
		return nil, fmt.Errorf("league config: season_start_at must be an RFC3339 timestamp")
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return nil, fmt.Errorf("league config: league.timezone %q is not a valid IANA zone", cfg.Timezone)
	}
	switch cfg.ScoringFormat {
	case "half_ppr", "ppr", "standard":
	default:
		return nil, fmt.Errorf("league config: scoring_format must be one of half_ppr, ppr, standard")
	}
	if cfg.Season < 2020 || cfg.Season > 2100 {
		return nil, fmt.Errorf("league config: league.season must be 2020 to 2100")
	}

	if err := validateRoster(cfg); err != nil {
		return nil, err
	}
	if err := validateWaivers(cfg.Waivers); err != nil {
		return nil, err
	}
	if err := validateTrades(cfg.Trades); err != nil {
		return nil, err
	}
	if err := ValidatePostseasonConfig(cfg.Postseason, n, len(divisionCounts)); err != nil {
		return nil, err
	}
	if err := validateMembership(cfg.Membership); err != nil {
		return nil, err
	}
	if !cfg.Auth.InviteLinks && !cfg.Auth.Google {
		return nil, fmt.Errorf("league config: auth.invite_links and auth.google cannot both be false; at least one sign-in method must stay enabled")
	}

	if n > teamCountWarnAbove {
		warnings = append(warnings, fmt.Sprintf(
			"leagues above %d teams thin the player pool; the deep-league preset (superflex, smaller bench) keeps them viable",
			teamCountWarnAbove))
	}
	// GC-1 fix 2: warn when scoring_format's implied reception value
	// disagrees with defaultScoringRules' shipped reception default
	// (0.5, half_ppr's value — the only value baked into the rule table
	// literal). A fresh league seeds "reception" from scoring_format at
	// first boot (Store.InitReceptionFromScoringFormat), but this warning
	// fires for every non-half_ppr league regardless: it is a config
	// sanity signal, not a claim about what any particular league's
	// persisted state currently holds — leaguecheck never opens the state
	// database (see its own doc comment) and cannot see a commissioner's
	// own edit either way.
	if rule, ok := scoringRuleByKey("reception"); ok {
		if implied := ReceptionPointsForScoringFormat(cfg.ScoringFormat); implied != rule.Points {
			warnings = append(warnings, fmt.Sprintf(
				"scoring_format %q implies a %g-point reception rule, but the shipped default reception rule is %g; a fresh league seeds reception from scoring_format at first boot — check Scoring Settings for any league that already has commissioner-edited scoring",
				cfg.ScoringFormat, implied, rule.Points))
		}
	}
	return warnings, nil
}

// validRosterSlotKeys is the roster-ops spec section 4.1 slot table's key
// set, in the order roster.slots.%s error messages should recognize them.
var validRosterSlotKeys = []string{"QB", "RB", "WR", "TE", "FLEX", "SUPERFLEX", "DST", "K", "P"}

// validateRoster implements the roster-ops spec section 10 table: preset
// vs. explicit-shape conflict, slot/bench ranges, and the slot-total ==
// draft.rounds invariant, in the preset or explicit-shape message shape
// the spec pins for each path.
func validateRoster(cfg *Config) error {
	explicit := len(cfg.Roster.Slots) > 0 || cfg.Roster.Bench > 0
	if cfg.RosterPresetName != "" {
		if _, ok := rosterPresets[cfg.RosterPresetName]; !ok {
			names := presetNames()
			return fmt.Errorf("league config: unknown roster.preset %q; valid presets: %s", cfg.RosterPresetName, strings.Join(names, ", "))
		}
		if cfg.RosterConflict {
			return fmt.Errorf("league config: set roster.preset or roster.slots/roster.bench, not both")
		}
		if err := validateZonesAndLimits(cfg.Roster.Reserve, cfg.Roster.IR, cfg.Roster.Limits, "league config"); err != nil {
			return err
		}
		// cfg.Roster is the preset already merged with any Reserve/IR/
		// Limits (resolveRosterBlock) — its own Total() is the one that
		// must equal draft.rounds, not the raw, unmerged preset's.
		if cfg.Roster.Total() != cfg.Rounds {
			return fmt.Errorf("league config: roster.preset %q needs %d roster spots but draft.rounds is %d", cfg.RosterPresetName, cfg.Roster.Total(), cfg.Rounds)
		}
		return nil
	}
	if !explicit {
		// Absent block: resolveRosterBlock already set the gridiron-house
		// fallback; nothing further to validate here (its own totals are
		// pinned by TestGridironHousePresetShape). Unreachable through the
		// real load path today (loadConfigFile always stamps
		// RosterPresetName to "gridiron-house" when the block is absent,
		// so cfg.RosterPresetName != "" catches it above); kept as a
		// defensive branch for any other Config construction path.
		return nil
	}
	for key, count := range cfg.Roster.Slots {
		if !validSlotKey(key) {
			return fmt.Errorf("league config: roster.slots: unknown slot %q; valid slots: %s", key, strings.Join(validRosterSlotKeys, ", "))
		}
		if count < 0 || count > 4 {
			return fmt.Errorf("league config: roster.slots.%s must be 0 to 4", key)
		}
	}
	if cfg.Roster.Starters() < 1 {
		return fmt.Errorf("league config: roster.slots must total at least 1 starter")
	}
	if cfg.Roster.Bench < 0 || cfg.Roster.Bench > 10 {
		return fmt.Errorf("league config: roster.bench must be 0 to 10")
	}
	if err := validateZonesAndLimits(cfg.Roster.Reserve, cfg.Roster.IR, cfg.Roster.Limits, "league config"); err != nil {
		return err
	}
	if cfg.Roster.Total() != cfg.Rounds {
		return fmt.Errorf("league config: roster slots plus bench plus reserve must equal draft.rounds; got %d + %d + %d with %d rounds",
			cfg.Roster.Starters(), cfg.Roster.Bench, cfg.Roster.ReserveTotal(), cfg.Rounds)
	}
	return nil
}

func validSlotKey(key string) bool {
	for _, k := range validRosterSlotKeys {
		if k == key {
			return true
		}
	}
	return false
}

// RosterPresetNames returns every named roster preset the config loader
// accepts for roster.preset, sorted. The setup wizard's roster step (design
// section 4.1, step 4) reads this instead of hardcoding the list a second
// time.
func RosterPresetNames() []string {
	return presetNames()
}

func presetNames() []string {
	names := make([]string, 0, len(rosterPresets))
	for name := range rosterPresets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var processTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func validateWaivers(w WaiversBlock) error {
	switch w.Mode {
	case "perf-priority", "faab":
	default:
		return fmt.Errorf("league config: waivers.mode must be one of perf-priority, faab")
	}
	if w.SeasonWeightPct < 0 || w.SeasonWeightPct > 100 {
		return fmt.Errorf("league config: waivers.season_weight_pct must be 0 to 100")
	}
	if w.FAABBudget < 1 || w.FAABBudget > 1000 {
		return fmt.Errorf("league config: waivers.faab_budget must be 1 to 1000")
	}
	if w.ClearDays < 0 || w.ClearDays > 7 {
		return fmt.Errorf("league config: waivers.clear_days must be 0 to 7")
	}
	if !processTimePattern.MatchString(w.ProcessTime) {
		return fmt.Errorf("league config: waivers.process_time must be HH:MM")
	}
	return nil
}

// applyActiveConfig publishes a validated Config into the package-level
// vars the rest of the codebase reads: activeTeams (model.go),
// DraftRounds/DefaultDraftAt/DefaultDraftTZ/DefaultSeasonStartAt
// (model.go), and ActiveRosterPreset (lineup.go). Default() is the only
// caller, once, before the server starts accepting requests — see its
// doc comment in service.go. No test calls this: tests construct Service
// literals directly and never touch the Default() singleton, so they keep
// seeing DefaultConfig()'s neutral values for the lifetime of the test
// binary (config_test.go tests LoadConfig/validateConfig as pure
// functions instead, precisely to avoid this global-state hazard).
func applyActiveConfig(cfg Config) {
	activeTeams = teamsFromSeeds(cfg.Teams)
	DraftRounds = cfg.Rounds
	// A rounds increase can turn an already-complete draft (by the old
	// round count) incomplete again (draftComplete, roster.go); re-arm the
	// singleton's completion latch so a pick that re-completes it under the
	// new count emits draft:state. defaultSvc is nil on the very first,
	// boot-time call (Default() has not constructed it yet), which this
	// guards against. currentDefaultService (service.go) reads defaultSvc
	// under defaultMu rather than directly: this function is itself a
	// direct call site (from inside Default()'s own sync.Once and from
	// every test that calls applyActiveConfig without ever calling
	// Default()), so a plain package-var read here is not ordered against
	// Default()'s own assignment for a caller that skips Default().
	if svc := currentDefaultService(); svc != nil {
		svc.draftCompleteEmitted.Store(false)
	}
	DefaultDraftAt = cfg.DraftAt.Format(time.RFC3339)
	DefaultDraftTZ = cfg.Timezone
	DefaultSeasonStartAt = cfg.SeasonStartAt.Format(time.RFC3339)
	ActiveRosterPreset = cfg.Roster
}

// domainPattern accepts a bare domain: one or more dot-separated labels
// of letters, digits, and hyphens, ending in a letters-only label of at
// least two characters (registration wave, build item 5's "bare domain,
// no @" validation rule).
var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)

// validateMembership checks the "membership" config block (build item
// 5): an empty allowed_domain (the default) needs no validation — no
// domain gate. A set value must be a bare domain, not an email address,
// and must match domainPattern.
func validateMembership(m MembershipBlock) error {
	domain := strings.ToLower(strings.TrimSpace(m.AllowedDomain))
	if domain == "" {
		return nil
	}
	if strings.Contains(domain, "@") {
		return fmt.Errorf("league config: membership.allowed_domain must be a bare domain, not an email address")
	}
	if !domainPattern.MatchString(domain) {
		return fmt.Errorf("league config: membership.allowed_domain %q is not a valid domain", domain)
	}
	return nil
}

func validateTrades(t TradesBlock) error {
	switch t.Veto {
	case "commissioner", "vote", "both", "none":
	default:
		return fmt.Errorf("league config: trades.veto must be one of commissioner, vote, both, none")
	}
	if t.ReviewHours < 1 || t.ReviewHours > 72 {
		return fmt.Errorf("league config: trades.review_hours must be 1 to 72")
	}
	if strings.TrimSpace(t.Deadline) != "" {
		if _, err := time.Parse(time.RFC3339, t.Deadline); err != nil {
			return fmt.Errorf("league config: trades.deadline must be an RFC3339 timestamp or empty")
		}
	}
	return nil
}

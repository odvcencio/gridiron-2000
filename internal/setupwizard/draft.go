package setupwizard

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gridiron-2000/internal/league"
)

// IdentityAlias is one commissioner identity alias entry (design section
// 4.1, step 9): an additional email that signs in as the same canonical
// commissioner. Becomes an IDENTITY_ALIASES entry in runtime.env at commit
// — never a league.json field (design section 4.3).
type IdentityAlias struct {
	Alias     string
	Canonical string
}

// Draft is the wizard's in-progress state.
type Draft struct {
	// Config is the configFile-shaped draft every step POST merges into
	// (design section 4.2). It always marshals to a byte-identical shape
	// to what a hand-authored league.json would use.
	Config league.ConfigFile
	// MemberEmails is the Tier 0 admission list collected at the
	// membership step. Not a league.json field: it becomes
	// Store.AddInvite entries and minted invite_links rows at commit
	// (design section 4.5, step 3).
	MemberEmails []string
	// CommissionerEmail becomes COMMISSIONER_EMAILS in runtime.env.
	CommissionerEmail string
	// IdentityAliases become IDENTITY_ALIASES entries in runtime.env.
	IdentityAliases []IdentityAlias
}

// persistedDraft is Draft's JSON-on-disk shape (setup_draft.draft_json).
// A distinct type from Draft itself so a future field can be added to
// Draft without an implicit, unreviewed change to what setup_draft
// persists.
type persistedDraft struct {
	Config            league.ConfigFile `json:"config"`
	MemberEmails      []string          `json:"member_emails"`
	CommissionerEmail string            `json:"commissioner_email"`
	IdentityAliases   []IdentityAlias   `json:"identity_aliases"`
}

// NewDraft returns a fresh draft seeded with neutral, internally
// consistent placeholder values (design section 4.2: "marshals the draft
// with neutral placeholders for not-yet-visited steps"), so step 1 can be
// validated before any later step has been visited.
func NewDraft() Draft {
	return Draft{Config: neutralSeed()}
}

// neutralSeed is deliberately generic — no real league's name, dates, or
// team identities — matching DefaultConfig's own "clearly fake" shipped
// defaults (config.go), expressed in ConfigFile's wire shape instead of
// the resolved Config shape.
func neutralSeed() league.ConfigFile {
	return league.ConfigFile{
		Version: league.ConfigSchemaVersion,
		League: league.ConfigFileLeague{
			Name: "THE LEAGUE", ShortCode: "TL", Tagline: "Fantasy Football League",
			ModeLabel: "DYNASTY", URL: "http://localhost:8080",
			Timezone: "America/New_York", Season: time.Now().Year(),
		},
		Teams: []league.TeamSeed{
			{ID: "team-1", Name: "Team One", Abbreviation: "TM1"},
			{ID: "team-2", Name: "Team Two", Abbreviation: "TM2"},
			{ID: "team-3", Name: "Team Three", Abbreviation: "TM3"},
			{ID: "team-4", Name: "Team Four", Abbreviation: "TM4"},
		},
		Draft: league.ConfigFileDraft{
			At: "2099-01-01T00:00:00Z", Rounds: league.ResolveRosterTotal(league.RosterBlock{Preset: "standard"}),
		},
		SeasonStartAt: "2099-01-08T00:00:00Z",
		ScoringFormat: "half_ppr",
		Roster:        league.RosterBlock{Preset: "standard"},
		Waivers: league.WaiversBlock{
			Mode: "perf-priority", SeasonWeightPct: 60, FAABBudget: 100,
			ClearDays: 2, ProcessTime: "09:00",
		},
		Trades: league.TradesBlock{Veto: "commissioner", ReviewHours: 24},
	}
}

// Validate marshals config and runs it through league.LoadConfigBytes —
// the exact same decode-and-validate path leaguecheck and boot use
// (design section 4.2: "No validator is duplicated"). It never mutates
// config's caller; the result tells the caller whether to keep the
// candidate.
func Validate(config league.ConfigFile) (league.Config, []string, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return league.Config{}, nil, fmt.Errorf("encode wizard draft: %w", err)
	}
	return league.LoadConfigBytes("setup-wizard-draft.json", raw)
}

// stepFieldOwners maps a validateConfig error message's fixed substring to
// the wizard step that owns it (design section 4.2's "static field->step
// table"). Order matters: more specific substrings are checked first
// (draft.rounds before the generic "roster." owner it is folded into,
// since the wizard auto-derives rounds from the roster step instead of
// asking twice).
var stepFieldOwners = []struct {
	substr string
	step   string
}{
	{"draft.rounds", "roster"},
	{"version must be", "identity"},
	{"league.name", "identity"},
	{"league.short_code", "identity"},
	{"league.timezone", "identity"},
	{"league.season", "identity"},
	{"teams must number", "teams"},
	{"team count must be even", "teams"},
	{"team id", "teams"},
	{"duplicate team id", "teams"},
	{"team %q", "teams"},
	{`team "`, "teams"},
	{"duplicate team abbreviation", "teams"},
	{"division", "teams"},
	{"draft.pick_clock_seconds", "draft"},
	{"draft.at", "draft"},
	{"season_start_at", "draft"},
	{"scoring_format", "scoring"},
	{"roster.", "roster"},
	{"roster slots plus bench", "roster"},
	{"waivers.", "waivers"},
	{"trades.", "trades"},
	{"membership.", "membership"},
	{"auth.", "commissioner"},
}

// FieldStepOwner returns the wizard step slug that owns a validateConfig
// error message, or "" when no known field prefix matches (an unknown
// message is treated as belonging to whichever step is currently being
// saved — see State.ApplyStep).
func FieldStepOwner(message string) string {
	for _, entry := range stepFieldOwners {
		if strings.Contains(message, entry.substr) {
			return entry.step
		}
	}
	return ""
}

// State is the wizard's full session-scoped working state: the draft plus
// every step's persisted status.
type State struct {
	Draft  Draft
	Status StatusMap
}

// NewState returns a fresh State: a neutral draft and every step TODO.
func NewState() State {
	return State{Draft: NewDraft(), Status: StatusMap{}}
}

// ApplyStep validates config as the candidate result of saving step slug.
// On success, or when every validation error belongs to a step later than
// slug (design section 4.2: "an error for a later step's field is
// deferred, not shown early"), the candidate is kept, slug is marked DONE,
// and any later step the error names is marked STALE. Otherwise the
// current draft is left untouched and the error is returned for the
// caller to show on slug's own page.
func (s *State) ApplyStep(slug string, config league.ConfigFile) (warnings []string, err error) {
	_, warnings, verr := Validate(config)
	if verr == nil {
		s.Draft.Config = config
		s.Status.MarkDone(slug)
		return warnings, nil
	}
	owner := FieldStepOwner(verr.Error())
	if owner != "" && StepIndex(owner) > StepIndex(slug) {
		s.Draft.Config = config
		s.Status.MarkDone(slug)
		s.Status.MarkStale(owner)
		return nil, nil
	}
	return nil, verr
}

// Save persists the draft's non-secret fields and step-status map (design
// section 4.4). Secrets never reach this method — none exist in this
// slice's step set (Tier 2/3 secrets are a later slice's addition, held in
// the bound session only per design section 4.3).
func (s *State) Save(store *league.Store, now time.Time) error {
	draftJSON, err := json.Marshal(persistedDraft{
		Config: s.Draft.Config, MemberEmails: s.Draft.MemberEmails,
		CommissionerEmail: s.Draft.CommissionerEmail, IdentityAliases: s.Draft.IdentityAliases,
	})
	if err != nil {
		return fmt.Errorf("encode setup draft: %w", err)
	}
	statusJSON, err := json.Marshal(s.Status)
	if err != nil {
		return fmt.Errorf("encode setup step status: %w", err)
	}
	return store.SaveSetupDraft(draftJSON, statusJSON, now)
}

// LoadState reads a previously saved draft, or returns a fresh State when
// none has been saved yet.
func LoadState(store *league.Store) (State, error) {
	saved, found, err := store.LoadSetupDraft()
	if err != nil {
		return State{}, err
	}
	if !found {
		return NewState(), nil
	}
	var persisted persistedDraft
	if err := json.Unmarshal(saved.DraftJSON, &persisted); err != nil {
		return State{}, fmt.Errorf("decode setup draft: %w", err)
	}
	status := StatusMap{}
	if len(saved.StepStatusJSON) > 0 {
		if err := json.Unmarshal(saved.StepStatusJSON, &status); err != nil {
			return State{}, fmt.Errorf("decode setup step status: %w", err)
		}
	}
	return State{
		Draft: Draft{
			Config: persisted.Config, MemberEmails: persisted.MemberEmails,
			CommissionerEmail: persisted.CommissionerEmail, IdentityAliases: persisted.IdentityAliases,
		},
		Status: status,
	}, nil
}

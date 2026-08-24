// Package v1 defines and validates the versioned, read-only Gridiron HQ
// commissioner-summary contract. It deliberately contains no transport,
// storage, or application dependencies.
package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	ContractName  = "gridiron.hq.commissioner-summary"
	SchemaVersion = "1.0"
)

type Summary struct {
	Contract       string          `json:"contract"`
	SchemaVersion  string          `json:"schema_version"`
	Instance       Instance        `json:"instance"`
	League         League          `json:"league"`
	Capabilities   []string        `json:"capabilities"`
	Competition    Competition     `json:"competition"`
	Draft          *Draft          `json:"draft"`
	Readiness      *Readiness      `json:"readiness"`
	Membership     Membership      `json:"membership"`
	Lineup         Lineup          `json:"lineup"`
	Waivers        Waivers         `json:"waivers"`
	Trades         Trades          `json:"trades"`
	Pickem         Pickem          `json:"pickem"`
	Warnings       []Warning       `json:"warnings"`
	Calendar       Calendar        `json:"calendar"`
	AttentionItems []AttentionItem `json:"attention_items"`
	RecentActivity RecentActivity  `json:"recent_activity"`
	Configuration  Configuration   `json:"configuration"`
	DataHealth     DataHealth      `json:"data_health"`
	Release        Release         `json:"release"`
	Links          Links           `json:"links"`
	ProducedAt     string          `json:"produced_at"`
}

type Instance struct {
	ID       string `json:"id"`
	LeagueID string `json:"league_id"`
}

type League struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Format    string `json:"format"`
	Season    int    `json:"season"`
	Timezone  string `json:"timezone"`
}

type Competition struct {
	Phase string     `json:"phase"`
	Teams TeamCounts `json:"teams"`
}

type TeamCounts struct {
	Occupied *int `json:"occupied"`
	Vacant   *int `json:"vacant"`
	Total    *int `json:"total"`
}

type Draft struct {
	State         string  `json:"state"`
	ScheduledAt   *string `json:"scheduled_at"`
	OrderStatus   *string `json:"order_status"`
	PickCount     *int    `json:"pick_count"`
	PickCapacity  *int    `json:"pick_capacity"`
	DraftRounds   *int    `json:"draft_rounds"`
	ReadyTeams    *int    `json:"ready_teams"`
	ExpectedTeams *int    `json:"expected_teams"`
	OnClockTeamID *string `json:"on_clock_team_id"`
	BoardGapCount *int    `json:"board_gap_count"`
}

type Readiness struct {
	Severity string          `json:"severity"`
	Items    []ReadinessItem `json:"items"`
}

type ReadinessItem struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Count    int    `json:"count"`
	Label    string `json:"label"`
}

type Membership struct {
	ClaimedTeams    *int `json:"claimed_teams"`
	OpenTeams       *int `json:"open_teams"`
	PendingInvites  *int `json:"pending_invites"`
	PrimaryManagers *int `json:"primary_managers"`
	CoManagers      *int `json:"co_managers"`
}

type Lineup struct {
	IssueCount *int    `json:"issue_count"`
	NextLockAt *string `json:"next_lock_at"`
}

type Waivers struct {
	Mode       *string `json:"mode"`
	OpenClaims *int    `json:"open_claims"`
	NextRunAt  *string `json:"next_run_at"`
}

type Trades struct {
	PendingCount          *int `json:"pending_count"`
	CommissionerDecisions *int `json:"commissioner_decisions"`
}

type Pickem struct {
	Week           *int    `json:"week"`
	Unpicked       *int    `json:"unpicked"`
	NextDeadlineAt *string `json:"next_deadline_at"`
}

type Warning struct {
	Code     string  `json:"code"`
	Severity string  `json:"severity"`
	Summary  string  `json:"summary"`
	Source   *string `json:"source,omitempty"`
}

type Calendar struct {
	NextDeadline *Deadline  `json:"next_deadline"`
	Deadlines    []Deadline `json:"deadlines"`
}

type Deadline struct {
	Code         string  `json:"code"`
	Category     string  `json:"category"`
	Title        string  `json:"title"`
	At           *string `json:"at"`
	Timezone     string  `json:"timezone"`
	RelativeText string  `json:"relative_text"`
	State        string  `json:"state"`
	Href         *string `json:"href"`
}

type AttentionItem struct {
	Code     string  `json:"code"`
	Category string  `json:"category"`
	Severity string  `json:"severity"`
	Title    string  `json:"title"`
	Summary  string  `json:"summary"`
	LeagueID string  `json:"league_id"`
	DueAt    *string `json:"due_at"`
	State    string  `json:"state"`
	Source   string  `json:"source"`
	Href     *string `json:"href"`
}

type RecentActivity struct {
	Items     []ActivityItem `json:"items"`
	Truncated bool           `json:"truncated"`
	AsOf      string         `json:"as_of"`
}

type ActivityItem struct {
	ID         string  `json:"id"`
	OccurredAt string  `json:"occurred_at"`
	Category   string  `json:"category"`
	Summary    string  `json:"summary"`
	Href       *string `json:"href,omitempty"`
}

type Configuration struct {
	Format        *string `json:"format"`
	TeamCount     *int    `json:"team_count"`
	RosterModel   *string `json:"roster_model"`
	ScoringModel  *string `json:"scoring_model"`
	WaiverMode    *string `json:"waiver_mode"`
	TradePolicy   *string `json:"trade_policy"`
	PickemEnabled *bool   `json:"pickem_enabled"`
	BlitzEnabled  *bool   `json:"blitz_enabled"`
	Timezone      *string `json:"timezone"`
}

type DataHealth struct {
	Quality         string  `json:"quality"`
	SourceMode      *string `json:"source_mode"`
	SourceState     *string `json:"source_state"`
	PlayerCount     *int    `json:"player_count"`
	LastDegradation *string `json:"last_degradation"`
	LastSuccessAt   *string `json:"last_success_at"`
	AsOf            *string `json:"as_of"`
}

type Release struct {
	GitSHA      string  `json:"git_sha"`
	BuiltAt     string  `json:"built_at"`
	ImageDigest *string `json:"image_digest"`
}

type Links struct {
	League       *string `json:"league"`
	Overview     *string `json:"overview"`
	Join         *string `json:"join"`
	Draft        *string `json:"draft"`
	Board        *string `json:"board"`
	Team         *string `json:"team"`
	Players      *string `json:"players"`
	Trades       *string `json:"trades"`
	Pickem       *string `json:"pickem"`
	Blitz        *string `json:"blitz"`
	Activity     *string `json:"activity"`
	Commissioner *string `json:"commissioner"`
}

// Decode parses one JSON value, verifies that all v1-required keys are present,
// permits additive unknown fields, and validates the resulting summary.
func Decode(data []byte) (Summary, error) {
	if len(data) > 256*1024 {
		return Summary{}, fmt.Errorf("commissioner summary exceeds 256 KiB")
	}
	if !utf8.Valid(data) {
		return Summary{}, fmt.Errorf("commissioner summary must be valid UTF-8")
	}
	if !validJSONUnicodeEscapes(data) {
		return Summary{}, fmt.Errorf("commissioner summary must use well-formed Unicode escapes")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Summary{}, fmt.Errorf("decode commissioner summary: %w", err)
	}
	if err := consumeEOF(dec); err != nil {
		return Summary{}, err
	}
	if err := validateRequiredJSON(raw); err != nil {
		return Summary{}, err
	}
	var summary Summary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return Summary{}, fmt.Errorf("decode commissioner summary fields: %w", err)
	}
	if err := summary.Validate(); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func DecodeReader(r io.Reader) (Summary, error) {
	data, err := io.ReadAll(io.LimitReader(r, 256*1024+1))
	if err != nil {
		return Summary{}, fmt.Errorf("read commissioner summary: %w", err)
	}
	if len(data) > 256*1024 {
		return Summary{}, fmt.Errorf("commissioner summary exceeds 256 KiB")
	}
	return Decode(data)
}

func consumeEOF(dec *json.Decoder) error {
	var extra json.RawMessage
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing commissioner summary data: %w", err)
	}
	return fmt.Errorf("commissioner summary contains multiple JSON values")
}

// validJSONUnicodeEscapes rejects unpaired UTF-16 surrogate escapes before
// encoding/json can normalize them to U+FFFD. Other JSON syntax remains the
// decoder's responsibility.
func validJSONUnicodeEscapes(data []byte) bool {
	inString := false
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || i+1 >= len(data) {
				continue
			}
			i++
			if data[i] != 'u' {
				continue
			}
			code, ok := decodeHex16(data, i+1)
			if !ok {
				continue
			}
			i += 4
			if code >= 0xdc00 && code <= 0xdfff {
				return false
			}
			if code < 0xd800 || code > 0xdbff {
				continue
			}
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, lowOK := decodeHex16(data, i+3)
			if !lowOK || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

func decodeHex16(data []byte, start int) (uint16, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var value uint16
	for _, b := range data[start : start+4] {
		value <<= 4
		switch {
		case b >= '0' && b <= '9':
			value |= uint16(b - '0')
		case b >= 'a' && b <= 'f':
			value |= uint16(b-'a') + 10
		case b >= 'A' && b <= 'F':
			value |= uint16(b-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

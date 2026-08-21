package commissionerhq

import "time"

const SchemaVersion = 1

// Summary is the deliberately small, PII-free contract one league exposes to
// another commissioner dashboard. It must never grow manager identities,
// invites, session data, or mutation credentials.
type Summary struct {
	SchemaVersion int         `json:"schemaVersion"`
	GeneratedAt   time.Time   `json:"generatedAt"`
	Instance      Instance    `json:"instance"`
	Runtime       Runtime     `json:"runtime"`
	Membership    Membership  `json:"membership"`
	Draft         Draft       `json:"draft"`
	Pool          Pool        `json:"pool"`
	Attention     []Attention `json:"attention"`
}

type Instance struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortCode string `json:"shortCode"`
	PublicURL string `json:"publicURL"`
	Mode      string `json:"mode"`
	Season    int    `json:"season"`
}

type Runtime struct {
	Ready      bool   `json:"ready"`
	AppVersion string `json:"appVersion"`
	GitSHA     string `json:"gitSHA"`
}

type Membership struct {
	Seats        int `json:"seats"`
	ClaimedSeats int `json:"claimedSeats"`
	ReadySeats   int `json:"readySeats"`
	Members      int `json:"members"`
}

type Draft struct {
	ScheduledAt time.Time `json:"scheduledAt"`
	Status      string    `json:"status"`
	Started     bool      `json:"started"`
	StartedAt   time.Time `json:"startedAt,omitzero"`
	Rounds      int       `json:"rounds"`
	Picks       int       `json:"picks"`
	OrderSet    bool      `json:"orderSet"`
	ClockArmed  bool      `json:"clockArmed"`
	ClockPaused bool      `json:"clockPaused"`
	Deadline    time.Time `json:"clockDeadline,omitzero"`
}

type Pool struct {
	Mode           string    `json:"mode"`
	Players        int       `json:"players"`
	Target         int       `json:"target"`
	RosterCapacity int       `json:"rosterCapacity"`
	Cushion        int       `json:"cushion"`
	Coverage       float64   `json:"coverage"`
	LastSync       time.Time `json:"lastSync,omitzero"`
	Error          string    `json:"error,omitempty"`
}

type Attention struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Count    int    `json:"count,omitempty"`
	Message  string `json:"message"`
	Href     string `json:"href,omitempty"`
}

type FleetEntry struct {
	PeerID  string
	Summary Summary
	Error   string
}

func (e FleetEntry) Available() bool { return e.Error == "" }

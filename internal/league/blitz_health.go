package league

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Blitz source states are intentionally shared by the page, process health,
// and commissioner summary. They describe provenance, not whether the page
// happens to have rows to render: an empty loading/error snapshot is never a
// verified zero-game slate.
const (
	BlitzStateDisabled = "disabled"
	BlitzStateLoading  = "loading"
	BlitzStateReady    = "ready"
	BlitzStateDegraded = "degraded"
	BlitzStateError    = "error"
	BlitzStateStale    = "stale"
)

// BlitzSlateHealth is the source contract for one slate. ExpectedGames is
// the number of games the provider said belong to the slate and FetchedGames
// is the number with usable kickoff data retained in the snapshot. Complete
// is true only when the source has accounted for all expected games (including
// an explicitly verified zero). Final is true only when a complete slate has
// final status for every expected game.
type BlitzSlateHealth struct {
	State         string    `json:"state"`
	LastAttempt   time.Time `json:"lastAttempt,omitzero"`
	LastSuccess   time.Time `json:"lastSuccess,omitzero"`
	Error         string    `json:"error,omitempty"`
	ExpectedGames int       `json:"expectedGames"`
	FetchedGames  int       `json:"fetchedGames"`
	FinalGames    int       `json:"finalGames"`
	Complete      bool      `json:"complete"`
	Final         bool      `json:"final"`
	VerifiedZero  bool      `json:"verifiedZero"`
}

// BlitzHealth is the reusable health contract carried by BlitzSnapshot and
// projected into /api/health, commissioner summary, and /blitz view data.
// SafeError must contain provider-independent copy only; callers must never
// place URLs, API keys, request headers, or raw provider errors in it.
type BlitzHealth struct {
	Enabled       bool                        `json:"enabled"`
	State         string                      `json:"state"`
	LastAttempt   time.Time                   `json:"lastAttempt,omitzero"`
	LastSuccess   time.Time                   `json:"lastSuccess,omitzero"`
	SafeError     string                      `json:"error,omitempty"`
	ExpectedGames int                         `json:"expectedGames"`
	FetchedGames  int                         `json:"fetchedGames"`
	FinalGames    int                         `json:"finalGames"`
	Complete      bool                        `json:"complete"`
	Final         bool                        `json:"final"`
	VerifiedZero  bool                        `json:"verifiedZero"`
	Slates        map[string]BlitzSlateHealth `json:"slates,omitempty"`
}

// BlitzPre1Health tracks the one-time preseason-week-1 evidence map. It uses
// the same state vocabulary, but expected/fetched refer to final box scores.
type BlitzPre1Health struct {
	State         string    `json:"state"`
	LastAttempt   time.Time `json:"lastAttempt,omitzero"`
	LastSuccess   time.Time `json:"lastSuccess,omitzero"`
	SafeError     string    `json:"error,omitempty"`
	ExpectedGames int       `json:"expectedGames"`
	FetchedGames  int       `json:"fetchedGames"`
	Complete      bool      `json:"complete"`
}

// BlitzPre1Snapshot is the pre1 map plus provenance. A partial map remains
// usable, but page copy can label it provisional instead of calling absent
// players "no snaps" as if the source were complete.
type BlitzPre1Snapshot struct {
	Stats  map[string]map[string]float64
	Health BlitzPre1Health
}

// BlitzDependencyHealth is the process-health projection: live slates and
// the optional week-1 evidence pipeline remain separately observable so a
// partial pre1 refresh cannot make already-healthy live scores look offline.
type BlitzDependencyHealth struct {
	Source BlitzHealth     `json:"source"`
	Pre1   BlitzPre1Health `json:"pre1"`
}

// BlitzPre1Source supplies the current pre1 snapshot without doing network
// work. It is deliberately separate from BlitzSource because pre1 is a
// one-time evidence pass while pre2/pre3 are live slates.
type BlitzPre1SnapshotSource func() BlitzPre1Snapshot

// NewBlitzPre1Health infers a compatibility health value for tests and
// integrations that still attach only a raw map through SetBlitzPre1Source.
func NewBlitzPre1Health(stats map[string]map[string]float64) BlitzPre1Health {
	if stats == nil {
		return BlitzPre1Health{State: BlitzStateDisabled}
	}
	return BlitzPre1Health{State: BlitzStateReady, Complete: true}
}

// SafeBlitzError classifies provider failures into stable, non-sensitive copy
// suitable for public/process health. The original error remains server-log
// only. This intentionally does not echo arbitrary provider text because
// errors from URL builders and HTTP clients can carry credentials or hosts.
func SafeBlitzError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "source request cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "source request timed out"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "401"), strings.Contains(message, "403"), strings.Contains(message, "unauthorized"), strings.Contains(message, "forbidden"):
		return "source authorization failed"
	case strings.Contains(message, "429"), strings.Contains(message, "rate limit"):
		return "source rate limit reached"
	case strings.Contains(message, "404"):
		return "source endpoint unavailable"
	case strings.Contains(message, "no games"), strings.Contains(message, "no week param"):
		return "source has not published this slate"
	case strings.Contains(message, "empty"), strings.Contains(message, "parse"):
		return "source returned unusable data"
	default:
		return "source temporarily unavailable"
	}
}

// SafeBlitzErrorText accepts a provider message while stripping URL-like
// values as a defensive final boundary for callers that only have text.
func SafeBlitzErrorText(message string) string {
	if message == "" {
		return ""
	}
	if parsed, err := url.Parse(strings.TrimSpace(message)); err == nil && parsed.IsAbs() {
		return "source endpoint unavailable"
	}
	return SafeBlitzError(errors.New(message))
}

// BlitzHealthFromSnapshot keeps legacy fixture snapshots useful while making
// an explicitly empty health-bearing snapshot remain unknown. A snapshot
// with games and no health predates the contract and is treated as complete
// for backwards compatibility with pure league tests.
func BlitzHealthFromSnapshot(health BlitzHealth, games []BlitzGame) BlitzHealth {
	if len(health.Slates) == 0 && len(games) > 0 && health.State == BlitzStateReady {
		health.Slates = inferBlitzSlateHealth(games)
		if health.ExpectedGames == 0 {
			health.ExpectedGames = len(games)
		}
		if health.FetchedGames == 0 {
			health.FetchedGames = len(games)
		}
		if health.FinalGames == 0 {
			health.FinalGames = countFinalBlitzGames(games)
		}
		if !health.Complete {
			health.Complete = len(health.Slates) == 2
		}
		if health.Complete && health.ExpectedGames > 0 {
			health.Final = health.FinalGames == health.ExpectedGames
		}
		return health
	}
	if health.State != "" || health.Enabled || health.LastAttempt.IsZero() == false || len(health.Slates) > 0 {
		return health
	}
	if len(games) == 0 {
		return BlitzHealth{State: BlitzStateLoading}
	}
	slates := inferBlitzSlateHealth(games)
	complete := len(slates) == 2
	final := complete
	for _, status := range slates {
		final = final && status.Final
	}
	return BlitzHealth{
		Enabled:       true,
		State:         BlitzStateReady,
		ExpectedGames: len(games),
		FetchedGames:  len(games),
		FinalGames:    countFinalBlitzGames(games),
		Complete:      complete,
		Final:         final,
		Slates:        slates,
	}
}

func inferBlitzSlateHealth(games []BlitzGame) map[string]BlitzSlateHealth {
	out := map[string]BlitzSlateHealth{}
	for _, slate := range []string{"pre2", "pre3"} {
		count := 0
		final := 0
		for _, game := range games {
			if game.Slate != slate {
				continue
			}
			count++
			if game.Final {
				final++
			}
		}
		if count == 0 {
			continue
		}
		out[slate] = BlitzSlateHealth{
			State:         BlitzStateReady,
			ExpectedGames: count,
			FetchedGames:  count,
			FinalGames:    final,
			Complete:      true,
			Final:         final == count,
		}
	}
	return out
}

func countFinalBlitzGames(games []BlitzGame) int {
	count := 0
	for _, game := range games {
		if game.Final {
			count++
		}
	}
	return count
}

func allBlitzGamesFinal(games []BlitzGame) bool {
	return len(games) > 0 && countFinalBlitzGames(games) == len(games)
}

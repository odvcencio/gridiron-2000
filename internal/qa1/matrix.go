// Package qa1 contains the finite, product-facing dimensions of the QA-1
// acceptance matrix. It has no runtime dependencies and is intentionally
// safe for tests, evidence tooling, and future render harnesses to reuse.
package qa1

import "fmt"

// Mode is the configured league format shown by public and commissioner
// surfaces. The matrix deliberately keeps both supported product modes in
// every acceptance run.
type Mode string

const (
	ModeDynasty Mode = "dynasty"
	ModeRedraft Mode = "redraft"
)

// Identity is the canonical public-entry role state. Seatless is split into
// open and full because the permitted action differs (/join versus /pickem).
// CommissionerOverlay is an orthogonal capability over a base identity, not
// a new seat state.
type Identity string

const (
	IdentityAnonymous           Identity = "anonymous"
	IdentityPending             Identity = "pending"
	IdentitySeatlessOpen        Identity = "seatless-open"
	IdentitySeatlessFull        Identity = "seatless-full"
	IdentityPrimary             Identity = "primary"
	IdentityCoManager           Identity = "co-manager"
	IdentityCommissionerOverlay Identity = "commissioner-overlay"
)

// Phase is the externally named lifecycle state accepted by the commissioner
// summary contract, including the intentionally explicit unknown fallback.
type Phase string

const (
	PhasePreDraft   Phase = "pre-draft"
	PhaseDraft      Phase = "draft"
	PhasePreseason  Phase = "preseason"
	PhaseRegular    Phase = "regular"
	PhasePostseason Phase = "postseason"
	PhaseComplete   Phase = "complete"
	PhaseUnknown    Phase = "unknown"
)

// Health is the source/recovery posture exercised by the acceptance matrix.
// Validation represents an untrusted/invalid source tuple, while Recovery
// represents a fresh live generation after a prior degraded generation.
type Health string

const (
	HealthHealthy    Health = "healthy"
	HealthStale      Health = "stale"
	HealthDegraded   Health = "degraded"
	HealthOffline    Health = "offline"
	HealthValidation Health = "validation"
	HealthRecovery   Health = "recovery"
)

// Case is one Cartesian-product acceptance row.
type Case struct {
	Mode     Mode
	Identity Identity
	Phase    Phase
	Health   Health
}

// Name is stable evidence/test output: changing dimension ordering does not
// change the meaning of a row, and no user or league data enters its label.
func (c Case) Name() string {
	return fmt.Sprintf("%s/%s/%s/%s", c.Mode, c.Identity, c.Phase, c.Health)
}

// Matrix returns the complete bounded QA-1 Cartesian product: 2 modes × 7
// identity postures × 7 lifecycle phases × 6 source-health postures.
func Matrix() []Case {
	modes := []Mode{ModeDynasty, ModeRedraft}
	identities := []Identity{
		IdentityAnonymous,
		IdentityPending,
		IdentitySeatlessOpen,
		IdentitySeatlessFull,
		IdentityPrimary,
		IdentityCoManager,
		IdentityCommissionerOverlay,
	}
	phases := []Phase{
		PhasePreDraft,
		PhaseDraft,
		PhasePreseason,
		PhaseRegular,
		PhasePostseason,
		PhaseComplete,
		PhaseUnknown,
	}
	healths := []Health{
		HealthHealthy,
		HealthStale,
		HealthDegraded,
		HealthOffline,
		HealthValidation,
		HealthRecovery,
	}

	rows := make([]Case, 0, len(modes)*len(identities)*len(phases)*len(healths))
	for _, mode := range modes {
		for _, identity := range identities {
			for _, phase := range phases {
				for _, health := range healths {
					rows = append(rows, Case{Mode: mode, Identity: identity, Phase: phase, Health: health})
				}
			}
		}
	}
	return rows
}

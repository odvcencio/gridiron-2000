package league

import "fmt"

// ClaimField identifies the form control responsible for a fantasy-seat
// claim validation failure. It is carried by ClaimValidationError so the
// action boundary can attach feedback to the authoritative control without
// parsing member-facing error text.
type ClaimField string

const (
	ClaimFieldTeamName ClaimField = "team_name"
	ClaimFieldMotif    ClaimField = "motif"
)

// FormKey returns the managed-action form key for field. Keeping this
// conversion beside the typed field constants prevents page handlers from
// re-inventing stringly field classification.
func (f ClaimField) FormKey() string {
	return string(f)
}

// ClaimValidationError is a member-safe validation failure emitted by the
// fantasy-seat claim boundary. Err retains the original sentinel/type for
// errors.Is/errors.As callers while Field remains the authoritative form
// attribution. Service claim paths wrap name failures as ClaimFieldTeamName
// and badge availability failures as ClaimFieldMotif; persistence and
// identity failures remain form-level.
type ClaimValidationError struct {
	Field ClaimField
	Err   error
}

func (e *ClaimValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("invalid claim field %q", e.Field)
}

func (e *ClaimValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func claimValidationError(field ClaimField, err error) error {
	if err == nil {
		return nil
	}
	return &ClaimValidationError{Field: field, Err: err}
}

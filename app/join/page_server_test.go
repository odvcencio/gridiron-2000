package join

import (
	"errors"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
)

func TestSignupClaimFieldUsesAuthoritativeServiceClassification(t *testing.T) {
	tests := []struct {
		name  string
		field league.ClaimField
		want  string
	}{
		{name: "team name", field: league.ClaimFieldTeamName, want: "team_name"},
		{name: "badge motif", field: league.ClaimFieldMotif, want: "motif"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := &league.ClaimValidationError{Field: tc.field, Err: errors.New("validation")}
			got, ok := signupClaimField(err)
			if !ok || got != tc.want {
				t.Fatalf("signupClaimField = %q, %v; want %q, true", got, ok, tc.want)
			}
		})
	}
}

func TestSignupClaimFieldKeepsNonFieldFailuresFormLevel(t *testing.T) {
	tests := []error{
		errors.New("the league is full"),
		league.ErrInternal,
		&league.ClaimValidationError{Field: league.ClaimFieldMotif, Err: league.ErrPersistenceIndeterminate},
	}
	for _, err := range tests {
		if got, ok := signupClaimField(err); ok || got != "" {
			t.Fatalf("signupClaimField(%v) = %q, %v; want empty, false", err, got, ok)
		}
	}
}

func TestSignupClaimValidationRetainsBothSubmittedValues(t *testing.T) {
	submitted := map[string]string{
		"team_name": "  My Retained Team  ",
		"motif":     "wolf",
	}
	ctx := &action.Context{FormData: submitted}
	claimErr := &league.ClaimValidationError{
		Field: league.ClaimFieldTeamName,
		Err:   errors.New("enter a team name"),
	}
	validation, ok := signupClaimValidation(ctx, claimErr).(*action.ResultError)
	if !ok {
		t.Fatalf("validation result = %T, want *action.ResultError", signupClaimValidation(ctx, claimErr))
	}
	result := validation.ActionResult()

	if got := result.FieldErrors["team_name"]; got != "enter a team name" {
		t.Fatalf("team_name field error = %q, want validation message", got)
	}
	if _, ok := result.FieldErrors["motif"]; ok {
		t.Fatal("team-name validation must not mark the badge field")
	}
	for key, want := range submitted {
		if got := result.Values[key]; got != want {
			t.Fatalf("retained %s = %q, want %q", key, got, want)
		}
	}
}

func TestSignupClaimValidationRetainsValuesForFormLevelFailure(t *testing.T) {
	submitted := map[string]string{"team_name": "Team", "motif": "wolf"}
	ctx := &action.Context{FormData: submitted}
	claimErr := &league.ClaimValidationError{
		Field: league.ClaimFieldMotif,
		Err:   league.ErrInternal,
	}
	validation, ok := signupClaimValidation(ctx, claimErr).(*action.ResultError)
	if !ok {
		t.Fatalf("validation result = %T, want *action.ResultError", signupClaimValidation(ctx, claimErr))
	}
	result := validation.ActionResult()
	if len(result.FieldErrors) != 0 {
		t.Fatalf("form-level failure produced field errors: %#v", result.FieldErrors)
	}
	for key, want := range submitted {
		if got := result.Values[key]; got != want {
			t.Fatalf("retained %s = %q, want %q", key, got, want)
		}
	}
}

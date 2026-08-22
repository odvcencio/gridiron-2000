package actionui

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
)

// memberSafeTestError is a stand-in MemberMessenger for tests. Its Error()
// text deliberately differs from its MemberMessage() text so a test that
// asserts on the wrong one fails loudly.
type memberSafeTestError struct{ msg string }

func (e *memberSafeTestError) Error() string         { return "unsafe internal detail: " + e.msg }
func (e *memberSafeTestError) MemberMessage() string { return e.msg }

// TestMessageInternalErrorReturnsFallbackAndLogsOriginal covers the one
// case where Message must not pass the error text through: an error that
// carries league.ErrInternal. The member reads FallbackMessage; the full
// original error still reaches the log for operators.
func TestMessageInternalErrorReturnsFallbackAndLogsOriginal(t *testing.T) {
	original := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(original)

	driverErr := errors.New("sql: no such table: teams")
	poisoned := fmt.Errorf("%w: %w", league.ErrInternal, fmt.Errorf("persist picks: %w", driverErr))

	got := Message("draft", poisoned)
	if got != FallbackMessage {
		t.Fatalf("Message = %q, want FallbackMessage %q", got, FallbackMessage)
	}
	if !strings.Contains(buf.String(), driverErr.Error()) {
		t.Fatalf("log output = %q, want it to contain the original error text %q", buf.String(), driverErr.Error())
	}
}

// TestMessageOrdinaryErrorPassesThroughUnchanged is phase 1's default: an
// error that is neither internal nor a MemberMessenger reaches the member
// exactly as written today.
func TestMessageOrdinaryErrorPassesThroughUnchanged(t *testing.T) {
	err := errors.New("that player has already been drafted")
	if got := Message("draft", err); got != err.Error() {
		t.Fatalf("Message = %q, want %q (unchanged)", got, err.Error())
	}
}

// TestMessageMemberMessengerYieldsItsOwnText covers a directly-typed
// MemberMessenger: Message must read MemberMessage(), not Error().
func TestMessageMemberMessengerYieldsItsOwnText(t *testing.T) {
	err := &memberSafeTestError{msg: "that badge is already claimed"}
	if got := Message("badges", err); got != "that badge is already claimed" {
		t.Fatalf("Message = %q, want the MemberMessage text", got)
	}
}

// TestMessageMemberMessengerWrappedByFmtErrorfStillResolves proves the
// errors.As traversal reaches a MemberMessenger through an intervening
// fmt.Errorf("%w", ...) wrap, the shape every real call site uses.
func TestMessageMemberMessengerWrappedByFmtErrorfStillResolves(t *testing.T) {
	inner := &memberSafeTestError{msg: "you already claimed a badge this week"}
	wrapped := fmt.Errorf("claim badge: %w", inner)
	if got := Message("badges", wrapped); got != inner.msg {
		t.Fatalf("Message = %q, want the wrapped MemberMessage text %q", got, inner.msg)
	}
}

// TestValidationBuildsFieldErrorFromMessage exercises the actual call-site
// shape: one field key carries the same safe message as Result.Message.
func TestValidationBuildsFieldErrorFromMessage(t *testing.T) {
	ctx := &action.Context{FormData: map[string]string{"blitz": "7"}}
	err := errors.New("that player has already been drafted")

	result := Validation(ctx, "blitz", "blitz", err)
	var resultErr *action.ResultError
	if !errors.As(result, &resultErr) {
		t.Fatalf("Validation returned %T, want *action.ResultError", result)
	}
	if resultErr.Result.Message != err.Error() {
		t.Fatalf("Result.Message = %q, want %q", resultErr.Result.Message, err.Error())
	}
	if resultErr.Result.FieldErrors["blitz"] != err.Error() {
		t.Fatalf("Result.FieldErrors[blitz] = %q, want %q", resultErr.Result.FieldErrors["blitz"], err.Error())
	}
	if resultErr.Result.Values["blitz"] != "7" {
		t.Fatalf("Result.Values[blitz] = %q, want ctx.FormData carried through", resultErr.Result.Values["blitz"])
	}
}

// TestValidationFieldsDerivesFieldsFromSafeMessage covers the irregular
// wire/sightingFieldErrors shape: the field-mapping callback receives the
// already-sanitized message, never the raw error.
func TestValidationFieldsDerivesFieldsFromSafeMessage(t *testing.T) {
	ctx := &action.Context{FormData: map[string]string{}}
	err := errors.New("summary is required")

	result := ValidationFields(ctx, "wire", err, func(message string) map[string]string {
		return map[string]string{"summary": message}
	})
	var resultErr *action.ResultError
	if !errors.As(result, &resultErr) {
		t.Fatalf("ValidationFields returned %T, want *action.ResultError", result)
	}
	if resultErr.Result.FieldErrors["summary"] != err.Error() {
		t.Fatalf("Result.FieldErrors[summary] = %q, want %q", resultErr.Result.FieldErrors["summary"], err.Error())
	}
}

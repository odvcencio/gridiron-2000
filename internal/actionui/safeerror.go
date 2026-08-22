package actionui

import (
	"errors"
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
)

// MemberMessenger marks an error whose text is safe for a league member to
// read. A type that implements it opts its own message text into Message,
// even when the error is also wrapped by fmt.Errorf("%w", ...) on its way
// up the call stack.
type MemberMessenger interface{ MemberMessage() string }

// FallbackMessage is what a member reads when an error carries no safe
// text: it wraps league.ErrInternal and implements no MemberMessenger.
const FallbackMessage = "Something went wrong on our side. Try again."

// Message returns the member-safe text for err. Phase 1's default is
// pass-through: err.Error() reaches the member unchanged, exactly as it
// does today, unless one of two conditions marks the error unsafe:
//
//   - err implements MemberMessenger (directly, or reachable through
//     errors.As across an intervening fmt.Errorf("%w", ...) wrap): Message
//     returns that type's own MemberMessage() text instead of Error().
//   - err carries league.ErrInternal (errors.Is): Message logs the full
//     error under surface, for operators, and returns FallbackMessage. The
//     member never sees a driver message or a filesystem path.
//
// surface names the page or feature for the log line; it carries no member
// meaning and is never shown.
func Message(surface string, err error) string {
	if err == nil {
		return ""
	}
	var messenger MemberMessenger
	if errors.As(err, &messenger) {
		return messenger.MemberMessage()
	}
	if errors.Is(err, league.ErrInternal) {
		log.Printf("%s action failed: %v", surface, err)
		return FallbackMessage
	}
	return err.Error()
}

// Validation builds a member-safe action.Validation result for a single
// form field. surface names the page for the log line (see Message); field
// is the form field key the error attaches to, matching the call site's
// existing map[string]string{field: message} shape.
func Validation(ctx *action.Context, surface, field string, err error) error {
	message := Message(surface, err)
	var formData map[string]string
	if ctx != nil {
		formData = ctx.FormData
	}
	return action.Validation(message, map[string]string{field: message}, formData)
}

// ValidationFields builds a member-safe action.Validation result whose
// field errors are derived from the already-sanitized message. fields
// receives that safe text, never the raw error, so a caller that maps one
// error onto several field keys (see app/wire's sightingFieldErrors) cannot
// reintroduce a leak through its own logic.
func ValidationFields(ctx *action.Context, surface string, err error, fields func(message string) map[string]string) error {
	message := Message(surface, err)
	var formData map[string]string
	if ctx != nil {
		formData = ctx.FormData
	}
	return action.Validation(message, fields(message), formData)
}

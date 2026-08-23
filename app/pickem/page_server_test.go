package pickem

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/action"
)

func TestPickemRedirectTargetCanonicalizesWeekAndRejectsHostileValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "selected week", raw: "2", want: "/pickem?week=2"},
		{name: "trimmed week", raw: " 02 ", want: "/pickem?week=2"},
		{name: "empty", raw: "", want: "/pickem"},
		{name: "zero", raw: "0", want: "/pickem"},
		{name: "negative", raw: "-3", want: "/pickem"},
		{name: "non numeric", raw: "week-two", want: "/pickem"},
		{name: "query injection", raw: "2&next=https://evil.example", want: "/pickem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickemRedirectTarget(tt.raw); got != tt.want {
				t.Fatalf("pickemRedirectTarget(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPickemValidationPreservesWeekForNativeFormsOnly(t *testing.T) {
	tests := []struct {
		name         string
		accept       string
		wantRedirect string
	}{
		{name: "native", wantRedirect: "/pickem?week=2"},
		{name: "managed", accept: "application/json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/pickem/__actions/pickem-set?week=2", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			ctx := &action.Context{
				Request:  req,
				FormData: map[string]string{"week": "2"},
			}

			err := pickemValidation(ctx, ctx.FormData["week"], errors.New("this game is locked"))
			result, ok := err.(*action.ResultError)
			if !ok {
				t.Fatalf("pickemValidation returned %T, want *action.ResultError", err)
			}
			if got := result.Result.Redirect; got != tt.wantRedirect {
				t.Fatalf("validation redirect = %q, want %q", got, tt.wantRedirect)
			}
			if got := result.Result.FieldErrors["pickem"]; got != "this game is locked" {
				t.Fatalf("validation field error = %q, want locked message", got)
			}
		})
	}
}

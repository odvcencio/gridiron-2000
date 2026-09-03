package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gridiron-2000/internal/actionui"
	"m31labs.dev/gosx/action"
)

func TestTeamIdentityReturnTargetKeepsEditorOpen(t *testing.T) {
	if got, want := teamIdentityReturnTarget, "/team?identity=edit#team-identity"; got != want {
		t.Fatalf("identity return target = %q, want %q", got, want)
	}
}

// TestTeamIdentityManagedSuccessStripsTheEditorAnchorAndOmitsReservedValue
// pins actionui's own wave 8 hotfix (item 2, commissioner: "moving
// players on my big board doesn't feel interactive, it resets the
// scroll"): a managed request's RedirectBackWithNotice always answers
// with the fragment-free fallback, never the section anchor
// teamIdentityReturnTarget itself carries — GoSX's runtime only
// preserves scroll when the JSON redirect has no "#...". See
// internal/actionui/feedback.go's own doc comment for the full
// mechanism; this pins the observable effect on this page's own
// return-target constant.
func TestTeamIdentityManagedSuccessStripsTheEditorAnchorAndOmitsReservedValue(t *testing.T) {
	values := url.Values{
		action.ReturnTargetField: {teamIdentityReturnTarget},
		"name":                   {"New Name"},
	}
	request := httptest.NewRequest(http.MethodPost, "/team/__actions/team-rename", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	action.ServeHandler(response, request, func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached Team identity action")
		}
		actionui.RedirectBackWithNotice(ctx, teamIdentityReturnTarget, "Team renamed.")
		return nil
	})

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	var result action.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	const wantRedirect = "/team?identity=edit"
	if !result.OK || result.Redirect != wantRedirect || result.Message != "Team renamed." {
		t.Fatalf("managed result = %+v, want fragment-free editor target %q and message", result, wantRedirect)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", result.Values)
	}
}

// TestTeamIdentityHostileReturnTargetFallsBackToEditor also covers a
// managed request throughout (Accept: application/json): the fallback's
// own anchor is stripped the same way regardless of what the (here,
// rejected) submitted return_to carried.
func TestTeamIdentityHostileReturnTargetFallsBackToEditor(t *testing.T) {
	values := url.Values{
		action.ReturnTargetField: {"//evil.example/steal"},
		"name":                   {"New Name"},
	}
	request := httptest.NewRequest(http.MethodPost, "/team/__actions/team-rename", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	action.ServeHandler(response, request, func(ctx *action.Context) error {
		actionui.RedirectBackWithNotice(ctx, teamIdentityReturnTarget, "Team renamed.")
		return nil
	})

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	var result action.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	const wantRedirect = "/team?identity=edit"
	if result.Redirect != wantRedirect {
		t.Fatalf("hostile managed redirect = %q, want %q", result.Redirect, wantRedirect)
	}
}

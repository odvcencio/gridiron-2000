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

func TestTeamIdentityManagedSuccessUsesSubmittedTargetAndOmitsReservedValue(t *testing.T) {
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
	if !result.OK || result.Redirect != teamIdentityReturnTarget || result.Message != "Team renamed." {
		t.Fatalf("managed result = %+v, want editor target and message", result)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", result.Values)
	}
}

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
	if result.Redirect != teamIdentityReturnTarget {
		t.Fatalf("hostile managed redirect = %q, want %q", result.Redirect, teamIdentityReturnTarget)
	}
}

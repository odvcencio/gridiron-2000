package blitz

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

func TestBlitzRedirectTargetKeepsSlateAndEntryBuilder(t *testing.T) {
	tests := map[string]string{
		"":                         "/blitz#blitz-entry",
		"pre2":                     "/blitz?slate=pre2#blitz-entry",
		" PRE3 ":                   "/blitz?slate=pre3#blitz-entry",
		"pre2&next=//evil.example": "/blitz#blitz-entry",
		"//evil.example/steal":     "/blitz#blitz-entry",
	}
	for raw, want := range tests {
		if got := blitzRedirectTarget(raw); got != want {
			t.Errorf("blitzRedirectTarget(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestBlitzManagedSuccessHostileReturnTargetFallsBackAndOmitsReservedValue(t *testing.T) {
	values := url.Values{
		action.ReturnTargetField: {"https://evil.example/steal"},
		"slate":                  {"pre2"},
		"player_id":              {"player-1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/blitz/__actions/blitz-add", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	action.ServeHandler(response, request, func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached Blitz action")
		}
		actionui.RedirectBackWithNotice(ctx, blitzRedirectTarget(ctx.FormData["slate"]), "Entry saved.")
		return nil
	})

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	var result action.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	want := "/blitz?slate=pre2#blitz-entry"
	if !result.OK || result.Redirect != want || result.Message != "Entry saved." {
		t.Fatalf("managed result = %+v, want bounded slate target %q", result, want)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", result.Values)
	}
}

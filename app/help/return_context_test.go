package help

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestReturnPathForRequestPreservesQueryAndFocus(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/team?team=team-2&week=3", nil)
	got := ReturnPathForRequest(request, "#lineup")
	want := "/team?team=team-2&week=3#lineup"
	if got != want {
		t.Fatalf("ReturnPathForRequest() = %q, want %q", got, want)
	}
	if got := ReturnPathForRequest(nil, "lineup"); got != "" {
		t.Fatalf("ReturnPathForRequest(nil) = %q, want empty", got)
	}
}

func TestContextualTopicURLCopiesParamsAndCarriesSafeReturn(t *testing.T) {
	params := url.Values{"field": []string{"validation"}}
	got := ContextualTopicURL("commissioner-operations", params, "/team?team=team-2&week=3#lineup")
	want := "/help/commissioner-operations?field=validation&return_to=%2Fteam%3Fteam%3Dteam-2%26week%3D3%23lineup"
	if got != want {
		t.Fatalf("ContextualTopicURL() = %q, want %q", got, want)
	}
	if _, present := params[ReturnToQuery]; present {
		t.Fatal("ContextualTopicURL mutated the caller's params")
	}

	if got := ContextualTopicURL("commissioner-operations", nil, "/"); got != "/help/commissioner-operations?return_to=%2F" {
		t.Fatalf("explicit root return target = %q, want encoded root", got)
	}
}

func TestContextualTopicURLDropsUnsafeReturnTargets(t *testing.T) {
	for _, raw := range []string{
		"https://evil.example/steal",
		"//evil.example/steal",
		"/login#again",
		"/auth/google/start#again",
		"/team/__actions/save#again",
		"/avatar/upload#again",
		"/team#bad%5Ctarget",
	} {
		t.Run(raw, func(t *testing.T) {
			if got := ContextualTopicURL("commissioner-operations", nil, raw); got != "/help/commissioner-operations" {
				t.Fatalf("ContextualTopicURL(%q) = %q, want no return_to", raw, got)
			}
		})
	}
}

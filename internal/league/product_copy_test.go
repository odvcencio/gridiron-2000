package league

import (
	"net/http"
	"testing"
)

func TestLoginDataBuildsSafeOAuthTarget(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/login?next=%2Fdraft%3Fweek%3D1", nil)
	data := service.LoginData(request, true)

	if got := data["oauth_start"]; got != "/auth/google/start?next=%2Fdraft%3Fweek%3D1" {
		t.Errorf("oauth_start = %q, want encoded draft target", got)
	}
	if got := data["return_path"]; got != "/draft?week=1" {
		t.Errorf("return_path = %q, want /draft?week=1", got)
	}
	if got := data["has_return_path"]; got != true {
		t.Errorf("has_return_path = %v, want true", got)
	}

	request, _ = http.NewRequest(http.MethodGet, "/login?next=https://evil.example/", nil)
	data = service.LoginData(request, true)
	if got := data["oauth_start"]; got != "/auth/google/start?next=%2F" {
		t.Errorf("external oauth_start = %q, want root fallback", got)
	}

	request, _ = http.NewRequest(http.MethodGet, "/login?next=%2Fdraft%3Fweek%3D1&next=https%3A%2F%2Fevil.example%2F", nil)
	data = service.LoginData(request, true)
	if got := data["oauth_start"]; got != "/auth/google/start?next=%2Fdraft%3Fweek%3D1" {
		t.Errorf("duplicate next oauth_start = %q, want first safe target only", got)
	}

	request, _ = http.NewRequest(http.MethodGet, "/login?next=https%3A%2F%2Fevil.example%2F&next=%2Fdraft", nil)
	data = service.LoginData(request, true)
	if got := data["oauth_start"]; got != "/auth/google/start?next=%2F" {
		t.Errorf("hostile-first duplicate oauth_start = %q, want root fallback", got)
	}
}

func TestLeagueMapFormatBlurbMatchesConfiguredFormat(t *testing.T) {
	service := newTestService(t, false)
	for mode, want := range map[string]string{
		"DYNASTY": "dynasty format",
		"REDRAFT": "redraft format",
		"":        "custom fantasy format",
	} {
		service.cfg.ModeLabel = mode
		if got := service.leagueMap()["format_blurb"]; got != want {
			t.Errorf("mode %q format_blurb = %q, want %q", mode, got, want)
		}
	}
}

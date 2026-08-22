package actionui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
)

func TestRedirectWithNoticeReturnsOneManagedResultMessage(t *testing.T) {
	registry := action.NewRegistry()
	registry.Register("save", func(ctx *action.Context) error {
		RedirectWithNotice(ctx, "/team", "  Lineup saved.  ")
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/__actions/save", strings.NewReader("team_id=team-1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("name", "save")
	res := httptest.NewRecorder()
	registry.ServeHTTP(res, req)

	if res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusSeeOther)
	}
	var result action.Result
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Message != "Lineup saved." || result.Redirect != "/team" {
		t.Fatalf("result = %+v", result)
	}

	RedirectWithNotice(nil, "/team", "ignored")
}

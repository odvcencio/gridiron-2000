package actionui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
)

func TestRedirectWithNoticeReturnsOneManagedResultMessage(t *testing.T) {
	registry := action.NewRegistry()
	registry.Register("save", func(ctx *action.Context) error {
		RedirectWithNotice(ctx, "/team", "  Lineup saved.  ")
		return nil
	})
	manager, err := session.New("actionui-managed-session-secret", session.Options{CookieName: "actionui_managed", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/__actions/save", strings.NewReader("team_id=team-1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("name", "save")
	res := httptest.NewRecorder()
	manager.Middleware(registry).ServeHTTP(res, req)

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
	if cookies := res.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("managed action wrote %d session cookie(s); custom notice would duplicate the structured message", len(cookies))
	}

	RedirectWithNotice(nil, "/team", "ignored")
}

func TestRedirectWithNoticePreservesNativeFlash(t *testing.T) {
	registry := action.NewRegistry()
	registry.Register("save", func(ctx *action.Context) error {
		RedirectWithNotice(ctx, "/team", "  Lineup saved.  ")
		return nil
	})
	manager, err := session.New("actionui-native-session-secret", session.Options{CookieName: "actionui_native", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := manager.Middleware(registry)

	req := httptest.NewRequest(http.MethodPost, "/__actions/save", strings.NewReader("team_id=team-1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.SetPathValue("name", "save")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusSeeOther)
	}
	if location := res.Header().Get("Location"); location != "/team" {
		t.Fatalf("Location = %q, want /team", location)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("native action wrote %d cookies, want 1 notice session", len(cookies))
	}

	readReq := httptest.NewRequest(http.MethodGet, "/team", nil)
	readReq.AddCookie(cookies[0])
	readRes := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flashes := session.Current(r).Flashes("notice")
		if len(flashes) != 1 || flashes[0] != "Lineup saved." {
			t.Fatalf("notice flashes = %#v, want one trimmed notice", flashes)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(readRes, readReq)
	if readRes.Code != http.StatusNoContent {
		t.Fatalf("read status = %d, want %d", readRes.Code, http.StatusNoContent)
	}
}

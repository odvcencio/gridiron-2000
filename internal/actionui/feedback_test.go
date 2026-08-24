package actionui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestRedirectBackWithNoticeManagedResultUsesSubmittedTargetAndKeepsReservedFieldPrivate(t *testing.T) {
	registry := action.NewRegistry()
	registry.Register("save", func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached the managed action handler")
		}
		RedirectBackWithNotice(ctx, "/fallback", "  Board saved.  ")
		return nil
	})
	manager := session.MustNew("actionui-back-managed-secret", session.Options{
		CookieName:    "actionui_back_managed",
		AllowInsecure: true,
	})
	returnTarget := "/board?q=Tom+%26+%2F&page=3#board-pool"
	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"name":                   {"Ada"},
	}
	req := httptest.NewRequest(http.MethodPost, "/__actions/save", strings.NewReader(values.Encode()))
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
	if !result.OK || result.Message != "Board saved." || result.Redirect != returnTarget {
		t.Fatalf("result = %+v, want managed return target %q", result, returnTarget)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", result.Values)
	}
	if cookies := res.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("managed action wrote %d session cookie(s); native notice must not duplicate the structured message", len(cookies))
	}
}

func TestRedirectBackWithNoticeNativePRGUsesSubmittedTargetAndPreservesNativeFlash(t *testing.T) {
	registry := action.NewRegistry()
	registry.Register("save", func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached the native action handler")
		}
		RedirectBackWithNotice(ctx, "/fallback", "  Announcement posted.  ")
		return nil
	})
	manager := session.MustNew("actionui-back-native-secret", session.Options{
		CookieName:    "actionui_back_native",
		AllowInsecure: true,
	})
	returnTarget := "/admin?section=announcements#admin-announcements"
	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"name":                   {"Announcement"},
	}
	req := httptest.NewRequest(http.MethodPost, "/__actions/save", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.SetPathValue("name", "save")
	postRes := httptest.NewRecorder()
	handler := manager.Middleware(registry)
	handler.ServeHTTP(postRes, req)

	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", postRes.Code, http.StatusSeeOther)
	}
	if got := postRes.Header().Get("Location"); got != returnTarget {
		t.Fatalf("Location = %q, want %q", got, returnTarget)
	}
	cookies := postRes.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("native action wrote %d cookies, want one session cookie", len(cookies))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin?section=announcements", nil)
	getReq.AddCookie(cookies[0])
	getRes := httptest.NewRecorder()
	handler = manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := session.Current(r)
		if store == nil {
			t.Fatal("native GET missing session")
		}
		flashes := store.Flashes("notice")
		if len(flashes) != 1 || flashes[0] != "Announcement posted." {
			t.Fatalf("notice flashes = %#v, want one trimmed notice", flashes)
		}
		view, ok := action.State(r, "save")
		if !ok {
			t.Fatal("native action did not flash redirect-back state")
		}
		if view.Message() != "Announcement posted." || view.Redirect() != returnTarget {
			t.Fatalf("flashed view = %+v, want message and target", view)
		}
		if _, ok := view.Result.Values[action.ReturnTargetField]; ok {
			t.Fatalf("reserved return target leaked into native values: %#v", view.Result.Values)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want %d", getRes.Code, http.StatusNoContent)
	}
}

func TestRedirectBackWithNoticeManagedFallbackRejectsAbsentAndHostileTargets(t *testing.T) {
	tests := []struct {
		name      string
		submitted string
		fallback  string
		want      string
	}{
		{
			name:     "absent target keeps admin anchor",
			fallback: "/admin?section=draft-order#admin-draft-order",
			want:     "/admin?section=draft-order#admin-draft-order",
		},
		{
			name:      "hostile target keeps board fallback",
			submitted: "//evil.example/steal",
			fallback:  "/board?q=Tom+%26+%2F&page=3#board-pool",
			want:      "/board?q=Tom+%26+%2F&page=3#board-pool",
		},
		{
			name:      "invalid fallback is safe root",
			submitted: "https://evil.example/steal",
			fallback:  "https://evil.example/fallback",
			want:      "/",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := url.Values{"name": {"Ada"}}
			if test.submitted != "" {
				values.Set(action.ReturnTargetField, test.submitted)
			}
			req := httptest.NewRequest(http.MethodPost, "/__actions/save", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "application/json")
			req.SetPathValue("name", "save")
			res := httptest.NewRecorder()
			action.ServeHandler(res, req, func(ctx *action.Context) error {
				RedirectBackWithNotice(ctx, test.fallback, "saved")
				return nil
			})
			if res.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", res.Code, http.StatusSeeOther)
			}
			var result action.Result
			if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result.Redirect != test.want {
				t.Fatalf("redirect = %q, want %q", result.Redirect, test.want)
			}
		})
	}
}

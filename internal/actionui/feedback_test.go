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

// TestRedirectBackWithNoticeManagedResultUsesFragmentFreeFallbackAndKeepsReservedFieldPrivate
// pins the wave 8 hotfix (item 2, commissioner: "moving players on my big
// board doesn't feel interactive, it resets the scroll"). Before the fix,
// a managed request's submitted return_to unconditionally won over
// fallback, fragment and all: GoSX's runtime only preserves scroll when
// the JSON "redirect" carries no "#..." (client/runtime/host/
// navigation.ts), so every managed save re-scrolled the page to whatever
// section anchor the return_to carried. RedirectBackWithNotice now always
// answers a managed request with the fragment-stripped fallback — the
// gosx/action package exposes no way to read the submitted return_to back
// out with only its own fragment removed, and every real caller builds
// fallback from the identical per-page state (pos/q/page, an admin
// section id, ...) that produced the return_to hidden field in the first
// place, so the two already agree on destination up to the anchor this
// test's own submitted target still carries.
func TestRedirectBackWithNoticeManagedResultUsesFragmentFreeFallbackAndKeepsReservedFieldPrivate(t *testing.T) {
	registry := action.NewRegistry()
	registry.Register("save", func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached the managed action handler")
		}
		RedirectBackWithNotice(ctx, "/board?q=Tom+%26+%2F&page=3", "  Board saved.  ")
		return nil
	})
	manager := session.MustNew("actionui-back-managed-secret", session.Options{
		CookieName:    "actionui_back_managed",
		AllowInsecure: true,
	})
	// The submitted return_to mirrors production symmetry: the same
	// path/query as fallback, plus the section anchor the page rendered it
	// with. A managed redirect must land on fallback's own fragment-free
	// form regardless.
	returnTarget := "/board?q=Tom+%26+%2F&page=3#board-pool"
	want := "/board?q=Tom+%26+%2F&page=3"
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
	if !result.OK || result.Message != "Board saved." || result.Redirect != want {
		t.Fatalf("result = %+v, want fragment-free fallback %q", result, want)
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

// TestRedirectBackWithNoticeManagedFallbackRejectsAbsentAndHostileTargets
// covers a managed request (Accept: application/json) throughout: every
// "want" strips the fallback's own section anchor too (item 2, wave 8
// hotfix) since a managed redirect answers with the fragment-free fallback
// unconditionally, never the request's own submitted return_to.
func TestRedirectBackWithNoticeManagedFallbackRejectsAbsentAndHostileTargets(t *testing.T) {
	tests := []struct {
		name      string
		submitted string
		fallback  string
		want      string
	}{
		{
			name:     "absent target still strips the admin anchor",
			fallback: "/admin?section=draft-order#admin-draft-order",
			want:     "/admin?section=draft-order",
		},
		{
			name:      "hostile submitted target still strips the board fallback's anchor",
			submitted: "//evil.example/steal",
			fallback:  "/board?q=Tom+%26+%2F&page=3#board-pool",
			want:      "/board?q=Tom+%26+%2F&page=3",
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

// TestRedirectBackWithNoticeManagedRequestStripsSectionAnchor is item 2's
// test (a): a managed request (Accept: application/json) whose submitted
// return_to carries "#board-pool", posted alongside a fallback the calling
// page built from the same pos/q filter state, redirects to the
// fragment-free path+query. Without this fix GoSX's runtime re-scrolled
// the Big Board to #board-pool on every save (client/runtime/host/
// navigation.ts only preserves scroll when the JSON "redirect" has no
// hash) — the commissioner's "moving players on my big board doesn't feel
// interactive, it resets the scroll".
func TestRedirectBackWithNoticeManagedRequestStripsSectionAnchor(t *testing.T) {
	values := url.Values{
		action.ReturnTargetField: {"/board?pos=RB&page=2#board-pool"},
		"pos":                    {"RB"},
	}
	req := httptest.NewRequest(http.MethodPost, "/__actions/board-move", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("name", "board-move")
	res := httptest.NewRecorder()
	action.ServeHandler(res, req, func(ctx *action.Context) error {
		RedirectBackWithNotice(ctx, "/board?pos="+ctx.FormData["pos"], "Board order updated.")
		return nil
	})

	if res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusSeeOther)
	}
	var result action.Result
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Redirect != "/board?pos=RB" {
		t.Fatalf("Redirect = %q, want %q", result.Redirect, "/board?pos=RB")
	}
}

// TestRedirectBackWithNoticePlainRequestKeepsSectionAnchor is item 2's test
// (b): a plain (no-JS) form POST is a full-page native navigation, where
// landing on the section anchor is the wanted, pre-existing behavior — the
// browser's own hash handling scrolls the reloaded page to it once. This
// path stays on RedirectBackWithMessage untouched.
func TestRedirectBackWithNoticePlainRequestKeepsSectionAnchor(t *testing.T) {
	values := url.Values{
		action.ReturnTargetField: {"/board?pos=RB&page=2#board-pool"},
		"pos":                    {"RB"},
	}
	req := httptest.NewRequest(http.MethodPost, "/__actions/board-move", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.SetPathValue("name", "board-move")
	res := httptest.NewRecorder()
	action.ServeHandler(res, req, func(ctx *action.Context) error {
		RedirectBackWithNotice(ctx, "/board?pos="+ctx.FormData["pos"], "Board order updated.")
		return nil
	})

	if res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusSeeOther)
	}
	if got := res.Header().Get("Location"); got != "/board?pos=RB&page=2#board-pool" {
		t.Fatalf("Location = %q, want submitted target with anchor kept", got)
	}
}

// TestRedirectWithNoticeManagedRequestStripsExplicitTargetAnchor covers the
// sibling helper (item 2's "any sibling helper" note): explicit-target
// callers like teamLineupTarget ("...#lineup") and waiverRedirectTarget
// ("...#waivers") carry the identical section-anchor pattern, so a managed
// request must strip it there too, while a plain request still lands on
// the anchor.
func TestRedirectWithNoticeManagedRequestStripsExplicitTargetAnchor(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/__actions/lineup-set", strings.NewReader("slot=WR1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("name", "lineup-set")
	res := httptest.NewRecorder()
	action.ServeHandler(res, req, func(ctx *action.Context) error {
		RedirectWithNotice(ctx, "/team?week=3#lineup", "Lineup set.")
		return nil
	})

	if res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusSeeOther)
	}
	var result action.Result
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Redirect != "/team?week=3" {
		t.Fatalf("Redirect = %q, want %q", result.Redirect, "/team?week=3")
	}

	plainReq := httptest.NewRequest(http.MethodPost, "/__actions/lineup-set", strings.NewReader("slot=WR1"))
	plainReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	plainReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	plainReq.SetPathValue("name", "lineup-set")
	plainRes := httptest.NewRecorder()
	action.ServeHandler(plainRes, plainReq, func(ctx *action.Context) error {
		RedirectWithNotice(ctx, "/team?week=3#lineup", "Lineup set.")
		return nil
	})
	if got := plainRes.Header().Get("Location"); got != "/team?week=3#lineup" {
		t.Fatalf("plain Location = %q, want anchor kept", got)
	}
}

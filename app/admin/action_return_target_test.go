package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gridiron-2000/internal/actionui"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
)

func TestAdminDraftOrderRedirectBackUsesSubmittedAnchorManaged(t *testing.T) {
	returnTarget := adminSectionTarget("draft-order")
	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"confirm":                {"REDRAW ORDER"},
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/__actions/order-randomize", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.SetPathValue("name", "order-randomize")
	response := httptest.NewRecorder()

	action.ServeHandler(response, request, func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached the Admin action handler")
		}
		actionui.RedirectBackWithNotice(ctx, adminSectionTarget("draft-order"), "Order drawn.")
		return nil
	})

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	var result action.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Message != "Order drawn." || result.Redirect != returnTarget {
		t.Fatalf("result = %+v, want managed draft-order target %q", result, returnTarget)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", result.Values)
	}
}

func TestAdminAnnouncementRedirectBackUsesSubmittedAnchorNative(t *testing.T) {
	returnTarget := adminSectionTarget("announcements")
	manager := session.MustNew("admin-return-target-native-secret", session.Options{
		CookieName:    "admin_return_target_native",
		AllowInsecure: true,
	})
	handler := manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			action.ServeHandler(writer, request, func(ctx *action.Context) error {
				if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
					t.Fatal("reserved return target reached the Announcement action handler")
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("announcements"), "Announcement posted.")
				return nil
			})
			return
		}

		store := session.Current(request)
		if store == nil {
			t.Fatal("native Admin GET missing session")
		}
		flashes := store.Flashes("notice")
		if len(flashes) != 1 || flashes[0] != "Announcement posted." {
			t.Fatalf("notice flashes = %#v, want one trimmed notice", flashes)
		}
		view, ok := action.State(request, "announcement-post")
		if !ok {
			t.Fatal("native announcement action did not flash redirect-back state")
		}
		if view.Redirect() != returnTarget || view.Message() != "Announcement posted." {
			t.Fatalf("flashed view = %+v, want anchor and message", view)
		}
		if _, ok := view.Result.Values[action.ReturnTargetField]; ok {
			t.Fatalf("reserved return target leaked into native values: %#v", view.Result.Values)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"body":                   {"The draft room opens soon."},
	}
	postRequest := httptest.NewRequest(http.MethodPost, "/admin/__actions/announcement-post", strings.NewReader(values.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Accept", "text/html,application/xhtml+xml")
	postRequest.SetPathValue("name", "announcement-post")
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)

	if postResponse.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", postResponse.Code)
	}
	if got := postResponse.Header().Get("Location"); got != returnTarget {
		t.Fatalf("Location = %q, want submitted announcement target %q", got, returnTarget)
	}
	cookies := postResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("POST wrote %d session cookies, want one", len(cookies))
	}
	getRequest := httptest.NewRequest(http.MethodGet, "/admin?section=announcements", nil)
	getRequest.AddCookie(cookies[0])
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getResponse.Code)
	}
}

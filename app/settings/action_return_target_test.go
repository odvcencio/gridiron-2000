package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/auth"
)

// TestSettingsNotificationSetRedirectsToOwnFieldsetFragment pins F9
// (2026-09-04 UX pass): saving a category used to redirect to the bare
// "/settings", throwing a manager back to the top of a six-screen page.
// It now redirects to that category's own fieldset id
// (NotificationRow, page.gsx, "notify-"+Category) — kept for a native
// form post, stripped for a managed one, exactly like every other
// RedirectWithNotice call site (internal/actionui/feedback.go).
func TestSettingsNotificationSetRedirectsToOwnFieldsetFragment(t *testing.T) {
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "settings-fragment-manager", Email: "settings-fragment-manager@example.com"}, true
	})})

	tests := []struct {
		name   string
		accept string
		want   string
	}{
		{name: "native keeps the fragment", accept: "", want: "/settings#notify-draft_reminders"},
		{name: "managed strips the fragment", accept: "application/json", want: "/settings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/settings/__actions/notification-set", strings.NewReader("category=draft_reminders&enabled=false"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
			}
			recorder := httptest.NewRecorder()
			authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				action.ServeHandler(w, r, setNotificationPreference)
			})).ServeHTTP(recorder, request)

			if recorder.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303; body=%s", recorder.Code, recorder.Body.String())
			}
			if tt.accept == "" {
				if got := recorder.Header().Get("Location"); got != tt.want {
					t.Fatalf("native Location = %q, want %q", got, tt.want)
				}
				return
			}
			var result action.Result
			if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
				t.Fatalf("decode managed result: %v", err)
			}
			if result.Redirect != tt.want {
				t.Fatalf("managed Redirect = %q, want %q", result.Redirect, tt.want)
			}
		})
	}
}

// TestSettingsNotificationSetConfirmationNamesCategoryAndState pins the
// other half of F9: a manager saving several of the ten toggles in a row
// must be able to tell which one just changed from the confirmation
// alone, instead of the former generic "Notification preference saved."
func TestSettingsNotificationSetConfirmationNamesCategoryAndState(t *testing.T) {
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "settings-flash-manager", Email: "settings-flash-manager@example.com"}, true
	})})
	request := httptest.NewRequest(http.MethodPost, "/settings/__actions/notification-set", strings.NewReader("category=league_news&enabled=false"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action.ServeHandler(w, r, setNotificationPreference)
	})).ServeHTTP(recorder, request)

	var result action.Result
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("decode managed result: %v", err)
	}
	if !strings.HasPrefix(result.Message, "League news is now OFF.") {
		t.Fatalf("message = %q, want it to name the category (\"League news\") and its new state (\"OFF\")", result.Message)
	}
}

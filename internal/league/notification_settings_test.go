package league

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"gridiron-2000/internal/notify"
	"m31labs.dev/gosx/auth"
)

func TestNotificationSettingsDataExposesCatalogCategoryCounts(t *testing.T) {
	service := newTestService(t, true)
	data := service.NotificationSettingsData(httptest.NewRequest(http.MethodGet, "/settings", nil))

	live, ok := data["preferences"].([]NotificationPreference)
	if !ok {
		t.Fatalf("preferences type = %T, want []NotificationPreference", data["preferences"])
	}
	liveCount, ok := data["live_category_count"].(int)
	if !ok {
		t.Fatalf("live_category_count type = %T, want int", data["live_category_count"])
	}
	if liveCount != len(live) || liveCount != len(notificationPreferenceCategories) {
		t.Fatalf("live category counts = data:%d rows:%d catalog:%d", liveCount, len(live), len(notificationPreferenceCategories))
	}
	for key, want := range map[string]int{"draft_preferences": 3, "weekly_preferences": 4, "league_preferences": 3} {
		group, ok := data[key].([]NotificationPreference)
		if !ok || len(group) != want {
			t.Errorf("%s = %T len %d, want []NotificationPreference len %d", key, data[key], len(group), want)
		}
	}
	for _, preference := range live {
		if preference.Planned {
			t.Errorf("live preference %q = %+v, want not planned", preference.Category, preference)
		}
	}
	planned, ok := data["planned_preferences"].([]NotificationPreference)
	if !ok {
		t.Fatalf("planned preferences type = %T, want []NotificationPreference", data["planned_preferences"])
	}
	plannedCount, ok := data["planned_category_count"].(int)
	if !ok {
		t.Fatalf("planned_category_count type = %T, want int", data["planned_category_count"])
	}
	if plannedCount != len(planned) {
		t.Fatalf("planned category counts = data:%d rows:%d", plannedCount, len(planned))
	}
	gotPlanned := make([]string, 0, len(planned))
	for _, preference := range planned {
		gotPlanned = append(gotPlanned, preference.Category)
		if preference.CanEdit || !preference.Planned || preference.State != "PLANNED" {
			t.Errorf("planned preference %q = %+v, want non-editable PLANNED", preference.Category, preference)
		}
	}
	if !reflect.DeepEqual(gotPlanned, []string{}) {
		t.Fatalf("planned categories = %#v, want none after NT-1", gotPlanned)
	}
	if data["read_only"] != true || data["demo_mode"] != true {
		t.Fatalf("demo read-only flags = read_only:%v demo_mode:%v", data["read_only"], data["demo_mode"])
	}
}

func TestSetNotificationPreferenceRejectsUnsignedUnknownAndNonLiteralInputs(t *testing.T) {
	service := newTestService(t, false)
	before := service.store.Snapshot()
	if err := service.SetNotificationPreference(httptest.NewRequest(http.MethodPost, "/settings", nil), categoryDraftLive, false); err == nil {
		t.Fatal("unsigned preference write unexpectedly succeeded")
	}

	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "manager", Email: "manager@example.com"}, true
	})})
	for _, category := range []string{"unknown", " " + categoryDraftLive} {
		var setErr error
		authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setErr = service.SetNotificationPreference(r, category, false)
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/settings", nil))
		if setErr == nil {
			t.Errorf("category %q unexpectedly succeeded", category)
		}
	}
	var setWeeklyErr error
	authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setWeeklyErr = service.SetNotificationPreference(r, categoryWeeklyRecap, false)
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/settings", nil))
	if setWeeklyErr != nil {
		t.Fatalf("live weekly recap preference rejected: %v", setWeeklyErr)
	}
	after := service.store.Snapshot()
	// The accepted weekly-recap write is the only expected difference;
	// the rejected categories above must not create any other preference.
	if got, ok := after.NotifyPrefs["manager@example.com"][categoryWeeklyRecap]; !ok || got {
		t.Fatalf("weekly recap override = (%v,%v), want stored false", got, ok)
	}
	if len(after.NotifyPrefs) != 1 || len(after.NotifyPrefs["manager@example.com"]) != 1 {
		t.Fatalf("unexpected state after category validation: %#v (before %#v)", after.NotifyPrefs, before.NotifyPrefs)
	}
}

func TestSetNotifyPrefUsesCanonicalAuthenticatedIdentity(t *testing.T) {
	service := newTestService(t, false)
	service.identityResolver = testIdentityResolver(t)
	service.store.identityResolver = service.identityResolver
	canonical := identityCanonicalEmail
	if _, _, err := service.store.AssignMember(canonical, "Canonical"); err != nil {
		t.Fatal(err)
	}

	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: identityAliasEmail, Email: identityAliasEmail, Name: "Alias"}, true
	})})
	var setErr error
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setErr = service.SetNotificationPreference(r, categoryDraftLive, false)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/settings", nil))
	if setErr != nil {
		t.Fatalf("SetNotificationPreference(alias) = %v", setErr)
	}
	state := service.store.Snapshot()
	if got, ok := state.NotifyPrefs[canonical][categoryDraftLive]; !ok || got {
		t.Fatalf("canonical preference = (%v,%v), want stored false", got, ok)
	}
	if _, ok := state.NotifyPrefs[identityAliasEmail]; ok {
		t.Fatalf("alias preference key survived: %#v", state.NotifyPrefs)
	}
	if service.notifyEnabled(state, canonical, categoryDraftLive) {
		t.Fatal("disabled preference did not suppress draft-live delivery")
	}
	var enableErr error
	handler = authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enableErr = service.SetNotificationPreference(r, categoryDraftLive, true)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/settings", nil))
	if enableErr != nil {
		t.Fatalf("SetNotificationPreference(enable) = %v", enableErr)
	}
	if !service.notifyEnabled(service.store.Snapshot(), canonical, categoryDraftLive) {
		t.Fatal("re-enabled preference did not restore draft-live delivery")
	}
}

func TestSetNotifyPrefDemoIsReadOnly(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodPost, "/settings", nil)
	before := service.store.Snapshot()
	if err := service.SetNotificationPreference(request, categoryBroadcast, false); err == nil {
		t.Fatal("demo SetNotificationPreference unexpectedly succeeded")
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("demo preference write changed state\nbefore: %#v\nafter: %#v", before, after)
	}
}

func TestNotificationSettingsDataReportsDeliveryReadinessHonestly(t *testing.T) {
	service := newTestService(t, false)
	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	data := service.NotificationSettingsData(request)
	if data["delivery_ready"] != false || data["delivery_message"] != "Email delivery is not configured. Your choices are saved and take effect if the commissioner enables email." {
		t.Fatalf("disabled delivery status = ready:%v message:%q", data["delivery_ready"], data["delivery_message"])
	}
	service.notifyQueue = &notify.Queue{}
	service.notifyTransportEnabled = true
	data = service.NotificationSettingsData(request)
	if data["delivery_ready"] != true || data["delivery_message"] != "Email delivery is configured. Changes apply to future alerts; mail already queued may still arrive." {
		t.Fatalf("ready delivery status = ready:%v message:%q", data["delivery_ready"], data["delivery_message"])
	}
}

func TestNotificationSettingsDataReadsCanonicalOverride(t *testing.T) {
	resolver := testIdentityResolver(t)
	service := newTestService(t, false)
	service.identityResolver = resolver
	service.store.identityResolver = resolver
	if err := service.store.SetNotifyPref(identityCanonicalEmail, categoryTransactions, false); err != nil {
		t.Fatal(err)
	}

	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: identityAliasEmail, Email: identityAliasEmail}, true
	})})
	var data map[string]any
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data = service.NotificationSettingsData(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/settings", nil))
	live, ok := data["preferences"].([]NotificationPreference)
	if !ok {
		t.Fatalf("preferences type = %T", data["preferences"])
	}
	for _, preference := range live {
		if preference.Category == categoryTransactions && preference.Enabled {
			t.Fatalf("canonical transactions override was not read: %+v", preference)
		}
	}
	if data["email"] != identityCanonicalEmail || data["read_only"] != false {
		t.Fatalf("identity/read-only data = email:%v read_only:%v", data["email"], data["read_only"])
	}
}

// TestNotificationCategoryLabelNamesKnownCategoriesAndFallsBack pins F9
// (2026-09-04 UX pass): the settings action's own saved-preference
// confirmation names the category ("Draft reminders is now OFF.") instead
// of leaving a manager to guess which of ten toggles just changed.
func TestNotificationCategoryLabelNamesKnownCategoriesAndFallsBack(t *testing.T) {
	tests := map[string]string{
		categoryDraftReminders: "Draft reminders",
		categoryLeagueNews:     "League news",
		categoryTransactions:   "Transactions",
		"unknown-category":     "unknown-category",
	}
	for category, want := range tests {
		if got := NotificationCategoryLabel(category); got != want {
			t.Errorf("NotificationCategoryLabel(%q) = %q, want %q", category, got, want)
		}
	}
}

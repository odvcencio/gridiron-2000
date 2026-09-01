package league

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
)

func TestStoreRecordCommissionerEventPersistsAndSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	store.draftLifecycleBypass = true
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	event, err := store.RecordCommissionerEvent(
		"alex@example.com", "Alex", "announcement.post", "posted an announcement",
		CommissionerEventRefs{TeamID: "team-1"}, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID == "" || event.ActorEmail != "alex@example.com" || event.ActorName != "Alex" {
		t.Fatalf("recorded event = %+v", event)
	}
	if event.Kind != "announcement.post" || event.Summary != "posted an announcement" {
		t.Fatalf("recorded event kind/summary = %+v", event)
	}
	if event.Refs.TeamID != "team-1" || event.At.IsZero() {
		t.Fatalf("recorded event refs/at = %+v", event)
	}

	got := store.Snapshot().CommissionerEvents
	if len(got) != 1 || got[0].ID != event.ID {
		t.Fatalf("CommissionerEvents = %+v, want one row matching %+v", got, event)
	}

	// The event must survive a fresh Store pointed at the same database —
	// the same reload proof PostLocker's own persistence tests apply, and
	// the collectionSpecs entry must round-trip every field, not just ID.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	t.Cleanup(func() { _ = reloaded.Close() })
	if err := reloaded.StartupError(); err != nil {
		t.Fatal(err)
	}
	reloadedEvents := reloaded.Snapshot().CommissionerEvents
	if len(reloadedEvents) != 1 {
		t.Fatalf("reloaded CommissionerEvents = %+v, want 1", reloadedEvents)
	}
	got0 := reloadedEvents[0]
	if got0.ID != event.ID || got0.ActorEmail != event.ActorEmail || got0.ActorName != event.ActorName ||
		got0.Kind != event.Kind || got0.Summary != event.Summary || got0.Refs != event.Refs ||
		!got0.At.Equal(event.At) {
		t.Fatalf("reloaded event = %+v, want %+v", got0, event)
	}
}

func TestStoreRecordCommissionerEventValidatesRequiredFields(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	if _, err := store.RecordCommissionerEvent("", "Alex", "announcement.post", "posted an announcement", CommissionerEventRefs{}, now); err == nil {
		t.Fatal("an empty actor email was accepted")
	}
	if _, err := store.RecordCommissionerEvent("alex@example.com", "Alex", "  ", "posted an announcement", CommissionerEventRefs{}, now); err == nil {
		t.Fatal("an empty kind was accepted")
	}
	if _, err := store.RecordCommissionerEvent("alex@example.com", "Alex", "announcement.post", "   ", CommissionerEventRefs{}, now); err == nil {
		t.Fatal("an empty summary was accepted")
	}
	if len(store.Snapshot().CommissionerEvents) != 0 {
		t.Fatal("a rejected call left a row behind")
	}
}

func TestStoreRecordCommissionerEventAppendsInOrder(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if _, err := store.RecordCommissionerEvent("alex@example.com", "Alex", "announcement.post", "posted an announcement", CommissionerEventRefs{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordCommissionerEvent("alex@example.com", "Alex", "announcement.delete", "deleted an announcement", CommissionerEventRefs{}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot().CommissionerEvents
	if len(got) != 2 || got[0].Kind != "announcement.post" || got[1].Kind != "announcement.delete" {
		t.Fatalf("CommissionerEvents = %+v, want append order preserved", got)
	}
}

// commissionerRequest builds a request middleware-authenticated as email
// (with display name), mirroring commissioner_identity_test.go's own
// commissionerForEmail helper.
func commissionerRequest(email, name string) *http.Request {
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: name}, true
		}),
	})
	var captured *http.Request
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/commissioner", nil))
	return captured
}

func TestServiceRecordCommissionerEventRequiresCommissioner(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("COMMISSIONER_EMAILS", "commissioner@example.com")

	request := commissionerRequest("not-the-commissioner@example.com", "Not Alex")
	if _, err := service.RecordCommissionerEvent(request, "announcement.post", "posted an announcement", CommissionerEventRefs{}); err == nil {
		t.Fatal("a non-commissioner request was accepted")
	}
	if len(service.store.Snapshot().CommissionerEvents) != 0 {
		t.Fatal("a rejected request left a row behind")
	}
}

func TestServiceRecordCommissionerEventAttributesThePerson(t *testing.T) {
	service := newTestService(t, false)
	t.Setenv("COMMISSIONER_EMAILS", "commissioner@example.com")

	request := commissionerRequest("commissioner@example.com", "Alex Rivera")
	event, err := service.RecordCommissionerEvent(request, "announcement.post", "posted an announcement", CommissionerEventRefs{Week: 3})
	if err != nil {
		t.Fatal(err)
	}
	if event.ActorEmail != "commissioner@example.com" || event.ActorName != "Alex Rivera" {
		t.Fatalf("event actor = %+v, want the person's identity, not a seat code", event)
	}
	if event.Kind != "announcement.post" || event.Summary != "posted an announcement" || event.Refs.Week != 3 {
		t.Fatalf("event = %+v", event)
	}
	if got := service.store.Snapshot().CommissionerEvents; len(got) != 1 {
		t.Fatalf("CommissionerEvents = %+v, want 1", got)
	}
}

func TestServiceRecordCommissionerEventDemoModeAttributesStableIdentity(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodGet, "/commissioner", nil)
	event, err := service.RecordCommissionerEvent(request, "reset.league", "reset the league", CommissionerEventRefs{})
	if err != nil {
		t.Fatal(err)
	}
	if event.ActorEmail == "" || event.ActorName == "" {
		t.Fatalf("demo-mode event carries no actor identity: %+v", event)
	}
}

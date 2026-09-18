package league

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSetPushSubscriptionStoresOnePerEndpointAndSurvivesReload pins the
// store contract: a member's push subscriptions are keyed by endpoint, a
// repeat registration replaces the keys, removal is a no-op for an
// unknown endpoint, ClearPushSubscriptions drops a member's whole set,
// and the set persists through the additive kv set.
func TestSetPushSubscriptionStoresOnePerEndpointAndSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	sub := PushSubscription{Endpoint: "https://push.example/abc", P256dh: "p", Auth: "a", UserAgent: "Chrome"}
	if err := store.SetPushSubscription("ash@example.com", sub, now); err != nil {
		t.Fatal(err)
	}
	sub.Auth = "a2"
	if err := store.SetPushSubscription("ash@example.com", sub, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPushSubscription("ash@example.com", PushSubscription{Endpoint: "https://push.example/def", P256dh: "p", Auth: "b"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPushSubscription("ash@example.com", PushSubscription{Endpoint: "", P256dh: "p", Auth: "b"}, now); err == nil {
		t.Fatal("a subscription without an endpoint was accepted")
	}
	subs := store.Snapshot().PushSubscriptions["ash@example.com"]
	if len(subs) != 2 || subs["https://push.example/abc"].Auth != "a2" {
		t.Fatalf("subscriptions = %+v", subs)
	}
	if err := store.RemovePushSubscription("ash@example.com", "https://push.example/nope"); err != nil {
		t.Fatalf("removing an unknown endpoint must be a no-op, got %v", err)
	}
	if err := store.RemovePushSubscription("ash@example.com", "https://push.example/def"); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path).Snapshot()
	if got := reloaded.PushSubscriptions["ash@example.com"]; len(got) != 1 || got["https://push.example/abc"].P256dh != "p" {
		t.Fatalf("after reload = %+v", got)
	}
	if err := store.ClearPushSubscriptions("ash@example.com"); err != nil {
		t.Fatal(err)
	}
	if got := NewStore(path).Snapshot().PushSubscriptions["ash@example.com"]; len(got) != 0 {
		t.Fatalf("after clear = %+v, want none", got)
	}
}

// TestRegisterPushSubscriptionParsesTheBrowserPayloadForTheSignedInMember
// pins the service contract: the browser's PushSubscription JSON
// (endpoint plus the p256dh and auth keys) registers for the signed-in
// member only when push is configured, malformed payloads are refused,
// and the settings view counts the member's devices.
func TestRegisterPushSubscriptionParsesTheBrowserPayloadForTheSignedInMember(t *testing.T) {
	svc := newTestService(t, false)
	if _, _, err := svc.store.AssignMember("ash@example.com", "Ash"); err != nil {
		t.Fatal(err)
	}
	payload := `{"endpoint":"https://push.example/abc","expirationTime":null,"keys":{"p256dh":"BP256","auth":"AUTH"}}`
	withPublicEntryRequest(t, svc, "ash@example.com", func(r *http.Request) {
		if _, err := svc.RegisterPushSubscription(r, payload, "Chrome"); err == nil {
			t.Fatal("registered a subscription while push is not configured")
		}
		svc.SetPushConfig(PushConfig{PublicKey: "VAPIDPUB"}, func(sub PushSubscription, payload PushPayload) error { return nil })
		if _, err := svc.RegisterPushSubscription(r, `{"endpoint":"x"}`, "Chrome"); err == nil {
			t.Fatal("a payload without keys was accepted")
		}
		message, err := svc.RegisterPushSubscription(r, payload, "Chrome")
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if !strings.Contains(message, "this device") {
			t.Fatalf("message = %q", message)
		}
		view := svc.PushSettingsView(r)
		if !view.Available || view.DeviceCount != 1 || view.PublicKey != "VAPIDPUB" {
			t.Fatalf("view = %+v", view)
		}
		if _, err := svc.ClearPushSubscriptionsFor(r); err != nil {
			t.Fatal(err)
		}
		if again := svc.PushSettingsView(r); again.DeviceCount != 0 {
			t.Fatalf("after clear: %+v", again)
		}
	})
}

// TestRecordAndSendAlsoPushesToEveryDeviceAndDropsGoneOnes pins the send
// path: a queued notification also goes to each of the member's devices
// as a compact payload (title, body, url), a sender reporting the
// endpoint gone removes that subscription, and a member without devices
// costs nothing.
func TestRecordAndSendAlsoPushesToEveryDeviceAndDropsGoneOnes(t *testing.T) {
	svc := newTestService(t, false)
	if _, _, err := svc.store.AssignMember("ash@example.com", "Ash"); err != nil {
		t.Fatal(err)
	}
	now := svc.clock()
	for _, endpoint := range []string{"https://push.example/live", "https://push.example/gone"} {
		if err := svc.store.SetPushSubscription("ash@example.com", PushSubscription{Endpoint: endpoint, P256dh: "p", Auth: "a"}, now); err != nil {
			t.Fatal(err)
		}
	}
	var sent []string
	svc.SetPushConfig(PushConfig{PublicKey: "VAPIDPUB"}, func(sub PushSubscription, payload PushPayload) error {
		sent = append(sent, sub.Endpoint+"|"+payload.Title+"|"+payload.URL)
		if strings.HasSuffix(sub.Endpoint, "/gone") {
			return ErrPushSubscriptionGone
		}
		return nil
	})
	svc.pushNotify("ash@example.com", renderedNotification{Category: categoryPickem, Subject: "WEEK 2 PICKS DUE", Text: "Three games lock at 1:00 PM.\nMore text."}, "/pickem")
	svc.waitForPush()
	if len(sent) != 2 {
		t.Fatalf("sent = %v, want one push per device", sent)
	}
	for _, line := range sent {
		if !strings.Contains(line, "|WEEK 2 PICKS DUE|/pickem") {
			t.Fatalf("payload = %q", line)
		}
	}
	if subs := svc.store.Snapshot().PushSubscriptions["ash@example.com"]; len(subs) != 1 || subs["https://push.example/live"].Endpoint == "" {
		t.Fatalf("subscriptions after a gone endpoint = %+v, want only the live one", subs)
	}
}

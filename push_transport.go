package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"gridiron-2000/internal/league"
)

// pushSenderFromEnv wires Web Push when both VAPID keys are set:
// PUSH_VAPID_PUBLIC_KEY, PUSH_VAPID_PRIVATE_KEY, and PUSH_CONTACT (a
// mailto: or https: contact the push services may reach the operator at;
// defaults to the first COMMISSIONER_EMAILS entry). Generate a key pair
// once with `go run github.com/SherClockHolmes/webpush-go/cmd/...` or any
// VAPID tool; the private key never leaves this closure. Off when either
// key is empty, which leaves /settings without the device panel.
func pushSenderFromEnv() (league.PushConfig, league.PushSender, bool) {
	public := strings.TrimSpace(os.Getenv("PUSH_VAPID_PUBLIC_KEY"))
	private := strings.TrimSpace(os.Getenv("PUSH_VAPID_PRIVATE_KEY"))
	if public == "" || private == "" {
		return league.PushConfig{}, nil, false
	}
	contact := strings.TrimSpace(os.Getenv("PUSH_CONTACT"))
	if contact == "" {
		if first, _, _ := strings.Cut(os.Getenv("COMMISSIONER_EMAILS"), ","); strings.TrimSpace(first) != "" {
			contact = "mailto:" + strings.TrimSpace(first)
		} else {
			contact = "mailto:league@example.invalid"
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	sender := func(sub league.PushSubscription, payload league.PushPayload) error {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		resp, err := webpush.SendNotification(body, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{
			HTTPClient:      client,
			Subscriber:      contact,
			VAPIDPublicKey:  public,
			VAPIDPrivateKey: private,
			TTL:             3600,
			Urgency:         webpush.UrgencyNormal,
		})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			return league.ErrPushSubscriptionGone
		case resp.StatusCode >= 400:
			return fmt.Errorf("push service replied %d", resp.StatusCode)
		}
		return nil
	}
	return league.PushConfig{PublicKey: public}, sender, true
}

var errPushOff = errors.New("push is not configured")

// pushClientScript is the nonce-carried head script: it registers the
// service worker on every page (inert until a device subscribes) and, on
// /settings, turns the native "turn on push" form into a subscribe flow:
// permission, subscription with the league's public key, then the same
// form posts the browser's subscription JSON to push-subscribe. Under
// the app's strict CSP only a nonced inline script or something it loads
// may run, so this lives beside the navigation runtime tag.
const pushClientScript = `(function(){
  if (!("serviceWorker" in navigator)) { return; }
  navigator.serviceWorker.register("/sw.js").catch(function(){});
  var form = document.getElementById("push-enable-form");
  if (!form) { return; }
  var field = form.querySelector("input[name=subscription]");
  var status = document.getElementById("push-status");
  function say(text){ if (status) { status.textContent = text; } }
  function toKey(b64){
    var pad = "=".repeat((4 - b64.length % 4) % 4);
    var raw = atob((b64 + pad).replace(/-/g, "+").replace(/_/g, "/"));
    var out = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) { out[i] = raw.charCodeAt(i); }
    return out;
  }
  form.addEventListener("submit", function(ev){
    if (field.value) { return; }
    ev.preventDefault();
    if (!("PushManager" in window) || !("Notification" in window)) { say("This browser does not support push notifications."); return; }
    Notification.requestPermission().then(function(permission){
      if (permission !== "granted") { say("Notifications are blocked for this site. Allow them in the browser and try again."); return null; }
      return navigator.serviceWorker.ready.then(function(reg){
        return reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: toKey(form.getAttribute("data-push-key") || "") });
      }).then(function(sub){
        field.value = JSON.stringify(sub);
        if (form.requestSubmit) { form.requestSubmit(); } else { form.submit(); }
      });
    }).catch(function(err){ say("Could not turn on push: " + ((err && err.message) || err)); });
  });
})();`

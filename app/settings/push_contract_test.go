package settings

import (
	"os"
	"strings"
	"testing"
)

// TestSettingsPushPanelContract pins the web push surface: a panel that
// renders only when push is available to the viewer, a native enable
// form carrying the CSRF token, an empty subscription field the nonced
// client script fills, and the public key as a data attribute; a
// turn-off form once devices exist; both actions registered against
// league.Service; and the service worker present at the public root.
func TestSettingsPushPanelContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<If cond={data.push_available}>`, `id="push"`,
		`id="push-enable-form"`, `data-push-key={data.push_public_key}`,
		`name="csrf_token" value={data.csrf_token}`, `name="subscription" value=""`,
		`<If cond={data.push_has_devices}>`, `action={data.push_unsubscribe_action}`,
		`id="push-status"`, `{data.push_device_label}`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx is missing the push piece %q", want)
		}
	}
	if !strings.Contains(source, `id="push-enable-form" data-gosx-managed="false"`) {
		t.Error("the enable form must be an explicitly native post; the client script resubmits it after subscribing")
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"push-subscribe":   subscribePush,`, `"push-unsubscribe": unsubscribePush,`,
		`RegisterPushSubscription(ctx.Request, ctx.FormData["subscription"], ctx.Request.UserAgent())`,
		`ClearPushSubscriptionsFor(ctx.Request)`,
	} {
		if !strings.Contains(string(server), want) {
			t.Errorf("page.server.go is missing %q", want)
		}
	}
	if _, err := os.Stat("../../public/sw.js"); err != nil {
		t.Errorf("public/sw.js must exist at the public root so it can control the whole origin: %v", err)
	}
	transport, err := os.ReadFile("../../push_transport.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`navigator.serviceWorker.register("/sw.js")`, `push-enable-form`, `applicationServerKey`} {
		if !strings.Contains(string(transport), want) {
			t.Errorf("push client script is missing %q", want)
		}
	}
}

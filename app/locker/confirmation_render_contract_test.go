package locker

import (
	"os"
	"strings"
	"testing"
)

// TestLockerRemoveExposesNativeConfirmation guards wave-6 item 9:
// "Remove" (both for a top-level post and a reply) was a single ungated
// click with no consequence text. It now uses the same gated
// <details>/required-checkbox pattern already used for drop, add-drop,
// and trade accept/decline.
func TestLockerRemoveExposesNativeConfirmation(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, marker := range []string{
		`<details class="action-confirmation">`,
		"Remove post",
		"Remove reply",
		`value="remove-locker-item"`,
		`required="required"`,
		"Confirm remove",
		"cannot be undone from this screen",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("locker template missing confirmation marker %q", marker)
		}
	}
	if count := strings.Count(body, `value="remove-locker-item"`); count != 2 {
		t.Errorf("locker template has %d remove-locker-item confirmations, want 2 (post and reply)", count)
	}
}

// TestLockerRendersRehearsalModeDisclosure guards wave-6 item 6: the
// Locker Room is a mutating demo surface (posting and moderation are
// unconditionally read-only in demo mode — lockerRequireWriter,
// RemoveLockerPost) and now carries its own copy of the REHEARSAL MODE
// disclosure, gated on the same top-level demo_mode key /admin and
// /draft already use.
func TestLockerRendersRehearsalModeDisclosure(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if !strings.Contains(body, "<If cond={data.demo_mode}>") {
		t.Fatal("locker template does not gate a notice on data.demo_mode")
	}
	if !strings.Contains(body, "REHEARSAL MODE:") {
		t.Fatal("locker template is missing the REHEARSAL MODE disclosure")
	}
	if !strings.Contains(body, "read-only while demo mode is on") {
		t.Fatal("locker's disclosure must say posting is read-only, not open, in demo mode")
	}
}

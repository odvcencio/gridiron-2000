package admin

import (
	"os"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

func TestAdminNotificationReceiptCopyAndAnchors(t *testing.T) {
	queued := adminNotificationReceiptText(league.NotificationReceipt{
		Requested: 3, Queued: 1, PreferenceSuppressed: 1, QueueDrops: 1,
	})
	for _, want := range []string{"1 queued", "1 suppressed", "1 dropped (queue full)"} {
		if !strings.Contains(queued, want) {
			t.Errorf("receipt copy %q lacks %q", queued, want)
		}
	}
	if strings.Contains(queued, "@") {
		t.Fatalf("receipt copy contains recipient PII: %q", queued)
	}

	for _, tc := range []struct {
		name    string
		receipt league.NotificationReceipt
		want    string
	}{
		{name: "disabled", receipt: league.NotificationReceipt{Requested: 2, TransportDisabled: true}, want: "delivery off"},
		{name: "not wired", receipt: league.NotificationReceipt{Requested: 1, TransportNotWired: true}, want: "delivery not wired"},
		{name: "ledger failure", receipt: league.NotificationReceipt{Requested: 1, LedgerFailures: 1}, want: "partial failure: 1 ledger failure(s)"},
		{name: "no recipients", receipt: league.NotificationReceipt{}, want: "no recipients requested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := adminNotificationReceiptText(tc.receipt)
			if got != tc.want {
				t.Fatalf("receipt copy = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "@") {
				t.Fatalf("receipt copy contains recipient PII: %q", got)
			}
		})
	}

	for _, tc := range []struct{ section, want string }{
		{section: "draft-order", want: "/admin?section=draft-order#admin-draft-order"},
		{section: "announcements", want: "/admin?section=announcements#admin-announcements"},
		{section: "../../danger", want: "/admin"},
	} {
		if got := adminSectionTarget(tc.section); got != tc.want {
			t.Errorf("adminSectionTarget(%q) = %q, want %q", tc.section, got, tc.want)
		}
	}
}

func TestAdminNotificationFormsPreserveSectionTargets(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	for _, target := range []string{
		"value={data.admin_draft_order_return_target}",
		"value={data.admin_announcements_return_target}",
	} {
		if !strings.Contains(markup, target) {
			t.Errorf("admin form is missing native return target %q", target)
		}
	}
	if strings.Count(markup, "name={data.admin_return_target_field}") < 3 {
		t.Fatal("order draw (initial and redraw) and announcement forms must preserve their section targets")
	}
}

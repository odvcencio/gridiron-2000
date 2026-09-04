package main

import (
	"os"
	"strings"
	"testing"
)

// backHomeLabelOldLabels are the four labels F17 (2026-09-04 UX pass) found
// naming the identical destination (the league's own /) five different
// ways across five pages, one of which — Privacy's "Return to league
// access" — silently pointed at /login instead. One label, "Back to Home",
// replaces all four.
var backHomeLabelOldLabels = []string{
	"Back to league HQ",
	"Return to the league",
	"Return to HQ",
	"Return to league access",
}

// backHomeLabelPages are the pages F17 named. Each must carry "Back to
// Home" linking to "/" and none of the four retired labels.
var backHomeLabelPages = []string{
	"app/settings/page.gsx",
	"app/terms/page.gsx",
	"app/open-source/page.gsx",
	"app/privacy/page.gsx",
	"app/not-found.gsx",
}

func TestBackHomeLabelContractUsesOneLabelAndDestination(t *testing.T) {
	for _, path := range backHomeLabelPages {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		source := string(body)
		for _, old := range backHomeLabelOldLabels {
			if strings.Contains(source, old) {
				t.Errorf("%s still carries the retired label %q, want \"Back to Home\"", path, old)
			}
		}
		if !strings.Contains(source, `data-gosx-link class="button button--primary">Back to Home</a>`) &&
			!strings.Contains(source, `data-gosx-link class="button button--ghost">Back to Home</a>`) {
			t.Errorf("%s is missing a \"Back to Home\" button", path)
		}
		if !strings.Contains(source, `href="/" data-gosx-link`) {
			t.Errorf("%s's back-home link does not target \"/\"", path)
		}
	}
}

// TestSettingsAccountAccessLinkRelabeled pins F17's second half: /settings'
// second footer link, previously the unexplained "Account access" (both
// footer buttons read as roughly the same action), is now named for what
// it actually does.
func TestSettingsAccountAccessLinkRelabeled(t *testing.T) {
	body, err := os.ReadFile("app/settings/page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	if strings.Contains(source, ">Account access<") {
		t.Error("app/settings/page.gsx still carries the retired \"Account access\" label")
	}
	if !strings.Contains(source, `href="/login" data-gosx-link class="button button--compact">Sign-in and account</a>`) {
		t.Error("app/settings/page.gsx is missing the relabeled \"Sign-in and account\" link to /login")
	}
}

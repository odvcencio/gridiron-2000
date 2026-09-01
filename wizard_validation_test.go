package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestWizardStepValidationErrorPreservesSubmittedValues proves the wizard
// validates every step through the same league.LoadConfigBytes path
// (design section 4.2) and, on failure, shows the operator's own submitted
// value back to them alongside the exact validation message — not a
// generic error, and not a reset to the prior draft.
func TestWizardStepValidationErrorPreservesSubmittedValues(t *testing.T) {
	h := newWizardE2EHarness(t)
	status, body := h.postStep(t, "identity", url.Values{
		"name": {""}, // league.name is required
		"short_code": {"TL"}, "tagline": {"Kept on redisplay"},
		"mode_label": {"DYNASTY"}, "url": {"https://example.com"},
		"timezone": {"America/New_York"}, "season": {"2026"},
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if !strings.Contains(body, "league.name is required") {
		t.Fatalf("the exact validateConfig message must reach the page:\n%s", body)
	}
	if !strings.Contains(body, "Kept on redisplay") {
		t.Fatalf("a failed submission must redisplay the operator's own values, not reset them:\n%s", body)
	}
	// The step must not have been marked DONE.
	if strings.Contains(body, "1. League identity — DONE") {
		t.Fatal("a failed step must not be marked DONE")
	}
}

// TestWizardRosterStepValidationErrorOnMismatchedExplicitShape proves the
// roster step's own field-ownership: an explicit shape whose total the
// wizard cannot make consistent (roster.slots totalling zero starters)
// surfaces on the roster step itself, in the exact validateConfig wording.
func TestWizardRosterStepValidationErrorOnMismatchedExplicitShape(t *testing.T) {
	h := newWizardE2EHarness(t)
	// An explicit shape (bench > 0 keeps it from being treated as an
	// absent block, which would otherwise fall back to the gridiron-house
	// preset) with zero starter slots: roster.slots must total at least 1
	// starter.
	status, body := h.postStep(t, "roster", url.Values{
		"preset": {""}, "slots": {""}, "bench": {"1"}, "ir": {"0"},
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if !strings.Contains(body, "roster.slots must total at least 1 starter") {
		t.Fatalf("expected the roster step's own validation message:\n%s", body)
	}
}

// TestWizardCommissionerStepRejectsMalformedEmail covers the
// commissioner-step-only well-formedness check (not a league.json field,
// so LoadConfigBytes never sees it — see registerCommissionerStep's doc
// comment).
func TestWizardCommissionerStepRejectsMalformedEmail(t *testing.T) {
	h := newWizardE2EHarness(t)
	status, body := h.postStep(t, "commissioner", url.Values{
		"commissioner_email": {"not-an-email"}, "aliases": {""},
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if !strings.Contains(body, "Enter a valid commissioner email address") {
		t.Fatalf("expected the commissioner email validation message:\n%s", body)
	}
}

// TestWizardMembershipStepRejectsMalformedMemberEmail covers the
// membership step's own member-email well-formedness check.
func TestWizardMembershipStepRejectsMalformedMemberEmail(t *testing.T) {
	h := newWizardE2EHarness(t)
	status, body := h.postStep(t, "membership", url.Values{
		"allowed_domain": {""}, "member_emails": {"not-an-email\nvalid@example.com"},
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if !strings.Contains(body, "Not a valid email: not-an-email") {
		t.Fatalf("expected the member-email validation message:\n%s", body)
	}
}

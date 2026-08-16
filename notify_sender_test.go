package main

import (
	"errors"
	"testing"

	"gridiron-2000/internal/mailer"
	"gridiron-2000/internal/notify"
)

// TestNotificationSenderWrapsSMTPFailureAsPermanent checks that
// notificationSender marks an SMTP-transport failure Permanent (design
// spec section 6.3; finding M3): SMTP can fail after the message was
// already accepted, so a retry risks a duplicate, and at-most-once
// delivery is preferred there.
func TestNotificationSenderWrapsSMTPFailureAsPermanent(t *testing.T) {
	// Port 1 on loopback: nothing listens there, so the dial fails fast
	// with connection-refused, no live network access required.
	cfg := mailer.Config{Host: "127.0.0.1", Port: "1", User: "u", Pass: "p", From: "commish@example.com"}
	sender := notificationSender(cfg)

	err := sender(notify.Message{To: "manager@example.com", Subject: "Hi", Text: "Body", Category: "draft_live"})
	if err == nil {
		t.Fatal("expected a dial failure against a closed SMTP port")
	}
	if !errors.Is(err, notify.ErrPermanent) {
		t.Errorf("an SMTP-transport failure must be wrapped Permanent: %v", err)
	}
}

// TestNotificationSenderNonSMTPFailureStaysRetryable checks that a failure
// that is not an SMTP-transport error (standing in for a Resend failure,
// which never wraps mailer.ErrSMTPTransport) is returned unwrapped and
// stays retryable (finding M3): Resend's Idempotency-Key header, not
// notify.Permanent, is what protects a Resend retry from double-sending.
func TestNotificationSenderNonSMTPFailureStaysRetryable(t *testing.T) {
	cfg := mailer.Config{} // no transport configured at all: Enabled() is false
	sender := notificationSender(cfg)

	err := sender(notify.Message{To: "manager@example.com", Subject: "Hi", Text: "Body"})
	if err == nil {
		t.Fatal("expected an error from a disabled mailer config")
	}
	if errors.Is(err, notify.ErrPermanent) {
		t.Errorf("a non-SMTP-transport failure must stay retryable, not Permanent: %v", err)
	}
}

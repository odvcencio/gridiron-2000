package main

import (
	"errors"
	"testing"

	"gridiron-2000/internal/league"
)

func TestPersistenceHealthMakesPoisonedStorageNotReady(t *testing.T) {
	poison := errors.New("sqlite /srv/gridiron/data/league.sqlite: disk I/O error")
	ready, status, publicError := persistenceHealth(poison)
	if ready {
		t.Fatal("poisoned persistence reported ready")
	}
	if status != 503 {
		t.Fatalf("poisoned persistence status = %d, want 503", status)
	}
	if publicError != "persistence unavailable" {
		t.Fatalf("public persistence error = %q, want neutral failure class", publicError)
	}
	if publicError == poison.Error() {
		t.Fatal("persistence health leaked the operator error")
	}
}

func TestPersistenceHealthKeepsHealthyStorageReady(t *testing.T) {
	ready, status, publicError := persistenceHealth(nil)
	if !ready {
		t.Fatal("healthy persistence reported not ready")
	}
	if status != 200 {
		t.Fatalf("healthy persistence status = %d, want 200", status)
	}
	if publicError != "" {
		t.Fatalf("healthy persistence public error = %q, want empty", publicError)
	}
}

func TestPersistenceHealthTransitionsFromWriteFailureToRecovery(t *testing.T) {
	writeFailure := errors.New("injected ordinary write failure")
	ready, status, publicError := persistenceHealth(writeFailure)
	if ready || status != 503 || publicError != "persistence unavailable" {
		t.Fatalf("failed-write health = ready:%v status:%d error:%q, want false/503/neutral", ready, status, publicError)
	}
	ready, status, publicError = persistenceHealth(nil)
	if !ready || status != 200 || publicError != "" {
		t.Fatalf("recovered health = ready:%v status:%d error:%q, want true/200/empty", ready, status, publicError)
	}
}

func TestLivenessPayloadDoesNotDependOnPersistence(t *testing.T) {
	payload := livenessPayload()
	if payload["ok"] != true || payload["liveness"] != true {
		t.Fatalf("liveness payload = %#v, want an always-live response", payload)
	}
	if _, exists := payload["readiness"]; exists {
		t.Fatal("liveness payload must not imply persistence readiness")
	}
}

func TestStateSchemaPayloadExposesOnlyCompatibilityEvidence(t *testing.T) {
	payload := stateSchemaPayload(league.StateSchemaCompatibility{
		PersistedVersion:         9,
		SupportedVersion:         8,
		PersistedDatabaseVersion: 7,
		SupportedDatabaseVersion: 6,
		Compatible:               false,
	})
	if payload["persistedVersion"] != 9 || payload["supportedVersion"] != 8 || payload["compatible"] != false {
		t.Fatalf("state schema payload = %#v", payload)
	}
	if payload["persistedDatabaseVersion"] != 7 || payload["supportedDatabaseVersion"] != 6 {
		t.Fatalf("state database schema payload = %#v", payload)
	}
	if len(payload) != 5 {
		t.Fatalf("state schema payload exposed unexpected fields: %#v", payload)
	}
}

// F6: the co-manager welcome flash must credit the seat's primary manager,
// never the invitee who just signed in to consume the invite.
func TestCoManagerWelcomeFlashNamesThePrimaryNotTheInvitee(t *testing.T) {
	got := coManagerWelcomeFlash("Caleb's Corn Dogs", "Melanie Miguel Villavicencio")
	want := "You're co-managing Caleb's Corn Dogs alongside its primary manager, Melanie Miguel Villavicencio."
	if got != want {
		t.Fatalf("coManagerWelcomeFlash = %q, want %q", got, want)
	}
}

func TestCoManagerWelcomeFlashFallsBackWhenNoPrimaryName(t *testing.T) {
	got := coManagerWelcomeFlash("Caleb's Corn Dogs", "")
	want := "You're co-managing Caleb's Corn Dogs alongside its primary manager, the primary manager."
	if got != want {
		t.Fatalf("coManagerWelcomeFlash = %q, want %q", got, want)
	}
}

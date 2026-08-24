package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1provider"
	"gridiron-2000/internal/commissionerhq/v1transport"
	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/openstats"
)

func commissionerHQV1TestConfig(t *testing.T, address string) v1provider.Config {
	t.Helper()
	credential, err := v1transport.NewCredentials("test-key", []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return v1provider.Config{Enabled: true, Address: address, Credential: credential}
}

func TestCommissionerHQV1RuntimeBindsSynchronouslyAndOwnsNoPublicRoutes(t *testing.T) {
	runtime, err := newCommissionerHQV1Runtime(commissionerHQV1TestConfig(t, "127.0.0.1:0"), func(context.Context) (hqv1.Summary, error) {
		return hqv1.Summary{}, v1transport.ErrTemporarilyUnavailable
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.Listening() {
		t.Fatal("runtime did not report its bound listener")
	}
	served := make(chan error, 1)
	go func() { served <- runtime.Serve() }()
	response, err := http.Get("http://" + runtime.listener.Addr().String() + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("public-style route status = %d, want 404", response.StatusCode)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil && err != http.ErrServerClosed {
		t.Fatalf("Serve() = %v", err)
	}
	if runtime.Listening() {
		t.Fatal("runtime remained listening after shutdown")
	}
}

func TestCommissionerHQV1RuntimeBindFailureIsSynchronousAndSanitized(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().String()
	_, err = newCommissionerHQV1Runtime(commissionerHQV1TestConfig(t, address), func(context.Context) (hqv1.Summary, error) {
		return hqv1.Summary{}, nil
	})
	if err == nil || strings.Contains(err.Error(), address) {
		t.Fatalf("bind error = %v", err)
	}
}

func TestCommissionerHQV1ReleaseSnapshot(t *testing.T) {
	oldSHA, oldDate := appGitSHA, appBuildDate
	t.Cleanup(func() { appGitSHA, appBuildDate = oldSHA, oldDate })
	appGitSHA = "1234567890abcdef1234567890abcdef12345678"
	appBuildDate = "2026-08-24T12:34:56Z"
	t.Setenv("APP_IMAGE_DIGEST", "sha256:"+strings.Repeat("a", 64))
	release, err := commissionerHQV1ReleaseSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if release.GitSHA != appGitSHA || !release.BuiltAt.Equal(time.Date(2026, 8, 24, 12, 34, 56, 0, time.UTC)) || release.ImageDigest == "" {
		t.Fatalf("release = %#v", release)
	}

	appGitSHA = "HOSTILE-NOT-A-SHA"
	_, err = commissionerHQV1ReleaseSnapshot()
	if err == nil || strings.Contains(err.Error(), appGitSHA) {
		t.Fatalf("invalid release error = %v", err)
	}
	t.Setenv("APP_IMAGE_DIGEST", "HOSTILE-DIGEST")
	appGitSHA = "1234567890abcdef1234567890abcdef12345678"
	_, err = commissionerHQV1ReleaseSnapshot()
	if err == nil || strings.Contains(err.Error(), "HOSTILE-DIGEST") {
		t.Fatalf("invalid digest error = %v", err)
	}
}

func TestCommissionerHQV1DataSnapshotMapsRuntimeTruth(t *testing.T) {
	now := time.Date(2026, 8, 24, 20, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		state, quality, code string
	}{
		{state: "live", quality: "healthy"},
		{state: "cached", quality: "degraded", code: "stale"},
		{state: "stale", quality: "degraded", code: "stale"},
		{state: "degraded", quality: "degraded", code: "partial"},
		{state: "offline", quality: "degraded", code: "unreachable"},
		{state: "unavailable", quality: "degraded", code: "unreachable"},
		{state: "future-state", quality: "not_reported"},
	} {
		t.Run(test.state, func(t *testing.T) {
			data := commissionerHQV1DataSnapshot(fantasy.Status{Mode: "cache", State: test.state, Players: 340, LastSync: now}, openstats.ScheduleSnapshot{})
			if data.Quality != test.quality || data.DegradationCode != test.code {
				t.Fatalf("data = %#v", data)
			}
			if test.quality == "not_reported" && (!data.AsOf.IsZero() || data.PlayerCount != nil) {
				t.Fatalf("not-reported data retained claimed facts: %#v", data)
			}
		})
	}
	data := commissionerHQV1DataSnapshot(fantasy.Status{State: "live", Players: 340}, openstats.ScheduleSnapshot{})
	if data.Quality != "not_reported" || data.PlayerCount != nil {
		t.Fatalf("unsynchronized source = %#v", data)
	}
}

func TestCommissionerHQV1ReleaseSnapshotRejectsUnsetBuildInfoWhenEnabled(t *testing.T) {
	oldSHA, oldDate := appGitSHA, appBuildDate
	t.Cleanup(func() { appGitSHA, appBuildDate = oldSHA, oldDate })
	appGitSHA, appBuildDate = "unknown", "unknown"
	t.Setenv("APP_IMAGE_DIGEST", "")
	if _, err := commissionerHQV1ReleaseSnapshot(); err == nil {
		t.Fatal("release snapshot accepted local sentinel build info")
	}
}

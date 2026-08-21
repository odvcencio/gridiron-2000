package main

import (
	"testing"

	"m31labs.dev/gosx"
)

func TestBuildInfoPayloadExposesReleaseAndFrameworkSeparately(t *testing.T) {
	oldVersion, oldSHA, oldDate := appVersion, appGitSHA, appBuildDate
	t.Cleanup(func() {
		appVersion, appGitSHA, appBuildDate = oldVersion, oldSHA, oldDate
	})
	appVersion = "release-test"
	appGitSHA = "abc123"
	appBuildDate = "2026-08-21T00:00:00Z"

	got := buildInfoPayload()
	if got["appVersion"] != appVersion || got["gitSHA"] != appGitSHA || got["buildDate"] != appBuildDate {
		t.Fatalf("build metadata = %#v", got)
	}
	if got["frameworkVersion"] != gosx.Version {
		t.Fatalf("frameworkVersion = %q, want %q", got["frameworkVersion"], gosx.Version)
	}
}

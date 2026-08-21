package main

import "m31labs.dev/gosx"

// These values are replaced by the release build. Keeping the defaults useful
// makes local binaries honest without requiring a release toolchain.
var (
	appVersion   = "dev"
	appGitSHA    = "unknown"
	appBuildDate = "unknown"
)

func buildInfoPayload() map[string]string {
	return map[string]string{
		"appVersion":       appVersion,
		"frameworkVersion": gosx.Version,
		"gitSHA":           appGitSHA,
		"buildDate":        appBuildDate,
	}
}

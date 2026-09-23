package scoring

import (
	"os"
	"testing"
)

// TestMain defaults APP_ENV to "test" for the whole test binary process
// when the environment does not already set one. Fail-closed boot
// (app_build.go's AppConfig.validate, internal/league's IsLocalAppEnv) now
// treats an unset APP_ENV as production, not as local — but league.Default()
// is a process-wide sync.Once singleton, and several subprocess fixtures in
// this package build their child's environment from os.Environ() without
// pinning their own APP_ENV. Both need a local APP_ENV to resolve before
// this package's first Default() call or its first subprocess spawn; a
// per-test t.Setenv would be too late for either. Restoring the previous
// value keeps this scoped to the test binary's lifetime.
func TestMain(m *testing.M) {
	setAppEnv := false
	if os.Getenv("APP_ENV") == "" {
		os.Setenv("APP_ENV", "test")
		setAppEnv = true
	}
	status := m.Run()
	if setAppEnv {
		os.Unsetenv("APP_ENV")
	}
	os.Exit(status)
}

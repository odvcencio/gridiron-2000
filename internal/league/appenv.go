package league

import "strings"

// IsLocalAppEnv reports whether appEnv names a local, non-deployed
// environment. It is an allow-list, not a "production" match: APP_ENV=prod,
// APP_ENV=staging, and every unknown label are deployments. This is the one
// answer every boundary decision shares — the session cookie policy
// (main.go's gridironSessionOptions), the demo-mode gate below, and the
// setup-wizard boot state's fail-closed rule — so none of them can disagree
// about where the process runs.
func IsLocalAppEnv(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "", "local", "development", "test":
		return true
	}
	return false
}

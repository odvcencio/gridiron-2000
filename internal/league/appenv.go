package league

import "strings"

// IsLocalAppEnv reports whether appEnv names a local, non-deployed
// environment. It is an allow-list, not a "production" match: an unset
// APP_ENV, APP_ENV=prod, APP_ENV=staging, and every unknown label are all
// deployments. Local or development behavior needs an explicit
// APP_ENV=local, APP_ENV=development, or APP_ENV=test. This is the one
// answer every boundary decision shares — the session cookie policy
// (main.go's gridironSessionOptions), the SESSION_SECRET fail-closed check
// and the demo-mode gate below, and the setup-wizard boot state's
// fail-closed rule — so none of them can disagree about where the process
// runs, and a forgotten APP_ENV fails closed as production rather than
// open as local.
func IsLocalAppEnv(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "local", "development", "test":
		return true
	}
	return false
}

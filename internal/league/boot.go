// Boot state machine (setup-wizard design section 3.1). Two truthful
// states, plus one fail-closed refusal — never a silent third thing. main()
// calls DetermineBootState before constructing the process-wide Service
// singleton (Default()), so it can choose which router to build: the full
// league app, the setup-only wizard app, or a static operator-error page.
package league

import "gridiron-2000/internal/identity"

// BootState names one of the three boot outcomes.
type BootState string

const (
	// BootConfigured: a league.json resolves. The normal app boots.
	BootConfigured BootState = "configured"
	// BootSetup: no league.json resolves, no setup_state marker, and the
	// database holds no members and no picks. The first-boot wizard runs.
	BootSetup BootState = "setup"
	// BootFailClosed: no league.json resolves, but the database already
	// carries the setup_state marker or already holds members or picks. A
	// mount failure (the config volume/ConfigMap went missing after a
	// completed setup), not a fresh instance. Setup never re-arms on it.
	BootFailClosed BootState = "fail_closed"
)

// BootDecision is what main() needs to choose an app to build.
type BootDecision struct {
	State BootState
	// ConfigPath is set only when State == BootConfigured: the resolved
	// league.json path (informational; the normal Default() boot path
	// re-resolves it independently).
	ConfigPath string
	// Store is open and ready only when State == BootSetup: the wizard
	// needs a live database, at the same path Default() will later open,
	// to persist draft progress and, at commit, the invite rows and the
	// setup_state marker. The caller owns closing it (or, more likely,
	// keeps it open and lets the process exit/restart at commit).
	// CONFIGURED opens its own Store the normal way, through Default();
	// FAIL_CLOSED never keeps one open past this decision's own read.
	Store *Store
}

// DetermineBootState decides BootConfigured, BootSetup, or BootFailClosed
// without constructing the process-wide Service singleton, so main() can
// choose a router before any league code runs a real league's worth of
// validation, pollers, or side effects.
func DetermineBootState() (BootDecision, error) {
	path, found, err := ConfigFileResolves()
	if err != nil {
		return BootDecision{}, err
	}
	if found {
		return BootDecision{State: BootConfigured, ConfigPath: path}, nil
	}

	resolver, err := identity.FromEnv()
	if err != nil {
		return BootDecision{}, err
	}
	store := NewStoreWithIdentity(DataFilePath(), resolver)
	if err := store.StartupError(); err != nil {
		_ = store.Close()
		return BootDecision{}, err
	}
	_, hasMarker, err := store.SetupCompletion()
	if err != nil {
		_ = store.Close()
		return BootDecision{}, err
	}
	snapshot := store.Snapshot()
	hasData := len(snapshot.Members) > 0 || len(snapshot.Picks) > 0
	if hasMarker || hasData {
		_ = store.Close()
		return BootDecision{State: BootFailClosed}, nil
	}
	return BootDecision{State: BootSetup, Store: store}, nil
}

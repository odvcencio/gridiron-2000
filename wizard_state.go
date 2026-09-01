package main

import (
	"sync"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/setupwizard"
)

// wizardStateManager owns the single in-process setupwizard.State for a
// SETUP-state process: load once at boot, mutate under a mutex per
// request, persist to the store after every successful step save (design
// section 4.4). One process serves one wizard session at a time — the
// setup token guard (setup_token.go) already enforces that upstream, so
// this manager does not need its own session-keying.
type wizardStateManager struct {
	mu    sync.Mutex
	store *league.Store
	state setupwizard.State
	now   func() time.Time
}

func newWizardStateManager(store *league.Store, now func() time.Time) (*wizardStateManager, error) {
	if now == nil {
		now = time.Now
	}
	state, err := setupwizard.LoadState(store)
	if err != nil {
		return nil, err
	}
	return &wizardStateManager{store: store, state: state, now: now}, nil
}

// View returns a snapshot of the current state for rendering.
func (m *wizardStateManager) View() setupwizard.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// ApplyStep validates and applies a league.json-field step save
// (identity/teams/scoring/roster/draft/waivers/trades/membership's
// allowed_domain), persisting on success.
func (m *wizardStateManager) ApplyStep(slug string, config league.ConfigFile) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	warnings, err := m.state.ApplyStep(slug, config)
	if err != nil {
		return nil, err
	}
	if err := m.state.Save(m.store, m.now()); err != nil {
		return nil, err
	}
	return warnings, nil
}

// SetMemberEmails records the membership step's Tier 0 email list (not a
// league.json field) after ApplyStep has already accepted the step's
// allowed_domain field.
func (m *wizardStateManager) SetMemberEmails(emails []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Draft.MemberEmails = emails
	return m.state.Save(m.store, m.now())
}

// SetCommissioner records the commissioner step's runtime-env fields
// (COMMISSIONER_EMAILS/IDENTITY_ALIASES — never league.json fields, design
// section 4.3) and marks that step DONE. There is nothing here for
// LoadConfigBytes to validate; email well-formedness is checked by the
// caller before this is invoked.
func (m *wizardStateManager) SetCommissioner(email string, aliases []setupwizard.IdentityAlias) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Draft.CommissionerEmail = email
	m.state.Draft.IdentityAliases = aliases
	m.state.Status.MarkDone("commissioner")
	return m.state.Save(m.store, m.now())
}

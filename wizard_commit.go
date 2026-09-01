package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/setupwizard"
)

// mintedInviteLink is one Tier 0 invite link the atomic commit minted, for
// the completion page's one-time display (design section 6.2: "renders
// exactly once, at mint time").
type mintedInviteLink struct {
	Email string
	URL   string
}

// commitContext carries the atomic commit's working state through its
// ordered steps (design section 4.5). Each step is independently callable
// (and independently testable — see wizard_commit_test.go's staged
// kill -9 coverage): "each earlier failure leaves a resumable SETUP
// state."
type commitContext struct {
	store             *league.Store
	dataDir           string
	draft             setupwizard.Draft
	now               time.Time
	appVersion        string
	sessionSecret     string
	finalConfigBytes  []byte
	finalConfigSHA256 string
	mintedLinks       []mintedInviteLink
}

// newCommitContext resolves the final config bytes are computed by
// commitStepValidate; this constructor only gathers the inputs a commit
// needs.
func newCommitContext(store *league.Store, draft setupwizard.Draft, dataDir, appVersion string, now time.Time) (*commitContext, error) {
	sessionSecret, err := randomSetupToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate session secret: %w", err)
	}
	return &commitContext{
		store: store, dataDir: dataDir, draft: draft,
		now: now, appVersion: appVersion, sessionSecret: sessionSecret,
	}, nil
}

// commitStepValidate is design section 4.5 step 1: "Re-validate the final
// bytes via LoadConfigBytes. Refuse on any error." This is the same
// validation path every earlier step already ran; running it again here,
// against the fully assembled draft, is the review step's own gate before
// anything durable happens.
func commitStepValidate(ctx *commitContext) error {
	raw, err := json.Marshal(ctx.draft.Config)
	if err != nil {
		return fmt.Errorf("encode final config: %w", err)
	}
	if _, _, err := league.LoadConfigBytes("league.json", raw); err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	ctx.finalConfigBytes = raw
	ctx.finalConfigSHA256 = hex.EncodeToString(sum[:])
	return nil
}

// commitStepWriteRuntimeEnv is design section 4.5 step 2: write
// runtime.env (0600, owner-only), tmp -> fsync -> rename.
func commitStepWriteRuntimeEnv(ctx *commitContext) error {
	aliases := make([]identityAliasPair, 0, len(ctx.draft.IdentityAliases))
	for _, alias := range ctx.draft.IdentityAliases {
		aliases = append(aliases, identityAliasPair{Alias: alias.Alias, Canonical: alias.Canonical})
	}
	content := renderRuntimeEnv(ctx.draft.CommissionerEmail, aliases, ctx.sessionSecret)
	return writeFileAtomic(runtimeEnvPath(ctx.dataDir), content, 0o600)
}

// commitStepWriteInvites is design section 4.5 step 3: "Write member
// invites and minted invite-link rows into the database... in one
// transaction." Each email's admission (Store.AddInvite) and its Tier 0
// link mint happen together (Store.MintInviteLinkWithAdmission — see its
// doc comment for why this is not one literal SQL transaction across both
// concerns: each per-email write is independently durable and the whole
// step is safely re-runnable, which is what "a resumable SETUP state"
// actually requires).
func commitStepWriteInvites(ctx *commitContext) error {
	ctx.mintedLinks = ctx.mintedLinks[:0]
	leagueURL := strings.TrimSuffix(strings.TrimSpace(ctx.draft.Config.League.URL), "/")
	for _, email := range ctx.draft.MemberEmails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		token, _, err := ctx.store.MintInviteLinkWithAdmission(email, ctx.draft.CommissionerEmail, 0, ctx.now)
		if err != nil {
			return fmt.Errorf("mint invite link for %s: %w", email, err)
		}
		url := "/auth/invite/" + token
		if leagueURL != "" {
			url = leagueURL + url
		}
		ctx.mintedLinks = append(ctx.mintedLinks, mintedInviteLink{Email: email, URL: url})
	}
	return nil
}

// commitStepWriteLeagueJSON is design section 4.5 step 4: write league.json
// tmp -> fsync -> rename. This rename is the state flip — lookup rule 4
// (config.go:396) finds it beside DATA_FILE with no new mount.
func commitStepWriteLeagueJSON(ctx *commitContext) error {
	return writeFileAtomic(filepath.Join(ctx.dataDir, "league.json"), ctx.finalConfigBytes, 0o644)
}

// commitStepMarkSetupComplete is design section 4.5 step 5: write the
// setup_state marker row. A crash between commitStepWriteLeagueJSON and
// this step still boots CONFIGURED — the file is what DetermineBootState
// actually keys on; the marker is redundant confirmation, not the trigger.
func commitStepMarkSetupComplete(ctx *commitContext) error {
	return ctx.store.MarkSetupComplete(league.SetupCompletion{
		CompletedAt: ctx.now, CompletedBy: ctx.draft.CommissionerEmail,
		ConfigSHA256: ctx.finalConfigSHA256, AppVersion: ctx.appVersion,
	})
}

// commitSteps is the atomic commit's exact ordered sequence (design
// section 4.5). Exported as a slice, not inlined into one function, so a
// test can run any prefix of it and inspect the resulting on-disk/database
// state — the kill -9-at-each-stage acceptance criterion.
var commitSteps = []func(*commitContext) error{
	commitStepValidate,
	commitStepWriteRuntimeEnv,
	commitStepWriteInvites,
	commitStepWriteLeagueJSON,
	commitStepMarkSetupComplete,
}

// commitWizard runs every step in order, stopping at the first error. The
// caller (wizard_review.go's commit action) renders the completion page
// only after every step returns nil.
func commitWizard(ctx *commitContext) error {
	for _, step := range commitSteps {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

// dataDirFromEnv resolves <dir(DATA_FILE)>, the directory both
// runtime.env and league.json land in (owner decision; config.go's lookup
// rule 4).
func dataDirFromEnv() string {
	return filepath.Dir(league.DataFilePath())
}

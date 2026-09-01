package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/setupwizard"
)

// commitTestEnv points DATA_FILE/LEAGUE_FILE/GOSX_APP_ROOT at a fresh temp
// directory, isolating league.DetermineBootState from the real repo tree
// (mirrors internal/league/boot_test.go's bootTestEnv, duplicated here
// because DetermineBootState is exported and this package's own commit
// logic is what needs to prove its crash-recovery states, not
// internal/league's).
func commitTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LEAGUE_FILE", "")
	t.Setenv("GOSX_APP_ROOT", dir)
	t.Setenv("DATA_FILE", filepath.Join(dir, "league-state.json"))
	return dir
}

func testCommitDraft(commissionerEmail string, memberEmails []string) setupwizard.Draft {
	draft := setupwizard.NewDraft()
	draft.Config.League.Name = "Kill Nine League"
	draft.Config.League.ShortCode = "K9L"
	draft.Config.League.URL = "https://k9l.example.com"
	draft.CommissionerEmail = commissionerEmail
	draft.MemberEmails = memberEmails
	return draft
}

// TestCommitWizardStagedCrashRecovery is the design's slice-3 acceptance
// criterion: "kill -9 at each commit stage lands in a documented state."
// A real SIGKILL guarantees only that whatever was already fsynced (a
// completed step here — every step commits its own file rename or SQLite
// transaction before returning) survives and nothing after it does; this
// test proves the resulting boot state after each possible stopping point
// matches the design's documented recovery narrative (section 4.5)
// exactly, without the timing nondeterminism of an actual forked-process
// SIGKILL.
func TestCommitWizardStagedCrashRecovery(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	for stage := 0; stage <= len(commitSteps); stage++ {
		t.Run(stageName(stage), func(t *testing.T) {
			dataDir := commitTestEnv(t)
			store := league.NewStore(filepath.Join(dataDir, "league-state.json"))
			t.Cleanup(func() { _ = store.Close() })
			if err := store.StartupError(); err != nil {
				t.Fatal(err)
			}
			draft := testCommitDraft("commish@example.com", []string{"member1@example.com", "member2@example.com"})
			ctx, err := newCommitContext(store, draft, dataDir, "test", now)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < stage; i++ {
				if err := commitSteps[i](ctx); err != nil {
					t.Fatalf("stage %d (%s) failed: %v", i, stageName(i+1), err)
				}
			}

			decision, err := league.DetermineBootState()
			if err != nil {
				t.Fatalf("DetermineBootState after stage %d: %v", stage, err)
			}
			if decision.Store != nil {
				defer decision.Store.Close()
			}

			leagueJSONPath := filepath.Join(dataDir, "league.json")
			_, statErr := os.Stat(leagueJSONPath)
			leagueJSONExists := statErr == nil

			switch {
			case stage < 4: // before commitStepWriteLeagueJSON returns
				if leagueJSONExists {
					t.Fatalf("stage %d: league.json must not exist before step 4 completes", stage)
				}
				if decision.State != league.BootSetup {
					t.Fatalf("stage %d: boot state = %q, want %q (design: \"a crash before 4 resumes SETUP with the draft intact\")", stage, decision.State, league.BootSetup)
				}
				// The draft itself must have survived stage 0 (nothing run
				// yet, so nothing to survive) is trivially true; for later
				// stages the draft was never saved by commitWizard (saving
				// setup_draft is the wizard step handlers' job, not the
				// commit sequence's), so this only proves the boot
				// decision, not draft persistence — that is covered by
				// TestSaveAndLoadStateRoundTrip in internal/setupwizard.
			default: // stage 4 or 5: league.json is already in place
				if !leagueJSONExists {
					t.Fatalf("stage %d: league.json must exist once step 4 has completed", stage)
				}
				if decision.State != league.BootConfigured {
					t.Fatalf("stage %d: boot state = %q, want %q (design: \"A crash between 4 and 5 still boots CONFIGURED\")", stage, decision.State, league.BootConfigured)
				}
			}

			if stage >= 2 { // runtime.env written
				info, err := os.Stat(runtimeEnvPath(dataDir))
				if err != nil {
					t.Fatalf("stage %d: runtime.env should exist: %v", stage, err)
				}
				if perm := info.Mode().Perm(); perm != 0o600 {
					t.Fatalf("stage %d: runtime.env mode = %o, want 0600", stage, perm)
				}
			}

			if stage >= 3 { // invites/invite_links written
				if !store.Invited("member1@example.com") || !store.Invited("member2@example.com") {
					t.Fatalf("stage %d: member emails should be admitted after step 3", stage)
				}
				links, err := store.InviteLinksForEmail("member1@example.com")
				if err != nil || len(links) != 1 {
					t.Fatalf("stage %d: expected exactly one minted invite link for member1, got %v (err=%v)", stage, links, err)
				}
			}

			if stage >= 5 { // setup_state marker written
				completion, found, err := store.SetupCompletion()
				if err != nil || !found {
					t.Fatalf("stage %d: setup_state marker should exist: found=%v err=%v", stage, found, err)
				}
				if completion.CompletedBy != "commish@example.com" {
					t.Fatalf("stage %d: CompletedBy = %q", stage, completion.CompletedBy)
				}
			} else if stage >= 4 {
				// Stage exactly 4: league.json exists but the marker does
				// not yet — the design's explicit "crash between 4 and 5"
				// window. DetermineBootState must still resolve CONFIGURED
				// (already asserted above) purely from the file's
				// presence, never from the marker.
				if _, found, err := store.SetupCompletion(); err != nil || found {
					t.Fatalf("stage %d: setup_state marker must not exist yet (found=%v err=%v)", stage, found, err)
				}
			}
		})
	}
}

func stageName(stage int) string {
	names := []string{
		"stage0_nothing_run",
		"stage1_validated_only",
		"stage2_runtime_env_written",
		"stage3_invites_written",
		"stage4_league_json_written",
		"stage5_marker_written",
	}
	if stage < 0 || stage >= len(names) {
		return "stage_unknown"
	}
	return names[stage]
}

// TestCommitWizardEndToEndSucceeds proves the full sequence, uninterrupted,
// leaves a CONFIGURED, leaguecheck-clean instance with the expected
// artifacts.
func TestCommitWizardEndToEndSucceeds(t *testing.T) {
	dataDir := commitTestEnv(t)
	store := league.NewStore(filepath.Join(dataDir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	draft := testCommitDraft("commish@example.com", []string{"member1@example.com"})
	ctx, err := newCommitContext(store, draft, dataDir, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := commitWizard(ctx); err != nil {
		t.Fatalf("commitWizard: %v", err)
	}
	if len(ctx.mintedLinks) != 1 || ctx.mintedLinks[0].Email != "member1@example.com" {
		t.Fatalf("mintedLinks = %+v, want exactly one entry for member1@example.com", ctx.mintedLinks)
	}
	if !strings.HasPrefix(ctx.mintedLinks[0].URL, "https://k9l.example.com/auth/invite/") {
		t.Fatalf("minted link URL = %q, want the league URL as a prefix", ctx.mintedLinks[0].URL)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, "league.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := league.LoadConfigFileBytes(filepath.Join(dataDir, "league.json"), raw); err != nil {
		t.Fatalf("the committed league.json must itself pass LoadConfigBytes (leaguecheck's own path): %v", err)
	}

	decision, err := league.DetermineBootState()
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != league.BootConfigured {
		t.Fatalf("boot state after a full commit = %q, want %q", decision.State, league.BootConfigured)
	}
}

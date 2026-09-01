package main

import "testing"

// TestDefaultRestartHookHonorsGridironSupervised is the design's hybrid
// restart behavior (owner decision, design open decision 7): os.Exit(0)
// only when GRIDIRON_SUPERVISED=1; otherwise a no-op. osExit is swapped
// for the duration of the test so this never actually exits the test
// binary.
func TestDefaultRestartHookHonorsGridironSupervised(t *testing.T) {
	original := osExit
	var exitCode int
	var exited bool
	osExit = func(code int) { exited = true; exitCode = code }
	defer func() { osExit = original }()

	t.Run("supervised calls exit", func(t *testing.T) {
		exited, exitCode = false, -1
		t.Setenv("GRIDIRON_SUPERVISED", "1")
		defaultRestartHook()()
		if !exited {
			t.Fatal("GRIDIRON_SUPERVISED=1 must call osExit")
		}
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", exitCode)
		}
	})

	t.Run("unsupervised is a no-op", func(t *testing.T) {
		exited = false
		t.Setenv("GRIDIRON_SUPERVISED", "")
		defaultRestartHook()()
		if exited {
			t.Fatal("without GRIDIRON_SUPERVISED=1, the restart hook must not call osExit")
		}
	})

	t.Run("any other value is a no-op", func(t *testing.T) {
		exited = false
		t.Setenv("GRIDIRON_SUPERVISED", "true")
		defaultRestartHook()()
		if exited {
			t.Fatal(`GRIDIRON_SUPERVISED="true" (not exactly "1") must not call osExit`)
		}
	})
}

// TestCommitCompletionRecordsSupervisedFlag proves the completion result
// carries the same GRIDIRON_SUPERVISED reading the completion page uses to
// choose its copy (setupCompletionNode: "The server restarts now" vs.
// "restart this process now... manually").
func TestCommitCompletionRecordsSupervisedFlag(t *testing.T) {
	for _, tc := range []struct {
		name       string
		envValue   string
		supervised bool
	}{
		{"supervised", "1", true},
		{"unset", "", false},
		{"non-canonical value", "yes", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWizardE2EHarness(t)
			t.Setenv("GRIDIRON_SUPERVISED", tc.envValue)
			runFullWizardHappyPath(t, h, "SUP", "SUP League")

			completion := h.rt.Completion()
			if completion == nil {
				t.Fatal("commit did not record a completion result")
			}
			if completion.Supervised != tc.supervised {
				t.Fatalf("Supervised = %v, want %v", completion.Supervised, tc.supervised)
			}
		})
	}
}

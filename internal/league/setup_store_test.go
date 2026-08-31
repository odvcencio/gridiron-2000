package league

import (
	"path/filepath"
	"testing"
	"time"
)

func newSetupTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("StartupError: %v", err)
	}
	return store
}

func TestSetupCompletionRoundTrip(t *testing.T) {
	store := newSetupTestStore(t)
	if _, found, err := store.SetupCompletion(); err != nil || found {
		t.Fatalf("fresh store: found=%v err=%v, want found=false", found, err)
	}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	completion := SetupCompletion{
		CompletedAt: now, CompletedBy: "commish@example.com",
		ConfigSHA256: "deadbeef", AppVersion: "1.2.3",
	}
	if err := store.MarkSetupComplete(completion); err != nil {
		t.Fatalf("MarkSetupComplete: %v", err)
	}
	got, found, err := store.SetupCompletion()
	if err != nil || !found {
		t.Fatalf("found=%v err=%v, want found=true", found, err)
	}
	if !got.CompletedAt.Equal(now) || got.CompletedBy != completion.CompletedBy ||
		got.ConfigSHA256 != completion.ConfigSHA256 || got.AppVersion != completion.AppVersion {
		t.Fatalf("SetupCompletion() = %+v, want %+v", got, completion)
	}
}

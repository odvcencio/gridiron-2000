package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRuntimeEnvFileAbsentIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	if err := loadRuntimeEnvFile(dir); err != nil {
		t.Fatalf("loadRuntimeEnvFile with no file: %v", err)
	}
}

func TestLoadRuntimeEnvFileSetsUnsetKeysOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COMMISSIONER_EMAILS", "real-env@example.com")
	t.Setenv("SESSION_SECRET", "")
	content := "COMMISSIONER_EMAILS=from-file@example.com\nSESSION_SECRET=file-secret\n"
	if err := os.WriteFile(runtimeEnvPath(dir), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadRuntimeEnvFile(dir); err != nil {
		t.Fatal(err)
	}
	// Real env wins: COMMISSIONER_EMAILS was already set, so the file's
	// value must not override it.
	if got := os.Getenv("COMMISSIONER_EMAILS"); got != "real-env@example.com" {
		t.Fatalf("COMMISSIONER_EMAILS = %q, want the real env value preserved", got)
	}
	// SESSION_SECRET was unset (t.Setenv to "" still counts as "unset" per
	// loadRuntimeEnvFile's TrimSpace check), so the file's value applies.
	if got := os.Getenv("SESSION_SECRET"); got != "file-secret" {
		t.Fatalf("SESSION_SECRET = %q, want file-secret", got)
	}
}

func TestLoadRuntimeEnvFileIgnoresCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IDENTITY_ALIASES", "")
	content := "# a comment\n\nIDENTITY_ALIASES=alt@example.com=commish@example.com\n"
	if err := os.WriteFile(runtimeEnvPath(dir), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadRuntimeEnvFile(dir); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("IDENTITY_ALIASES"); got != "alt@example.com=commish@example.com" {
		t.Fatalf("IDENTITY_ALIASES = %q", got)
	}
}

func TestLoadRuntimeEnvFileRejectsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(runtimeEnvPath(dir), []byte("NOT_A_VALID_LINE_NO_EQUALS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadRuntimeEnvFile(dir); err == nil {
		t.Fatal("expected an error for a malformed runtime.env line")
	}
}

func TestRenderRuntimeEnvContent(t *testing.T) {
	content := string(renderRuntimeEnv("commish@example.com", []identityAliasPair{
		{Alias: "alt@example.com", Canonical: "commish@example.com"},
	}, "supersecret"))
	for _, want := range []string{
		"COMMISSIONER_EMAILS=commish@example.com",
		"IDENTITY_ALIASES=alt@example.com=commish@example.com",
		"SESSION_SECRET=supersecret",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered runtime.env missing %q:\n%s", want, content)
		}
	}
}

func TestWriteFileAtomicWritesModeAndContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := writeFileAtomic(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 0600", perm)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want hello", got)
	}
	// No leftover temp files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory has %d entries, want exactly 1 (out.txt)", len(entries))
	}
}

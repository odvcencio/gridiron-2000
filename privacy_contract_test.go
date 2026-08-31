package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var trackedEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// TestTrackedPublicRepositoryPrivacyContract keeps examples and source safe to
// publish. The forbidden values are assembled from pieces so this contract
// does not reintroduce the exact personal tokens it is designed to catch.
func TestTrackedPublicRepositoryPrivacyContract(t *testing.T) {
	root := privacyRepositoryRoot(t)
	paths, err := privacyScanPaths(root)
	if err != nil {
		t.Fatal(err)
	}

	var violations []string
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read tracked file %s: %v", rel, err)
		}
		if !utf8.Valid(body) {
			continue
		}
		text := string(body)
		lower := strings.ToLower(text)
		for _, token := range privacyForbiddenTokens() {
			if strings.Contains(lower, strings.ToLower(token)) {
				violations = append(violations, fmt.Sprintf("%s contains prohibited personal token", rel))
			}
		}
		for _, email := range trackedEmailPattern.FindAllString(text, -1) {
			if !reservedPrivacyEmail(email) {
				violations = append(violations, fmt.Sprintf("%s contains a non-reserved email-shaped value", rel))
			}
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("public-repository privacy contract failed:\n- %s", strings.Join(violations, "\n- "))
	}
}

func privacyRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed while locating repository root")
	}
	sourceDir := filepath.Dir(source)
	candidates := []string{sourceDir}
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, workingDir)
		if !filepath.IsAbs(sourceDir) {
			candidates = append(candidates, filepath.Join(workingDir, sourceDir))
		}
	}
	for _, candidate := range candidates {
		root, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
	}
	t.Fatalf("could not locate repository root from source %q or working directory", source)
	return ""
}

func privacyScanPaths(root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		paths, err := gitPrivacyPaths(root)
		if err != nil {
			return nil, err
		}
		return paths, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat repository metadata: %w", err)
	}
	return walkPrivacyPaths(root)
}

func gitPrivacyPaths(root string) ([]string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	trimmed := strings.TrimSuffix(string(output), "\x00")
	if trimmed == "" {
		return nil, nil
	}
	var paths []string
	for _, raw := range strings.Split(trimmed, "\x00") {
		path := filepath.ToSlash(raw)
		if path == "" {
			continue
		}
		if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, "../") {
			return nil, fmt.Errorf("git ls-files returned unsafe relative path")
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func walkPrivacyPaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel != "." && privacyPathExcluded(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if privacyPathExcluded(rel) || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repository: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func privacyPathExcluded(path string) bool {
	normalized := filepath.ToSlash(path)
	base := filepath.Base(normalized)
	if base == ".env" || strings.HasSuffix(base, ".log") || strings.HasSuffix(base, ".orig") {
		return true
	}
	if normalized == "league.json" || normalized == "config/league.json" {
		return true
	}
	// .claude holds agent-tool worktrees and session state: never repository
	// content, and its checkouts duplicate tracked files under scan.
	for _, privatePrefix := range []string{".claude", "deploy/local", "docs/plans", "docs/superpowers"} {
		if normalized == privatePrefix || strings.HasPrefix(normalized, privatePrefix+"/") {
			return true
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		switch part {
		case ".git", ".buckley", ".cache", ".canopy", ".gosx", ".analyses", "artifacts", "build", "data", "dist", "node_modules", "vendor":
			return true
		}
	}
	return false
}

func privacyForbiddenTokens() []string {
	return []string{
		strings.Join([]string{"os", "car", ".", "villavi", "cencio", "@", "stablekernel.com"}, ""),
		strings.Join([]string{"os", "car", "@", "m31labs.dev"}, ""),
		strings.Join([]string{
			strings.Join([]string{"os", "car"}, ""),
			strings.Join([]string{"villavi", "cencio"}, ""),
		}, " "),
		strings.Join([]string{"os", "car", "'", "s"}, ""),
		strings.Join([]string{"os", "car"}, ""),
		strings.Join([]string{"villavi", "cencio"}, ""),
		strings.Join([]string{"/home/", "draco"}, ""),
		strings.Join([]string{"os", "car", "villavi", "cencio", "@", "Melanies", "-", "MacBook-Air.local"}, ""),
		strings.Join([]string{"Melanies", "-", "MacBook-Air.local"}, ""),
	}
}

func reservedPrivacyEmail(email string) bool {
	separator := strings.LastIndexByte(email, '@')
	if separator < 1 || separator == len(email)-1 {
		return false
	}
	domain := strings.ToLower(strings.TrimSuffix(email[separator+1:], "."))
	for _, reserved := range []string{"example.com", "example.org", "example.net"} {
		if domain == reserved || strings.HasSuffix(domain, "."+reserved) {
			return true
		}
	}
	// RFC-reserved special-use domains are safe in credential and fixture
	// examples too, including service.example and owner@example.test.
	for _, reservedTLD := range []string{"example", "test", "invalid", "localhost"} {
		if domain == reservedTLD || strings.HasSuffix(domain, "."+reservedTLD) {
			return true
		}
	}
	return false
}

func TestPrivacyGitPathSelectionIncludesTrackedAndPublicFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		".gitignore":             ".env\n",
		"docs/plans/tracked.txt": "tracked plan fixture\n",
		".env":                   "operator-only fixture\n",
		"public-untracked.txt":   "public fixture\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runPrivacyGitCommand(t, root, "init", "--quiet")
	runPrivacyGitCommand(t, root, "add", ".gitignore", "docs/plans/tracked.txt")

	paths, err := privacyScanPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if !privacyHasPath(paths, "docs/plans/tracked.txt") || !privacyHasPath(paths, "public-untracked.txt") {
		t.Fatalf("git scan paths = %#v, want tracked and public files", paths)
	}
	if privacyHasPath(paths, ".env") {
		t.Fatalf("git scan included ignored operator file: %#v", paths)
	}
}

func TestPrivacyArchiveFallbackExcludesOperatorFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		".env":               "operator-only fixture\n",
		"docs/plans/plan.md": "operator-only plan\n",
		"public.txt":         "public fixture\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := privacyScanPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if !privacyHasPath(paths, "public.txt") || privacyHasPath(paths, ".env") || privacyHasPath(paths, "docs/plans/plan.md") {
		t.Fatalf("archive scan paths = %#v, want only public fixture among test files", paths)
	}
}

func runPrivacyGitCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	if _, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func privacyHasPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

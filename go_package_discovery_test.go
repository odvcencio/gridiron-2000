package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestGoCheckDiscoversEveryTrackedPackageExactlyOnce(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot, "scripts", "go-check.sh")
	actual := runGoCheck(t, repoRoot, script, "list")
	if len(actual) == 0 {
		t.Fatal("go-check list returned no packages")
	}
	if !sort.StringsAreSorted(actual) {
		t.Fatalf("go-check list is not sorted: %v", actual)
	}
	if duplicate := duplicateString(actual); duplicate != "" {
		t.Fatalf("go-check list contains package %q more than once", duplicate)
	}

	dirs := trackedGoDirectories(t, repoRoot)
	expected := explicitGoList(t, repoRoot, dirs)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("go-check package list differs from explicit tracked-directory go list\nactual:\n%s\nexpected:\n%s", strings.Join(actual, "\n"), strings.Join(expected, "\n"))
	}
	for _, required := range []string{"gridiron-2000", "gridiron-2000/app/help", "gridiron-2000/app/help/_topic_id"} {
		if !containsString(actual, required) {
			t.Errorf("go-check list omitted required package %q", required)
		}
	}

	fixture := newGoCheckFixture(t, script)
	fixturePackages := runGoCheck(t, fixture, filepath.Join(fixture, "scripts", "go-check.sh"), "list")
	wantFixture := []string{"fixture.local/pkg/_hidden", "fixture.local/pkg/ordinary"}
	if strings.Join(fixturePackages, "\n") != strings.Join(wantFixture, "\n") {
		t.Fatalf("fixture package list = %v, want %v; untracked generated directories must be ignored", fixturePackages, wantFixture)
	}
}

func TestGoCheckForwardsTestAndVetFlags(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot, "scripts", "go-check.sh")
	fixture := newGoCheckFixture(t, script)
	for _, command := range []string{"test", "vet"} {
		code, output := runGoCheckExit(t, fixture, filepath.Join(fixture, "scripts", "go-check.sh"), command, "-this-flag-does-not-exist")
		if code == 0 {
			t.Fatalf("go-check %s ignored forwarded invalid flag; output=%s", command, output)
		}
	}
}

func TestGoCheckTestArgsCannotHideUnderscorePackage(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := newGoCheckFixture(t, filepath.Join(repoRoot, "scripts", "go-check.sh"))
	code, output := runGoCheckExit(t, fixture, filepath.Join(fixture, "scripts", "go-check.sh"), "test", "-run", "^TestHiddenFixture$", "-args", "-test.v")
	if code != 0 {
		t.Fatalf("go-check test with -args exit = %d: %s", code, output)
	}
	if !strings.Contains(output, "fixture.local/pkg/_hidden") {
		t.Fatalf("go-check test with -args omitted underscore package: %s", output)
	}
}

func TestGoCheckRejectsInvalidUsage(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot, "scripts", "go-check.sh")
	for _, args := range [][]string{{}, {"unknown"}, {"list", "-count=1"}} {
		code, _ := runGoCheckExit(t, repoRoot, script, args...)
		if code != 2 {
			t.Errorf("go-check %v exit = %d, want usage exit 2", args, code)
		}
	}
	code, output := runGoCheckExit(t, repoRoot, script, "--help")
	if code != 0 || !strings.Contains(output, "go-check.sh list") {
		t.Fatalf("go-check --help exit/output = %d/%q", code, output)
	}
}

func trackedGoDirectories(t *testing.T, repoRoot string) []string {
	t.Helper()
	raw := runCommand(t, repoRoot, "git", "ls-files", "-z", "--", "*.go")
	var dirs []string
	for _, pathBytes := range bytes.Split(raw, []byte{0}) {
		if len(pathBytes) == 0 {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(string(pathBytes)))
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return uniqueStrings(dirs)
}

func explicitGoList(t *testing.T, repoRoot string, dirs []string) []string {
	t.Helper()
	var packages []string
	for _, dir := range dirs {
		arg := "."
		if dir != "." {
			arg = "./" + dir
		}
		lines := strings.Fields(string(runCommand(t, repoRoot, "go", "list", arg)))
		if len(lines) != 1 {
			t.Fatalf("go list %q returned %v; every tracked source directory must resolve to exactly one package", arg, lines)
		}
		packages = append(packages, lines[0])
	}
	sort.Strings(packages)
	if duplicate := duplicateString(packages); duplicate != "" {
		t.Fatalf("explicit tracked directories resolve to duplicate package %q", duplicate)
	}
	return packages
}

func runGoCheck(t *testing.T, dir, script string, args ...string) []string {
	t.Helper()
	code, output := runGoCheckExit(t, dir, script, args...)
	if code != 0 {
		t.Fatalf("go-check %v exit = %d: %s", args, code, output)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func runGoCheckExit(t *testing.T, dir, script string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(script, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String() + stderr.String()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), stdout.String() + stderr.String()
	}
	t.Fatalf("run %s %v: %v", script, args, err)
	return -1, ""
}

func runCommand(t *testing.T, dir, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v: %v\n%s", name, args, err, output)
	}
	return output
}

func newGoCheckFixture(t *testing.T, sourceScript string) string {
	t.Helper()
	fixture := filepath.Join(t.TempDir(), "repo with spaces")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(fixture, "go.mod"), "module fixture.local\n\ngo 1.26\n", 0o644)
	writeFixtureFile(t, filepath.Join(fixture, "pkg", "ordinary", "ordinary.go"), "package ordinary\n\nconst Value = 1\n", 0o644)
	writeFixtureFile(t, filepath.Join(fixture, "pkg", "ordinary", "ordinary_test.go"), "package ordinary\n\nimport \"testing\"\n\nfunc TestFixture(t *testing.T) {}\n", 0o644)
	writeFixtureFile(t, filepath.Join(fixture, "pkg", "_hidden", "hidden.go"), "package hidden\n\nconst Value = 1\n", 0o644)
	writeFixtureFile(t, filepath.Join(fixture, "pkg", "_hidden", "hidden_test.go"), "package hidden\n\nimport \"testing\"\n\nfunc TestHiddenFixture(t *testing.T) {}\n", 0o644)
	writeFixtureFile(t, filepath.Join(fixture, "dist", "generated", "ghost.go"), "package ghost\n", 0o644)
	writeFixtureFile(t, filepath.Join(fixture, "pkg", "_generated", "ghost.go"), "package ghost\n", 0o644)
	script := filepath.Join(fixture, "scripts", "go-check.sh")
	source, err := os.ReadFile(sourceScript)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, script, string(source), 0o755)
	runCommand(t, fixture, "git", "init", "-q")
	runCommand(t, fixture, "git", "add", "go.mod", "pkg/ordinary/ordinary.go", "pkg/ordinary/ordinary_test.go", "pkg/_hidden/hidden.go", "pkg/_hidden/hidden_test.go", "scripts/go-check.sh")
	return fixture
}

func writeFixtureFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := []string{values[0]}
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func duplicateString(values []string) string {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return values[index]
		}
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

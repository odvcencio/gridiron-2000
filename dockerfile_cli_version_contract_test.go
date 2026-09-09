package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var goSXRequireLine = regexp.MustCompile(`(?m)^\s*(?:require\s+)?m31labs\.dev/gosx\s+(v[^\s]+)(?:\s+//.*)?\s*$`)
var hardcodedGoSXCLI = regexp.MustCompile(`m31labs\.dev/gosx/cmd/gosx@v[0-9]`)

const goSXVersionAwk = `$1 == "m31labs.dev/gosx" { print $2; exit } $1 == "require" && $2 == "m31labs.dev/gosx" { print $3; exit }`

// TestDockerfileDerivesGoSXCLIFromGoMod keeps the image's CLI and the
// application module on one source of truth. A copied @v0.55.2 command can
// otherwise keep building an older runtime after go.mod moves to a newer
// GoSX release.
func TestDockerfileDerivesGoSXCLIFromGoMod(t *testing.T) {
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	match := goSXRequireLine.FindStringSubmatch(string(goMod))
	if len(match) != 2 {
		t.Fatal("go.mod must declare m31labs.dev/gosx with a version")
	}
	version := match[1]

	dockerfile, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	docker := string(dockerfile)
	if hardcodedGoSXCLI.MatchString(docker) {
		t.Fatalf("Dockerfile hardcodes a GoSX CLI version; derive it from go.mod instead")
	}
	if strings.Contains(docker, "m31labs.dev/gosx "+version) {
		t.Fatalf("Dockerfile hardcodes the GoSX module version %s; derive it from go.mod instead", version)
	}

	for _, want := range []string{
		"awk '" + goSXVersionAwk + "' go.mod",
		`gosx_version="$(awk`,
		`go mod download "m31labs.dev/gosx@$gosx_version"`,
		`go install "m31labs.dev/gosx/cmd/gosx@$gosx_version"`,
		`ERROR: go.mod must declare m31labs.dev/gosx`,
		`ERROR: cannot fetch m31labs.dev/gosx@$gosx_version`,
		`ERROR: cannot install GoSX CLI @$gosx_version`,
	} {
		if !strings.Contains(docker, want) {
			t.Errorf("Dockerfile omitted dynamic GoSX build contract %q", want)
		}
	}
}

// TestDockerfileGoSXVersionExtractionFixtures executes the exact awk program
// used by Dockerfile against both supported require forms and a missing pin.
// This keeps the shell parser behavior covered, not only the surrounding
// Dockerfile text.
func TestDockerfileGoSXVersionExtractionFixtures(t *testing.T) {
	for _, fixture := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "require block",
			body: "module fixture.local\n\nrequire (\n\tm31labs.dev/gosx v0.55.2\n)\n",
			want: "v0.55.2",
		},
		{
			name: "single require",
			body: "module fixture.local\n\nrequire m31labs.dev/gosx v0.55.2\n",
			want: "v0.55.2",
		},
		{
			name: "missing require",
			body: "module fixture.local\n\nrequire example.com/other v1.0.0\n",
			want: "",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			cmd := exec.Command("awk", goSXVersionAwk)
			cmd.Stdin = strings.NewReader(fixture.body)
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("run Dockerfile's GoSX version parser: %v", err)
			}
			if got := strings.TrimSpace(string(output)); got != fixture.want {
				t.Fatalf("GoSX version parser returned %q, want %q", got, fixture.want)
			}
		})
	}
}

// Command fleetgen compiles an explicit fleet document and publishes the
// resulting reviewable Kubernetes bundle. It never talks to Kubernetes, DNS,
// OAuth, a registry, or a secret store.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gridiron-2000/internal/fleetconfig"
)

const usage = `Usage:
  fleetgen render --file <fleet.json> --out <directory>
  fleetgen check  --file <fleet.json> --out <directory>

Commands:
  render  validate, compile, and atomically publish a deterministic bundle
  check   validate, compile, and compare the expected bundle read-only

Options:
  --file <path>  explicit fleet document; league paths resolve beside it
  --out <dir>    explicit generated/owned output directory
  -h, --help     show this help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}
	if isHelp(args[0]) || args[0] == "help" || args[0] == "usage" {
		writeUsage(stdout)
		return 0
	}
	command := args[0]
	if command != "render" && command != "check" {
		fmt.Fprintf(stderr, "fleetgen: usage error: unknown command %q\n", command)
		writeUsage(stderr)
		return 2
	}
	file, out, err := parseOptions(args[1:])
	if err != nil {
		if err == errHelp {
			writeUsage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "fleetgen: usage error: %v\n", err)
		writeUsage(stderr)
		return 2
	}

	fleet, warnings, err := fleetconfig.Load(file)
	if err != nil {
		fmt.Fprintf(stderr, "fleetgen %s: %v\n", command, err)
		return 1
	}
	bundle, err := fleetconfig.CompileFleet(fleet)
	if err != nil {
		fmt.Fprintf(stderr, "fleetgen %s: %v\n", command, err)
		return 1
	}
	for _, warning := range warnings {
		fmt.Fprintf(stderr, "fleetgen: warning: %s\n", warning)
	}
	expected, err := fleetconfig.ExpectedFiles(bundle)
	if err != nil {
		fmt.Fprintf(stderr, "fleetgen %s: %v\n", command, err)
		return 1
	}

	switch command {
	case "render":
		if err := fleetconfig.Publish(bundle, out); err != nil {
			fmt.Fprintf(stderr, "fleetgen render: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "fleetgen render: published %d files to %s\n", len(expected), out)
		return 0
	case "check":
		drift, err := fleetconfig.Check(bundle, out)
		if err != nil {
			fmt.Fprintf(stderr, "fleetgen check: %v\n", err)
			return 1
		}
		if !drift.Clean() {
			fmt.Fprint(stdout, drift.String())
			return 1
		}
		fmt.Fprintf(stdout, "fleetgen check: clean (%d files)\n", len(expected))
		return 0
	default:
		panic("unreachable")
	}
}

func parseOptions(args []string) (string, string, error) {
	var file, out string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-h", "--help":
			if len(args) != 1 {
				return "", "", fmt.Errorf("help cannot be combined with other options")
			}
			return "", "", errHelp
		case "--file":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return "", "", fmt.Errorf("--file requires a path")
			}
			if file != "" {
				return "", "", fmt.Errorf("--file specified more than once")
			}
			file = args[index+1]
			index++
		case "--out":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return "", "", fmt.Errorf("--out requires a directory")
			}
			if out != "" {
				return "", "", fmt.Errorf("--out specified more than once")
			}
			out = args[index+1]
			index++
		default:
			return "", "", fmt.Errorf("unknown option %q", args[index])
		}
	}
	if file == "" {
		return "", "", fmt.Errorf("--file is required")
	}
	if out == "" {
		return "", "", fmt.Errorf("--out is required")
	}
	return file, out, nil
}

var errHelp = fmt.Errorf("help requested")

func writeUsage(writer io.Writer) {
	fmt.Fprint(writer, usage)
}

func isHelp(value string) bool {
	return value == "-h" || value == "--help"
}

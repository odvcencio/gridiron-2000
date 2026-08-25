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
  fleetgen adopt  --file <fleet.json> --inventory <existing.json> [--format text|json]

Commands:
  render  validate, compile, and atomically publish a deterministic bundle
  check   validate, compile, and compare the expected bundle read-only
  adopt   compare a generated v2 bundle to an explicit existing-resource
          inventory and print a read-only adoption plan; never talks to a
          cluster and never reads Secret values

Options:
  --file <path>  explicit fleet document; league paths resolve beside it
  --out <dir>    explicit generated/owned output directory
  --inventory <path>  explicit secret/PII-free existing-resource inventory
  --format <text|json>  adoption plan output format (default: text)
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
	if command != "render" && command != "check" && command != "adopt" && command != "preflight" {
		fmt.Fprintf(stderr, "fleetgen: usage error: unknown command %q\n", command)
		writeUsage(stderr)
		return 2
	}
	if command == "adopt" || command == "preflight" {
		return runAdoption(args[1:], stdout, stderr)
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

func runAdoption(args []string, stdout, stderr io.Writer) int {
	file, inventoryPath, format, err := parseAdoptionOptions(args)
	if err != nil {
		if err == errHelp {
			writeUsage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "fleetgen adopt: usage error: %v\n", err)
		writeUsage(stderr)
		return 2
	}
	fleet, warnings, err := fleetconfig.Load(file)
	if err != nil {
		fmt.Fprintf(stderr, "fleetgen adopt: %v\n", err)
		return 1
	}
	bundle, err := fleetconfig.CompileFleet(fleet)
	if err != nil {
		fmt.Fprintf(stderr, "fleetgen adopt: %v\n", err)
		return 1
	}
	for _, warning := range warnings {
		fmt.Fprintf(stderr, "fleetgen: warning: %s\n", warning)
	}
	inventory, err := fleetconfig.LoadAdoptionInventory(inventoryPath)
	if err != nil {
		fmt.Fprintf(stderr, "fleetgen adopt: %v\n", err)
		return 1
	}
	plan, err := fleetconfig.PlanExistingAdoption(bundle, inventory)
	if err != nil {
		fmt.Fprintf(stderr, "fleetgen adopt: %v\n", err)
		return 1
	}
	if format == "json" {
		data, err := plan.JSON()
		if err != nil {
			fmt.Fprintf(stderr, "fleetgen adopt: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(data)
	} else {
		_, _ = io.WriteString(stdout, plan.Text())
	}
	if !plan.Ready {
		return 1
	}
	return 0
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

func parseAdoptionOptions(args []string) (string, string, string, error) {
	var file, inventory, format string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-h", "--help":
			if len(args) != 1 {
				return "", "", "", fmt.Errorf("help cannot be combined with other options")
			}
			return "", "", "", errHelp
		case "--file":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return "", "", "", fmt.Errorf("--file requires a path")
			}
			if file != "" {
				return "", "", "", fmt.Errorf("--file specified more than once")
			}
			file = args[index+1]
			index++
		case "--inventory":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return "", "", "", fmt.Errorf("--inventory requires a path")
			}
			if inventory != "" {
				return "", "", "", fmt.Errorf("--inventory specified more than once")
			}
			inventory = args[index+1]
			index++
		case "--format":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return "", "", "", fmt.Errorf("--format requires text or json")
			}
			if format != "" {
				return "", "", "", fmt.Errorf("--format specified more than once")
			}
			format = args[index+1]
			index++
		default:
			return "", "", "", fmt.Errorf("unknown option %q", args[index])
		}
	}
	if file == "" {
		return "", "", "", fmt.Errorf("--file is required")
	}
	if inventory == "" {
		return "", "", "", fmt.Errorf("--inventory is required")
	}
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return "", "", "", fmt.Errorf("--format must be text or json")
	}
	return file, inventory, format, nil
}

var errHelp = fmt.Errorf("help requested")

func writeUsage(writer io.Writer) {
	fmt.Fprint(writer, usage)
}

func isHelp(value string) bool {
	return value == "-h" || value == "--help"
}

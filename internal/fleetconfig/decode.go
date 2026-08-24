package fleetconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gridiron-2000/internal/league"
)

func load(path string) (Fleet, []Warning, error) {
	if strings.TrimSpace(path) == "" {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: resolve fleet document path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document %q: %w", path, err)
	}
	if info.IsDir() {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document %q is a directory", path)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: read fleet document %q: %w", path, err)
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document %q: %w", path, err)
	}
	var fleet Fleet
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fleet); err != nil {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document %q: trailing JSON data", path)
		}
		return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document %q: trailing JSON: %w", path, err)
	}
	if err := validateFleet(fleet, raw); err != nil {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: fleet document %q: %w", path, err)
	}

	fleet.FleetPath = abs
	fleet.Resolved = make([]ResolvedInstance, 0, len(fleet.Instances))
	warnings := make([]Warning, 0)
	var failures []string
	for _, instance := range fleet.Instances {
		resolvedPath, err := resolveLeaguePath(abs, instance.LeagueConfigPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("instance %q league path %q: %v", instance.ID, instance.LeagueConfigPath, err))
			continue
		}
		cfg, leagueWarnings, err := league.LoadConfigFile(resolvedPath)
		if err != nil {
			// The canonical loader's errors contain validation context and the
			// path, but never source bytes. Do not wrap or print file contents.
			failures = append(failures, fmt.Sprintf("instance %q league config %q: %v", instance.ID, instance.LeagueConfigPath, err))
			continue
		}
		source, err := os.ReadFile(resolvedPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("instance %q league config %q: read validated source: %v", instance.ID, instance.LeagueConfigPath, err))
			continue
		}
		source = normalizeSourceJSON(source)
		resolved := ResolvedInstance{
			Spec: instance, Path: resolvedPath, SourceJSON: source,
			Config: cfg, Warnings: append([]string(nil), leagueWarnings...),
		}
		fleet.Resolved = append(fleet.Resolved, resolved)
		for _, message := range leagueWarnings {
			warnings = append(warnings, Warning{InstanceID: instance.ID, Path: instance.LeagueConfigPath, Message: message})
		}
	}
	if len(failures) > 0 {
		return Fleet{}, nil, fmt.Errorf("fleetconfig: league preflight failed: %s", strings.Join(failures, "; "))
	}
	fleet.Warnings = append([]Warning(nil), warnings...)
	return fleet, warnings, nil
}
func resolveLeaguePath(fleetPath, relative string) (string, error) {
	if err := validateLeaguePathValue(relative); err != nil {
		return "", err
	}
	base := filepath.Dir(fleetPath)
	target := filepath.Join(base, filepath.Clean(relative))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the fleet document directory")
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve fleet directory: %w", err)
	}
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	realRel, err := filepath.Rel(baseReal, targetReal)
	if err != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the fleet document directory")
	}
	info, err := os.Stat(targetReal)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	return targetReal, nil
}

func normalizeSourceJSON(raw []byte) []byte {
	trimmed := bytes.TrimSpace(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	return append(append([]byte(nil), trimmed...), '\n')
}

// rejectDuplicateJSONKeys walks JSON tokens before decoding into the strict
// struct. encoding/json intentionally accepts duplicate object keys, so this
// explicit pass is required for fail-closed fleet input.
func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := walkJSONValue(decoder); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON data")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch delimiter := token.(type) {
	case json.Delim:
		switch delimiter {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				keyString, ok := key.(string)
				if !ok {
					return fmt.Errorf("object key is not a string")
				}
				if _, exists := seen[keyString]; exists {
					return fmt.Errorf("duplicate object key %q", keyString)
				}
				seen[keyString] = struct{}{}
				if err := walkJSONValue(decoder); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walkJSONValue(decoder); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected delimiter %q", delimiter)
		}
	}
	return nil
}

func requireFleetKeys(raw []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("invalid JSON object: %w", err)
	}
	for _, key := range []string{"version", "image", "statrelay_origin", "ingress_class", "certificate_issuer", "instances"} {
		value, ok := object[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

func requireInstanceKeys(raw []byte, index int) error {
	var object map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&object); err != nil {
		return err
	}
	instances, ok := object["instances"]
	if !ok {
		return fmt.Errorf("instances is required")
	}
	var values []json.RawMessage
	if err := json.Unmarshal(instances, &values); err != nil {
		return fmt.Errorf("instances must be an array")
	}
	if index >= len(values) {
		return fmt.Errorf("instances[%d] is missing", index)
	}
	var instance map[string]json.RawMessage
	if err := json.Unmarshal(values[index], &instance); err != nil {
		return fmt.Errorf("instances[%d] must be an object", index)
	}
	for _, key := range []string{"id", "namespace", "resource_prefix", "public_origin", "league_config_path", "pvc_storage", "hq_participant"} {
		value, ok := instance[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("instances[%d].%s is required", index, key)
		}
	}
	return nil
}

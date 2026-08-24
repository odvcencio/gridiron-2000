package v1fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func registryFixture(key, leagueID, secretEnv, secretFile string, enabled bool, order int) string {
	return fmt.Sprintf(`{
  "version": 1,
  "enabled": true,
  "connections": [%s]
}`, registryConnectionFixture(key, leagueID, secretEnv, secretFile, enabled, order))
}

func registryConnectionFixture(key, leagueID, secretEnv, secretFile string, enabled bool, order int) string {
	return fmt.Sprintf(`{
    "key": %q, "order": %d, "enabled": %t,
    "league_id": %q, "display_name": "League", "short_code": "LG",
    "accent": "cyan",
    "provider_origin": "http://provider.ns.svc.cluster.local:8091",
    "public_origin": "https://league.example",
    "capabilities": ["readiness.v1", "draft.v1", "data-health.v1"],
    "links": {
      "league": "/", "overview": "/", "join": "/join", "draft": "/draft",
      "board": "/board", "team": "/team", "players": "/players",
      "trades": "/trades", "pickem": "/pickem", "blitz": "/blitz",
      "activity": "/activity", "commissioner": "/admin"
    },
    "credential": {"key_id": "hq-key", "secret_env": %q, "secret_file": %q}
  }`, key, order, enabled, leagueID, secretEnv, secretFile)
}

func writeRegistry(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigFromEnvAbsentDisablesHosting(t *testing.T) {
	value, present := os.LookupEnv(RegistryEnvironment)
	if err := os.Unsetenv(RegistryEnvironment); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(RegistryEnvironment, value)
		}
	})
	config, err := ConfigFromEnv()
	if err != nil || config.Enabled || config.Connections != nil {
		t.Fatalf("ConfigFromEnv() = %#v, %v", config, err)
	}
}

func TestLoadResolvesValidEnabledConnectionAndPreservesCapabilities(t *testing.T) {
	t.Setenv("HQ_TEST_SECRET", strings.Repeat("s", 32))
	config, err := Load(writeRegistry(t, registryFixture("league", "league-id", "HQ_TEST_SECRET", "", true, 10)))
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || len(config.Connections) != 1 {
		t.Fatalf("config = %#v", config)
	}
	connection := config.Connections[0]
	if connection.Misconfigured || connection.Key != "league" || connection.PublicOrigin != "https://league.example" {
		t.Fatalf("connection = %#v", connection)
	}
	want := []string{"readiness.v1", "draft.v1", "data-health.v1"}
	if strings.Join(connection.Capabilities, ",") != strings.Join(want, ",") {
		t.Fatalf("capabilities = %v", connection.Capabilities)
	}
}

func TestLoadMissingEnabledSecretIsConnectionLocalMisconfiguration(t *testing.T) {
	_ = os.Unsetenv("HQ_MISSING_SECRET")
	config, err := Load(writeRegistry(t, registryFixture("league", "league-id", "HQ_MISSING_SECRET", "", true, 10)))
	if err != nil {
		t.Fatal(err)
	}
	if !config.Connections[0].Misconfigured {
		t.Fatalf("connection = %#v, want misconfigured", config.Connections[0])
	}
}

func TestLoadDisabledConnectionDoesNotReadSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "HOSTILE-UNREADABLE")
	config, err := Load(writeRegistry(t, registryFixture("league", "league-id", "", path, false, 10)))
	if err != nil {
		t.Fatal(err)
	}
	if config.Connections[0].Misconfigured || config.Connections[0].Enabled {
		t.Fatalf("disabled connection = %#v", config.Connections[0])
	}
}

func TestLoadAllowsCanonicalNullableCapabilityLinks(t *testing.T) {
	payload := registryFixture("league", "league-id", "HQ_UNUSED_SECRET", "", false, 10)
	payload = strings.Replace(payload,
		`"capabilities": ["readiness.v1", "draft.v1", "data-health.v1"]`,
		`"capabilities": []`, 1)
	for _, route := range []string{"draft", "board", "pickem", "blitz"} {
		payload = strings.Replace(payload, fmt.Sprintf(`%q: "/%s"`, route, route), fmt.Sprintf(`%q: null`, route), 1)
	}
	config, err := Load(writeRegistry(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	links := config.Connections[0].Links
	if links.Draft != nil || links.Board != nil || links.Pickem != nil || links.Blitz != nil {
		t.Fatalf("optional links = %#v", links)
	}
	if links.League == nil || links.Team == nil || links.Commissioner == nil {
		t.Fatalf("core links = %#v", links)
	}
}

func TestLoadSecretFileUsesExactBoundedBytes(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secretPath, append([]byte(strings.Repeat("s", 31)), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(writeRegistry(t, registryFixture("league", "league-id", "", secretPath, true, 10)))
	if err != nil || config.Connections[0].Misconfigured {
		t.Fatalf("exact 32-byte secret = %#v, %v", config, err)
	}
	if err := os.WriteFile(secretPath, []byte(strings.Repeat("x", maxSecretBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err = Load(writeRegistry(t, registryFixture("league", "league-id", "", secretPath, true, 10)))
	if err != nil || !config.Connections[0].Misconfigured {
		t.Fatalf("oversized secret = %#v, %v", config, err)
	}
}

func TestLoadRejectsUnsafeRegistryStructure(t *testing.T) {
	valid := registryFixture("league", "league-id", "HQ_TEST_SECRET", "", true, 10)
	cases := map[string]string{
		"unknown field":        strings.Replace(valid, `"version": 1,`, `"version": 1, "unknown": true,`, 1),
		"trailing":             valid + `{}`,
		"version":              strings.Replace(valid, `"version": 1`, `"version": 2`, 1),
		"public http":          strings.Replace(valid, "https://league.example", "http://league.example", 1),
		"public credentials":   strings.Replace(valid, "https://league.example", "https://user@league.example", 1),
		"public path":          strings.Replace(valid, "https://league.example", "https://league.example/private", 1),
		"public query":         strings.Replace(valid, "https://league.example", "https://league.example?secret=x", 1),
		"public bad port":      strings.Replace(valid, "https://league.example", "https://league.example:bad", 1),
		"provider public http": strings.Replace(valid, "http://provider.ns.svc.cluster.local:8091", "http://provider.example:8091", 1),
		"provider path":        strings.Replace(valid, "http://provider.ns.svc.cluster.local:8091", "http://provider.ns.svc.cluster.local:8091/private", 1),
		"bad link":             strings.Replace(valid, `"team": "/team"`, `"team": "/admin"`, 1),
		"both secrets":         strings.Replace(valid, `"secret_file": ""`, `"secret_file": "/secret"`, 1),
		"relative file":        strings.Replace(registryFixture("league", "league-id", "", "/secret", true, 10), `"secret_file": "/secret"`, `"secret_file": "secret"`, 1),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeRegistry(t, payload)); err == nil {
				t.Fatal("Load accepted unsafe registry")
			}
		})
	}
}

func TestLoadRejectsDuplicateIdentityAndOrdersRegardlessOfArrayOrder(t *testing.T) {
	payload := fmt.Sprintf(`{"version":1,"enabled":true,"connections":[%s,%s]}`,
		registryConnectionFixture("a", "league-a", "HQ_TEST_SECRET", "", false, 20),
		registryConnectionFixture("b", "league-b", "HQ_TEST_SECRET", "", false, 10))
	config, err := Load(writeRegistry(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	if config.Connections[0].Key != "b" || config.Connections[1].Key != "a" {
		t.Fatalf("order = %v, %v", config.Connections[0].Key, config.Connections[1].Key)
	}
	for name, duplicate := range map[string]string{
		"key":    strings.Replace(payload, `"key": "b"`, `"key": "a"`, 1),
		"league": strings.Replace(payload, `"league_id": "league-b"`, `"league_id": "league-a"`, 1),
		"order":  strings.Replace(payload, `"order": 10`, `"order": 20`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeRegistry(t, duplicate)); err == nil {
				t.Fatal("Load accepted duplicate registry identity")
			}
		})
	}
}

func TestLoadSupportsThirtyThirdConnectionAndRejectsSixtyFifth(t *testing.T) {
	objects := make([]string, 65)
	for index := range objects {
		objects[index] = registryConnectionFixture("league-"+fmt.Sprint(index), "league-id-"+fmt.Sprint(index), "HQ_TEST_SECRET", "", false, index)
	}
	for _, count := range []int{33, 64} {
		payload := fmt.Sprintf(`{"version":1,"enabled":true,"connections":[%s]}`, strings.Join(objects[:count], ","))
		config, err := Load(writeRegistry(t, payload))
		if err != nil || len(config.Connections) != count {
			t.Fatalf("count %d = %d, %v", count, len(config.Connections), err)
		}
	}
	payload := fmt.Sprintf(`{"version":1,"enabled":true,"connections":[%s]}`, strings.Join(objects, ","))
	if _, err := Load(writeRegistry(t, payload)); err == nil {
		t.Fatal("Load accepted 65 connections")
	}
}

func TestLoadErrorsDoNotLeakPathsOrSecretValues(t *testing.T) {
	hostilePath := filepath.Join(t.TempDir(), "HOSTILE-PATH")
	_, err := Load(hostilePath)
	if err == nil || strings.Contains(err.Error(), hostilePath) {
		t.Fatalf("path error = %v", err)
	}
	hostileSecret := "HOSTILE-SECRET-" + strings.Repeat("x", 32)
	t.Setenv("HQ_HOSTILE_SECRET", hostileSecret)
	payload := strings.Replace(registryFixture("league", "league-id", "HQ_HOSTILE_SECRET", "", true, 10), `"key_id": "hq-key"`, `"key_id": "bad key"`, 1)
	_, err = Load(writeRegistry(t, payload))
	if err != nil && (strings.Contains(err.Error(), hostileSecret) || strings.Contains(err.Error(), "bad key")) {
		t.Fatalf("secret error = %v", err)
	}
}

func TestResolvedConnectionFormattingCannotExposeCredentialOrProviderOrigin(t *testing.T) {
	secret := "HOSTILE-SECRET-" + strings.Repeat("x", 32)
	t.Setenv("HQ_FORMAT_SECRET", secret)
	payload := strings.Replace(registryFixture("league", "league-id", "HQ_FORMAT_SECRET", "", true, 10),
		"http://provider.ns.svc.cluster.local:8091", "http://HOSTILE-PROVIDER.ns.svc.cluster.local:8091", 1)
	payload = strings.Replace(payload, `"key_id": "hq-key"`, `"key_id": "HOSTILE-KEY-ID"`, 1)
	config, err := Load(writeRegistry(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Sprintf("%#v %#v", config, config.Connections[0])
	for _, sentinel := range []string{secret, "HOSTILE-PROVIDER", "HOSTILE-KEY-ID", "HQ_FORMAT_SECRET"} {
		if strings.Contains(formatted, sentinel) {
			t.Fatalf("connection formatting leaked %q: %s", sentinel, formatted)
		}
	}
}

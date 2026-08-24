package v1provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanProviderEnv(t *testing.T) {
	t.Helper()
	names := append([]string{"COMMISSIONER_INSTANCE_ID"}, providerEnvironment...)
	for _, name := range names {
		value, present := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		name, value, present := name, value, present
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

func setCompleteEnv(t *testing.T) {
	t.Helper()
	t.Setenv("COMMISSIONER_INSTANCE_ID", "gridiron")
	t.Setenv("COMMISSIONER_HQ_LEAGUE_ID", "league-gridiron")
	t.Setenv("COMMISSIONER_HQ_PROVIDER_KEY_ID", "hq-primary")
	t.Setenv("COMMISSIONER_HQ_PROVIDER_SECRET", strings.Repeat("s", 32))
}

func TestConfigFromEnvDisabledOnlyWhenProviderVariablesAreAbsent(t *testing.T) {
	cleanProviderEnv(t)
	t.Setenv("COMMISSIONER_INSTANCE_ID", "legacy-only")
	config, err := ConfigFromEnv()
	if err != nil || config.Enabled {
		t.Fatalf("ConfigFromEnv() = %#v, %v; want disabled", config, err)
	}

	t.Setenv("COMMISSIONER_HQ_PROVIDER_ADDR", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv() accepted a partial provider configuration")
	}
}

func TestConfigFromEnvCompleteSecretValue(t *testing.T) {
	cleanProviderEnv(t)
	setCompleteEnv(t)
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.InstanceID != "gridiron" || config.LeagueID != "league-gridiron" || config.Address != defaultAddress || config.Credential.KeyID() != "hq-primary" {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestConfigFromEnvSecretSourcesAreExclusiveAndExact(t *testing.T) {
	cleanProviderEnv(t)
	setCompleteEnv(t)
	secretPath := filepath.Join(t.TempDir(), "provider-secret")
	if err := os.WriteFile(secretPath, []byte(strings.Repeat("f", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMISSIONER_HQ_PROVIDER_SECRET_FILE", secretPath)
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv() accepted both secret sources")
	}

	if err := os.Unsetenv("COMMISSIONER_HQ_PROVIDER_SECRET"); err != nil {
		t.Fatal(err)
	}
	config, err := ConfigFromEnv()
	if err != nil || config.Credential.KeyID() != "hq-primary" {
		t.Fatalf("file ConfigFromEnv() = %#v, %v", config, err)
	}

	if err := os.WriteFile(secretPath, append([]byte(strings.Repeat("x", 30)), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv() accepted a 31-byte exact secret")
	}
}

func TestConfigFromEnvRejectsUnsafeSecretFilesWithoutLeakingInputs(t *testing.T) {
	cleanProviderEnv(t)
	setCompleteEnv(t)
	if err := os.Unsetenv("COMMISSIONER_HQ_PROVIDER_SECRET"); err != nil {
		t.Fatal(err)
	}
	hostilePath := filepath.Join(t.TempDir(), "secret-HOSTILE-PATH")
	t.Setenv("COMMISSIONER_HQ_PROVIDER_SECRET_FILE", hostilePath)
	_, err := ConfigFromEnv()
	if err == nil || strings.Contains(err.Error(), hostilePath) {
		t.Fatalf("unavailable file error = %v", err)
	}

	relative := "HOSTILE-RELATIVE-SECRET"
	t.Setenv("COMMISSIONER_HQ_PROVIDER_SECRET_FILE", relative)
	_, err = ConfigFromEnv()
	if err == nil || strings.Contains(err.Error(), relative) {
		t.Fatalf("relative file error = %v", err)
	}

	if err := os.WriteFile(hostilePath, []byte(strings.Repeat("z", maxSecretBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMISSIONER_HQ_PROVIDER_SECRET_FILE", hostilePath)
	_, err = ConfigFromEnv()
	if err == nil || strings.Contains(err.Error(), hostilePath) {
		t.Fatalf("oversized file error = %v", err)
	}
}

func TestConfigFromEnvRejectsIncompleteIdentityAndSecretLeak(t *testing.T) {
	cleanProviderEnv(t)
	hostile := "HOSTILE-SECRET-" + strings.Repeat("x", 32)
	t.Setenv("COMMISSIONER_HQ_LEAGUE_ID", "league")
	t.Setenv("COMMISSIONER_HQ_PROVIDER_KEY_ID", "key")
	t.Setenv("COMMISSIONER_HQ_PROVIDER_SECRET", hostile)
	if _, err := ConfigFromEnv(); err == nil || strings.Contains(err.Error(), hostile) {
		t.Fatalf("identity error = %v", err)
	}

	t.Setenv("COMMISSIONER_INSTANCE_ID", "instance")
	t.Setenv("COMMISSIONER_HQ_PROVIDER_SECRET", "too-short-HOSTILE")
	if _, err := ConfigFromEnv(); err == nil || strings.Contains(err.Error(), "too-short-HOSTILE") {
		t.Fatalf("credential error = %v", err)
	}
}

func TestConfigFromEnvRejectsProviderIdentitiesThatCannotValidate(t *testing.T) {
	for name, identity := range map[string]string{"control": "bad\nidentity", "oversized": strings.Repeat("x", 257)} {
		t.Run(name, func(t *testing.T) {
			cleanProviderEnv(t)
			setCompleteEnv(t)
			t.Setenv("COMMISSIONER_HQ_LEAGUE_ID", identity)
			if _, err := ConfigFromEnv(); err == nil || strings.Contains(err.Error(), identity) {
				t.Fatalf("identity error = %v", err)
			}
		})
	}
}

func TestConfigFromEnvValidatesNumericBindAddress(t *testing.T) {
	for _, address := range []string{":8091", "127.0.0.1:8091", "[::1]:8091"} {
		t.Run("valid_"+strings.NewReplacer(":", "_", "[", "", "]", "").Replace(address), func(t *testing.T) {
			cleanProviderEnv(t)
			setCompleteEnv(t)
			t.Setenv("COMMISSIONER_HQ_PROVIDER_ADDR", address)
			config, err := ConfigFromEnv()
			if err != nil || config.Address != address {
				t.Fatalf("ConfigFromEnv() = %#v, %v", config, err)
			}
		})
	}
	for _, address := range []string{"", "localhost:8091", "http://127.0.0.1:8091", ":0", ":65536", "8091"} {
		t.Run("invalid_"+strings.NewReplacer(":", "_", "/", "_", ".", "_").Replace(address), func(t *testing.T) {
			cleanProviderEnv(t)
			setCompleteEnv(t)
			t.Setenv("COMMISSIONER_HQ_PROVIDER_ADDR", address)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatalf("ConfigFromEnv() accepted %q", address)
			}
		})
	}
}

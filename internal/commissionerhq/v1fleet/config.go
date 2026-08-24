// Package v1fleet collects the frozen Commissioner HQ v1 contract from an
// operator-managed fleet while keeping every league's trust and failure domain
// isolated.
package v1fleet

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

const (
	RegistryEnvironment = "COMMISSIONER_HQ_V1_REGISTRY_FILE"
	maxRegistryBytes    = 1 << 20
	maxConnections      = 64
	maxSecretBytes      = 4096
)

var (
	connectionKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	tokenPattern         = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	secretEnvPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
)

type Config struct {
	Enabled     bool
	Connections []Connection
}

func (config Config) String() string   { return "Commissioner HQ v1 fleet configuration" }
func (config Config) GoString() string { return config.String() }

type Connection struct {
	Key          string
	Order        int
	Enabled      bool
	LeagueID     string
	DisplayName  string
	ShortCode    string
	Accent       string
	PublicOrigin string
	Capabilities []string
	Links        hqv1.Links
	target       v1transport.Target
	// Misconfigured is safe state, not a raw error. It is set only after the
	// registry's structural trust boundary has validated.
	Misconfigured bool
}

type registryDocument struct {
	Version     int                  `json:"version"`
	Enabled     *bool                `json:"enabled"`
	Connections []registryConnection `json:"connections"`
}

type registryConnection struct {
	Key            string             `json:"key"`
	Order          *int               `json:"order"`
	Enabled        *bool              `json:"enabled"`
	LeagueID       string             `json:"league_id"`
	DisplayName    string             `json:"display_name"`
	ShortCode      string             `json:"short_code"`
	Accent         string             `json:"accent"`
	ProviderOrigin string             `json:"provider_origin"`
	PublicOrigin   string             `json:"public_origin"`
	Capabilities   []string           `json:"capabilities"`
	Links          map[string]*string `json:"links"`
	Credential     registryCredential `json:"credential"`
}

type registryCredential struct {
	KeyID      string `json:"key_id"`
	SecretEnv  string `json:"secret_env"`
	SecretFile string `json:"secret_file"`
}

func ConfigFromEnv() (Config, error) {
	path, present := os.LookupEnv(RegistryEnvironment)
	if !present {
		return Config{}, nil
	}
	if path == "" || strings.TrimSpace(path) != path || !filepath.IsAbs(path) {
		return Config{}, errors.New("Commissioner HQ v1 registry path must be explicit and absolute")
	}
	return Load(path)
}

func Load(path string) (Config, error) {
	if path == "" || strings.TrimSpace(path) != path || !filepath.IsAbs(path) {
		return Config{}, errors.New("Commissioner HQ v1 registry path must be explicit and absolute")
	}
	file, err := os.Open(path)
	if err != nil {
		return Config{}, errors.New("Commissioner HQ v1 registry is unavailable")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxRegistryBytes+1))
	if err != nil {
		return Config{}, errors.New("Commissioner HQ v1 registry is unreadable")
	}
	if len(payload) > maxRegistryBytes {
		return Config{}, errors.New("Commissioner HQ v1 registry is too large")
	}
	return decodeRegistry(payload)
}

func decodeRegistry(payload []byte) (Config, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var document registryDocument
	if err := decoder.Decode(&document); err != nil {
		return Config{}, errors.New("Commissioner HQ v1 registry JSON is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, errors.New("Commissioner HQ v1 registry must contain one JSON value")
	}
	if document.Version != 1 || document.Enabled == nil || document.Connections == nil || len(document.Connections) > maxConnections {
		return Config{}, errors.New("Commissioner HQ v1 registry shape is invalid")
	}

	config := Config{Enabled: *document.Enabled, Connections: make([]Connection, 0, len(document.Connections))}
	keys := make(map[string]struct{}, len(document.Connections))
	orders := make(map[int]struct{}, len(document.Connections))
	leagues := make(map[string]struct{}, len(document.Connections))
	for _, raw := range document.Connections {
		connection, secretReference, err := validateConnection(raw)
		if err != nil {
			return Config{}, err
		}
		if _, duplicate := keys[connection.Key]; duplicate {
			return Config{}, errors.New("Commissioner HQ v1 registry has duplicate connection identity")
		}
		if _, duplicate := orders[connection.Order]; duplicate {
			return Config{}, errors.New("Commissioner HQ v1 registry has duplicate connection order")
		}
		if _, duplicate := leagues[connection.LeagueID]; duplicate {
			return Config{}, errors.New("Commissioner HQ v1 registry has duplicate league identity")
		}
		keys[connection.Key], orders[connection.Order], leagues[connection.LeagueID] = struct{}{}, struct{}{}, struct{}{}

		if config.Enabled && connection.Enabled {
			secret, ok := resolveSecret(secretReference)
			if !ok {
				connection.Misconfigured = true
			} else {
				credential, credentialErr := v1transport.NewCredentials(raw.Credential.KeyID, secret)
				target, targetErr := v1transport.NewTarget(raw.ProviderOrigin, connection.LeagueID, credential)
				if credentialErr != nil || targetErr != nil {
					connection.Misconfigured = true
				} else {
					connection.target = target
				}
			}
		}
		config.Connections = append(config.Connections, connection)
	}
	sort.Slice(config.Connections, func(i, j int) bool {
		if config.Connections[i].Order != config.Connections[j].Order {
			return config.Connections[i].Order < config.Connections[j].Order
		}
		return config.Connections[i].Key < config.Connections[j].Key
	})
	return config, nil
}

func (connection Connection) String() string {
	return "Commissioner HQ connection " + connection.Key
}

func (connection Connection) GoString() string { return connection.String() }

type secretReference struct {
	environment string
	file        string
}

func validateConnection(raw registryConnection) (Connection, secretReference, error) {
	if !connectionKeyPattern.MatchString(raw.Key) || raw.Order == nil || *raw.Order < 0 || raw.Enabled == nil ||
		!safeText(raw.LeagueID, 256) || !safeText(raw.DisplayName, 256) || !safeText(raw.ShortCode, 16) || !tokenPattern.MatchString(raw.Accent) {
		return Connection{}, secretReference{}, errors.New("Commissioner HQ v1 connection identity is invalid")
	}
	publicOrigin, ok := normalizePublicOrigin(raw.PublicOrigin)
	if !ok {
		return Connection{}, secretReference{}, errors.New("Commissioner HQ v1 public origin is invalid")
	}
	dummyCredential, _ := v1transport.NewCredentials("registry-validation", []byte(strings.Repeat("x", 32)))
	if _, err := v1transport.NewTarget(raw.ProviderOrigin, raw.LeagueID, dummyCredential); err != nil {
		return Connection{}, secretReference{}, errors.New("Commissioner HQ v1 provider origin is invalid")
	}
	capabilities, ok := normalizeCapabilities(raw.Capabilities)
	if !ok {
		return Connection{}, secretReference{}, errors.New("Commissioner HQ v1 capabilities are invalid")
	}
	links, ok := normalizeLinks(raw.Links, capabilities)
	if !ok {
		return Connection{}, secretReference{}, errors.New("Commissioner HQ v1 links are invalid")
	}
	if raw.Credential.KeyID == "" || strings.TrimSpace(raw.Credential.KeyID) != raw.Credential.KeyID {
		return Connection{}, secretReference{}, errors.New("Commissioner HQ v1 credential reference is invalid")
	}
	hasEnvironment := raw.Credential.SecretEnv != ""
	hasFile := raw.Credential.SecretFile != ""
	if hasEnvironment == hasFile || hasEnvironment && !secretEnvPattern.MatchString(raw.Credential.SecretEnv) ||
		hasFile && (!filepath.IsAbs(raw.Credential.SecretFile) || strings.TrimSpace(raw.Credential.SecretFile) != raw.Credential.SecretFile) {
		return Connection{}, secretReference{}, errors.New("Commissioner HQ v1 credential reference is invalid")
	}
	return Connection{
		Key: raw.Key, Order: *raw.Order, Enabled: *raw.Enabled, LeagueID: raw.LeagueID,
		DisplayName: raw.DisplayName, ShortCode: raw.ShortCode, Accent: raw.Accent,
		PublicOrigin: publicOrigin, Capabilities: capabilities, Links: links,
	}, secretReference{environment: raw.Credential.SecretEnv, file: raw.Credential.SecretFile}, nil
}

func resolveSecret(reference secretReference) ([]byte, bool) {
	if reference.environment != "" {
		secret, present := os.LookupEnv(reference.environment)
		return []byte(secret), present && len(secret) >= 32 && len(secret) <= maxSecretBytes
	}
	file, err := os.Open(reference.file)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	secret, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
	return secret, err == nil && len(secret) >= 32 && len(secret) <= maxSecretBytes
}

func normalizePublicOrigin(raw string) (string, bool) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || strings.ToLower(parsed.Scheme) != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := parsed.Port(); port != "" && port != "443" {
		host += ":" + port
	}
	return "https://" + host, true
}

func normalizeCapabilities(values []string) ([]string, bool) {
	if values == nil {
		return nil, false
	}
	result := append([]string(nil), values...)
	seen := make(map[string]struct{}, len(result))
	for _, value := range result {
		if !tokenPattern.MatchString(value) {
			return nil, false
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, false
		}
		seen[value] = struct{}{}
	}
	return result, true
}

func normalizeLinks(values map[string]*string, capabilities []string) (hqv1.Links, bool) {
	allowed := map[string]string{
		"league": "/", "overview": "/", "join": "/join", "draft": "/draft",
		"board": "/board", "team": "/team", "players": "/players", "trades": "/trades",
		"pickem": "/pickem", "blitz": "/blitz", "activity": "/activity", "commissioner": "/admin",
	}
	if len(values) != len(allowed) {
		return hqv1.Links{}, false
	}
	for key, want := range allowed {
		value, present := values[key]
		if !present || value != nil && *value != want {
			return hqv1.Links{}, false
		}
	}
	for key := range values {
		if _, present := allowed[key]; !present {
			return hqv1.Links{}, false
		}
	}
	for _, key := range []string{"league", "overview", "join", "team", "players", "trades", "activity", "commissioner"} {
		if values[key] == nil {
			return hqv1.Links{}, false
		}
	}
	hasDraft := false
	for _, capability := range capabilities {
		if capability == "draft.v1" {
			hasDraft = true
			break
		}
	}
	draftAvailable := values["draft"] != nil && values["board"] != nil
	if hasDraft != draftAvailable || (values["draft"] == nil) != (values["board"] == nil) {
		return hqv1.Links{}, false
	}
	return hqv1.Links{
		League: copyString(values["league"]), Overview: copyString(values["overview"]), Join: copyString(values["join"]),
		Draft: copyString(values["draft"]), Board: copyString(values["board"]), Team: copyString(values["team"]),
		Players: copyString(values["players"]), Trades: copyString(values["trades"]), Pickem: copyString(values["pickem"]),
		Blitz: copyString(values["blitz"]), Activity: copyString(values["activity"]), Commissioner: copyString(values["commissioner"]),
	}, true
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func safeText(value string, limit int) bool {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

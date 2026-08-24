// Package v1provider owns the league-local Commissioner HQ v1 listener
// configuration. It is intentionally separate from the legacy HQ peer
// configuration: the read-only HMAC credential must never be reused as the
// legacy bearer token.
package v1provider

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"gridiron-2000/internal/commissionerhq/v1transport"
)

const (
	defaultAddress = ":8091"
	maxSecretBytes = 4096
)

var providerEnvironment = []string{
	"COMMISSIONER_HQ_LEAGUE_ID",
	"COMMISSIONER_HQ_PROVIDER_KEY_ID",
	"COMMISSIONER_HQ_PROVIDER_SECRET",
	"COMMISSIONER_HQ_PROVIDER_SECRET_FILE",
	"COMMISSIONER_HQ_PROVIDER_ADDR",
}

// Config is either completely disabled or contains all identity, credential,
// and bind information required to start the private provider listener.
type Config struct {
	Enabled    bool
	InstanceID string
	LeagueID   string
	Address    string
	Credential v1transport.Credentials
}

// ConfigFromEnv is deliberately all-or-nothing. A checkout with no provider
// variables stays disabled; any provider variable opts in and must supply a
// complete, valid configuration.
func ConfigFromEnv() (Config, error) {
	configured := false
	for _, name := range providerEnvironment {
		if _, present := os.LookupEnv(name); present {
			configured = true
			break
		}
	}
	if !configured {
		return Config{}, nil
	}

	instanceID := strings.TrimSpace(os.Getenv("COMMISSIONER_INSTANCE_ID"))
	leagueID := strings.TrimSpace(os.Getenv("COMMISSIONER_HQ_LEAGUE_ID"))
	keyID := strings.TrimSpace(os.Getenv("COMMISSIONER_HQ_PROVIDER_KEY_ID"))
	if !validIdentity(instanceID) || !validIdentity(leagueID) || keyID == "" {
		return Config{}, errors.New("Commissioner HQ provider identity is incomplete")
	}

	secretValue, hasSecret := os.LookupEnv("COMMISSIONER_HQ_PROVIDER_SECRET")
	secretFile, hasSecretFile := os.LookupEnv("COMMISSIONER_HQ_PROVIDER_SECRET_FILE")
	if hasSecret == hasSecretFile {
		return Config{}, errors.New("Commissioner HQ provider requires exactly one secret source")
	}
	var secret []byte
	var err error
	if hasSecret {
		secret = []byte(secretValue)
	} else {
		secret, err = readSecretFile(secretFile)
		if err != nil {
			return Config{}, err
		}
	}
	credential, err := v1transport.NewCredentials(keyID, secret)
	if err != nil {
		return Config{}, errors.New("Commissioner HQ provider credential is invalid")
	}

	address := defaultAddress
	if value, present := os.LookupEnv("COMMISSIONER_HQ_PROVIDER_ADDR"); present {
		address = strings.TrimSpace(value)
	}
	if err := validateAddress(address); err != nil {
		return Config{}, err
	}
	return Config{
		Enabled:    true,
		InstanceID: instanceID,
		LeagueID:   leagueID,
		Address:    address,
		Credential: credential,
	}, nil
}

func readSecretFile(path string) ([]byte, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("Commissioner HQ provider secret file must be absolute")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("Commissioner HQ provider secret file is unavailable")
	}
	defer file.Close()
	secret, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
	if err != nil {
		return nil, errors.New("Commissioner HQ provider secret file is unreadable")
	}
	if len(secret) > maxSecretBytes {
		return nil, errors.New("Commissioner HQ provider secret file is too large")
	}
	return secret, nil
}

func validateAddress(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil || portText == "" {
		return errors.New("Commissioner HQ provider address must be a numeric host and port")
	}
	if host != "" && net.ParseIP(host) == nil {
		return errors.New("Commissioner HQ provider address host must be numeric")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("Commissioner HQ provider address port must be between 1 and 65535")
	}
	return nil
}

func validIdentity(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

package commissionerhq

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const maxPeers = 8

var instanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type Peer struct {
	ID      string
	BaseURL *url.URL
}

type Config struct {
	InstanceID string
	Token      string
	Peers      []Peer
	Timeout    time.Duration
}

func ConfigFromEnv() (Config, error) {
	instanceID := strings.TrimSpace(os.Getenv("COMMISSIONER_INSTANCE_ID"))
	if instanceID == "" {
		instanceID = "local"
	}
	if !instanceIDPattern.MatchString(instanceID) {
		return Config{}, fmt.Errorf("COMMISSIONER_INSTANCE_ID must match %s", instanceIDPattern)
	}
	timeout := 1500 * time.Millisecond
	if raw := strings.TrimSpace(os.Getenv("COMMISSIONER_HQ_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 || parsed > 10*time.Second {
			return Config{}, fmt.Errorf("COMMISSIONER_HQ_TIMEOUT must be between 0 and 10s")
		}
		timeout = parsed
	}
	peers, err := parsePeers(instanceID, os.Getenv("COMMISSIONER_HQ_PEERS"))
	if err != nil {
		return Config{}, err
	}
	token := strings.TrimSpace(os.Getenv("COMMISSIONER_HQ_TOKEN"))
	if len(peers) > 0 && token == "" {
		return Config{}, fmt.Errorf("COMMISSIONER_HQ_TOKEN is required when peers are configured")
	}
	return Config{InstanceID: instanceID, Token: token, Peers: peers, Timeout: timeout}, nil
}

func parsePeers(selfID, raw string) ([]Peer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxPeers {
		return nil, fmt.Errorf("COMMISSIONER_HQ_PEERS supports at most %d peers", maxPeers)
	}
	seen := map[string]bool{selfID: true}
	peers := make([]Peer, 0, len(parts))
	for _, part := range parts {
		id, rawURL, ok := strings.Cut(strings.TrimSpace(part), "=")
		id, rawURL = strings.TrimSpace(id), strings.TrimSpace(rawURL)
		if !ok || !instanceIDPattern.MatchString(id) || seen[id] {
			return nil, fmt.Errorf("invalid or duplicate commissioner peer %q", id)
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return nil, fmt.Errorf("commissioner peer %q must be an http(s) origin without credentials, path, query, or fragment", id)
		}
		parsed.Path, parsed.RawPath = "", ""
		seen[id] = true
		peers = append(peers, Peer{ID: id, BaseURL: parsed})
	}
	return peers, nil
}

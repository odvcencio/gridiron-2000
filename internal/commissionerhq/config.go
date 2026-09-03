package commissionerhq

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultFetchConcurrency = 8
	maxFetchConcurrency     = 64
)

var instanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type Peer struct {
	ID         string
	ServiceURL *url.URL
	PublicURL  *url.URL
}

type Config struct {
	InstanceID string
	Token      string
	Peers      []Peer
	Timeout    time.Duration
	// FetchConcurrency bounds simultaneous peer reads without limiting how
	// many league instances may participate in the fleet.
	FetchConcurrency int
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
	concurrency := defaultFetchConcurrency
	if raw := strings.TrimSpace(os.Getenv("COMMISSIONER_HQ_CONCURRENCY")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxFetchConcurrency {
			return Config{}, fmt.Errorf("COMMISSIONER_HQ_CONCURRENCY must be between 1 and %d", maxFetchConcurrency)
		}
		concurrency = parsed
	}
	peers, err := parsePeers(instanceID, os.Getenv("COMMISSIONER_HQ_PEERS"))
	if err != nil {
		return Config{}, err
	}
	token := strings.TrimSpace(os.Getenv("COMMISSIONER_HQ_TOKEN"))
	if len(peers) > 0 && token == "" {
		return Config{}, fmt.Errorf("COMMISSIONER_HQ_TOKEN is required when peers are configured")
	}
	return Config{
		InstanceID: instanceID, Token: token, Peers: peers, Timeout: timeout,
		FetchConcurrency: concurrency,
	}, nil
}

func parsePeers(selfID, raw string) ([]Peer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	seen := map[string]bool{selfID: true}
	peers := make([]Peer, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Count(part, "=") != 1 {
			return nil, fmt.Errorf("commissioner peer must use id=service|public grammar")
		}
		id, spec, _ := strings.Cut(part, "=")
		id, spec = strings.TrimSpace(id), strings.TrimSpace(spec)
		if !instanceIDPattern.MatchString(id) || seen[id] {
			return nil, fmt.Errorf("invalid or duplicate commissioner peer %q", id)
		}
		if strings.Count(spec, "|") != 1 {
			return nil, fmt.Errorf("commissioner peer %q must use id=service|public grammar", id)
		}
		serviceRaw, publicRaw, _ := strings.Cut(spec, "|")
		serviceURL, _, err := normalizeOrigin(serviceRaw)
		if err != nil {
			return nil, fmt.Errorf("commissioner peer %q service origin: %w", id, err)
		}
		publicURL, _, err := normalizeOrigin(publicRaw)
		if err != nil {
			return nil, fmt.Errorf("commissioner peer %q public origin: %w", id, err)
		}
		seen[id] = true
		peers = append(peers, Peer{ID: id, ServiceURL: serviceURL, PublicURL: publicURL})
	}
	return peers, nil
}

func normalizeOrigin(raw string) (*url.URL, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", fmt.Errorf("origin is empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
		return nil, "", fmt.Errorf("origin must be an absolute http(s) origin")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, "", fmt.Errorf("origin must use http or https")
	}
	if parsed.User != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(raw, "#") || parsed.Fragment != "" || parsed.RawFragment != "" {
		return nil, "", fmt.Errorf("origin cannot contain credentials, path, query, or fragment")
	}
	if parsed.Hostname() == "" {
		return nil, "", fmt.Errorf("origin host is empty")
	}
	port := parsed.Port()
	if strings.Contains(parsed.Host, ":") && port == "" && !strings.Contains(parsed.Host, "]") {
		return nil, "", fmt.Errorf("origin port is invalid")
	}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, "", fmt.Errorf("origin port is invalid")
		}
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if port != "" {
		host += ":" + port
	}
	normalized := scheme + "://" + host
	return &url.URL{Scheme: scheme, Host: host}, normalized, nil
}

func normalizedPublicURL(raw string) (string, error) {
	_, normalized, err := normalizeOrigin(raw)
	return normalized, err
}

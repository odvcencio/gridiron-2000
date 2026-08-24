package fleetconfig

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var hqTokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func validateLoadedFleet(fleet Fleet) error {
	if err := validateFleet(fleet, nil); err != nil {
		return err
	}
	for _, instance := range fleet.Instances {
		if fleet.ResolvedFor(instance.ID) == nil {
			return fmt.Errorf("fleetconfig: instance %q is missing league preflight", instance.ID)
		}
	}
	return nil
}

func validateFleet(fleet Fleet, raw []byte) error {
	if fleet.Version != SchemaVersion {
		return fmt.Errorf("schema version must be exactly %d", SchemaVersion)
	}
	if raw != nil {
		if err := requireFleetKeys(raw); err != nil {
			return err
		}
	}
	if err := validateImage(fleet.Image); err != nil {
		return fmt.Errorf("image: %w", err)
	}
	if _, _, err := validateOrigin(fleet.StatrelayOrigin, false); err != nil {
		return fmt.Errorf("statrelay_origin: %w", err)
	}
	if err := validateK8sQualifiedName("ingress_class", fleet.IngressClass); err != nil {
		return err
	}
	if err := validateK8sQualifiedName("certificate_issuer", fleet.CertificateIssuer); err != nil {
		return err
	}
	if len(fleet.Instances) == 0 {
		return fmt.Errorf("instances must contain at least one instance")
	}

	seenNames := map[string]string{}
	seenIDs := map[string]struct{}{}
	seenHosts := map[string]string{}
	seenCallbacks := map[string]string{}
	seenHQLeagueIDs := map[string]string{}
	seenHQOrders := map[int]string{}
	seenHQKeyIDs := map[string]string{}
	participantCount := 0
	hostCount := 0
	for index, instance := range fleet.Instances {
		if raw != nil {
			if err := requireInstanceKeys(raw, index); err != nil {
				return err
			}
		}
		if err := validateK8sName(fmt.Sprintf("instances[%d].id", index), instance.ID); err != nil {
			return err
		}
		if _, ok := seenIDs[instance.ID]; ok {
			return fmt.Errorf("duplicate instance id %q", instance.ID)
		}
		seenIDs[instance.ID] = struct{}{}
		if err := validateK8sName(fmt.Sprintf("instances[%d].namespace", index), instance.Namespace); err != nil {
			return err
		}
		if err := validateK8sName(fmt.Sprintf("instances[%d].resource_prefix", index), instance.ResourcePrefix); err != nil {
			return err
		}
		if err := validateResourceNames(instance.ResourcePrefix); err != nil {
			return fmt.Errorf("instance %q: %w", instance.ID, err)
		}
		if err := validateLeaguePathValue(instance.LeagueConfigPath); err != nil {
			return fmt.Errorf("instance %q league_config_path: %w", instance.ID, err)
		}
		if err := validateQuantity(instance.PVCStorage); err != nil {
			return fmt.Errorf("instance %q pvc_storage: %w", instance.ID, err)
		}
		if instance.CommissionerHQ != nil {
			participantCount++
			if participantCount > 64 {
				return fmt.Errorf("Commissioner HQ participants must not exceed 64")
			}
			if err := validateCommissionerHQ(instance.CommissionerHQ); err != nil {
				return fmt.Errorf("instance %q commissioner_hq: %w", instance.ID, err)
			}
			hq := instance.CommissionerHQ
			if previous, ok := seenHQLeagueIDs[hq.LeagueID]; ok {
				return fmt.Errorf("instances %q and %q have duplicate commissioner_hq league_id %q", previous, instance.ID, hq.LeagueID)
			}
			seenHQLeagueIDs[hq.LeagueID] = instance.ID
			if previous, ok := seenHQOrders[hq.Order]; ok {
				return fmt.Errorf("instances %q and %q have duplicate commissioner_hq order %d", previous, instance.ID, hq.Order)
			}
			seenHQOrders[hq.Order] = instance.ID
			if previous, ok := seenHQKeyIDs[hq.KeyID]; ok {
				return fmt.Errorf("instances %q and %q have duplicate commissioner_hq key_id %q", previous, instance.ID, hq.KeyID)
			}
			seenHQKeyIDs[hq.KeyID] = instance.ID
			if hq.Host {
				hostCount++
			}
		}
		publicOrigin, parsed, err := validateOrigin(instance.PublicOrigin, true)
		if err != nil {
			return fmt.Errorf("instance %q public_origin: %w", instance.ID, err)
		}
		if parsed.Hostname() == "" {
			return fmt.Errorf("instance %q public_origin: host is empty", instance.ID)
		}
		hostKey := strings.ToLower(parsed.Host)
		if previous, ok := seenHosts[hostKey]; ok {
			return fmt.Errorf("instances %q and %q have the same public host/origin %q", previous, instance.ID, parsed.Host)
		}
		seenHosts[hostKey] = instance.ID
		callback := publicOrigin + "/auth/google/callback"
		if previous, ok := seenCallbacks[callback]; ok {
			return fmt.Errorf("instances %q and %q have the same OAuth callback %q", previous, instance.ID, callback)
		}
		seenCallbacks[callback] = instance.ID

		for _, named := range []struct {
			kind string
			name string
		}{
			{kind: "namespace", name: instance.Namespace},
			{kind: "resource prefix", name: instance.ResourcePrefix},
			{kind: "deployment", name: instance.ResourcePrefix},
			{kind: "service", name: instance.ResourcePrefix},
			{kind: "pvc", name: instance.ResourcePrefix + "-data"},
			{kind: "league configmap", name: instance.ResourcePrefix + "-league-config"},
			{kind: "secret", name: instance.ResourcePrefix + "-secrets"},
			{kind: "ingress", name: instance.ResourcePrefix},
			{kind: "http ingress", name: instance.ResourcePrefix + "-http"},
			{kind: "redirect middleware", name: instance.ResourcePrefix + "-redirect-https"},
			{kind: "security middleware", name: instance.ResourcePrefix + "-security-headers"},
			{kind: "tls secret", name: instance.ResourcePrefix + "-tls"},
			{kind: "HQ provider service", name: instance.ResourcePrefix + "-hq-v1"},
			{kind: "HQ network policy", name: instance.ResourcePrefix + "-hq-v1-network-policy"},
			{kind: "HQ registry configmap", name: instance.ResourcePrefix + "-hq-v1-registry"},
			{kind: "HQ client secret", name: instance.ResourcePrefix + "-hq-v1-client-secrets"},
		} {
			if previous, ok := seenNames[named.name]; ok && previous != instance.ID {
				return fmt.Errorf("instance %q %s name %q collides with %s", instance.ID, named.kind, named.name, previous)
			}
			seenNames[named.name] = instance.ID
		}
	}
	if participantCount == 0 && hostCount != 0 {
		return fmt.Errorf("zero commissioner_hq participants requires zero hosts")
	}
	if participantCount > 0 && hostCount != 1 {
		return fmt.Errorf("commissioner_hq participants require exactly one host (got %d)", hostCount)
	}
	return nil
}

func validateCommissionerHQ(value *CommissionerHQ) error {
	if value == nil {
		return nil
	}
	if !hqTokenPattern.MatchString(value.LeagueID) {
		return fmt.Errorf("league_id must be a lowercase safe token")
	}
	if value.Order < 0 {
		return fmt.Errorf("order must be nonnegative")
	}
	if !hqTokenPattern.MatchString(value.Accent) {
		return fmt.Errorf("accent must be a lowercase safe token")
	}
	if !hqTokenPattern.MatchString(value.KeyID) {
		return fmt.Errorf("key_id must be a lowercase safe token")
	}
	return nil
}

func validateImage(image string) error {
	if strings.TrimSpace(image) != image || image == "" || strings.Count(image, "@") != 1 {
		return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
	}
	repository, digest, _ := strings.Cut(image, "@")
	if !imageDigestPattern.MatchString(digest) || strings.ToLower(repository) != repository || strings.Contains(repository, "//") {
		return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
	}
	components := strings.Split(repository, "/")
	for index, component := range components {
		if component == "" {
			return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
		}
		if strings.Contains(component, ":") {
			// Only the first component may be a registry with a numeric port.
			if index != 0 || strings.Count(component, ":") != 1 || len(components) < 2 {
				return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
			}
			host, port, _ := strings.Cut(component, ":")
			if host == "" || port == "" || !validRegistryHost(host) {
				return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
			}
			portNumber, err := strconv.Atoi(port)
			if err != nil || portNumber < 1 || portNumber > 65535 {
				return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
			}
			continue
		}
		if index == 0 && len(components) > 1 && strings.Contains(component, ".") && !validRegistryHost(component) {
			return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
		}
		if !imageComponentPattern.MatchString(component) {
			return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
		}
	}
	registryHost := isUnambiguousRegistryHost(components)
	pathComponents := components
	if registryHost {
		pathComponents = components[1:]
	}
	repositoryPath := strings.Join(pathComponents, "/")
	if !registryHost && len(components) == 1 {
		repositoryPath = "library/" + repositoryPath
	}
	if len(repositoryPath) > 255 {
		return fmt.Errorf("must be an immutable repository@sha256:<64 lowercase hex> image")
	}
	return nil
}

func validateK8sName(field, value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return fmt.Errorf("%s must be a non-empty lowercase Kubernetes name", field)
	}
	if len(value) > 63 || !k8sNamePattern.MatchString(value) {
		return fmt.Errorf("%s must be a lowercase Kubernetes name no longer than 63 characters", field)
	}
	return nil
}

func validateK8sQualifiedName(field, value string) error {
	if strings.TrimSpace(value) != value || value == "" || len(value) > 253 {
		return fmt.Errorf("%s must be a non-empty lowercase Kubernetes name", field)
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || !k8sNamePattern.MatchString(label) {
			return fmt.Errorf("%s must be a lowercase Kubernetes name", field)
		}
	}
	return nil
}

func validateResourceNames(prefix string) error {
	for _, suffix := range []string{"", "-data", "-league-config", "-secrets", "-http", "-redirect-https", "-security-headers", "-tls", "-hq-v1", "-hq-v1-network-policy", "-hq-v1-registry", "-hq-v1-client-secrets"} {
		name := prefix + suffix
		if len(name) > 63 || !k8sNamePattern.MatchString(name) {
			return fmt.Errorf("resource prefix %q produces unsafe %q (Kubernetes names must be <=63 characters)", prefix, name)
		}
	}
	return nil
}

func validateLeaguePathValue(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("must be a non-empty fleet-relative path")
	}
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return fmt.Errorf("must be a safe fleet-relative path")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("must be a safe fleet-relative path")
		}
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must not escape the fleet document directory")
	}
	return nil
}

func validateQuantity(value string) error {
	if strings.TrimSpace(value) != value || value == "" || !quantityPattern.MatchString(value) {
		return fmt.Errorf("must be a positive integer storage quantity in Mi or Gi")
	}
	return nil
}

func validateOrigin(raw string, public bool) (string, *url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", nil, fmt.Errorf("must be an absolute HTTP(S) origin without credentials, path, query, or fragment")
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", nil, fmt.Errorf("must be an absolute HTTP(S) origin without credentials, path, query, or fragment")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
		return "", nil, fmt.Errorf("must be an absolute HTTP(S) origin without credentials, path, query, or fragment")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" || public && scheme != "https" {
		return "", nil, fmt.Errorf("must use %s", map[bool]string{true: "HTTPS", false: "HTTP or HTTPS"}[public])
	}
	if parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" || strings.Contains(raw, "#") {
		return "", nil, fmt.Errorf("must be an origin with no credentials, path, query, or fragment")
	}
	hostname := parsed.Hostname()
	if hostname == "" || strings.ContainsAny(hostname, " \t\r\n") {
		return "", nil, fmt.Errorf("must contain a valid host")
	}
	if strings.ToLower(hostname) != hostname {
		return "", nil, fmt.Errorf("host must be lowercase")
	}
	if public {
		if err := validatePublicHostname(hostname); err != nil {
			return "", nil, err
		}
	} else if err := validateRelayHostname(hostname); err != nil {
		return "", nil, err
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return "", nil, fmt.Errorf("port is invalid")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", nil, fmt.Errorf("port is invalid")
		}
	}
	if strings.Contains(hostname, ":") && net.ParseIP(hostname) == nil {
		return "", nil, fmt.Errorf("host is invalid")
	}
	// Public ingress hostnames cannot carry a port. A relay may use a port.
	if public && parsed.Port() != "" {
		return "", nil, fmt.Errorf("public HTTPS origin must not include a port")
	}
	return raw, parsed, nil
}

func validatePublicHostname(hostname string) error {
	if net.ParseIP(hostname) != nil || looksLikeDottedIPv4(hostname) {
		return fmt.Errorf("public HTTPS origin host must be a DNS-1123 subdomain, not an IP address")
	}
	if err := validateDNSHostname(hostname); err != nil {
		return fmt.Errorf("public HTTPS origin host: %w", err)
	}
	return nil
}

func validateRelayHostname(hostname string) error {
	if net.ParseIP(hostname) != nil {
		return nil
	}
	if err := validateDNSHostname(hostname); err != nil {
		return fmt.Errorf("relay origin host: %w", err)
	}
	return nil
}

func validateDNSHostname(hostname string) error {
	if len(hostname) == 0 || len(hostname) > 253 {
		return fmt.Errorf("must be a lowercase DNS-1123 subdomain")
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || !k8sNamePattern.MatchString(label) {
			return fmt.Errorf("must be a lowercase DNS-1123 subdomain")
		}
	}
	return nil
}

func validRegistryHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	return validateDNSHostname(host) == nil
}

func isUnambiguousRegistryHost(components []string) bool {
	if len(components) < 2 {
		return false
	}
	component := components[0]
	return component == "localhost" || strings.ContainsAny(component, ".:")
}

func looksLikeDottedIPv4(hostname string) bool {
	parts := strings.Split(hostname, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

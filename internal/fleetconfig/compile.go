package fleetconfig

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Load reads path, strictly decodes the fleet document, resolves every league
// path relative to the document directory, and preflights every league with
// league.LoadConfigFile. It returns warnings only for successful preflight
// entries; an invalid entry makes the entire load fail.
func Load(path string) (Fleet, []Warning, error) {
	fleet, warnings, err := load(path)
	if err != nil {
		return Fleet{}, nil, err
	}
	return fleet, warnings, nil
}

// LoadFile is an explicit-name alias for Load.
func LoadFile(path string) (Fleet, []Warning, error) { return Load(path) }

// LoadDocument is an explicit-name alias for Load.
func LoadDocument(path string) (Document, []Warning, error) { return Load(path) }

// LoadFleet is a descriptive alias for Load.
func LoadFleet(path string) (Fleet, []Warning, error) { return Load(path) }

// Parse is a descriptive alias for Load.
func Parse(path string) (Fleet, []Warning, error) { return Load(path) }

// Compile accepts either a fleet document path, Fleet value returned from
// Load, or *Fleet value returned from Load. The path form is the usual API;
// accepting the loaded forms makes it possible to inspect preflight results
// and then compile without reading files a second time.
func Compile(input any) (Bundle, error) {
	switch value := input.(type) {
	case string:
		fleet, _, err := Load(value)
		if err != nil {
			return Bundle{}, err
		}
		return CompileFleet(fleet)
	case Fleet:
		return CompileFleet(value)
	case *Fleet:
		if value == nil {
			return Bundle{}, fmt.Errorf("fleetconfig: nil fleet document")
		}
		return CompileFleet(*value)
	default:
		return Bundle{}, fmt.Errorf("fleetconfig: compile expects a fleet path or loaded Fleet")
	}
}

// CompileFile compiles a fleet document path.
func CompileFile(path string) (Bundle, error) { return Compile(path) }

// LoadAndCompile is a descriptive alias for Compile's path form.
func LoadAndCompile(path string) (Bundle, error) { return Compile(path) }

// CompileFleet derives all resources from a Fleet returned by Load.
func CompileFleet(fleet Fleet) (Bundle, error) {
	if fleet.FleetPath == "" || len(fleet.Resolved) != len(fleet.Instances) {
		return Bundle{}, fmt.Errorf("fleetconfig: fleet must be loaded with Load before compilation")
	}
	if err := validateLoadedFleet(fleet); err != nil {
		return Bundle{}, err
	}
	derived, err := deriveInstances(fleet)
	if err != nil {
		return Bundle{}, err
	}

	// Files are generated from a sorted copy so input order cannot leak into
	// output bytes. Peer topology is also built from this same stable order.
	ordered := append([]DerivedInstance(nil), derived...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Spec.ID < ordered[j].Spec.ID })
	files := make([]File, 0, len(ordered)*9+1)
	for _, instance := range ordered {
		files = append(files, instanceFiles(instance, fleet.ResolvedFor(instance.Spec.ID), ordered)...)
	}
	files = append(files, File{Path: "operator-checklist.md", Data: []byte(checklist(fleet, ordered))})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	warnings := append([]Warning(nil), fleet.Warnings...)
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].InstanceID != warnings[j].InstanceID {
			return warnings[i].InstanceID < warnings[j].InstanceID
		}
		return warnings[i].Path < warnings[j].Path
	})
	return Bundle{Fleet: fleet, Instances: ordered, Warnings: warnings, Files: files}, nil
}

// CompileDocument compiles a loaded document.
func CompileDocument(document Document) (Bundle, error) { return CompileFleet(document) }

// DeriveInstances returns deterministic derived values without generating
// files. The Fleet must have been loaded with Load.
func DeriveInstances(fleet Fleet) ([]DerivedInstance, error) {
	if fleet.FleetPath == "" || len(fleet.Resolved) != len(fleet.Instances) {
		return nil, fmt.Errorf("fleetconfig: fleet must be loaded with Load before derivation")
	}
	if err := validateLoadedFleet(fleet); err != nil {
		return nil, err
	}
	return deriveInstances(fleet)
}

func (f Fleet) ResolvedFor(id string) *ResolvedInstance {
	for i := range f.Resolved {
		if f.Resolved[i].Spec.ID == id {
			return &f.Resolved[i]
		}
	}
	return nil
}

func deriveInstances(fleet Fleet) ([]DerivedInstance, error) {
	participants := make([]Instance, 0)
	for _, instance := range fleet.Instances {
		if instance.CommissionerHQ != nil {
			participants = append(participants, instance)
		}
	}
	sort.Slice(participants, func(i, j int) bool {
		left, right := participants[i].CommissionerHQ, participants[j].CommissionerHQ
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return participants[i].ID < participants[j].ID
	})

	derived := make([]DerivedInstance, 0, len(fleet.Instances))
	for _, instance := range fleet.Instances {
		publicOrigin, parsed, _ := validateOrigin(instance.PublicOrigin, true)
		service := fmt.Sprintf("http://%s.%s.svc.cluster.local", instance.ResourcePrefix, instance.Namespace)
		name := resourceNames(instance.ResourcePrefix)
		imageDigest := ""
		if _, digest, ok := strings.Cut(fleet.Image, "@"); ok {
			imageDigest = digest
		}
		hqProviderService, hqProviderOrigin := "", ""
		hqRegistryFile, hqRegistrySecret, hqRegistryConfigMap, hqNetworkPolicy := "", "", "", ""
		if instance.CommissionerHQ != nil {
			hqProviderService = name.ProviderService
			hqProviderOrigin = fmt.Sprintf("http://%s.%s.svc.cluster.local:8091", name.ProviderService, instance.Namespace)
			hqNetworkPolicy = name.NetworkPolicy
			if instance.CommissionerHQ.Host {
				hqRegistryFile = "/etc/gridiron-hq/registry.json"
				hqRegistrySecret = name.ClientSecret
				hqRegistryConfigMap = name.RegistryConfigMap
			}
		}
		derived = append(derived, DerivedInstance{
			Spec:               instance,
			Image:              fleet.Image,
			ImageDigest:        imageDigest,
			IngressClass:       fleet.IngressClass,
			CertificateIssuer:  fleet.CertificateIssuer,
			PublicHost:         parsed.Host,
			ServiceOrigin:      service,
			OAuthCallback:      publicOrigin + "/auth/google/callback",
			Tank01BaseURL:      fleet.StatrelayOrigin,
			HQProviderOrigin:   hqProviderOrigin,
			HQRegistryFile:     hqRegistryFile,
			SecurityHeaders:    name.SecurityHeaders,
			RedirectMiddleware: name.RedirectMiddleware,
			TLSSecret:          name.TLSSecret,
			Deployment:         name.Deployment,
			Service:            name.Service,
			PVC:                name.PVC,
			LeagueConfigMap:    name.LeagueConfigMap,
			Secret:             name.Secret,
			ProviderService:    hqProviderService,
			RegistryConfigMap:  hqRegistryConfigMap,
			ClientSecret:       hqRegistrySecret,
			NetworkPolicy:      hqNetworkPolicy,
			HTTPSIngress:       name.HTTPSIngress,
			HTTPIngress:        name.HTTPIngress,
		})
	}
	registry := registryJSON(derived, fleet)
	for index := range derived {
		if derived[index].Spec.CommissionerHQ != nil && derived[index].Spec.CommissionerHQ.Host {
			derived[index].HQRegistryJSON = registry
		}
	}
	return derived, nil
}

type resourceNameSet struct {
	Deployment, Service, PVC, LeagueConfigMap, Secret                         string
	ProviderService, RegistryConfigMap, ClientSecret, NetworkPolicy           string
	HTTPSIngress, HTTPIngress, RedirectMiddleware, SecurityHeaders, TLSSecret string
}

func resourceNames(prefix string) resourceNameSet {
	return resourceNameSet{
		Deployment:         prefix,
		Service:            prefix,
		PVC:                prefix + "-data",
		LeagueConfigMap:    prefix + "-league-config",
		Secret:             prefix + "-secrets",
		ProviderService:    prefix + "-hq-v1",
		RegistryConfigMap:  prefix + "-hq-v1-registry",
		ClientSecret:       prefix + "-hq-v1-client-secrets",
		NetworkPolicy:      prefix + "-hq-v1-network-policy",
		HTTPSIngress:       prefix,
		HTTPIngress:        prefix + "-http",
		RedirectMiddleware: prefix + "-redirect-https",
		SecurityHeaders:    prefix + "-security-headers",
		TLSSecret:          prefix + "-tls",
	}
}

type registryDocument struct {
	Version     int                  `json:"version"`
	Enabled     bool                 `json:"enabled"`
	Connections []registryConnection `json:"connections"`
}

type registryConnection struct {
	Key            string             `json:"key"`
	Order          int                `json:"order"`
	Enabled        bool               `json:"enabled"`
	LeagueID       string             `json:"league_id"`
	DisplayName    string             `json:"display_name"`
	ShortCode      string             `json:"short_code"`
	Accent         string             `json:"accent"`
	ProviderOrigin string             `json:"provider_origin"`
	PublicOrigin   string             `json:"public_origin"`
	Capabilities   []string           `json:"capabilities"`
	Links          map[string]string  `json:"links"`
	Credential     registryCredential `json:"credential"`
}

type registryCredential struct {
	KeyID     string `json:"key_id"`
	SecretEnv string `json:"secret_env"`
}

func registryJSON(instances []DerivedInstance, fleet Fleet) string {
	connections := make([]registryConnection, 0)
	for _, instance := range instances {
		if instance.Spec.CommissionerHQ == nil {
			continue
		}
		resolved := fleet.ResolvedFor(instance.Spec.ID)
		if resolved == nil {
			continue
		}
		hq := instance.Spec.CommissionerHQ
		connections = append(connections, registryConnection{
			Key: instance.Spec.ID, Order: hq.Order, Enabled: true,
			LeagueID: hq.LeagueID, DisplayName: resolved.Config.Name, ShortCode: resolved.Config.ShortCode,
			Accent: hq.Accent, ProviderOrigin: instance.HQProviderOrigin,
			PublicOrigin: instance.Spec.PublicOrigin,
			Capabilities: []string{"draft.v1", "readiness.v1", "data-health.v1"},
			Links:        canonicalRegistryLinks(),
			Credential:   registryCredential{KeyID: hq.KeyID, SecretEnv: clientSecretEnv(instance.Spec.ID)},
		})
	}
	sort.Slice(connections, func(i, j int) bool {
		if connections[i].Order != connections[j].Order {
			return connections[i].Order < connections[j].Order
		}
		return connections[i].Key < connections[j].Key
	})
	payload, _ := json.Marshal(registryDocument{Version: 1, Enabled: true, Connections: connections})
	return string(payload) + "\n"
}

func canonicalRegistryLinks() map[string]string {
	return map[string]string{
		"league": "/", "overview": "/", "join": "/join", "draft": "/draft",
		"board": "/board", "team": "/team", "players": "/players", "trades": "/trades",
		"pickem": "/pickem", "blitz": "/blitz", "activity": "/activity", "commissioner": "/admin",
	}
}

func clientSecretEnv(id string) string {
	return "COMMISSIONER_HQ_V1_SECRET_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
}

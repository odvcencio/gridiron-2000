package fleetconfig

import (
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
		files = append(files, instanceFiles(instance, fleet.ResolvedFor(instance.Spec.ID))...)
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
		if instance.HQParticipant {
			participants = append(participants, instance)
		}
	}
	sort.Slice(participants, func(i, j int) bool { return participants[i].ID < participants[j].ID })

	derived := make([]DerivedInstance, 0, len(fleet.Instances))
	for _, instance := range fleet.Instances {
		publicOrigin, parsed, _ := validateOrigin(instance.PublicOrigin, true)
		service := fmt.Sprintf("http://%s.%s.svc.cluster.local", instance.ResourcePrefix, instance.Namespace)
		peers := make([]Peer, 0)
		if instance.HQParticipant {
			for _, peer := range participants {
				if peer.ID == instance.ID {
					continue
				}
				peerPublic, _, _ := validateOrigin(peer.PublicOrigin, true)
				peers = append(peers, Peer{
					ID:            peer.ID,
					ServiceOrigin: fmt.Sprintf("http://%s.%s.svc.cluster.local", peer.ResourcePrefix, peer.Namespace),
					PublicOrigin:  peerPublic,
				})
			}
		}
		peerParts := make([]string, 0, len(peers))
		for _, peer := range peers {
			peerParts = append(peerParts, peer.ID+"="+peer.ServiceOrigin+"|"+peer.PublicOrigin)
		}
		name := resourceNames(instance.ResourcePrefix)
		derived = append(derived, DerivedInstance{
			Spec:               instance,
			Image:              fleet.Image,
			IngressClass:       fleet.IngressClass,
			CertificateIssuer:  fleet.CertificateIssuer,
			PublicHost:         parsed.Host,
			ServiceOrigin:      service,
			OAuthCallback:      publicOrigin + "/auth/google/callback",
			Tank01BaseURL:      fleet.StatrelayOrigin,
			HQPeers:            peers,
			HQPeersValue:       strings.Join(peerParts, ","),
			SecurityHeaders:    name.SecurityHeaders,
			RedirectMiddleware: name.RedirectMiddleware,
			TLSSecret:          name.TLSSecret,
			Deployment:         name.Deployment,
			Service:            name.Service,
			PVC:                name.PVC,
			LeagueConfigMap:    name.LeagueConfigMap,
			Secret:             name.Secret,
			HTTPSIngress:       name.HTTPSIngress,
			HTTPIngress:        name.HTTPIngress,
		})
	}
	return derived, nil
}

type resourceNameSet struct {
	Deployment, Service, PVC, LeagueConfigMap, Secret                         string
	HTTPSIngress, HTTPIngress, RedirectMiddleware, SecurityHeaders, TLSSecret string
}

func resourceNames(prefix string) resourceNameSet {
	return resourceNameSet{
		Deployment:         prefix,
		Service:            prefix,
		PVC:                prefix + "-data",
		LeagueConfigMap:    prefix + "-league-config",
		Secret:             prefix + "-secrets",
		HTTPSIngress:       prefix,
		HTTPIngress:        prefix + "-http",
		RedirectMiddleware: prefix + "-redirect-https",
		SecurityHeaders:    prefix + "-security-headers",
		TLSSecret:          prefix + "-tls",
	}
}

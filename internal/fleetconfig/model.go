// Package fleetconfig validates a strict fleet document and derives the
// Kubernetes resources needed to run several isolated Gridiron instances.
//
// The package deliberately has no filesystem publication or Kubernetes API
// side effects. Loading reads the fleet document and each explicitly named
// league file; compilation returns deterministic in-memory files for an
// operator to review and apply separately.
package fleetconfig

import (
	"fmt"
	"regexp"

	"gridiron-2000/internal/league"
)

// SchemaVersion is the only fleet document schema this compiler accepts.
const SchemaVersion = 1

// FleetSchemaVersion is retained as a descriptive alias for callers that
// prefer the longer constant name.
const FleetSchemaVersion = SchemaVersion

// Fleet is the strict fleet.json wire document. The exported bookkeeping
// fields are populated by Load and are never accepted from JSON.
type Fleet struct {
	Version           int        `json:"version"`
	Image             string     `json:"image"`
	StatrelayOrigin   string     `json:"statrelay_origin"`
	IngressClass      string     `json:"ingress_class"`
	CertificateIssuer string     `json:"certificate_issuer"`
	Instances         []Instance `json:"instances"`

	// FleetPath is the absolute path used for resolution. Resolved contains
	// the validated league source and warnings, in input order.
	FleetPath string             `json:"-"`
	Resolved  []ResolvedInstance `json:"-"`
	Warnings  []Warning          `json:"-"`
}

// Document is a semantic alias for Fleet.
type Document = Fleet

// FleetDocument is a descriptive alias for Document.
type FleetDocument = Fleet

// Config is a semantic alias for Fleet, useful to callers that use config
// terminology for the input document.
type Config = Fleet

// Instance is one ordered fleet member. All string fields are required by
// the wire schema, including hq_participant (whose false value is meaningful).
type Instance struct {
	ID               string `json:"id"`
	Namespace        string `json:"namespace"`
	ResourcePrefix   string `json:"resource_prefix"`
	PublicOrigin     string `json:"public_origin"`
	LeagueConfigPath string `json:"league_config_path"`
	PVCStorage       string `json:"pvc_storage"`
	HQParticipant    bool   `json:"hq_participant"`
}

// FleetInstance is a descriptive alias for Instance.
type FleetInstance = Instance

// InstanceSpec is a descriptive alias for Instance.
type InstanceSpec = Instance

// Warning is a nonfatal warning returned by the canonical league loader,
// attributed to the instance and fleet-relative source path that produced it.
type Warning struct {
	InstanceID string `json:"instance_id"`
	Path       string `json:"path"`
	Message    string `json:"message"`
}

func (w Warning) String() string {
	return fmt.Sprintf("instance %q (%s): %s", w.InstanceID, w.Path, w.Message)
}

// ResolvedInstance is the side-effect-free preflight result for one instance.
// Config is the canonical league.Config; SourceJSON is the source copied into
// the generated ConfigMap.
type ResolvedInstance struct {
	Spec       Instance
	Path       string
	SourceJSON []byte
	Config     league.Config
	Warnings   []string
}

// Peer is one deterministic commissioner-HQ peer.
type Peer struct {
	ID            string
	ServiceOrigin string
	PublicOrigin  string
}

// DerivedInstance contains all values shared by generated resources and is
// exposed for tests and operators that want to inspect topology before YAML.
type DerivedInstance struct {
	Spec               Instance
	Image              string
	IngressClass       string
	CertificateIssuer  string
	PublicHost         string
	ServiceOrigin      string
	OAuthCallback      string
	Tank01BaseURL      string
	HQPeers            []Peer
	HQPeersValue       string
	SecurityHeaders    string
	RedirectMiddleware string
	TLSSecret          string
	Deployment         string
	Service            string
	PVC                string
	LeagueConfigMap    string
	Secret             string
	HTTPSIngress       string
	HTTPIngress        string
}

// File is one generated bundle member. Paths are always relative and sorted
// lexicographically by Compile.
type File struct {
	Path string
	Data []byte
}

// BundleFile is a descriptive alias for File.
type BundleFile = File

// Bundle is a complete deterministic in-memory publication. No method writes
// these files to disk or applies them to a cluster.
type Bundle struct {
	Fleet     Fleet
	Instances []DerivedInstance
	Warnings  []Warning
	Files     []File
}

var (
	k8sNamePattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	imagePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[0-9a-f]{64}$`)
	quantityPattern = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+|[numkKMGTPiE]*)$`)
)

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
// Version 2 replaces the legacy peer mesh with an explicit commissioner_hq
// topology object on each participant.
const SchemaVersion = 2

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

// Instance is one ordered fleet member. commissioner_hq is required in the
// wire document and may be null for a nonparticipant.
type Instance struct {
	ID               string          `json:"id"`
	Namespace        string          `json:"namespace"`
	ResourcePrefix   string          `json:"resource_prefix"`
	PublicOrigin     string          `json:"public_origin"`
	LeagueConfigPath string          `json:"league_config_path"`
	PVCStorage       string          `json:"pvc_storage"`
	CommissionerHQ   *CommissionerHQ `json:"commissioner_hq"`
}

// CommissionerHQ declares the private Commissioner HQ v1 topology for one
// participant. A nil value on Instance means that instance is not in the
// registry. All fields are explicit so operator-authored order, credential
// identity, and browser host selection cannot be inferred from input order.
type CommissionerHQ struct {
	LeagueID string `json:"league_id"`
	Order    int    `json:"order"`
	Accent   string `json:"accent"`
	KeyID    string `json:"key_id"`
	Host     bool   `json:"host"`
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

// DerivedInstance contains all values shared by generated resources and is
// exposed for tests and operators that want to inspect topology before YAML.
type DerivedInstance struct {
	Spec               Instance
	Image              string
	ImageDigest        string
	IngressClass       string
	CertificateIssuer  string
	PublicHost         string
	ServiceOrigin      string
	OAuthCallback      string
	Tank01BaseURL      string
	HQProviderOrigin   string
	HQRegistryFile     string
	HQRegistryJSON     string
	SecurityHeaders    string
	RedirectMiddleware string
	TLSSecret          string
	Deployment         string
	Service            string
	PVC                string
	LeagueConfigMap    string
	Secret             string
	ProviderService    string
	RegistryConfigMap  string
	ClientSecret       string
	NetworkPolicy      string
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
	k8sNamePattern        = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	imageDigestPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	imageComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	quantityPattern       = regexp.MustCompile(`^[1-9][0-9]*(Mi|Gi)$`)
)

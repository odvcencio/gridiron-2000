package fleetconfig

// The adoption contract is deliberately separate from Fleet. Fleet is the
// desired, generated v2 topology; AdoptionInventory is a small, operator-
// supplied description of the existing hand-authored resources. Keeping the
// two documents separate prevents an adoption check from treating a live
// Secret, ConfigMap, PVC, or member identity as generated source.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// AdoptionSchemaVersion is the version of the PII-free inventory contract.
	AdoptionSchemaVersion = 1
	maxAdoptionBytes      = 1 << 20
)

var adoptionEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// AdoptionInventory is a read-only inventory of an existing fleet. It must
// contain resource names and non-secret identity facts only. It intentionally
// has no field for Secret data, member identities, league JSON, or OAuth data.
type AdoptionInventory struct {
	Version   int                `json:"version"`
	Mode      string             `json:"mode"`
	Instances []AdoptionInstance `json:"instances"`
}

// AdoptionInstance describes the existing resource boundary for one isolated
// league. The booleans are pointers so omission cannot be confused with a
// deliberate false value during strict decoding.
type AdoptionInstance struct {
	ID             string                    `json:"id"`
	Namespace      string                    `json:"namespace"`
	ResourcePrefix string                    `json:"resource_prefix"`
	PublicOrigin   string                    `json:"public_origin"`
	Image          string                    `json:"image"`
	Resources      AdoptionResources         `json:"resources"`
	Legacy         AdoptionLegacyState       `json:"legacy"`
	Preserve       AdoptionPreservationState `json:"preserve"`
}

// AdoptionResources names the existing resources that must not be replaced
// implicitly by fleetgen. Names are metadata, not handles to live objects.
type AdoptionResources struct {
	Deployment   string `json:"deployment"`
	Service      string `json:"service"`
	PVC          string `json:"pvc"`
	LeagueConfig string `json:"league_config"`
	Secret       string `json:"secret"`
}

// AdoptionLegacyState records only whether old wiring was present. It never
// carries a legacy token or peer value.
type AdoptionLegacyState struct {
	PeerMeshConfigured *bool `json:"peer_mesh_configured"`
	HQV1Configured     *bool `json:"hq_v1_configured"`
}

// AdoptionPreservationState is an explicit acknowledgement that adoption is
// state-preserving. A false or missing acknowledgement fails closed.
type AdoptionPreservationState struct {
	PVC          *bool `json:"pvc"`
	LeagueConfig *bool `json:"league_config"`
	Secret       *bool `json:"secret"`
}

// AdoptionAction is one deterministic operator step. Resource values are
// names and kinds only; no Secret values, member identities, or league data
// are ever placed in a plan.
type AdoptionAction struct {
	InstanceID string `json:"instance_id"`
	Phase      string `json:"phase"`
	Resource   string `json:"resource"`
	Reason     string `json:"reason"`
}

// AdoptionSecretPlaceholder names a Secret key that an operator must
// provision through the external Secret workflow. Placeholder is deliberately
// not a credential; it is the exact marker emitted by the generated example
// Secret so a receipt can be compared without handling Secret data.
type AdoptionSecretPlaceholder struct {
	InstanceID  string `json:"instance_id"`
	Resource    string `json:"resource"`
	Environment string `json:"environment"`
	Placeholder string `json:"placeholder"`
	Role        string `json:"role"`
}

// AdoptionInstancePlan contains comparisons and steps for one instance.
type AdoptionInstancePlan struct {
	ID         string           `json:"id"`
	Ready      bool             `json:"ready"`
	Mismatches []string         `json:"mismatches"`
	Actions    []AdoptionAction `json:"actions"`
}

// AdoptionHQChecklist is the private, value-free operator contract emitted
// alongside an adoption plan. It names the resources and environment keys
// that must be reviewed, but never contains a credential or a Secret value.
type AdoptionHQChecklist struct {
	ProviderService           string `json:"provider_service"`
	NetworkPolicy             string `json:"network_policy"`
	ProviderSecretEnvironment string `json:"provider_secret_environment"`
	RegistryConfigMap         string `json:"registry_config_map,omitempty"`
	RegistryFile              string `json:"registry_file,omitempty"`
	ClientSecret              string `json:"client_secret,omitempty"`
	ClientSecretEnvironment   string `json:"client_secret_environment,omitempty"`
}

// AdoptionChecklist is the per-instance OAuth, storage, and HQ checklist.
// Storage consequences are intentionally explicit because a generated
// ReadWriteOnce local-path PVC is not a portable backup or HA boundary.
type AdoptionChecklist struct {
	InstanceID    string               `json:"instance_id"`
	OAuthCallback string               `json:"oauth_callback"`
	Storage       string               `json:"storage"`
	HQ            *AdoptionHQChecklist `json:"hq,omitempty"`
}

// AdoptionPlan is the stable, read-only result of comparing a generated v2
// bundle to an existing-resource inventory. Ready means the inventory is
// internally consistent and the operator may review the listed steps; it does
// not mean that Kubernetes resources were applied.
type AdoptionPlan struct {
	Version            int                         `json:"version"`
	Mode               string                      `json:"mode"`
	Ready              bool                        `json:"ready"`
	SecretValuesRead   bool                        `json:"secret_values_read"`
	PIIRead            bool                        `json:"pii_read"`
	SecretPlaceholders []AdoptionSecretPlaceholder `json:"secret_placeholders"`
	Instances          []AdoptionInstancePlan      `json:"instances"`
	Actions            []AdoptionAction            `json:"actions"`
	Checklist          []AdoptionChecklist         `json:"checklist"`
}

// LoadAdoptionInventory reads and strictly validates an adoption inventory.
// Errors intentionally omit the source path and raw decoder text so an
// operator cannot accidentally echo a private path or input value.
func LoadAdoptionInventory(path string) (AdoptionInventory, error) {
	if strings.TrimSpace(path) == "" {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory is unavailable")
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory is unavailable")
	}
	file, err := os.Open(abs)
	if err != nil {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory is unavailable")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxAdoptionBytes+1))
	if err != nil || len(payload) > maxAdoptionBytes {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory is unreadable")
	}
	if adoptionEmailPattern.Match(payload) {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory must not contain member identities")
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory JSON is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var inventory AdoptionInventory
	if err := decoder.Decode(&inventory); err != nil {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory JSON is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return AdoptionInventory{}, errors.New("fleetconfig: adoption inventory must contain one JSON value")
	}
	if err := validateAdoptionInventory(inventory, payload); err != nil {
		return AdoptionInventory{}, err
	}
	return inventory, nil
}

func validateAdoptionInventory(inventory AdoptionInventory, raw []byte) error {
	if inventory.Version != AdoptionSchemaVersion || inventory.Mode != "existing" || len(inventory.Instances) == 0 || len(inventory.Instances) > 64 {
		return errors.New("fleetconfig: adoption inventory shape is invalid")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return errors.New("fleetconfig: adoption inventory JSON is invalid")
	}
	for _, key := range []string{"version", "mode", "instances"} {
		value, ok := object[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("fleetconfig: adoption inventory %s is required", key)
		}
	}
	seen := make(map[string]struct{}, len(inventory.Instances))
	for index, instance := range inventory.Instances {
		if err := validateK8sName(fmt.Sprintf("adoption instances[%d].id", index), instance.ID); err != nil {
			return err
		}
		if _, exists := seen[instance.ID]; exists {
			return errors.New("fleetconfig: adoption inventory has duplicate instance identity")
		}
		seen[instance.ID] = struct{}{}
		if err := validateK8sName("adoption namespace", instance.Namespace); err != nil {
			return err
		}
		if err := validateK8sName("adoption resource prefix", instance.ResourcePrefix); err != nil {
			return err
		}
		if _, _, err := validateOrigin(instance.PublicOrigin, true); err != nil {
			return errors.New("fleetconfig: adoption public origin is invalid")
		}
		if err := validateImage(instance.Image); err != nil {
			return errors.New("fleetconfig: adoption image is invalid")
		}
		for _, resource := range []struct {
			name  string
			value string
		}{
			{"deployment", instance.Resources.Deployment},
			{"service", instance.Resources.Service},
			{"pvc", instance.Resources.PVC},
			{"league config", instance.Resources.LeagueConfig},
			{"secret", instance.Resources.Secret},
		} {
			if err := validateK8sName("adoption "+resource.name, resource.value); err != nil {
				return err
			}
		}
		if instance.Legacy.PeerMeshConfigured == nil || instance.Legacy.HQV1Configured == nil ||
			instance.Preserve.PVC == nil || instance.Preserve.LeagueConfig == nil || instance.Preserve.Secret == nil ||
			!*instance.Preserve.PVC || !*instance.Preserve.LeagueConfig || !*instance.Preserve.Secret {
			return errors.New("fleetconfig: adoption inventory must acknowledge legacy state and preserve existing state")
		}
	}
	return nil
}

// PlanExistingAdoption compares an already-loaded generated bundle to a
// secret/PII-free existing-resource inventory. It returns a plan even when
// mismatches exist so CI and operators can inspect every deterministic
// difference; callers should not proceed unless plan.Ready is true.
func PlanExistingAdoption(bundle Bundle, inventory AdoptionInventory) (AdoptionPlan, error) {
	if err := validateAdoptionInventoryValue(inventory); err != nil {
		return AdoptionPlan{}, err
	}
	if err := validateHQV1Bundle(bundle); err != nil {
		return AdoptionPlan{}, err
	}
	if len(bundle.Instances) == 0 || len(bundle.Instances) != len(inventory.Instances) {
		return AdoptionPlan{}, errors.New("fleetconfig: adoption inventory does not describe the generated fleet")
	}
	byID := make(map[string]AdoptionInstance, len(inventory.Instances))
	for _, instance := range inventory.Instances {
		byID[instance.ID] = instance
	}
	ordered := append([]DerivedInstance(nil), bundle.Instances...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Spec.ID < ordered[j].Spec.ID })
	plan := AdoptionPlan{
		Version: AdoptionSchemaVersion, Mode: "existing", Ready: true,
		SecretValuesRead: false, PIIRead: false,
		Instances: make([]AdoptionInstancePlan, 0, len(bundle.Instances)),
		Checklist: adoptionChecklist(ordered),
	}
	for _, generated := range ordered {
		current, exists := byID[generated.Spec.ID]
		instancePlan := AdoptionInstancePlan{ID: generated.Spec.ID, Ready: exists}
		if !exists {
			instancePlan.Mismatches = append(instancePlan.Mismatches, "existing instance is missing")
			instancePlan.Ready = false
			plan.Ready = false
			plan.Instances = append(plan.Instances, instancePlan)
			continue
		}
		compareAdoptionIdentity(&instancePlan, generated, current)
		if !instancePlan.Ready {
			plan.Ready = false
			plan.Instances = append(plan.Instances, instancePlan)
			continue
		}
		instancePlan.Actions = adoptionActions(generated, current)
		plan.SecretPlaceholders = append(plan.SecretPlaceholders, adoptionSecretPlaceholders(generated, ordered)...)
		plan.Actions = append(plan.Actions, instancePlan.Actions...)
		plan.Instances = append(plan.Instances, instancePlan)
	}
	for _, current := range inventory.Instances {
		found := false
		for _, generated := range ordered {
			if generated.Spec.ID == current.ID {
				found = true
				break
			}
		}
		if !found {
			plan.Ready = false
			plan.Instances = append(plan.Instances, AdoptionInstancePlan{
				ID: current.ID, Ready: false,
				Mismatches: []string{"inventory instance is not present in generated fleet"},
			})
		}
	}
	sort.Slice(plan.Instances, func(i, j int) bool { return plan.Instances[i].ID < plan.Instances[j].ID })
	sort.Slice(plan.Actions, func(i, j int) bool {
		left, right := plan.Actions[i], plan.Actions[j]
		if left.InstanceID != right.InstanceID {
			return left.InstanceID < right.InstanceID
		}
		if adoptionPhaseOrder(left.Phase) != adoptionPhaseOrder(right.Phase) {
			return adoptionPhaseOrder(left.Phase) < adoptionPhaseOrder(right.Phase)
		}
		return left.Resource < right.Resource
	})
	sort.Slice(plan.SecretPlaceholders, func(i, j int) bool {
		left, right := plan.SecretPlaceholders[i], plan.SecretPlaceholders[j]
		if left.InstanceID != right.InstanceID {
			return left.InstanceID < right.InstanceID
		}
		if left.Environment != right.Environment {
			return left.Environment < right.Environment
		}
		return left.Role < right.Role
	})
	return plan, nil
}

func adoptionSecretPlaceholders(instance DerivedInstance, all []DerivedInstance) []AdoptionSecretPlaceholder {
	if instance.Spec.CommissionerHQ == nil {
		return nil
	}
	placeholders := []AdoptionSecretPlaceholder{{
		InstanceID:  instance.Spec.ID,
		Resource:    "Secret/" + instance.Secret,
		Environment: "COMMISSIONER_HQ_PROVIDER_SECRET",
		Placeholder: "REPLACE_ME",
		Role:        "provider credential",
	}}
	host := hostInstance(all)
	if host.Spec.ID == instance.Spec.ID {
		placeholders = append(placeholders, AdoptionSecretPlaceholder{
			InstanceID:  instance.Spec.ID,
			Resource:    "Secret/" + host.ClientSecret,
			Environment: clientSecretEnv(instance.Spec.ID),
			Placeholder: "REPLACE_ME",
			Role:        "host client credential",
		})
	}
	return placeholders
}

const adoptionStorageConsequence = "retain existing node-local ReadWriteOnce PVC; deleting/recreating it may retain or delete the local volume according to the StorageClass reclaim policy and can make state unavailable; back up before maintenance"

func adoptionChecklist(instances []DerivedInstance) []AdoptionChecklist {
	checklist := make([]AdoptionChecklist, 0, len(instances))
	for _, instance := range instances {
		item := AdoptionChecklist{
			InstanceID:    instance.Spec.ID,
			OAuthCallback: instance.OAuthCallback,
			Storage:       adoptionStorageConsequence,
		}
		if instance.Spec.CommissionerHQ != nil {
			item.HQ = &AdoptionHQChecklist{
				ProviderService:           "Service/" + instance.ProviderService,
				NetworkPolicy:             "NetworkPolicy/" + instance.NetworkPolicy,
				ProviderSecretEnvironment: "COMMISSIONER_HQ_PROVIDER_SECRET",
			}
			if instance.Spec.CommissionerHQ.Host {
				item.HQ.RegistryConfigMap = "ConfigMap/" + instance.RegistryConfigMap
				item.HQ.RegistryFile = instance.HQRegistryFile
				item.HQ.ClientSecret = "Secret/" + instance.ClientSecret
				item.HQ.ClientSecretEnvironment = clientSecretEnv(instance.Spec.ID)
			}
		}
		checklist = append(checklist, item)
	}
	return checklist
}

// validateHQV1Bundle proves the generated side of the adoption boundary
// before a plan can be marked ready. It validates only topology, names,
// origins, and secret references; it never resolves or reads a credential.
func validateHQV1Bundle(bundle Bundle) error {
	participants := make(map[string]DerivedInstance)
	var host *DerivedInstance
	for _, instance := range bundle.Instances {
		if instance.Spec.CommissionerHQ == nil {
			continue
		}
		participants[instance.Spec.ID] = instance
		if instance.Spec.CommissionerHQ.Host {
			copy := instance
			host = &copy
		}
		deployment := adoptionBundleFile(bundle, "instances/"+instance.Spec.ID+"/deployment.yaml")
		secret := adoptionBundleFile(bundle, "instances/"+instance.Spec.ID+"/secret.example.yaml")
		for _, marker := range []string{
			"name: hq-v1",
			"containerPort: 8091",
			"COMMISSIONER_HQ_LEAGUE_ID",
			"COMMISSIONER_HQ_PROVIDER_KEY_ID",
			"COMMISSIONER_HQ_PROVIDER_ADDR",
			"COMMISSIONER_HQ_PROVIDER_SECRET",
		} {
			if !strings.Contains(deployment+secret, marker) {
				return errors.New("fleetconfig: generated HQ v1 listener configuration is incomplete")
			}
		}
	}
	if len(participants) == 0 {
		return nil
	}
	if host == nil || host.HQRegistryJSON == "" {
		return errors.New("fleetconfig: generated HQ v1 host registry is missing")
	}
	registry := registryDocument{}
	if err := rejectDuplicateJSONKeys([]byte(host.HQRegistryJSON)); err != nil {
		return errors.New("fleetconfig: generated HQ v1 registry is invalid")
	}
	if err := json.Unmarshal([]byte(host.HQRegistryJSON), &registry); err != nil || registry.Version != 1 || !registry.Enabled || len(registry.Connections) != len(participants) {
		return errors.New("fleetconfig: generated HQ v1 registry is invalid")
	}
	seen := make(map[string]struct{}, len(registry.Connections))
	for _, connection := range registry.Connections {
		instance, ok := participants[connection.Key]
		if !ok || !connection.Enabled || connection.LeagueID != instance.Spec.CommissionerHQ.LeagueID || connection.ProviderOrigin != instance.HQProviderOrigin || connection.PublicOrigin != instance.Spec.PublicOrigin || connection.Credential.KeyID != instance.Spec.CommissionerHQ.KeyID || connection.Credential.SecretEnv != clientSecretEnv(connection.Key) {
			return errors.New("fleetconfig: generated HQ v1 registry identity is inconsistent")
		}
		if _, duplicate := seen[connection.Key]; duplicate {
			return errors.New("fleetconfig: generated HQ v1 registry has duplicate identity")
		}
		seen[connection.Key] = struct{}{}
	}
	if len(seen) != len(participants) {
		return errors.New("fleetconfig: generated HQ v1 registry is incomplete")
	}
	registryFile := adoptionBundleFile(bundle, "instances/"+host.Spec.ID+"/hq-registry.yaml")
	clientSecret := adoptionBundleFile(bundle, "instances/"+host.Spec.ID+"/hq-client-secret.example.yaml")
	if !strings.Contains(registryFile, "registry.json: |-") || !strings.Contains(registryFile, `"version":1`) || !strings.Contains(clientSecret, "REPLACE_ME") {
		return errors.New("fleetconfig: generated HQ v1 host material is incomplete")
	}
	for key := range participants {
		if !strings.Contains(clientSecret, clientSecretEnv(key)) {
			return errors.New("fleetconfig: generated HQ v1 host client material is incomplete")
		}
	}
	return nil
}

func adoptionBundleFile(bundle Bundle, path string) string {
	for _, file := range bundle.Files {
		if file.Path == path {
			return string(file.Data)
		}
	}
	return ""
}

func validateAdoptionInventoryValue(inventory AdoptionInventory) error {
	if inventory.Version != AdoptionSchemaVersion || inventory.Mode != "existing" || len(inventory.Instances) == 0 || len(inventory.Instances) > 64 {
		return errors.New("fleetconfig: adoption inventory shape is invalid")
	}
	seen := map[string]struct{}{}
	for index, instance := range inventory.Instances {
		if instance.ID == "" || instance.Namespace == "" || instance.ResourcePrefix == "" || instance.PublicOrigin == "" || instance.Image == "" ||
			instance.Resources.Deployment == "" || instance.Resources.Service == "" || instance.Resources.PVC == "" || instance.Resources.LeagueConfig == "" || instance.Resources.Secret == "" ||
			instance.Legacy.PeerMeshConfigured == nil || instance.Legacy.HQV1Configured == nil || instance.Preserve.PVC == nil || instance.Preserve.LeagueConfig == nil || instance.Preserve.Secret == nil {
			return fmt.Errorf("fleetconfig: adoption inventory instance %d is incomplete", index)
		}
		if _, exists := seen[instance.ID]; exists {
			return errors.New("fleetconfig: adoption inventory has duplicate instance identity")
		}
		seen[instance.ID] = struct{}{}
		if adoptionEmailPattern.MatchString(instance.ID + instance.Namespace + instance.ResourcePrefix + instance.PublicOrigin + instance.Image) {
			return errors.New("fleetconfig: adoption inventory must not contain member identities")
		}
		if err := validateK8sName("adoption instance id", instance.ID); err != nil {
			return err
		}
		if err := validateK8sName("adoption namespace", instance.Namespace); err != nil {
			return err
		}
		if err := validateK8sName("adoption resource prefix", instance.ResourcePrefix); err != nil {
			return err
		}
		if _, _, err := validateOrigin(instance.PublicOrigin, true); err != nil {
			return errors.New("fleetconfig: adoption public origin is invalid")
		}
		if err := validateImage(instance.Image); err != nil {
			return errors.New("fleetconfig: adoption image is invalid")
		}
		for _, resource := range []string{
			instance.Resources.Deployment, instance.Resources.Service, instance.Resources.PVC,
			instance.Resources.LeagueConfig, instance.Resources.Secret,
		} {
			if err := validateK8sName("adoption resource", resource); err != nil {
				return err
			}
		}
		if !*instance.Preserve.PVC || !*instance.Preserve.LeagueConfig || !*instance.Preserve.Secret {
			return errors.New("fleetconfig: adoption inventory must preserve existing state")
		}
	}
	return nil
}

func compareAdoptionIdentity(plan *AdoptionInstancePlan, generated DerivedInstance, current AdoptionInstance) {
	checks := []struct {
		label, expected, actual string
	}{
		{"namespace", generated.Spec.Namespace, current.Namespace},
		{"resource prefix", generated.Spec.ResourcePrefix, current.ResourcePrefix},
		{"public origin", generated.Spec.PublicOrigin, current.PublicOrigin},
		{"image", generated.Image, current.Image},
		{"deployment", generated.Deployment, current.Resources.Deployment},
		{"service", generated.Service, current.Resources.Service},
		{"pvc", generated.PVC, current.Resources.PVC},
		{"league config", generated.LeagueConfigMap, current.Resources.LeagueConfig},
		{"secret", generated.Secret, current.Resources.Secret},
	}
	for _, check := range checks {
		if check.expected != check.actual {
			plan.Mismatches = append(plan.Mismatches, check.label+" differs")
		}
	}
	if generated.Spec.CommissionerHQ == nil && current.Legacy.HQV1Configured != nil && *current.Legacy.HQV1Configured {
		plan.Mismatches = append(plan.Mismatches, "nonparticipant cannot report HQ v1 configured")
	}
	plan.Ready = len(plan.Mismatches) == 0
}

func adoptionActions(generated DerivedInstance, current AdoptionInstance) []AdoptionAction {
	instanceID := generated.Spec.ID
	actions := []AdoptionAction{
		{InstanceID: instanceID, Phase: "verify", Resource: "Deployment/" + generated.Deployment, Reason: "retain the exact immutable image digest before the canary"},
		{InstanceID: instanceID, Phase: "preserve", Resource: "PersistentVolumeClaim/" + generated.PVC, Reason: "retain the existing node-local state claim; never recreate it during adoption"},
		{InstanceID: instanceID, Phase: "preserve", Resource: "ConfigMap/" + generated.LeagueConfigMap, Reason: "retain the live league identity and rules ConfigMap; do not apply generated neutral/example league data"},
		{InstanceID: instanceID, Phase: "preserve", Resource: "Secret/" + generated.Secret, Reason: "retain existing credentials and patch only reviewed HQ v1 fields; fleetgen never reads their values"},
	}
	if generated.Spec.CommissionerHQ != nil {
		actions = append(actions,
			AdoptionAction{InstanceID: instanceID, Phase: "create", Resource: "Service/" + generated.ProviderService, Reason: "add the private signed HQ v1 provider listener on port 8091"},
			AdoptionAction{InstanceID: instanceID, Phase: "create", Resource: "NetworkPolicy/" + generated.NetworkPolicy, Reason: "allow provider traffic only from the declared HQ host"},
			AdoptionAction{InstanceID: instanceID, Phase: "patch", Resource: "Secret/" + generated.Secret, Reason: "add the provider secret through the external secret workflow; never print or commit its value"},
		)
		if generated.Spec.CommissionerHQ.Host {
			actions = append(actions,
				AdoptionAction{InstanceID: instanceID, Phase: "create", Resource: "ConfigMap/" + generated.RegistryConfigMap, Reason: "install the signed HQ v1 registry with origins, links, key IDs, and secret references only"},
				AdoptionAction{InstanceID: instanceID, Phase: "create", Resource: "Secret/" + generated.ClientSecret, Reason: "install the host client Secret with one independently provisioned value per participant"},
			)
		}
	}
	if current.Legacy.PeerMeshConfigured != nil && *current.Legacy.PeerMeshConfigured {
		actions = append(actions, AdoptionAction{
			InstanceID: instanceID, Phase: "defer", Resource: "legacy peer/token wiring", Reason: "remove only after both sides of the v1 provider/client pair pass the SK-first canary; do not mix legacy and generated values in one patch",
		})
	}
	return actions
}

func adoptionPhaseOrder(phase string) int {
	switch phase {
	case "verify":
		return 0
	case "preserve":
		return 1
	case "patch":
		return 2
	case "create":
		return 3
	case "defer":
		return 4
	default:
		return 5
	}
}

// JSON returns a stable, indented representation suitable for a receipt or
// CI artifact. It contains no source payload, Secret value, or PII.
func (plan AdoptionPlan) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, errors.New("fleetconfig: adoption plan cannot be encoded")
	}
	return append(data, '\n'), nil
}

// Text returns deterministic human-readable output for an operator terminal.
func (plan AdoptionPlan) Text() string {
	var b strings.Builder
	if plan.Ready {
		b.WriteString("fleetgen adopt: ready (read-only; no Kubernetes or Secret values were read)\n")
	} else {
		b.WriteString("fleetgen adopt: blocked (read-only preflight; no Kubernetes or Secret values were read)\n")
	}
	for _, placeholder := range plan.SecretPlaceholders {
		b.WriteString("credential ")
		b.WriteString(placeholder.Role)
		b.WriteString(" ")
		b.WriteString(placeholder.InstanceID)
		b.WriteString(": ")
		b.WriteString(placeholder.Resource)
		b.WriteString(" ")
		b.WriteString(placeholder.Environment)
		b.WriteString("=")
		b.WriteString(placeholder.Placeholder)
		b.WriteString(" (provision without display)\n")
	}
	if len(plan.Checklist) > 0 {
		b.WriteString("operator checklist (OAuth/HQ/storage; review before any apply):\n")
		for _, item := range plan.Checklist {
			b.WriteString("  instance ")
			b.WriteString(item.InstanceID)
			b.WriteString(": register OAuth callback ")
			b.WriteString(item.OAuthCallback)
			b.WriteString("; ")
			b.WriteString(item.Storage)
			b.WriteString("\n")
			if item.HQ == nil {
				continue
			}
			b.WriteString("  HQ ")
			b.WriteString(item.InstanceID)
			b.WriteString(": provision ")
			b.WriteString(item.HQ.ProviderSecretEnvironment)
			b.WriteString(" for ")
			b.WriteString(item.HQ.ProviderService)
			b.WriteString(" on private port 8091; apply ")
			b.WriteString(item.HQ.NetworkPolicy)
			if item.HQ.RegistryConfigMap != "" {
				b.WriteString("; review ")
				b.WriteString(item.HQ.RegistryConfigMap)
				b.WriteString(" mounted at ")
				b.WriteString(item.HQ.RegistryFile)
				b.WriteString("; provision ")
				b.WriteString(item.HQ.ClientSecret)
				b.WriteString(" key ")
				b.WriteString(item.HQ.ClientSecretEnvironment)
			}
			b.WriteString("\n")
		}
	}
	for _, instance := range plan.Instances {
		b.WriteString("instance ")
		b.WriteString(instance.ID)
		if instance.Ready {
			b.WriteString(": ready\n")
		} else {
			b.WriteString(": blocked\n")
		}
		for _, mismatch := range instance.Mismatches {
			b.WriteString("  mismatch: ")
			b.WriteString(mismatch)
			b.WriteByte('\n')
		}
		for _, action := range instance.Actions {
			b.WriteString("  ")
			b.WriteString(action.Phase)
			b.WriteString(" ")
			b.WriteString(action.Resource)
			b.WriteString(" — ")
			b.WriteString(action.Reason)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

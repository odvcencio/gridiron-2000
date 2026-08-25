package fleetconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boolPointer(value bool) *bool { return &value }

func adoptionInventoryFor(bundle Bundle) AdoptionInventory {
	instances := make([]AdoptionInstance, 0, len(bundle.Instances))
	for _, generated := range bundle.Instances {
		instances = append(instances, AdoptionInstance{
			ID: generated.Spec.ID, Namespace: generated.Spec.Namespace,
			ResourcePrefix: generated.Spec.ResourcePrefix, PublicOrigin: generated.Spec.PublicOrigin,
			Image: generated.Image,
			Resources: AdoptionResources{
				Deployment: generated.Deployment, Service: generated.Service, PVC: generated.PVC,
				LeagueConfig: generated.LeagueConfigMap, Secret: generated.Secret,
			},
			Legacy:   AdoptionLegacyState{PeerMeshConfigured: boolPointer(true), HQV1Configured: boolPointer(false)},
			Preserve: AdoptionPreservationState{PVC: boolPointer(true), LeagueConfig: boolPointer(true), Secret: boolPointer(true)},
		})
	}
	return AdoptionInventory{Version: AdoptionSchemaVersion, Mode: "existing", Instances: instances}
}

func adoptionBundle(t *testing.T) Bundle {
	t.Helper()
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	fleet := testFleet()
	fleet.FleetPath = ""
	fleet.Instances[0].LeagueConfigPath = "league.json"
	fleetPath := writeFleet(t, dir, fleet)
	loaded, _, err := Load(fleetPath)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := CompileFleet(loaded)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func writeAdoptionInventory(t *testing.T, dir string, inventory AdoptionInventory) string {
	t.Helper()
	raw, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdoptionInventoryPlansExistingFleetWithoutSecretValues(t *testing.T) {
	bundle := adoptionBundle(t)
	inventory := adoptionInventoryFor(bundle)
	path := writeAdoptionInventory(t, t.TempDir(), inventory)
	loaded, err := LoadAdoptionInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanExistingAdoption(bundle, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready || plan.SecretValuesRead || plan.PIIRead {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Instances) != 1 || len(plan.Actions) == 0 {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.SecretPlaceholders) != 2 || plan.SecretPlaceholders[0].Environment != "COMMISSIONER_HQ_PROVIDER_SECRET" || plan.SecretPlaceholders[1].Environment != "COMMISSIONER_HQ_V1_SECRET_ALPHA" {
		t.Fatalf("secret placeholders = %#v", plan.SecretPlaceholders)
	}
	text := plan.Text()
	for _, forbidden := range []string{"member@example", "SESSION_SECRET"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("plan text contains %q: %s", forbidden, text)
		}
	}
	for _, expected := range []string{"COMMISSIONER_HQ_PROVIDER_SECRET=REPLACE_ME", "COMMISSIONER_HQ_V1_SECRET_ALPHA=REPLACE_ME", "provision without display"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("plan text does not contain %q: %s", expected, text)
		}
	}
	data, err := plan.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("COMMISSIONER_HQ_PROVIDER_SECRET")) || !bytes.Contains(data, []byte("COMMISSIONER_HQ_V1_SECRET_ALPHA")) || bytes.Contains(data, []byte("@example")) {
		t.Fatalf("plan JSON lacks exact placeholders or contains identity: %s", data)
	}
}

func TestAdoptionInventoryRejectsPIIAndMissingPreservation(t *testing.T) {
	bundle := adoptionBundle(t)
	valid := adoptionInventoryFor(bundle)
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	withPII := strings.Replace(string(raw), `"id":"alpha"`, `"id":"member@example.com"`, 1)
	if _, err := LoadAdoptionInventory(writeRawAdoptionInventory(t, t.TempDir(), withPII)); err == nil || !strings.Contains(err.Error(), "member identities") {
		t.Fatalf("PII load error = %v", err)
	}
	valid.Instances[0].Preserve.Secret = boolPointer(false)
	if _, err := PlanExistingAdoption(bundle, valid); err == nil || !strings.Contains(err.Error(), "preserve existing state") {
		t.Fatalf("preservation error = %v", err)
	}
}

func writeRawAdoptionInventory(t *testing.T, dir, raw string) string {
	t.Helper()
	path := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdoptionPlanFailsClosedOnIdentityDrift(t *testing.T) {
	bundle := adoptionBundle(t)
	inventory := adoptionInventoryFor(bundle)
	inventory.Instances[0].Image = strings.Replace(inventory.Instances[0].Image, "012345", "fedcba", 1)
	plan, err := PlanExistingAdoption(bundle, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ready || len(plan.Instances) != 1 || len(plan.Instances[0].Mismatches) != 1 || plan.Instances[0].Mismatches[0] != "image differs" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestAdoptionPlanJSONIsDeterministic(t *testing.T) {
	bundle := adoptionBundle(t)
	first, err := PlanExistingAdoption(bundle, adoptionInventoryFor(bundle))
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanExistingAdoption(bundle, adoptionInventoryFor(bundle))
	if err != nil {
		t.Fatal(err)
	}
	left, err := first.JSON()
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("plans differ:\n%s\n%s", left, right)
	}
}

func TestAdoptionChecklistExercisesTwoThreeAndThirtyThreeInstances(t *testing.T) {
	for _, count := range []int{2, 3, 33} {
		t.Run(fmt.Sprintf("%d instances", count), func(t *testing.T) {
			dir := t.TempDir()
			writeLeague(t, dir, "league.json")
			fleet := testFleet()
			fleet.Instances = make([]Instance, count)
			for index := range fleet.Instances {
				id := fmt.Sprintf("league-%02d", index)
				fleet.Instances[index] = instance(id, hq("league-id-"+id, index, "cyan", "key-"+id, index == 0))
			}
			path := writeFleet(t, dir, fleet)
			bundle, err := Compile(path)
			if err != nil {
				t.Fatal(err)
			}
			inventory := adoptionInventoryFor(bundle)
			plan, err := PlanExistingAdoption(bundle, inventory)
			if err != nil {
				t.Fatal(err)
			}
			if !plan.Ready || len(plan.Checklist) != count || len(plan.Instances) != count {
				t.Fatalf("plan = %#v", plan)
			}
			for index, item := range plan.Checklist {
				id := fmt.Sprintf("league-%02d", index)
				if item.InstanceID != id || item.OAuthCallback != "https://"+id+".example.test/auth/google/callback" {
					t.Fatalf("checklist[%d] = %#v", index, item)
				}
				for _, marker := range []string{"node-local", "ReadWriteOnce", "deleting/recreating", "reclaim policy"} {
					if !strings.Contains(item.Storage, marker) {
						t.Fatalf("checklist[%d] storage omits %q: %q", index, marker, item.Storage)
					}
				}
				if item.HQ == nil || item.HQ.ProviderService == "" || item.HQ.NetworkPolicy == "" || item.HQ.ProviderSecretEnvironment != "COMMISSIONER_HQ_PROVIDER_SECRET" {
					t.Fatalf("checklist[%d] HQ contract = %#v", index, item.HQ)
				}
				if index == 0 && (item.HQ.RegistryConfigMap == "" || item.HQ.RegistryFile != "/etc/gridiron-hq/registry.json" || item.HQ.ClientSecretEnvironment == "") {
					t.Fatalf("host checklist[%d] = %#v", index, item.HQ)
				}
				if index != 0 && item.HQ.RegistryConfigMap != "" {
					t.Fatalf("non-host checklist[%d] unexpectedly owns registry material: %#v", index, item.HQ)
				}
			}
			text := plan.Text()
			for _, marker := range []string{"OAuth/HQ/storage", "register OAuth callback", "private port 8091", "node-local", "reclaim policy"} {
				if !strings.Contains(text, marker) {
					t.Fatalf("adoption text omits %q: %s", marker, text)
				}
			}
			first, err := plan.JSON()
			if err != nil {
				t.Fatal(err)
			}
			secondPlan, err := PlanExistingAdoption(bundle, inventory)
			if err != nil {
				t.Fatal(err)
			}
			second, err := secondPlan.JSON()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Fatal("adoption checklist plan is not deterministic")
			}
			if bytes.Contains(first, []byte("@example.com")) || bytes.Contains(first, []byte("REPLACE_ME_WITH")) {
				t.Fatalf("adoption checklist leaked PII or credential placeholders: %s", first)
			}
		})
	}
}

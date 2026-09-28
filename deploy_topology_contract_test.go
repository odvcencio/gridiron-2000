// deploy/k8s contains portable examples, never the live operator configuration.
// These contracts validate topology and safety of the examples. Production
// manifests and release pins belong in a private directory outside this checkout.
package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCommissionerHQDeploymentsUseExplicitTrustedPeerOrigins(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		instanceID string
		peer       string
		secret     string
	}{
		{
			name:       "flagship",
			path:       "deploy/k8s/deployment.yaml",
			instanceID: "g2k",
			peer:       "lgb=http://gridiron-2000-b.league-b.svc.cluster.local|https://second.league.example.com",
			secret:     "gridiron-2000-secrets",
		},
		{
			name:       "league b",
			path:       "deploy/k8s/instance-b/deployment.yaml",
			instanceID: "lgb",
			peer:       "g2k=http://gridiron-2000.gridiron.svc.cluster.local|https://league.example.com",
			secret:     "gridiron-2000-b-secrets",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := string(raw)
			if !strings.Contains(manifest, "name: COMMISSIONER_INSTANCE_ID\n              value: \""+test.instanceID+"\"") {
				t.Fatalf("manifest does not set instance ID %q", test.instanceID)
			}
			if !strings.Contains(manifest, "name: COMMISSIONER_HQ_PEERS\n              value: \""+test.peer+"\"") {
				t.Fatalf("manifest does not set explicit service|public peer %q", test.peer)
			}
			if strings.Contains(manifest, "COMMISSIONER_HQ_PEERS\n              value: \"") && !strings.Contains(manifest, "|https://") {
				t.Fatal("peer wiring must include a trusted public origin")
			}
			if !strings.Contains(manifest, "envFrom:\n            - secretRef:\n                name: "+test.secret) {
				t.Fatalf("commissioner token must remain in the deployment Secret ref %q", test.secret)
			}
			if strings.Contains(manifest, "name: COMMISSIONER_HQ_TOKEN") || strings.Contains(manifest, "value: \"replace-with") {
				t.Fatal("commissioner token must not be embedded in a Deployment manifest")
			}
		})
	}
}

// TestLiveScoringDeploymentValuesArePinnedAndMirrored is rider item 12
// (review of ff2a9b3): the live-scoring cadence values must be present
// and identical on both the flagship and League B app manifests.
// The kill switch is pinned per manifest: the flagship flipped it to
// "true" on 2026-09-02 with release-2026.09.02-ee12ed7-wave6, ahead of
// the 2026-09-10 Thursday-night canary and kill-switch drill
// (docs/launch-checklist.md section 13); the League B league did
// not form for the 2026 season, so deploy/k8s/instance-b/deployment.yaml is not
// a live canary today and keeps the switch "false" as the template a
// future second live instance rolls from. Its cadence values still must
// not drift from the flagship's. Neither manifest
// may carry the deprecated LIVE_POLL_INTERVAL: a stale tracked 5s value
// would silently restore the pre-GC-2 blanket-polling cadence the moment
// someone re-added it, even though the code still accepts it as a
// self-hoster's alias. STATRELAY_DAILY_BUDGET must be pinned on the
// shared relay's own manifest.
func TestLiveScoringDeploymentValuesArePinnedAndMirrored(t *testing.T) {
	killSwitch := map[string]string{
		"deploy/k8s/deployment.yaml":            "name: LIVE_SCORING_ENABLED\n              value: \"true\"",
		"deploy/k8s/instance-b/deployment.yaml": "name: LIVE_SCORING_ENABLED\n              value: \"false\"",
	}
	liveScoringEnv := []string{
		"name: LIVE_SCOREBOARD_INTERVAL\n              value: \"10s\"",
		"name: LIVE_BOX_BASELINE\n              value: \"30s\"",
		"name: LIVE_BOX_FAST\n              value: \"20s\"",
		"name: LIVE_MAX_INFLIGHT\n              value: \"4\"",
		"name: LIVE_DAILY_BUDGET\n              value: \"9000\"",
	}
	for _, path := range []string{"deploy/k8s/deployment.yaml", "deploy/k8s/instance-b/deployment.yaml"} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := string(raw)
			if !strings.Contains(manifest, killSwitch[path]) {
				t.Errorf("%s omitted the pinned kill switch %q", path, killSwitch[path])
			}
			for _, want := range liveScoringEnv {
				if !strings.Contains(manifest, want) {
					t.Errorf("%s omitted %q", path, want)
				}
			}
			if strings.Contains(manifest, "name: LIVE_POLL_INTERVAL") {
				t.Errorf("%s still sets the deprecated LIVE_POLL_INTERVAL; use LIVE_SCOREBOARD_INTERVAL", path)
			}
		})
	}

	relay, err := os.ReadFile("deploy/k8s/statrelay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if want := "name: STATRELAY_DAILY_BUDGET\n              value: \"13000\""; !strings.Contains(string(relay), want) {
		t.Errorf("deploy/k8s/statrelay.yaml omitted %q", want)
	}
}

// manifestImageDigest matches a Deployment container's pinned `image:`
// field's own sha256 digest (the part after the @, never a tag).
var manifestImageDigest = regexp.MustCompile(`image: registry\.example\.com/gridiron/gridiron-2000@(sha256:[0-9a-f]{64})`)

// manifestAppImageDigestEnv matches the flagship's own inline
// APP_IMAGE_DIGEST env value.
var manifestAppImageDigestEnv = regexp.MustCompile(`name: APP_IMAGE_DIGEST\n\s+value: "(sha256:[0-9a-f]{64})"`)

// TestFlagshipManifestAppImageDigestMatchesPinnedImage is the regression
// test for the drift ops-drift hardening (2026-09-23) found and fixed:
// deploy/k8s/deployment.yaml's APP_IMAGE_DIGEST env value (self-reported
// to Commissioner HQ v1 peers by commissioner_hq_v1.go's
// commissionerHQV1ReleaseSnapshot) had fallen out of sync with the
// container's own pinned `image:` digest after an image-only roll was
// applied straight with a patch instead of through this manifest — a
// federated peer saw a stale, wrong release identity for this instance
// even though the right image was actually running. The two digests must
// always agree in the tracked manifest; keeping them equal here is what
// keeps `kubectl diff -f deploy/k8s/` clean after every real release roll.
func TestFlagshipManifestAppImageDigestMatchesPinnedImage(t *testing.T) {
	raw, err := os.ReadFile("deploy/k8s/deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(raw)
	imageMatch := manifestImageDigest.FindStringSubmatch(manifest)
	if imageMatch == nil {
		t.Fatal("deploy/k8s/deployment.yaml: no pinned registry.example.com/gridiron/gridiron-2000@sha256:... image digest found")
	}
	envMatch := manifestAppImageDigestEnv.FindStringSubmatch(manifest)
	if envMatch == nil {
		t.Fatal("deploy/k8s/deployment.yaml: no APP_IMAGE_DIGEST env value found")
	}
	if imageMatch[1] != envMatch[1] {
		t.Fatalf("deploy/k8s/deployment.yaml: image digest %q does not match APP_IMAGE_DIGEST %q; a federated peer would see the wrong release identity for this instance", imageMatch[1], envMatch[1])
	}
}

// TestFlagshipManifestCommissionerHQV1ProviderIsReconciled covers the
// live objects ops-drift hardening (2026-09-23) found running in the
// gridiron namespace with no matching tracked manifest: the Commissioner
// HQ v1 provider's env block, its private container port, and its
// registry ConfigMap mount on deploy/k8s/deployment.yaml, plus its
// Service (deploy/k8s/hq-provider-service.yaml), its restricting
// NetworkPolicy (deploy/k8s/network-policy.yaml), and its registry
// ConfigMap (deploy/k8s/hq-registry.yaml). Each assertion below was
// verified against the live cluster with `kubectl diff -f deploy/k8s/`
// at the time this test was written; a break here means either the
// manifest or the running cluster has drifted since.
func TestFlagshipManifestCommissionerHQV1ProviderIsReconciled(t *testing.T) {
	deployment := readManifestForTopology(t, "deploy/k8s/deployment.yaml")
	for _, want := range []string{
		"name: hq-v1\n              containerPort: 8091",
		"name: COMMISSIONER_HQ_LEAGUE_ID\n              value: \"g2k\"",
		"name: COMMISSIONER_HQ_PROVIDER_KEY_ID\n              value: \"g2k-hq-v1\"",
		"name: COMMISSIONER_HQ_PROVIDER_ADDR\n              value: \":8091\"",
		"name: COMMISSIONER_HQ_V1_REGISTRY_FILE\n              value: \"/etc/gridiron-hq/registry.json\"",
		"name: hq-registry\n              mountPath: /etc/gridiron-hq/registry.json",
		"name: hq-registry\n          configMap:\n            name: gridiron-2000-hq-v1-registry",
	} {
		if !strings.Contains(deployment, want) {
			t.Errorf("deploy/k8s/deployment.yaml missing %q", want)
		}
	}
	// The provider credential must never be an inline manifest value (it
	// arrives only through gridiron-2000-secrets' envFrom) — the same
	// invariant TestCommissionerHQDeploymentsUseExplicitTrustedPeerOrigins
	// already enforces for the legacy COMMISSIONER_HQ_TOKEN.
	if strings.Contains(deployment, "name: COMMISSIONER_HQ_PROVIDER_SECRET") {
		t.Error("deploy/k8s/deployment.yaml must not inline COMMISSIONER_HQ_PROVIDER_SECRET; it must arrive only through the Secret envFrom")
	}

	service := readManifestForTopology(t, "deploy/k8s/hq-provider-service.yaml")
	for _, want := range []string{"name: gridiron-2000-hq-v1", "port: 8091", "targetPort: hq-v1"} {
		if !strings.Contains(service, want) {
			t.Errorf("deploy/k8s/hq-provider-service.yaml missing %q", want)
		}
	}

	policy := readManifestForTopology(t, "deploy/k8s/network-policy.yaml")
	for _, want := range []string{"kind: NetworkPolicy", "port: 8091", "app: gridiron-2000"} {
		if !strings.Contains(policy, want) {
			t.Errorf("deploy/k8s/network-policy.yaml missing %q", want)
		}
	}

	registry := readManifestForTopology(t, "deploy/k8s/hq-registry.yaml")
	if !strings.Contains(registry, "name: gridiron-2000-hq-v1-registry") {
		t.Error("deploy/k8s/hq-registry.yaml missing the gridiron-2000-hq-v1-registry ConfigMap name")
	}
	if !strings.Contains(registry, "secret_env") {
		t.Error("deploy/k8s/hq-registry.yaml registry.json must name each connection's secret_env pointer, never a raw credential")
	}
}

func readManifestForTopology(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

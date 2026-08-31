package main

import (
	"os"
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
			peer:       "skl=http://gridiron-2000-sk.stablekernel.svc.cluster.local|https://sk.gridiron.draco.quest",
			secret:     "gridiron-2000-secrets",
		},
		{
			name:       "stable kernel",
			path:       "deploy/k8s/sk/deployment.yaml",
			instanceID: "skl",
			peer:       "g2k=http://gridiron-2000.gridiron.svc.cluster.local|https://gridiron.draco.quest",
			secret:     "gridiron-2000-sk-secrets",
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
// (review of ff2a9b3): the live-scoring env values must be present, with
// the kill switch off, on both the flagship and Stable Kernel app
// manifests. The Stable Kernel league did not form for the 2026 season —
// flagship is the only live instance, enabled behind a replay rehearsal
// and the Thursday-night kill-switch watch (docs/launch-checklist.md) —
// so deploy/k8s/sk/deployment.yaml is not a live canary today; it stays
// tracked as the template a future second live instance rolls from, and
// its values still must not drift from the flagship's. Neither manifest
// may carry the deprecated LIVE_POLL_INTERVAL: a stale tracked 5s value
// would silently restore the pre-GC-2 blanket-polling cadence the moment
// someone re-added it, even though the code still accepts it as a
// self-hoster's alias. STATRELAY_DAILY_BUDGET must be pinned on the
// shared relay's own manifest.
func TestLiveScoringDeploymentValuesArePinnedAndMirrored(t *testing.T) {
	liveScoringEnv := []string{
		"name: LIVE_SCORING_ENABLED\n              value: \"false\"",
		"name: LIVE_SCOREBOARD_INTERVAL\n              value: \"10s\"",
		"name: LIVE_BOX_BASELINE\n              value: \"60s\"",
		"name: LIVE_MAX_INFLIGHT\n              value: \"4\"",
		"name: LIVE_DAILY_BUDGET\n              value: \"5000\"",
	}
	for _, path := range []string{"deploy/k8s/deployment.yaml", "deploy/k8s/sk/deployment.yaml"} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := string(raw)
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
	if want := "name: STATRELAY_DAILY_BUDGET\n              value: \"10000\""; !strings.Contains(string(relay), want) {
		t.Errorf("deploy/k8s/statrelay.yaml omitted %q", want)
	}
}

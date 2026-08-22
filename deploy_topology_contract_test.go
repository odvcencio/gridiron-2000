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

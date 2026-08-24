package commissioner

import (
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
	"m31labs.dev/gosx/route"
)

func TestCommissionerReleaseMetadataRendersExactFieldsAndHonestUnknowns(t *testing.T) {
	fixed := time.Date(2026, time.August, 24, 5, 0, 0, 0, time.UTC)
	fullSHA := "0123456789abcdef0123456789abcdef01234567"
	entries := []commissionerhq.FleetEntry{
		{
			PeerID: "local", PublicURL: "https://local.gridiron.example",
			Summary: commissionerhq.Summary{
				GeneratedAt: fixed,
				Instance:    commissionerhq.Instance{Name: "LOCAL LEAGUE", ShortCode: "LOC", Mode: "dynasty", Season: 2026, PublicURL: "https://local.gridiron.example"},
				Runtime:     commissionerhq.Runtime{Ready: true, AppVersion: "release-2026.08.24", FrameworkVersion: "v0.53.7", GitSHA: fullSHA, Build: "2026-08-24T05:00:00Z"},
			},
		},
		{
			PeerID: "peer", PublicURL: "https://peer.gridiron.example",
			Summary: commissionerhq.Summary{
				GeneratedAt: fixed,
				Instance:    commissionerhq.Instance{Name: "PEER LEAGUE", ShortCode: "PEER", Mode: "dynasty", Season: 2026, PublicURL: "https://peer.gridiron.example"},
				Runtime:     commissionerhq.Runtime{Ready: true},
			},
		},
		{PeerID: "offline", PublicURL: "https://offline.gridiron.example", Error: "request timed out"},
	}
	props := readoutFromView(buildFleetView(entries, fixed), true, true)
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
		Values: map[string]any{"props": props},
	})
	if err != nil {
		t.Fatal(err)
	}

	metadata := renderedMetadataForPeer(t, body, "local")
	for _, want := range []string{
		"APP VERSION", "release-2026.08.24",
		"SOURCE GIT SHA", fullSHA,
		"BUILD TIMESTAMP", "2026-08-24T05:00:00Z",
		"FRAMEWORK VERSION", "v0.53.7",
	} {
		if !strings.Contains(metadata, want) {
			t.Errorf("known release metadata missing %q: %s", want, metadata)
		}
	}
	if strings.Contains(metadata, "sha256:") || strings.Contains(strings.ToLower(metadata), "digest") {
		t.Fatalf("release metadata invented an image digest claim: %s", metadata)
	}

	unknown := renderedMetadataForPeer(t, body, "peer")
	if got := strings.Count(unknown, "UNKNOWN"); got != 4 {
		t.Fatalf("unknown runtime metadata count = %d, want four explicit UNKNOWN values: %s", got, unknown)
	}
	if strings.Contains(unknown, "release-") || strings.Contains(unknown, "sha256:") {
		t.Fatalf("unknown peer rendered fabricated release metadata: %s", unknown)
	}

	if strings.Contains(body, "sha256:") || strings.Contains(strings.ToLower(body), "image digest") {
		t.Fatal("commissioner release UI must not claim an image digest")
	}
}

func renderedMetadataForPeer(t *testing.T, body, peerID string) string {
	t.Helper()
	cardStart := strings.Index(body, "data-peer-id=\""+peerID+"\"")
	if cardStart < 0 {
		t.Fatalf("rendered commissioner card %q not found: %s", peerID, body)
	}
	metadataStart := strings.Index(body[cardStart:], "class=\"commissioner-hq__provenance\"")
	if metadataStart < 0 {
		t.Fatalf("release metadata block for %q not found: %s", peerID, body[cardStart:])
	}
	metadataStart += cardStart
	metadataEnd := strings.Index(body[metadataStart:], "</section>")
	if metadataEnd < 0 {
		t.Fatalf("release metadata block for %q is not closed: %s", peerID, body[metadataStart:])
	}
	return body[metadataStart : metadataStart+metadataEnd]
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeatlessSeatTiedSurfacesRenderBrowseOnlyBranches(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		gate       string
		manager    []string
		browseOnly []string
	}{
		{
			name: "board",
			path: filepath.Join("board", "page.gsx"),
			gate: "data.can_edit",
			manager: []string{
				"Add players from the pool on the right. Your order is saved to your seat.",
				"<h2>Rank on your Big Board</h2>",
			},
			browseOnly: []string{
				"BROWSE THE PLAYER POOL",
				"A franchise seat is required before this page can save a private draft order.",
				"<h2>Browse available players</h2>",
			},
		},
		{
			name: "blitz",
			path: filepath.Join("blitz", "page.gsx"),
			gate: "data.can_enter",
			manager: []string{
				"Add up to 5 players from the eligible list below.",
				"<h2>Add to your entry</h2>",
			},
			browseOnly: []string{
				"<h2>Seat entry locked</h2>",
				"BROWSE THE ELIGIBLE POOL",
				"Entry controls unlock only for an identity that manages a franchise seat.",
				"<h2>Browse eligible players</h2>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			body := string(source)
			managerGate := "<If cond={" + tt.gate + "}>"
			browseGate := "<If cond={" + tt.gate + " == false}>"
			for _, copy := range tt.manager {
				copyAt := strings.Index(body, copy)
				if copyAt < 0 {
					t.Fatalf("%s missing manager-only copy %q", tt.path, copy)
				}
				gateAt := strings.LastIndex(body[:copyAt], managerGate)
				browseAt := strings.LastIndex(body[:copyAt], browseGate)
				if gateAt < 0 || gateAt < browseAt {
					t.Fatalf("%s manager-only copy %q is not inside the enabled branch", tt.path, copy)
				}
			}
			for _, copy := range tt.browseOnly {
				copyAt := strings.Index(body, copy)
				if copyAt < 0 {
					t.Fatalf("%s missing browse-only copy %q", tt.path, copy)
				}
				gateAt := strings.LastIndex(body[:copyAt], browseGate)
				managerAt := strings.LastIndex(body[:copyAt], managerGate)
				if gateAt < 0 || gateAt < managerAt {
					t.Fatalf("%s browse-only copy %q is not inside the locked branch", tt.path, copy)
				}
			}
			for _, want := range []string{
				"data-gosx-managed=\"true\"",
				"type=\"button\" disabled=\"disabled\">Locked</button>",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s lost progressive/native action fallback %q", tt.path, want)
				}
			}
		})
	}
}

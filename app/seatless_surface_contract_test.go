package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeatlessSurfacesUseCanonicalPublicEntryProjection(t *testing.T) {
	tests := []struct {
		name string
		path string
		old  string
	}{
		{
			name: "team",
			path: filepath.Join("team", "page.gsx"),
			old:  "data.fantasy_card.league_full",
		},
		{
			name: "board",
			path: filepath.Join("board", "page.gsx"),
			old:  "SIGN IN REQUIRED:",
		},
		{
			name: "blitz",
			path: filepath.Join("blitz", "page.gsx"),
			old:  "SIGN IN REQUIRED:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			body := string(source)
			for _, want := range []string{
				"data.public_entry.state_label",
				"data.public_entry.detail",
				"data.public_entry.action_href",
				"data.public_entry.action_label",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing canonical projection field %q", tt.path, want)
				}
			}
			if strings.Contains(body, tt.old) {
				t.Fatalf("%s retained page-local seatless truth %q", tt.path, tt.old)
			}
		})
	}
}

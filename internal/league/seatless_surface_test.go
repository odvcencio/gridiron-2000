package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func seatlessSurfaceData(t *testing.T, service *Service, email string) (map[string]any, map[string]any, map[string]any) {
	t.Helper()
	var team, board, blitz map[string]any
	withPublicEntryRequest(t, service, email, func(r *http.Request) {
		team = service.TeamData(r)
		board = service.BoardData(r)
		blitz = service.BlitzData(r)
	})
	return team, board, blitz
}

func anonymousSurfaceData(service *Service) (map[string]any, map[string]any, map[string]any) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	return service.TeamData(request), service.BoardData(request), service.BlitzData(request)
}

func publicEntryMap(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	entry, ok := data["public_entry"].(map[string]any)
	if !ok {
		t.Fatalf("public_entry = %#v, want map", data["public_entry"])
	}
	return entry
}

func TestSeatlessSurfaceRoleMatrixUsesCanonicalPublicEntry(t *testing.T) {
	tests := []struct {
		name           string
		email          string
		setup          func(*testing.T, *Service)
		configureEnv   func(*testing.T)
		wantState      PublicEntryState
		wantAction     string
		wantClaim      bool
		wantLeagueFull bool
	}{
		{
			name:       "anonymous",
			wantState:  PublicEntryAnonymous,
			wantAction: "/login",
		},
		{
			name:  "admitted open seatless",
			email: "open-surface@example.com",
			setup: func(t *testing.T, service *Service) {
				if _, err := service.EnsureMember("open-surface@example.com", "Open Surface"); err != nil {
					t.Fatal(err)
				}
				if err := service.store.BoardAdd("open-surface@example.com", service.players[0].ID); err != nil {
					t.Fatal(err)
				}
			},
			wantState:  PublicEntryAdmittedSeatlessOpen,
			wantAction: "/join",
			wantClaim:  true,
		},
		{
			name:  "authenticated but unrecorded",
			email: "unrecorded-surface@example.com",
			configureEnv: func(t *testing.T) {
				t.Setenv("LEAGUE_ALLOWED_EMAILS", "admitted-only@example.com")
			},
			wantState:  PublicEntryAuthenticatedPending,
			wantAction: "/guide#identity",
		},
		{
			name:  "admitted full seatless",
			email: "full-surface@example.com",
			setup: func(t *testing.T, service *Service) {
				for index := range service.Teams() {
					if _, _, err := service.store.AssignMember("claimed-surface-"+string(rune('a'+index))+"@example.com", "Claimed"); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := service.EnsureMember("full-surface@example.com", "Full Surface"); err != nil {
					t.Fatal(err)
				}
			},
			wantState:      PublicEntryAdmittedSeatlessFull,
			wantAction:     "/pickem",
			wantLeagueFull: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, false)
			if tt.configureEnv != nil {
				tt.configureEnv(t)
			}
			if tt.setup != nil {
				tt.setup(t, service)
			}

			var team, board, blitz map[string]any
			if tt.email == "" {
				team, board, blitz = anonymousSurfaceData(service)
			} else {
				team, board, blitz = seatlessSurfaceData(t, service, tt.email)
			}

			for surface, data := range map[string]map[string]any{
				"team": team, "board": board, "blitz": blitz,
			} {
				entry := publicEntryMap(t, data)
				if got := PublicEntryState(entry["state"].(string)); got != tt.wantState {
					t.Errorf("%s state = %q, want %q", surface, got, tt.wantState)
				}
				if got, _ := entry["action_href"].(string); got != tt.wantAction {
					t.Errorf("%s action = %q, want %q", surface, got, tt.wantAction)
				}
				if got, _ := entry["can_claim"].(bool); got != tt.wantClaim {
					t.Errorf("%s can_claim = %v, want %v", surface, got, tt.wantClaim)
				}
				if got, _ := entry["league_full"].(bool); got != tt.wantLeagueFull {
					t.Errorf("%s league_full = %v, want %v", surface, got, tt.wantLeagueFull)
				}
			}

			if got, _ := team["has_seat"].(bool); got {
				t.Fatal("seatless matrix team surface reported a seat")
			}
			if _, leaked := team["team"]; leaked {
				t.Fatalf("seatless team surface leaked team data: %#v", team["team"])
			}
			if got, _ := board["can_edit"].(bool); got {
				t.Fatal("seatless board surface exposed edit authority")
			}
			if got, _ := board["board_count"].(int); got != 0 {
				t.Fatalf("seatless board exposed private entries: %v", got)
			}
			if got, _ := blitz["can_enter"].(bool); got {
				t.Fatal("seatless blitz surface exposed entry authority")
			}
			if got, _ := blitz["slots_count"].(int); got != 0 {
				t.Fatalf("seatless blitz exposed private slots: %v", got)
			}
		})
	}
}

func TestSeatlessSurfaceCopyDoesNotUseAnonymousSignInForKnownIdentity(t *testing.T) {
	service := newTestService(t, false)
	if _, err := service.EnsureMember("known-surface@example.com", "Known"); err != nil {
		t.Fatal(err)
	}
	_, board, blitz := seatlessSurfaceData(t, service, "known-surface@example.com")
	for name, data := range map[string]map[string]any{"board": board, "blitz": blitz} {
		entry := publicEntryMap(t, data)
		if entry["state"] == string(PublicEntryAnonymous) {
			t.Fatalf("%s downgraded an authenticated seatless viewer to anonymous copy", name)
		}
		if detail, _ := entry["detail"].(string); strings.Contains(detail, "SIGN IN TO ENTER") {
			t.Fatalf("%s retained anonymous entry detail: %q", name, detail)
		}
	}
}

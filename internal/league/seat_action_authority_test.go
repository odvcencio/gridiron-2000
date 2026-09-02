package league

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func seatActionTestService(t *testing.T, demo bool) (*Service, time.Time) {
	t.Helper()
	now := time.Date(2026, time.August, 23, 18, 0, 0, 0, time.UTC)
	service := newTestService(t, demo)
	service.now = func() time.Time { return now }
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return blitzFixturePool(), 1, "live"
	})
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{
			Version: 1,
			Games:   blitzFixtureGames(now),
			Stats:   map[string]map[string]map[string]float64{},
		}
	})
	return service, now
}

func TestSeatTiedMutationsRejectSeatlessIdentitiesWithoutStateLeak(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		configureEnv func(*testing.T)
		setup        func(*testing.T, *Service)
		wantState    PublicEntryState
	}{
		{
			name:  "admitted open",
			email: "admitted-open-action@example.com",
			setup: func(t *testing.T, service *Service) {
				if _, err := service.EnsureMember("admitted-open-action@example.com", "Admitted Open"); err != nil {
					t.Fatal(err)
				}
			},
			wantState: PublicEntryAdmittedSeatlessOpen,
		},
		{
			name:  "admitted full",
			email: "admitted-full-action@example.com",
			setup: func(t *testing.T, service *Service) {
				for _, team := range service.Teams() {
					if _, _, err := service.store.AssignMember(team.ID+"@filled.example", team.Name); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := service.EnsureMember("admitted-full-action@example.com", "Admitted Full"); err != nil {
					t.Fatal(err)
				}
			},
			wantState: PublicEntryAdmittedSeatlessFull,
		},
		{
			name:      "authenticated unrecorded",
			email:     "unrecorded-action@example.com",
			wantState: PublicEntryAuthenticatedPending,
		},
		{
			name:  "authenticated ineligible",
			email: "ineligible-action@example.com",
			configureEnv: func(t *testing.T) {
				t.Setenv("LEAGUE_ALLOWED_EMAILS", "someone-else@example.com")
			},
			wantState: PublicEntryAuthenticatedPending,
		},
		{
			name:  "pending co-manager",
			email: "pending-co-action@example.com",
			setup: func(t *testing.T, service *Service) {
				primary, _, err := service.store.AssignMember("pending-primary@example.com", "Pending Primary")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.store.InviteCoManager(primary.TeamID, "pending-co-action@example.com"); err != nil {
					t.Fatal(err)
				}
			},
			wantState: PublicEntryCoManagerPending,
		},
		{
			name:  "commissioner without seat",
			email: "seatless-commissioner-action@example.com",
			configureEnv: func(t *testing.T) {
				t.Setenv("COMMISSIONER_EMAILS", "seatless-commissioner-action@example.com")
			},
			setup: func(t *testing.T, service *Service) {
				if _, err := service.EnsureMember("seatless-commissioner-action@example.com", "Seatless Commissioner"); err != nil {
					t.Fatal(err)
				}
			},
			wantState: PublicEntryAdmittedSeatlessOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _ := seatActionTestService(t, false)
			if tt.configureEnv != nil {
				tt.configureEnv(t)
			}
			if tt.setup != nil {
				tt.setup(t, service)
			}
			before := service.store.Snapshot()
			withPublicEntryRequest(t, service, tt.email, func(r *http.Request) {
				if got := service.PublicEntryView(r).State; got != tt.wantState {
					t.Fatalf("public entry state = %q, want %q", got, tt.wantState)
				}
				actions := []struct {
					name string
					call func() error
				}{
					{name: "board add", call: func() error {
						_, err := service.BoardAdd(r, "p-kc-1")
						return err
					}},
					{name: "board move", call: func() error {
						return service.BoardMove(r, "p-kc-1", "up")
					}},
					{name: "board move to", call: func() error {
						return service.BoardMoveTo(r, "p-kc-1", 0)
					}},
					{name: "board remove", call: func() error {
						return service.BoardRemove(r, "p-kc-1")
					}},
					{name: "board clear", call: func() error {
						return service.BoardClear(r, "clear-board")
					}},
					{name: "blitz add", call: func() error {
						return service.BlitzAdd(r, "pre2", "p-kc-1")
					}},
					{name: "blitz remove", call: func() error {
						return service.BlitzRemove(r, "pre2", "p-kc-1")
					}},
				}
				for _, action := range actions {
					if err := action.call(); err == nil || !strings.Contains(err.Error(), "claim a team seat") {
						t.Errorf("%s error = %v, want seat-required denial", action.name, err)
					}
					if after := service.store.Snapshot(); !reflect.DeepEqual(before, after) {
						t.Fatalf("%s mutated persisted state\nbefore: %#v\n after: %#v", action.name, before, after)
					}
				}
				board := service.BoardData(r)
				if board["can_edit"] != false || board["board_count"] != 0 {
					t.Fatalf("seatless board projection = can_edit:%v count:%v", board["can_edit"], board["board_count"])
				}
				blitz := service.BlitzData(r)
				if blitz["can_enter"] != false || blitz["slots_count"] != 0 {
					t.Fatalf("seatless blitz projection = can_enter:%v slots:%v", blitz["can_enter"], blitz["slots_count"])
				}
			})

			after := service.store.Snapshot()
			key := normalizeEmail(tt.email)
			if _, exists := after.Boards[key]; exists {
				t.Fatalf("denied identity acquired hidden board key %q", key)
			}
			if _, exists := after.BlitzEntries[key]; exists {
				t.Fatalf("denied identity acquired hidden blitz key %q", key)
			}
		})
	}
}

func TestSeatTiedMutationsSharePrimaryOwnerAcrossManagers(t *testing.T) {
	service, _ := seatActionTestService(t, false)
	primary, _, err := service.store.AssignMember("seat-primary@example.com", "Seat Primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager(primary.TeamID, "seat-co@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := service.BindCoManagerOnSignIn("seat-co@example.com", "Seat Co"); err != nil || !bound {
		t.Fatalf("bind co-manager = %v, %v", bound, err)
	}
	t.Setenv("COMMISSIONER_EMAILS", primary.Email)

	withPublicEntryRequest(t, service, primary.Email, func(r *http.Request) {
		authority, err := service.requestSeatAuthorityForState(r, service.store.Snapshot(), primary.TeamID)
		if err != nil || authority.TeamID != primary.TeamID || authority.OwnerKey != primary.Email {
			t.Fatalf("primary authority = %+v, %v", authority, err)
		}
		otherTeam := service.Teams()[1].ID
		if _, err := service.requestSeatAuthorityForState(r, service.store.Snapshot(), otherTeam); !errors.Is(err, errSeatActionWrong) {
			t.Fatalf("commissioner cross-seat authority error = %v, want %v", err, errSeatActionWrong)
		}
		if _, err := service.BoardAdd(r, "p-kc-1"); err != nil {
			t.Fatalf("primary board add: %v", err)
		}
		if err := service.BlitzAdd(r, "pre2", "p-kc-1"); err != nil {
			t.Fatalf("primary blitz add: %v", err)
		}
	})
	withPublicEntryRequest(t, service, "seat-co@example.com", func(r *http.Request) {
		authority, err := service.requestSeatAuthorityForState(r, service.store.Snapshot(), primary.TeamID)
		if err != nil || authority.OwnerKey != primary.Email {
			t.Fatalf("co-manager authority = %+v, %v", authority, err)
		}
		if _, err := service.BoardAdd(r, "p-den-1"); err != nil {
			t.Fatalf("co-manager board add: %v", err)
		}
		if err := service.BoardMove(r, "p-den-1", "up"); err != nil {
			t.Fatalf("co-manager board move: %v", err)
		}
		if err := service.BoardRemove(r, "p-kc-1"); err != nil {
			t.Fatalf("co-manager board remove: %v", err)
		}
		if err := service.BlitzAdd(r, "pre2", "p-den-1"); err != nil {
			t.Fatalf("co-manager blitz add: %v", err)
		}
		if err := service.BlitzRemove(r, "pre2", "p-kc-1"); err != nil {
			t.Fatalf("co-manager blitz remove: %v", err)
		}
		if data := service.BoardData(r); data["can_edit"] != true || data["board_count"] != 1 {
			t.Fatalf("co-manager board projection = %#v", data)
		}
		if data := service.BlitzData(r); data["can_enter"] != true || data["slots_count"] != 1 {
			t.Fatalf("co-manager blitz projection = %#v", data)
		}
	})

	state := service.store.Snapshot()
	if got := state.Boards[primary.Email]; len(got) != 1 || got[0] != "p-den-1" {
		t.Fatalf("shared seat board = %v, want [p-den-1]", got)
	}
	if _, exists := state.Boards["seat-co@example.com"]; exists {
		t.Fatal("co-manager acquired a separate board")
	}
	if got := state.BlitzEntries[primary.Email]["pre2"].Players; len(got) != 1 || got[0] != "p-den-1" {
		t.Fatalf("shared seat blitz entry = %v, want [p-den-1]", got)
	}
	if _, exists := state.BlitzEntries["seat-co@example.com"]; exists {
		t.Fatal("co-manager acquired a separate blitz entry")
	}
}

func TestSeatTiedMutationsPreserveDemoRehearsalAuthority(t *testing.T) {
	service, _ := seatActionTestService(t, true)
	request := httptest.NewRequest(http.MethodGet, "/board", nil)
	if _, err := service.BoardAdd(request, "p-kc-1"); err != nil {
		t.Fatalf("demo board add: %v", err)
	}
	if err := service.BlitzAdd(request, "pre2", "p-kc-1"); err != nil {
		t.Fatalf("demo blitz add: %v", err)
	}
	state := service.store.Snapshot()
	if got := state.Boards["demo-guest"]; len(got) != 1 || got[0] != "p-kc-1" {
		t.Fatalf("demo board = %v", got)
	}
	if got := state.BlitzEntries["demo-guest"]["pre2"].Players; len(got) != 1 || got[0] != "p-kc-1" {
		t.Fatalf("demo blitz entry = %v", got)
	}
	if board := service.BoardData(request); board["can_edit"] != true || board["board_count"] != 1 {
		t.Fatalf("demo board projection = %#v", board)
	}
	if blitz := service.BlitzData(request); blitz["can_enter"] != true || blitz["slots_count"] != 1 {
		t.Fatalf("demo blitz projection = %#v", blitz)
	}
}

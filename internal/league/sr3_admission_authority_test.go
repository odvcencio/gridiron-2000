package league

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSR3ActingTeamRejectsUnrecordedWithoutMutation(t *testing.T) {
	service := newTestService(t, false)
	const email = "sr3-unrecorded-action@example.com"
	before := service.store.Snapshot()

	withPublicEntryRequest(t, service, email, func(r *http.Request) {
		teamID, err := service.actingTeam(r, "team-1")
		if teamID != "" {
			t.Fatalf("actingTeam team = %q, want empty", teamID)
		}
		if err == nil || err.Error() != "claim a team seat before taking this action" {
			t.Fatalf("actingTeam error = %v, want seat-claim denial", err)
		}
	})
	if after := service.store.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("unrecorded actingTeam changed persisted state\nbefore: %#v\n after: %#v", before, after)
	}
}

func TestSR3PickemAdmissionRoleMatrix(t *testing.T) {
	now := time.Date(2026, time.August, 23, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		email            string
		wantState        PublicEntryState
		wantCommissioner bool
		wantCanPick      bool
		wantWrite        bool
		wantError        string
		configureEnv     func(*testing.T)
		setup            func(*testing.T, *Service)
	}{
		{
			name:      "unrecorded",
			email:     "sr3-unrecorded-pickem@example.com",
			wantState: PublicEntryAuthenticatedPending,
			wantError: "league admission is not recorded",
		},
		{
			name:      "pending co-manager",
			email:     "sr3-pending-co@example.com",
			wantState: PublicEntryCoManagerPending,
			wantError: "complete the pending co-manager invitation",
			setup: func(t *testing.T, service *Service) {
				primary, _, err := service.store.AssignMember("sr3-pending-primary@example.com", "Pending Primary")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.store.InviteCoManager(primary.TeamID, "sr3-pending-co@example.com"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:        "admitted open seatless",
			email:       "sr3-admitted-open@example.com",
			wantState:   PublicEntryAdmittedSeatlessOpen,
			wantCanPick: true,
			wantWrite:   true,
			setup: func(t *testing.T, service *Service) {
				if _, err := service.EnsureMember("sr3-admitted-open@example.com", "Admitted Open"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:        "admitted full seatless",
			email:       "sr3-admitted-full@example.com",
			wantState:   PublicEntryAdmittedSeatlessFull,
			wantCanPick: true,
			wantWrite:   true,
			setup: func(t *testing.T, service *Service) {
				for _, team := range service.Teams() {
					if _, _, err := service.store.AssignMember(team.ID+"@sr3-filled.example", team.Name); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := service.EnsureMember("sr3-admitted-full@example.com", "Admitted Full"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:        "primary",
			email:       "sr3-primary@example.com",
			wantState:   PublicEntryPrimary,
			wantCanPick: true,
			wantWrite:   true,
			setup: func(t *testing.T, service *Service) {
				if _, _, err := service.store.AssignMember("sr3-primary@example.com", "Primary"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:        "co-manager",
			email:       "sr3-co@example.com",
			wantState:   PublicEntryCoManager,
			wantCanPick: true,
			wantWrite:   true,
			setup: func(t *testing.T, service *Service) {
				primary, _, err := service.store.AssignMember("sr3-co-primary@example.com", "Co Primary")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.store.InviteCoManager(primary.TeamID, "sr3-co@example.com"); err != nil {
					t.Fatal(err)
				}
				if _, bound, err := service.BindCoManagerOnSignIn("sr3-co@example.com", "Co Manager"); err != nil || !bound {
					t.Fatalf("bind co-manager = %v, %v", bound, err)
				}
			},
		},
		{
			name:             "commissioner overlay without membership",
			email:            "sr3-overlay-unrecorded@example.com",
			wantState:        PublicEntryAuthenticatedPending,
			wantCommissioner: true,
			wantError:        "league admission is not recorded",
			configureEnv: func(t *testing.T) {
				t.Setenv("COMMISSIONER_EMAILS", "sr3-overlay-unrecorded@example.com")
			},
		},
		{
			name:             "recorded commissioner overlay",
			email:            "sr3-overlay-recorded@example.com",
			wantState:        PublicEntryAdmittedSeatlessOpen,
			wantCommissioner: true,
			wantCanPick:      true,
			wantWrite:        true,
			configureEnv: func(t *testing.T) {
				t.Setenv("COMMISSIONER_EMAILS", "sr3-overlay-recorded@example.com")
			},
			setup: func(t *testing.T, service *Service) {
				if _, err := service.EnsureMember("sr3-overlay-recorded@example.com", "Recorded Commissioner"); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, false)
			service.now = func() time.Time { return now }
			if tt.configureEnv != nil {
				tt.configureEnv(t)
			}
			if tt.setup != nil {
				tt.setup(t, service)
			}
			games := pickemFixture(now)
			service.SetScheduleSource(func() []GameInfo { return games })

			withPublicEntryRequest(t, service, tt.email, func(r *http.Request) {
				view := service.PublicEntryView(r)
				if view.State != tt.wantState {
					t.Fatalf("public entry state = %q, want %q", view.State, tt.wantState)
				}
				if view.IsCommissioner != tt.wantCommissioner {
					t.Fatalf("is commissioner = %v, want %v", view.IsCommissioner, tt.wantCommissioner)
				}
				data := service.PickemData(r)
				if got := data["can_pick"]; got != tt.wantCanPick {
					t.Fatalf("can_pick = %v, want %v", got, tt.wantCanPick)
				}

				before := service.store.Snapshot()
				_, err := service.PickemSet(r, "g-open", "KC")
				if tt.wantWrite {
					if err != nil {
						t.Fatalf("PickemSet: %v", err)
					}
					if got := service.store.Snapshot().Pickems[tt.email]["g-open"]; got != "KC" {
						t.Fatalf("Pickems[%q][g-open] = %q, want KC", tt.email, got)
					}
				} else {
					if err == nil || !strings.Contains(err.Error(), tt.wantError) {
						t.Fatalf("PickemSet error = %v, want %q denial", err, tt.wantError)
					}
					if after := service.store.Snapshot(); !reflect.DeepEqual(before, after) {
						t.Fatalf("%s PickemSet changed persisted state\nbefore: %#v\n after: %#v", tt.name, before, after)
					}
				}
			})
		})
	}
}

func TestSR3PickemPrimaryAndCoUseIndependentOwners(t *testing.T) {
	now := time.Date(2026, time.August, 23, 18, 0, 0, 0, time.UTC)
	service := newTestService(t, false)
	service.now = func() time.Time { return now }
	games := pickemFixture(now)
	service.SetScheduleSource(func() []GameInfo { return games })

	primary, _, err := service.store.AssignMember("sr3-independent-primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager(primary.TeamID, "sr3-independent-co@example.com"); err != nil {
		t.Fatal(err)
	}
	co, bound, err := service.BindCoManagerOnSignIn("sr3-independent-co@example.com", "Co Manager")
	if err != nil || !bound {
		t.Fatalf("bind co-manager = %v, %v", bound, err)
	}

	withPublicEntryRequest(t, service, primary.Email, func(r *http.Request) {
		if _, err := service.PickemSet(r, "g-open", "KC"); err != nil {
			t.Fatalf("primary PickemSet: %v", err)
		}
	})
	withPublicEntryRequest(t, service, co.Email, func(r *http.Request) {
		if _, err := service.PickemSet(r, "g-open", "DEN"); err != nil {
			t.Fatalf("co-manager PickemSet: %v", err)
		}
	})

	state := service.store.Snapshot()
	if got := state.Pickems[primary.Email]["g-open"]; got != "KC" {
		t.Fatalf("primary Pickems[g-open] = %q, want KC", got)
	}
	if got := state.Pickems[co.Email]["g-open"]; got != "DEN" {
		t.Fatalf("co Pickems[g-open] = %q, want DEN", got)
	}
	if reflect.DeepEqual(state.Pickems[primary.Email], state.Pickems[co.Email]) {
		t.Fatalf("primary and co Pick'em maps collapsed to one owner: %#v", state.Pickems)
	}

	for _, tt := range []struct {
		name, email, want string
	}{
		{name: "primary", email: primary.Email, want: "KC"},
		{name: "co-manager", email: co.Email, want: "DEN"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withPublicEntryRequest(t, service, tt.email, func(r *http.Request) {
				data := service.PickemData(r)
				rows, ok := data["games"].([]PickemGameRow)
				if !ok {
					t.Fatalf("games = %T, want []PickemGameRow", data["games"])
				}
				var open PickemGameRow
				for _, row := range rows {
					if row.ID == "g-open" {
						open = row
						break
					}
				}
				if open.ID != "g-open" || open.Pick != tt.want {
					t.Fatalf("g-open row = %+v, want pick %q", open, tt.want)
				}
			})
		})
	}
}

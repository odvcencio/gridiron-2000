package league

import (
	"net/http"
	"reflect"
	"testing"
)

func TestFragmentReadOnlyProjectionsDoNotProvisionOrMutate(t *testing.T) {
	service, _ := newPlayersTestService(t)
	request, err := http.NewRequest(http.MethodGet, "/players?pos=RB&q=open&page=2", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := service.store.Snapshot()
	players := service.PlayersDataReadOnly(request)
	activity := service.ActivityDataReadOnly(request)
	attention := service.CommissionerAttentionDataReadOnly(request)
	after := service.store.Snapshot()
	if players == nil || activity == nil || attention == nil {
		t.Fatal("read-only projections returned nil data")
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read-only fragment projections changed persisted league state")
	}
	if players["pos"] != "RB" || players["query"] != "open" || activity["query"] != "open" {
		t.Fatalf("fragment projections lost request-local filters: players=%#v activity=%#v", players, activity)
	}
	if attention["seat_count"] == nil || attention["seats"] == nil {
		t.Fatalf("commissioner attention projection omitted safe state: %#v", attention)
	}
}

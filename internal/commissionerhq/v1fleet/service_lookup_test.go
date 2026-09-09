package v1fleet

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

func TestConnectionLookupIsReadOnlyAndCredentialFree(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	connection := fleetTestConnection(t, "alpha", "alpha-league", 1)
	credentials, err := v1transport.NewCredentials("lookup-key", []byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	connection.target, err = v1transport.NewTarget("https://provider.internal", connection.LeagueID, credentials)
	if err != nil {
		t.Fatal(err)
	}
	service := newFleetTestService(t, []Connection{connection}, &now, func(context.Context, Connection) (hqv1.Summary, error) {
		return hqv1.Summary{}, nil
	}, Options{})
	if !service.HasConnection("alpha") || service.HasConnection("missing") || service.HasConnection("Bad") {
		t.Fatal("connection presence lookup was not exact")
	}
	copyValue, ok := service.LookupConnection("alpha")
	if !ok || copyValue.Key != "alpha" || !reflect.DeepEqual(copyValue.target, v1transport.Target{}) {
		t.Fatalf("lookup exposed credential target: %+v", copyValue)
	}
	copyValue.Capabilities[0] = "tampered"
	if copyValue.Links.League != nil {
		*copyValue.Links.League = "/tampered"
	}
	again, ok := service.LookupConnection("alpha")
	if !ok || again.Capabilities[0] == "tampered" || again.Links.League == nil || *again.Links.League == "/tampered" {
		t.Fatalf("lookup was not defensive: %+v", again)
	}
}

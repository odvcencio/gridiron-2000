package league

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"m31labs.dev/gosx/auth"
)

func TestTradesDataReadOnlyDoesNotProvisionAuthenticatedViewer(t *testing.T) {
	service := newTestService(t, false)
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "trade-observer", Email: "trade-observer@example.com", Name: "Trade Observer"}, true
	})})
	before := service.store.Snapshot()
	var data map[string]any
	handler := authn.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		data = service.TradesDataReadOnly(request)
		writer.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/trades/fragment?counterparty=team-2", nil))
	after := service.store.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("read-only Trade data mutated persistence\nbefore: %#v\n after: %#v", before, after)
	}
	viewer, _ := data["viewer"].(map[string]any)
	if viewer["signed_in"] != true || viewer["has_seat"] != false || viewer["email"] != "trade-observer@example.com" {
		t.Fatalf("read-only Trade viewer = %#v", viewer)
	}
	if data["compose_counterparty_id"] != "" {
		t.Fatalf("seatless Trade viewer selected a composer counterparty: %#v", data["compose_counterparty_id"])
	}
}

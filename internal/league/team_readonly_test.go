package league

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"m31labs.dev/gosx/auth"
)

func TestTeamDataReadOnlyDoesNotProvisionAuthenticatedViewer(t *testing.T) {
	service := newTestService(t, false)
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: "observer", Email: "observer@example.com", Name: "Observer"}, true
	})})
	before := service.store.Snapshot()
	var data map[string]any
	handler := authn.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		data = service.TeamDataReadOnly(request)
		writer.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/team/fragment?week=1", nil))
	after := service.store.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("read-only Team data mutated persistence\nbefore: %#v\n after: %#v", before, after)
	}
	viewer, _ := data["viewer"].(map[string]any)
	if viewer["signed_in"] != true || viewer["has_seat"] != false || viewer["email"] != "observer@example.com" {
		t.Fatalf("read-only Team viewer = %#v", viewer)
	}
}

package league

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/auth"
)

func TestCommissionerCanonicalIdentityAcceptsExplicitProviderAlias(t *testing.T) {
	service := newTestService(t, false)
	service.identityResolver = testIdentityResolver(t)
	t.Setenv("COMMISSIONER_EMAILS", identityCanonicalEmail)

	if !commissionerForEmail(t, service, "commissioner.alias@example.org") {
		t.Fatal("explicit Stable Kernel alias must resolve to the canonical commissioner")
	}
	if !commissionerForEmail(t, service, "commissioner@example.com") {
		t.Fatal("canonical m31labs.dev commissioner must be authorized case-insensitively")
	}
	if commissionerForEmail(t, service, "not-the-commissioner@example.com") {
		t.Fatal("an unlisted identity must not receive commissioner access")
	}
}

func commissionerForEmail(t *testing.T, service *Service, email string) bool {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email}, true
		}),
	})
	var allowed bool
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed = service.IsCommissioner(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/commissioner", nil))
	return allowed
}

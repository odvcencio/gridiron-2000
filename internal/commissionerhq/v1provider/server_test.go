package v1provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
	"gridiron-2000/internal/commissionerhq/v1transport"
)

func serverTestConfig(t *testing.T) Config {
	t.Helper()
	credential, err := v1transport.NewCredentials("provider-key", []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return Config{Enabled: true, Address: ":8091", Credential: credential}
}

func TestNewServerOwnsOnlyPrivateProviderHandler(t *testing.T) {
	server, err := NewServer(serverTestConfig(t), func(context.Context) (hqv1.Summary, error) {
		return hqv1.Summary{}, v1transport.ErrTemporarilyUnavailable
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.ReadHeaderTimeout != 2*time.Second || server.ReadTimeout != 5*time.Second || server.WriteTimeout != 5*time.Second || server.IdleTimeout != 30*time.Second || server.MaxHeaderBytes != 16<<10 {
		t.Fatalf("unsafe server bounds: %#v", server)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("public-style route status = %d, want 404", response.Code)
	}
}

func TestNewServerFailsClosed(t *testing.T) {
	if _, err := NewServer(Config{}, func(context.Context) (hqv1.Summary, error) { return hqv1.Summary{}, nil }); err == nil {
		t.Fatal("NewServer accepted disabled configuration")
	}
	if _, err := NewServer(serverTestConfig(t), nil); err == nil {
		t.Fatal("NewServer accepted nil source")
	}
}

package v1provider

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"gridiron-2000/internal/commissionerhq/v1transport"
)

// NewServer builds the private provider server without binding it. The caller
// owns the listener so bind failure can remain a synchronous startup failure.
func NewServer(config Config, source v1transport.SnapshotSource) (*http.Server, error) {
	if !config.Enabled || config.Address == "" {
		return nil, errors.New("Commissioner HQ provider server is disabled")
	}
	handler, err := v1transport.NewProvider(v1transport.ProviderOptions{
		Keys: []v1transport.Credentials{config.Credential},
	}, source)
	if err != nil {
		return nil, errors.New("Commissioner HQ provider server is misconfigured")
	}
	return &http.Server{
		Addr:              config.Address,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}, nil
}

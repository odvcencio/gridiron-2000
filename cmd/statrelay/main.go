package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// defaultHost mirrors internal/fantasy/model.go's DefaultHost: the Tank01
// NFL API host on RapidAPI. Kept as an independent constant, not an import
// of the fantasy package, so statrelay stays a small, dependency-free
// binary with no ties to the app's own module graph beyond the stdlib.
const defaultHost = "tank01-nfl-live-in-game-real-time-statistics-nfl.p.rapidapi.com"

func main() {
	apiKey := strings.TrimSpace(os.Getenv("TANK01_API_KEY"))
	if apiKey == "" {
		log.Fatal("statrelay: TANK01_API_KEY is required")
	}
	host := envString("TANK01_HOST", defaultHost)
	dataDir := envString("DATA_DIR", "data/statrelay")
	addr := envString("LISTEN_ADDR", ":8090")

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatalf("statrelay: create data dir %s: %v", dataDir, err)
	}

	relay := NewRelay(host, apiKey, dataDir, &http.Client{Timeout: 30 * time.Second}, time.Now)
	relay.LoadDisk()

	server := &http.Server{
		Addr:    addr,
		Handler: relay,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	log.Printf("statrelay: listening on %s (upstream host=%s, cache dir=%s)", addr, host, dataDir)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("statrelay: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("statrelay: shutdown: %v", err)
		}
	}
}

// envString reads key from the environment, trimmed, falling back to
// fallback when unset or blank.
func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

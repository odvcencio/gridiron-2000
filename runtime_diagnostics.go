package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"
)

// Operator diagnostics have a separate loopback listener, never the app mux.
// Heap profiles can contain private data: callers access this listener through
// an authorized SSH/kubectl port-forward and must keep profiles out of Git.
func startRuntimeDiagnostics(ctx context.Context, addr string) error {
	if addr == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("RUNTIME_DIAGNOSTICS_ADDR must use a literal loopback IP and port")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("runtime diagnostics listener: %w", err)
	}
	mux := http.NewServeMux()
	for _, name := range []string{"heap", "allocs", "goroutine"} {
		mux.Handle("GET /debug/pprof/"+name, pprof.Handler(name))
	}
	mux.HandleFunc("GET /memory", func(w http.ResponseWriter, r *http.Request) {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"heapAlloc": stats.HeapAlloc, "heapObjects": stats.HeapObjects,
			"heapSys": stats.HeapSys, "sys": stats.Sys, "gc": stats.NumGC,
			"goroutines": runtime.NumGoroutine(),
		})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = server.Close() }()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("runtime diagnostics: %v", err)
		}
	}()
	return nil
}

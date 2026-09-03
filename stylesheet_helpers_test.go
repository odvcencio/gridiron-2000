package main

import (
	"os"
	"testing"
)

// readStylesheet reads the shipped public/styles.css once per call and
// fails the calling test with a clear message on error, so every contract
// test that scans the compiled stylesheet shares one read path instead of
// repeating the same os.ReadFile-plus-error-check block.
func readStylesheet(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("public/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	return string(data)
}

package v1transport

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	hqv1 "gridiron-2000/internal/commissionerhq/v1"
)

var fixtureTime = time.Unix(1787592600, 0).UTC()

const fixtureRequestID = "req_0123456789abcdef"

func testCredentials(t *testing.T) Credentials {
	t.Helper()
	credentials, err := NewCredentials("league-a", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return credentials
}

func testSummary(t *testing.T) hqv1.Summary {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "v1", "testdata", "minimal_capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := hqv1.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func signRequest(t *testing.T, request *http.Request, credentials Credentials, at time.Time) {
	t.Helper()
	if err := applySignature(request, credentials, at); err != nil {
		t.Fatal(err)
	}
}

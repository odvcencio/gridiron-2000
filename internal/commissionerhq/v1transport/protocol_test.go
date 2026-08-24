package v1transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSignedRequestGolden(t *testing.T) {
	t.Parallel()
	var fixture struct {
		KeyID          string `json:"key_id"`
		Secret         string `json:"secret"`
		Timestamp      string `json:"timestamp"`
		CanonicalInput string `json:"canonical_input"`
		Signature      string `json:"signature"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "signed_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	credentials, err := NewCredentials(fixture.KeyID, []byte(fixture.Secret))
	if err != nil {
		t.Fatal(err)
	}
	if got := CanonicalInput(fixture.Timestamp); got != fixture.CanonicalInput {
		t.Fatalf("CanonicalInput() = %q", got)
	}
	if strings.HasSuffix(fixture.CanonicalInput, "\n") {
		t.Fatal("canonical input has a trailing newline")
	}
	if got := sign(credentials.secret, fixture.Timestamp); got != fixture.Signature {
		t.Fatalf("signature = %q, want %q", got, fixture.Signature)
	}
}

func TestCredentialsValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		key    string
		secret string
		ok     bool
	}{
		{"league-a", strings.Repeat("x", 32), true},
		{"", strings.Repeat("x", 32), false},
		{"bad key", strings.Repeat("x", 32), false},
		{"league-a", "short", false},
		{strings.Repeat("a", 65), strings.Repeat("x", 32), false},
	} {
		_, err := NewCredentials(test.key, []byte(test.secret))
		if (err == nil) != test.ok {
			t.Errorf("NewCredentials(%q) error = %v, ok=%v", test.key, err, test.ok)
		}
	}
}

func TestAuthorizationSkewAndHeaderStrictness(t *testing.T) {
	t.Parallel()
	credentials := testCredentials(t)
	ring, err := newKeyring([]Credentials{credentials})
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		request := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
		signRequest(t, request, credentials, fixtureTime.Add(offset))
		if !ring.authorize(request, fixtureTime) {
			t.Errorf("offset %s was rejected", offset)
		}
	}
	for _, offset := range []time.Duration{-31 * time.Second, 31 * time.Second} {
		request := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
		signRequest(t, request, credentials, fixtureTime.Add(offset))
		if ring.authorize(request, fixtureTime) {
			t.Errorf("offset %s was accepted", offset)
		}
	}

	base := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
	signRequest(t, base, credentials, fixtureTime)
	mutations := []func(*http.Request){
		func(r *http.Request) { r.Header.Add(HeaderKeyID, credentials.keyID) },
		func(r *http.Request) { r.Header.Set(HeaderTimestamp, "01") },
		func(r *http.Request) { r.Header.Set(HeaderSignature, "sha256="+strings.Repeat("A", 64)) },
		func(r *http.Request) { r.Header.Set(HeaderSignature, "sha256="+strings.Repeat("0", 64)) },
		func(r *http.Request) { r.Header.Del(HeaderSignature) },
	}
	for i, mutate := range mutations {
		request := base.Clone(base.Context())
		request.Header = base.Header.Clone()
		mutate(request)
		if ring.authorize(request, fixtureTime) {
			t.Errorf("mutation %d was accepted", i)
		}
	}
	unknown, _ := NewCredentials("unknown", []byte(strings.Repeat("u", 32)))
	request := httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
	signRequest(t, request, unknown, fixtureTime)
	if ring.authorize(request, fixtureTime) {
		t.Fatal("unknown key was accepted")
	}

	request = httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
	request.Header.Set(HeaderKeyID, credentials.keyID)
	request.Header.Set(HeaderTimestamp, strconv.FormatInt(fixtureTime.Unix(), 10))
	request.Header.Set(HeaderSignature, sign(credentials.secret, strconv.FormatInt(fixtureTime.Unix(), 10)))
	if !ring.authorize(request, fixtureTime) {
		t.Fatal("canonical request was rejected")
	}
	extremeTimestamp := "999999999999999999"
	request = httptest.NewRequest(http.MethodGet, "https://league.example"+ProviderPath, nil)
	request.Header.Set(HeaderKeyID, credentials.keyID)
	request.Header.Set(HeaderTimestamp, extremeTimestamp)
	request.Header.Set(HeaderSignature, sign(credentials.secret, extremeTimestamp))
	if ring.authorize(request, fixtureTime) {
		t.Fatal("extreme future timestamp was accepted")
	}
}

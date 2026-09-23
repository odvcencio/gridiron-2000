package main

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"gridiron-2000/internal/league"
)

// gcsTestServiceAccountCredentialFile writes a syntactically real
// "service_account"-type Google credential (RSA private key,
// PEM/PKCS8-encoded) to a temp file and returns its path.
// backupSinksFromEnv's own tests use this to prove the env-var wiring
// (GOOGLE_APPLICATION_CREDENTIALS in, a *gcsBackupSink out) without any
// network round trip — google.CredentialsFromJSON/FindDefaultCredentials
// parse a service_account key entirely locally. Production instead uses
// an "external_account" (Workload Identity Federation) config; that
// resolution path is golang.org/x/oauth2/google's own, separately
// tested, code, not this app's.
func gcsTestServiceAccountCredentialFile(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test RSA key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal test RSA key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	// example.com is an RFC 2606 reserved domain (never a real service
	// account) — this fixture only needs to parse as a syntactically
	// valid service_account credential, never to be a real one; the
	// tracked-repository privacy contract (privacy_contract_test.go)
	// also requires any email-shaped value in a tracked file to use a
	// reserved domain like this one.
	cred := map[string]string{
		"type":           "service_account",
		"project_id":     "test-project",
		"private_key_id": "test-key-id",
		"private_key":    string(pemBytes),
		"client_email":   "gridiron-backup-uploader@test-project.example.com",
		"client_id":      "000000000000000000000",
		"token_uri":      "https://oauth2.googleapis.com/token",
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// gcsFakeUploadServer stands in for the GCS JSON API's simple-upload
// endpoint. gcsBackupSink.Copy is the only code this app owns in the
// upload path — token acquisition itself is golang.org/x/oauth2/google's
// own external_account/Workload Identity Federation resolution (see the
// type's doc comment for why re-implementing that by hand was rejected),
// so these tests inject a static token source directly and verify only
// what this app's own Copy method controls: the snapshot, the object
// name, the auth header, and error propagation.
type gcsFakeUploadServer struct {
	requests     atomic.Int32
	lastAuth     atomic.Value // string
	lastURL      atomic.Value // string
	lastBody     atomic.Value // []byte
	responseCode int32        // 0 == 200
}

func newGCSFakeUploadServer() *gcsFakeUploadServer { return &gcsFakeUploadServer{} }

func (f *gcsFakeUploadServer) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.lastAuth.Store(r.Header.Get("Authorization"))
		f.lastURL.Store(r.URL.String())
		body, _ := io.ReadAll(r.Body)
		f.lastBody.Store(body)
		status := int(atomic.LoadInt32(&f.responseCode))
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}))
}

// gcsSinkForTest builds a gcsBackupSink directly (bypassing
// newGCSBackupSink/google.FindDefaultCredentials entirely — see the type
// doc comment), backed by league.Default() (TestMain's real,
// store-backed process singleton) and a static token source pointed at a
// fake upload server.
func gcsSinkForTest(t *testing.T) (*gcsBackupSink, *gcsFakeUploadServer) {
	t.Helper()
	fake := newGCSFakeUploadServer()
	server := fake.server()
	t.Cleanup(server.Close)

	return &gcsBackupSink{
		bucket:        "test-bucket",
		service:       league.Default(),
		httpClient:    server.Client(),
		now:           time.Now,
		tokenSource:   oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-access-token", TokenType: "Bearer"}),
		uploadBaseURL: server.URL,
	}, fake
}

func TestGCSBackupSinkName(t *testing.T) {
	sink, _ := gcsSinkForTest(t)
	if got := sink.Name(); got != "gcs:test-bucket" {
		t.Errorf("Name() = %q, want %q", got, "gcs:test-bucket")
	}
}

// TestGCSBackupSinkCopyUploadsGzippedSnapshot covers the end-to-end path
// this app owns: Copy takes its own VACUUM INTO snapshot and uploads the
// gzipped result with the token source's Bearer token — proving the
// object name, the Authorization header, and the uploaded body's shape
// (valid gzip, unpacking to a real SQLite file) all land correctly.
func TestGCSBackupSinkCopyUploadsGzippedSnapshot(t *testing.T) {
	sink, fake := gcsSinkForTest(t)

	if err := sink.Copy(t.Context(), "ignored-src-path"); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if got := fake.requests.Load(); got != 1 {
		t.Errorf("upload requests = %d, want 1", got)
	}
	if got, _ := fake.lastAuth.Load().(string); got != "Bearer test-access-token" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer test-access-token")
	}
	rawURL, _ := fake.lastURL.Load().(string)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse upload URL %q: %v", rawURL, err)
	}
	if !strings.HasPrefix(parsed.Path, "/upload/storage/v1/b/test-bucket/o") {
		t.Errorf("upload path = %q, want it under /upload/storage/v1/b/test-bucket/o", parsed.Path)
	}
	name := parsed.Query().Get("name")
	if !strings.HasPrefix(name, "gridiron/league-") || !strings.HasSuffix(name, ".db.gz") {
		t.Errorf("object name = %q, want gridiron/league-<timestamp>.db.gz", name)
	}

	body, _ := fake.lastBody.Load().([]byte)
	reader, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("uploaded body is not valid gzip: %v", err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("decompress uploaded body: %v", err)
	}
	const sqliteMagic = "SQLite format 3\x00"
	if len(raw) < len(sqliteMagic) || string(raw[:len(sqliteMagic)]) != sqliteMagic {
		t.Error("uploaded body does not decompress to a SQLite file")
	}
}

// TestGCSBackupSinkCopyPropagatesUploadFailure covers the failure path:
// a non-200 upload response must surface as an error, not a silent
// success, so backupHealthState (backup_sink.go's recordSink) reports it
// truthfully.
func TestGCSBackupSinkCopyPropagatesUploadFailure(t *testing.T) {
	sink, fake := gcsSinkForTest(t)
	fake.responseCode = http.StatusForbidden

	if err := sink.Copy(t.Context(), ""); err == nil {
		t.Fatal("Copy: want an error on a 403 upload response, got nil")
	}
}

// TestGCSBackupSinkCopyPropagatesTokenFailure covers the other failure
// path: a token source that cannot produce a token (an expired,
// unrefreshable Workload Identity Federation exchange, in production)
// must fail Copy before ever reaching the network, not silently upload
// unauthenticated.
func TestGCSBackupSinkCopyPropagatesTokenFailure(t *testing.T) {
	sink, fake := gcsSinkForTest(t)
	sink.tokenSource = errorTokenSource{}

	if err := sink.Copy(t.Context(), ""); err == nil {
		t.Fatal("Copy: want an error when the token source fails, got nil")
	}
	if got := fake.requests.Load(); got != 0 {
		t.Errorf("upload requests = %d, want 0 (a token failure must never reach the network)", got)
	}
}

type errorTokenSource struct{}

func (errorTokenSource) Token() (*oauth2.Token, error) {
	return nil, context.DeadlineExceeded
}

func TestNewGCSBackupSinkRejectsEmptyBucket(t *testing.T) {
	if _, err := newGCSBackupSink(t.Context(), "", league.Default(), http.DefaultClient, time.Now); err == nil {
		t.Error("want an error for an empty bucket, got nil")
	}
}

// TestNewGCSBackupSinkRejectsUnresolvableCredentials covers the
// construction-time failure path when GOOGLE_APPLICATION_CREDENTIALS
// names a file that is not a valid Google credential (any shape,
// external_account or otherwise) — newGCSBackupSink must fail loudly
// here, at boot, rather than on the first scheduled tick.
func TestNewGCSBackupSinkRejectsUnresolvableCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"type":"not_a_real_credential_type"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)

	if _, err := newGCSBackupSink(t.Context(), "bucket", league.Default(), http.DefaultClient, time.Now); err == nil {
		t.Error("want an error for an unresolvable credential config, got nil")
	}
}

func TestNewGCSBackupSinkRejectsMissingCredentialFile(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "does-not-exist.json"))

	if _, err := newGCSBackupSink(t.Context(), "bucket", league.Default(), http.DefaultClient, time.Now); err == nil {
		t.Error("want an error when GOOGLE_APPLICATION_CREDENTIALS names a missing file, got nil")
	}
}

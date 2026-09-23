package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"gridiron-2000/internal/league"
)

// gcsUploadScope is the narrowest GCS OAuth2 scope Google publishes that
// still permits an object write; there is no write-only scope. The
// impersonated service account's own IAM binding — objectCreator on one
// bucket only, see docs/backup-restore.md's "Google Cloud Storage"
// section — is the real access boundary. This scope does not widen it.
const gcsUploadScope = "https://www.googleapis.com/auth/devstorage.read_write"

// gcsBackupSink uploads a fresh, minimal gzip-compressed database
// snapshot to Google Cloud Storage on every scheduled tick — owner
// decision 2026-09-23 (ops-drift hardening), authenticated by keyless
// Workload Identity Federation (WIF): org policy on project bookt-cc
// (constraints/iam.disableServiceAccountKeyCreation) blocks both a
// service-account JSON key and an HMAC key, so no long-lived credential
// of any kind lives in a Secret or a ConfigMap here.
//
// Auth is golang.org/x/oauth2/google's own external_account credential
// resolution (google.CredentialsFromJSON): it reads the mounted
// gridiron-backup-gcp-config ConfigMap (an external_account JSON — see
// docs/backup-restore.md — that carries no secret, only URLs and a file
// path), exchanges the pod's own projected Kubernetes ServiceAccount
// token for a federated GCP token via Google's STS endpoint, then
// impersonates the "gridiron-backup" uploader service account in project
// bookt-cc (the bucket's real objectCreator — see docs/backup-restore.md
// for its exact email) for the actual upload token. Re-
// implementing that STS-plus-impersonation exchange by hand in stdlib
// would be substantial and risky to get right unverified against a real
// provider; golang.org/x/oauth2 was already an indirect dependency of
// this module before this change (the same trust tier as the already-
// vendored golang.org/x/net and golang.org/x/crypto), so taking it on
// directly adds no new module to the dependency graph — a materially
// smaller addition than the full cloud.google.com/go/storage SDK. The
// actual object upload stays a plain stdlib net/http POST against the
// GCS JSON API's simple-upload endpoint; the library supplies only the
// Authorization: Bearer token.
type gcsBackupSink struct {
	bucket      string
	service     *league.Service
	httpClient  *http.Client
	now         func() time.Time
	tokenSource oauth2.TokenSource
	// uploadBaseURL defaults to Google's real endpoint
	// (newGCSBackupSink); tests point it at an httptest.Server instead of
	// reaching the network.
	uploadBaseURL string
}

// newGCSBackupSink resolves the external_account credential config once,
// at construction, so a malformed config or an unreachable identity
// provider fails loudly at boot (via the caller's own log line — see
// backupSinksFromEnv) rather than silently on every scheduled tick.
// Credential resolution is google.FindDefaultCredentials, the library's
// own standard discovery order — GOOGLE_APPLICATION_CREDENTIALS (set in
// the manifest to the mounted gridiron-backup-gcp-config ConfigMap path)
// is the only source this deployment ever populates, but this stays the
// library's normal entry point rather than a narrower one this app would
// otherwise have to keep in sync with it by hand.
func newGCSBackupSink(ctx context.Context, bucket string, service *league.Service, httpClient *http.Client, now func() time.Time) (*gcsBackupSink, error) {
	if bucket == "" {
		return nil, errors.New("GCS backup sink requires a bucket name")
	}
	creds, err := google.FindDefaultCredentials(ctx, gcsUploadScope)
	if err != nil {
		return nil, fmt.Errorf("resolve GCS workload identity credentials: %w", err)
	}
	if creds.TokenSource == nil {
		return nil, errors.New("GCS credential config resolved no token source")
	}
	return &gcsBackupSink{
		bucket:        bucket,
		service:       service,
		httpClient:    httpClient,
		now:           now,
		tokenSource:   creds.TokenSource,
		uploadBaseURL: "https://storage.googleapis.com",
	}, nil
}

func (g *gcsBackupSink) Name() string { return "gcs:" + g.bucket }

// Copy takes its own fresh VACUUM INTO snapshot and uploads it to
// gs://<bucket>/gridiron/league-<UTC timestamp>.db.gz — see the type doc
// comment for why it ignores srcPath.
func (g *gcsBackupSink) Copy(ctx context.Context, _ string) error {
	path, cleanup, err := g.service.WriteDatabaseSnapshotGZ(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}

	token, err := g.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("GCS auth: %w", err)
	}

	object := fmt.Sprintf("gridiron/league-%s.db.gz", g.now().UTC().Format("20060102T150405Z"))
	uploadURL := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?uploadType=media&name=%s",
		g.uploadBaseURL, url.PathEscape(g.bucket), url.QueryEscape(object))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, file)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	token.SetAuthHeader(req)
	req.Header.Set("Content-Type", "application/gzip")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GCS upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GCS upload: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

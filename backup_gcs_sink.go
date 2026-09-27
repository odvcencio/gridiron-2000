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

// gcsBackupSink uploads a compressed SQLite snapshot to an operator-selected
// Google Cloud Storage bucket. The mounted ADC configuration supplies workload
// identity credentials; project, bucket, and account identities remain private
// operator configuration. The OAuth library handles token exchange and the
// upload uses the GCS JSON API.
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

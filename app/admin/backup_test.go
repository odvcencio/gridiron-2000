package admin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
)

func TestBackupDownloadRouteMountedCommissionerOnly(t *testing.T) {
	if !strings.Contains(rootPackageSource(t), `app.Mount("GET /admin/backup.tar.gz", adminpage.BackupDownloadHandler(league.Default(), appVersion))`) {
		t.Fatal("admin backup download route is not mounted")
	}
}

func TestBackupDownloadHandlerMethodNotAllowed(t *testing.T) {
	handler := backupDownloadHandler(
		func(*http.Request) (int, bool) { return 0, true },
		func(time.Time) string { return "x.tar.gz" },
		func(context.Context, io.Writer, time.Time, string) (league.BackupManifest, error) {
			t.Fatal("write must not run for a rejected method")
			return league.BackupManifest{}, nil
		},
		"test",
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/backup.tar.gz", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST = %d allow=%q, want 405 GET", response.Code, response.Header().Get("Allow"))
	}
}

func TestBackupDownloadHandlerRejectsUnauthorizedBeforeWrite(t *testing.T) {
	written := false
	handler := backupDownloadHandler(
		func(*http.Request) (int, bool) { return http.StatusForbidden, false },
		func(time.Time) string { return "x.tar.gz" },
		func(context.Context, io.Writer, time.Time, string) (league.BackupManifest, error) {
			written = true
			return league.BackupManifest{}, nil
		},
		"test",
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/backup.tar.gz", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("GET (unauthorized) = %d, want 403", response.Code)
	}
	if written {
		t.Fatal("the archive must never be written for a rejected caller")
	}
}

func TestBackupDownloadHandlerServiceUnavailable(t *testing.T) {
	handler := backupDownloadHandler(
		func(*http.Request) (int, bool) { return http.StatusServiceUnavailable, false },
		func(time.Time) string { return "x.tar.gz" },
		func(context.Context, io.Writer, time.Time, string) (league.BackupManifest, error) {
			return league.BackupManifest{}, nil
		},
		"test",
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/backup.tar.gz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET (no service) = %d, want 503", response.Code)
	}
}

func TestBackupDownloadHandlerStreamsArchiveWithHeadersAndFilename(t *testing.T) {
	var gotAppVersion string
	handler := backupDownloadHandler(
		func(*http.Request) (int, bool) { return 0, true },
		func(now time.Time) string { return "gridiron-backup-btl-" + now.Format("20060102") + ".tar.gz" },
		func(_ context.Context, w io.Writer, now time.Time, appVersion string) (league.BackupManifest, error) {
			gotAppVersion = appVersion
			gz := gzip.NewWriter(w)
			tw := tar.NewWriter(gz)
			body := []byte("fake archive body")
			_ = tw.WriteHeader(&tar.Header{Name: "league.db", Mode: 0o600, Size: int64(len(body))})
			_, _ = tw.Write(body)
			_ = tw.Close()
			_ = gz.Close()
			return league.BackupManifest{AppVersion: appVersion}, nil
		},
		"test-app-version",
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/backup.tar.gz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/gzip" {
		t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", response.Header().Get("Cache-Control"))
	}
	disposition := response.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment; filename=\"gridiron-backup-btl-") || !strings.HasSuffix(disposition, ".tar.gz\"") {
		t.Errorf("Content-Disposition = %q", disposition)
	}
	if gotAppVersion != "test-app-version" {
		t.Errorf("appVersion passed through = %q, want test-app-version", gotAppVersion)
	}
	gz, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatalf("response body is not valid gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil || header.Name != "league.db" {
		t.Fatalf("response body's first tar entry = %+v, err = %v", header, err)
	}
}

func TestBackupDownloadHandlerLogsWriteFailureWithoutErrorStatus(t *testing.T) {
	// The 200 and headers are already flushed once streaming starts; a
	// write failure mid-archive cannot become an HTTP error response. The
	// handler must not panic and must leave the already-sent 200 alone.
	handler := backupDownloadHandler(
		func(*http.Request) (int, bool) { return 0, true },
		func(time.Time) string { return "x.tar.gz" },
		func(context.Context, io.Writer, time.Time, string) (league.BackupManifest, error) {
			return league.BackupManifest{}, errors.New("injected failure")
		},
		"test",
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/backup.tar.gz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200 (headers already sent before the write failure)", response.Code)
	}
}

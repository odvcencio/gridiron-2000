package admin

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"gridiron-2000/internal/league"
)

// backupDownloadTimeout bounds VACUUM INTO plus the archive write. A league
// database is small (see docs/backup-restore.md); a slow or stuck snapshot
// must still fail the request rather than hold the connection open
// indefinitely.
const backupDownloadTimeout = 2 * time.Minute

// backupDownloadAccess is BackupDownloadHandler's authorization boundary:
// the same commissioner gate adminAttentionAccess uses (demo mode signs
// everyone in as commissioner; otherwise a signed-in session and
// COMMISSIONER_EMAILS membership are both required).
func backupDownloadAccess(service *league.Service) func(*http.Request) (int, bool) {
	return func(request *http.Request) (int, bool) {
		if service == nil {
			return http.StatusServiceUnavailable, false
		}
		if !service.DemoMode() {
			if _, signedIn := service.CurrentUser(request); !signedIn {
				return http.StatusUnauthorized, false
			}
		}
		if !service.IsCommissioner(request) {
			return http.StatusForbidden, false
		}
		return 0, true
	}
}

type backupArchiveWriter func(ctx context.Context, w io.Writer, now time.Time, appVersion string) (league.BackupManifest, error)
type backupFileNamer func(now time.Time) string

// backupDownloadHandler is the dependency-injected core: access decides who
// may pass, filename names the download, and write streams the archive.
// Separating these from BackupDownloadHandler's *league.Service wiring
// keeps this handler's method/authz/header contract testable without a
// live store, the same seam adminAttentionFragmentHandler already uses.
func backupDownloadHandler(access func(*http.Request) (int, bool), filename backupFileNamer, write backupArchiveWriter, appVersion string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if access == nil || write == nil || filename == nil {
			http.Error(writer, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		if status, allowed := access(request); !allowed {
			http.Error(writer, http.StatusText(status), status)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), backupDownloadTimeout)
		defer cancel()
		now := time.Now()
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Set("Content-Type", "application/gzip")
		writer.Header().Set("Content-Disposition", `attachment; filename="`+filename(now)+`"`)
		writer.WriteHeader(http.StatusOK)
		if _, err := write(ctx, writer, now, appVersion); err != nil {
			// The 200 and headers are already sent — streaming, not
			// buffered — so a mid-write failure cannot become a clean HTTP
			// error status. Logging is the only honest signal left; a
			// truncated download is itself evidence to the operator that
			// something went wrong.
			log.Printf("admin backup download: %v", err)
		}
	})
}

// BackupDownloadHandler serves GET /admin/backup.tar.gz: a commissioner-only,
// read-only download of one consistent league backup archive (see
// league.Service.WriteBackupArchive's doc comment for exactly what it
// contains). It is immediate and reversible — downloading changes nothing —
// so, unlike every Danger Zone control, it needs no typed confirmation; the
// admin page still states what the archive contains and does not contain
// before the link (product-experience contract: consequence copy before
// action).
//
// appVersion is the Gridiron release recorded in the manifest (main's
// appVersion — "dev" outside a release build).
func BackupDownloadHandler(service *league.Service, appVersion string) http.Handler {
	return backupDownloadHandler(
		backupDownloadAccess(service),
		func(now time.Time) string {
			if service == nil {
				return "gridiron-backup.tar.gz"
			}
			return service.BackupArchiveFileName(now)
		},
		func(ctx context.Context, w io.Writer, now time.Time, appVersion string) (league.BackupManifest, error) {
			return service.WriteBackupArchive(ctx, w, now, appVersion)
		},
		appVersion,
	)
}

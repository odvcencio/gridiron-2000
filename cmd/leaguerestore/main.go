// Command leaguerestore restores a Gridiron league backup archive
// (produced by the /admin backup download or the nightly scheduled
// snapshot — see docs/backup-restore.md) into a data directory.
//
// This is deliberately the only supported way back in, and it is
// deliberately offline. There is no web-facing restore/upload endpoint:
// accepting an arbitrary uploaded SQLite database over HTTP and opening it
// as the live league database is an RCE-adjacent footgun (a crafted
// database, or a crafted path inside a crafted archive, run through the
// same code path a browser can reach). leaguerestore instead runs as a
// separate offline step, by an operator, against a stopped app:
//
//  1. Stop Gridiron (it must not be writing to the target data directory
//     while this command runs).
//  2. Run leaguerestore against the archive and the target data directory.
//  3. Start Gridiron again.
//
// leaguerestore verifies the archive's manifest hash and refuses to
// restore an archive whose logical or physical schema is newer than this
// binary supports — the same rollback doctrine docs/launch-checklist.md
// applies to a live rollout. It writes league.db and league.json into the
// target directory only when that directory is empty, or the caller passed
// --force.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gridiron-2000/internal/league"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("leaguerestore", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archivePath := flags.String("archive", "", "path to the backup archive (gridiron-backup-*.tar.gz)")
	targetDir := flags.String("target", "", "target data directory (created if missing)")
	force := flags.Bool("force", false, "restore into a non-empty target directory")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: leaguerestore --archive <path> --target <data dir> [--force]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Stop Gridiron before running this command. It never runs against a live app.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *archivePath == "" || *targetDir == "" {
		flags.Usage()
		return 2
	}

	archive, err := os.Open(*archivePath)
	if err != nil {
		fmt.Fprintf(stderr, "leaguerestore: open archive: %v\n", err)
		return 1
	}
	defer archive.Close()

	empty, err := targetDirIsEmpty(*targetDir)
	if err != nil {
		fmt.Fprintf(stderr, "leaguerestore: inspect target directory: %v\n", err)
		return 1
	}
	if !empty && !*force {
		fmt.Fprintf(stderr, "leaguerestore: target directory %q is not empty; pass --force to restore into it anyway\n", *targetDir)
		return 1
	}

	stageDir, err := os.MkdirTemp(filepath.Dir(filepath.Clean(*targetDir))+string(os.PathSeparator), ".leaguerestore-stage-*")
	if err != nil {
		// filepath.Dir(target) may not exist yet for a brand-new target; a
		// system temp fallback still lets the restore proceed, at the cost
		// of a possible cross-device move below.
		stageDir, err = os.MkdirTemp("", "leaguerestore-stage-*")
		if err != nil {
			fmt.Fprintf(stderr, "leaguerestore: create staging directory: %v\n", err)
			return 1
		}
	}
	defer os.RemoveAll(stageDir)

	manifest, err := league.ExtractBackupArchive(archive, stageDir)
	if err != nil {
		switch {
		case errors.Is(err, league.ErrBackupSchemaTooNew):
			fmt.Fprintf(stderr, "leaguerestore: refusing to restore: %v\n", err)
			fmt.Fprintln(stderr, "This binary is older than the archive. Use a leaguerestore built from the same or a later release.")
		case errors.Is(err, league.ErrBackupHashMismatch):
			fmt.Fprintf(stderr, "leaguerestore: refusing to restore: %v\n", err)
			fmt.Fprintln(stderr, "The archive is corrupt or was modified after it was written. Use a known-good copy.")
		default:
			fmt.Fprintf(stderr, "leaguerestore: read archive: %v\n", err)
		}
		return 1
	}

	if err := os.MkdirAll(*targetDir, 0o750); err != nil {
		fmt.Fprintf(stderr, "leaguerestore: create target directory: %v\n", err)
		return 1
	}
	if err := moveFile(filepath.Join(stageDir, league.BackupDBEntryName), filepath.Join(*targetDir, league.BackupDBEntryName)); err != nil {
		fmt.Fprintf(stderr, "leaguerestore: restore %s: %v\n", league.BackupDBEntryName, err)
		return 1
	}
	if manifest.LeagueConfigIncluded {
		if err := moveFile(filepath.Join(stageDir, league.BackupConfigEntryName), filepath.Join(*targetDir, league.BackupConfigEntryName)); err != nil {
			fmt.Fprintf(stderr, "leaguerestore: restore %s: %v\n", league.BackupConfigEntryName, err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "Restored %s into %s\n", league.BackupDBEntryName, *targetDir)
	if manifest.LeagueConfigIncluded {
		fmt.Fprintf(stdout, "Restored %s into %s\n", league.BackupConfigEntryName, *targetDir)
	} else {
		fmt.Fprintln(stdout, "The archive carried no league.json; the target directory's own copy (if any) was left untouched.")
	}
	fmt.Fprintf(stdout, "Archive: app %s, created %s, state schema %d, database schema %d\n",
		manifest.AppVersion, manifest.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		manifest.PersistedVersion, manifest.PersistedDatabaseVersion)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Next steps:")
	fmt.Fprintln(stdout, "  1. Confirm Gridiron was stopped before this restore (this tool does not check).")
	fmt.Fprintln(stdout, "  2. Restore is complete; nothing else to run against this data directory.")
	fmt.Fprintln(stdout, "  3. Start Gridiron again, pointed at this data directory.")
	return 0
}

// targetDirIsEmpty reports whether dir has no entries. A directory that
// does not exist yet counts as empty: leaguerestore creates it.
func targetDirIsEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// moveFile renames src to dst, falling back to a copy-then-remove when the
// rename fails (for example, src and dst are on different filesystems).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

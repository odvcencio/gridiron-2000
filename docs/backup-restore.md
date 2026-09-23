# Backup and restore

This page describes Gridiron's backup and restore tools, including the
optional built-in off-host copy (`BACKUP_OFFHOST_DIR`, below). It does not
promise cloud or multi-region storage on its own. Point the off-host copy
at real off-host storage, or copy an archive off this host yourself.

Use these sources of truth in this order:

1. `/admin` (League configuration) for the on-demand backup download.
2. `data/backups/` for the nightly local snapshots.
3. This page for the restore procedure and the archive format.

## What a backup archive contains

Every backup is one `.tar.gz` file. It holds exactly three entries:

- `league.db`: a consistent snapshot of the league database. Gridiron takes
  it with SQLite's `VACUUM INTO`. This is never a raw file copy of the live
  database. A raw copy of a write-ahead-log database can capture a torn,
  inconsistent set of pages. `VACUUM INTO` always produces one complete,
  consistent database as of one instant.
- `league.json`: the configuration file this instance actually loaded, when
  one was found. A neutral, unconfigured instance has no `league.json`. Its
  archive omits this entry.
- `manifest.json`: a small, machine-readable record. It carries the
  archive's creation time, the Gridiron release that wrote it, both schema
  markers, and the database snapshot's SHA-256 hash. The two schema markers
  are the logical state schema and the physical database schema.

An archive never contains secrets or environment variables. It never
contains a Signal Wire or Open Stats cache. Those caches refetch on their
own after a restore. They do not need to travel with league state.

## Download a backup on demand

1. Sign in as a commissioner and open `/admin`.
2. Open the League configuration section. Read what the archive contains
   and does not contain.
3. Select **Download league backup**.

The download needs no typed confirmation. It only reads league state. It
never changes anything. The file downloads as
`gridiron-backup-<league>-<date>.tar.gz`.

## Nightly local snapshots

Gridiron also saves one snapshot automatically to `data/backups/`, on this
schedule:

| Setting | Default | Meaning |
| --- | --- | --- |
| `BACKUP_ENABLED` | `true` | Set `false` to turn off scheduled snapshots. |
| `BACKUP_KEEP` | `7` | How many rotated snapshots `data/backups/` keeps. |

The loop runs once shortly after startup. It then runs every 24 hours. Each
run logs one line on success or failure. A failed run never stops the loop.
The next scheduled run tries again.

Taking a snapshot briefly shares the database's single connection with the
live app. This is the same sharing every other write already causes. It
never blocks the app for longer than the `VACUUM INTO` step itself takes.

## Off-host copies

A snapshot that only ever lives on the same volume as the live database is
not a real disaster-recovery copy: a lost PVC, a bad node, or a corrupted
volume takes the backups with it. After each successful local snapshot,
Gridiron can copy that same archive to a second, off-host location.

| Setting | Default | Meaning |
| --- | --- | --- |
| `BACKUP_OFFHOST_DIR` | empty | A second directory each snapshot is also copied into. Empty means no off-host copy runs. |
| `BACKUP_OFFHOST_KEEP` | `7` | How many rotated snapshots `BACKUP_OFFHOST_DIR` keeps, independent of `BACKUP_KEEP`. |

`BACKUP_OFFHOST_DIR` is any directory Gridiron's process can write to. It
is meant for a mounted network volume — for example:

- A **Hetzner Storage Box**, mounted into the container over SMB or SSHFS
  (an `sshfs` or CIFS volume mount in your Kubernetes manifest or Compose
  file), with `BACKUP_OFFHOST_DIR` pointed at the mount path.
- Any other NFS/SMB mount, or a second PVC backed by different physical
  storage than the primary data volume.

The copy is atomic (written to a temp file on the destination, then
renamed) and bounded by its own retention, so a slow or interrupted
network copy never leaves a half-written archive at the kept filename, and
the off-host directory never grows without bound. A copy failure is
logged (`scheduled backup: off-host sink ... failed: ...`) and reported at
`/api/health` under `backups.sinks`; it never blocks the app or the local
snapshot, and the next scheduled run tries again.

### S3-compatible storage (Backblaze B2, Cloudflare R2, ...)

Gridiron does not ship a built-in S3-compatible uploader: doing that
safely needs either a new SDK dependency or a hand-rolled SigV4 signer,
and most deployments do not need one yet. Point `BACKUP_OFFHOST_DIR` at a
local directory, then run [`rclone`](https://rclone.org/) as a sidecar
container to mirror that directory to your bucket:

```sh
# Example sidecar command, run on a schedule (cron or a Kubernetes
# CronJob) against the same volume BACKUP_OFFHOST_DIR names:
rclone sync /mnt/gridiron-offhost b2:your-bucket/gridiron-backups \
  --min-age 5m
```

`--min-age 5m` skips a file still mid-copy from the directory sink above.
`rclone` supports Backblaze B2, Cloudflare R2, and most other
S3-compatible providers with the same `rclone sync` command; only the
remote name in its config file changes.

## Restore a backup

Restoring is a separate, deliberately offline step. Gridiron offers no
web-facing restore or upload endpoint, and never will. Accepting an
uploaded database over HTTP, then opening it as the live league database,
is an RCE-adjacent risk. Use the `leaguerestore` command instead. Run it
only against a stopped app.

1. Stop Gridiron. `leaguerestore` must not run against a live app.
2. Run `leaguerestore`:

   ```sh
   go run ./cmd/leaguerestore \
     --archive gridiron-backup-league-20260831.tar.gz \
     --target data
   ```

3. Start Gridiron again. Point it at the same data directory.

`leaguerestore` writes `league.db` and `league.json` into `--target`. It
refuses a non-empty target directory unless you also pass `--force`. Pass
`--force` to restore over an existing `data/` directory on purpose.
`leaguerestore` leaves any other pre-existing file in `--target` alone.

Before it writes anything, `leaguerestore` checks two things:

- **The database hash.** It recomputes the extracted `league.db`'s SHA-256.
  It compares that hash with the manifest's recorded hash. A mismatch means
  the archive is corrupt, or was changed after it was written. The restore
  stops.
- **Schema compatibility.** It reads the manifest's logical and physical
  schema markers. It refuses an archive whose schema is newer than the
  running `leaguerestore` binary supports. This is the same rollback
  doctrine `docs/launch-checklist.md` applies to a live release: an older
  binary must never read state a newer one wrote. Build and run
  `leaguerestore` from the same release as the archive, or a later one.

`leaguerestore` prints the exact next steps on success. Confirm Gridiron
was stopped. The restore is complete. Start Gridiron again.

## Related pages

- [Season operations handbook](season-operations.md): when to take a
  backup during draft night and season operations.
- [Launch checklist](launch-checklist.md): the schema-aware rollback
  doctrine `leaguerestore` also follows.
- [Configuration reference](configuration.md): `league.json`'s fields. One
  field set travels inside every backup archive.

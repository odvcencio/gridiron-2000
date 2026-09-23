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
Gridiron can copy a fresh snapshot to a second, off-host location.

| Setting | Default | Meaning |
| --- | --- | --- |
| `BACKUP_OFFHOST_DIR` | empty | A second directory each rotated local snapshot is also copied into. Empty means this sink is off. |
| `BACKUP_OFFHOST_KEEP` | `7` | How many rotated snapshots `BACKUP_OFFHOST_DIR` keeps, independent of `BACKUP_KEEP`. |
| `BACKUP_GCS_BUCKET` | empty | The Google Cloud Storage bucket the GCS sink uploads to. Empty means this sink is off. |

`BACKUP_OFFHOST_DIR` and `BACKUP_GCS_BUCKET` are independent: either,
both, or neither may be configured (`backupSinksFromEnv`,
`backup_sink.go`). A copy failure on either sink is logged
(`scheduled backup: off-host sink ... failed: ...`) and reported at
`/api/health` under `backups.sinks`; it never blocks the app or the local
snapshot, and the next scheduled run tries again.

### Google Cloud Storage (the owner's chosen target)

Owner decision 2026-09-23 (ops-drift hardening): Google Cloud Storage,
project `bookt-cc`, bucket `gs://m31labs-gridiron-backups` (region
`us-central1`). The database is tiny (`league.db` was 508KB, the whole
data PVC 37MB, at the time of this decision), so the off-host copy is
deliberately minimal: one fresh, gzip-compressed `VACUUM INTO` snapshot
per nightly tick — `internal/league/backup.go`'s
`Service.WriteDatabaseSnapshotGZ`, wired in by `backup_gcs_sink.go`'s
`gcsBackupSink` — uploaded straight to
`gs://m31labs-gridiron-backups/gridiron/league-<UTC timestamp>.db.gz`
over the GCS JSON API. No sidecar container, and no
`cloud.google.com/go/storage` SDK: the upload itself is a plain
`net/http` POST; `golang.org/x/oauth2/google` (already an indirect
dependency of this module before this change — the same trust tier as
`golang.org/x/net`/`golang.org/x/crypto`, not the much heavier Cloud
Storage SDK) supplies only the bearer token.

**Auth is keyless Workload Identity Federation (WIF), not a key.** Org
policy on project `bookt-cc`
(`constraints/iam.disableServiceAccountKeyCreation`) blocks both a
service-account JSON key and an HMAC key, so no long-lived credential of
any kind lives in a Secret or a ConfigMap. The pod's own projected
Kubernetes ServiceAccount token (short-lived, audience-scoped,
auto-rotated by the kubelet — `deploy/k8s/deployment.yaml`'s `gcp-wif`
volume) is exchanged for a federated GCP token via Google's STS
endpoint, which then impersonates the `gridiron-backup` service account
in project `bookt-cc` (its exact email is `gridiron-backup` at
`bookt-cc.iam.gserviceaccount.com` — split here only so this document
never carries a literal email-shaped value; already provisioned with
`roles/storage.objectCreator` on this bucket only) for the actual upload
token.

Already provisioned (by the owner, before this WIF switch):

```bash
PROJECT=bookt-cc
BUCKET=m31labs-gridiron-backups
SA_EMAIL="gridiron-backup@${PROJECT}.iam.gserviceaccount.com"

# The bucket, us-central1, uniform bucket-level access.
gcloud storage buckets create "gs://${BUCKET}" \
  --project="${PROJECT}" --location=us-central1 \
  --uniform-bucket-level-access

# The service account the WIF principal impersonates.
gcloud iam service-accounts create gridiron-backup \
  --project="${PROJECT}" \
  --display-name="Gridiron 2000 off-host backup uploader"

# objectCreator on this bucket only — never project-wide.
gcloud storage buckets add-iam-policy-binding "gs://${BUCKET}" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/storage.objectCreator"

# 14-day retention, the sole retention mechanism (gcsBackupSink itself
# never deletes an object): a lifecycle rule on the bucket.
cat > /tmp/gridiron-backup-lifecycle.json <<'JSON'
{
  "rule": [
    {
      "action": {"type": "Delete"},
      "condition": {"age": 14, "matchesPrefix": ["gridiron/"]}
    }
  ]
}
JSON
gcloud storage buckets update "gs://${BUCKET}" \
  --lifecycle-file=/tmp/gridiron-backup-lifecycle.json
```

Remaining, coordinator-run after the owner re-authenticates `gcloud`
(the WIF pool/provider need Owner- or IAM-Admin-level project access):

```bash
PROJECT=bookt-cc
PROJECT_NUMBER="$(gcloud projects describe "${PROJECT}" --format='value(projectNumber)')"
POOL_ID=gridiron-backup-pool
PROVIDER_ID=k3s-gridiron
SA_EMAIL="gridiron-backup@${PROJECT}.iam.gserviceaccount.com"
# The k3s API server's own OIDC issuer URL (--service-account-issuer at
# apiserver startup) and its JWKS document (--service-account-jwks-uri,
# or fetched live from the issuer's own /openid/v1/jwks if it is
# reachable). Substitute this cluster's real values.
ISSUER_URI="https://<this-cluster's-service-account-issuer>"
JWKS_FILE=/path/to/uploaded-jwks.json

# The pool.
gcloud iam workload-identity-pools create "${POOL_ID}" \
  --project="${PROJECT}" --location=global \
  --display-name="Gridiron 2000 k3s backup pool"

# The OIDC provider, trusting this cluster's own service-account issuer.
# --attribute-mapping's google.subject must match the "sub" claim a k8s
# projected ServiceAccount token actually carries:
# system:serviceaccount:<namespace>:<name>.
gcloud iam workload-identity-pools providers create-oidc "${PROVIDER_ID}" \
  --project="${PROJECT}" --location=global \
  --workload-identity-pool="${POOL_ID}" \
  --issuer-uri="${ISSUER_URI}" \
  --jwk-json-path="${JWKS_FILE}" \
  --attribute-mapping="google.subject=assertion.sub" \
  --attribute-condition="assertion.sub=='system:serviceaccount:gridiron:gridiron-backup'"

# Let that one k8s ServiceAccount impersonate the uploader SA — never a
# direct bucket-IAM binding for the WIF principal itself.
gcloud iam service-accounts add-iam-policy-binding "${SA_EMAIL}" \
  --project="${PROJECT}" \
  --role="roles/iam.workloadIdentityUser" \
  --member="principal://iam.googleapis.com/projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/${POOL_ID}/subject/system:serviceaccount:gridiron:gridiron-backup"
```

The exact audience string both the WIF provider and
`deploy/k8s/deployment.yaml`'s `gcp-wif` volume must agree on (the
provider's own full resource name, two related forms):

- The projected token's own `audience` (what the K8s API server embeds
  as the token's `aud` claim, and what `deploy/k8s/deployment.yaml`
  already sets):
  `https://iam.googleapis.com/projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL_ID/providers/PROVIDER_ID`
- The `external_account` config's own `"audience"` field (Google's
  scheme-less GCP resource-name form, used internally by STS to route
  the exchange — already set in
  `deploy/k8s/gridiron-backup-gcp-config.yaml`):
  `//iam.googleapis.com/projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL_ID/providers/PROVIDER_ID`

Fill in the real `PROJECT_NUMBER`, `POOL_ID` (`gridiron-backup-pool`
above), and `PROVIDER_ID` (`k3s-gridiron` above) in both
`deploy/k8s/deployment.yaml`'s `gcp-wif` volume and
`deploy/k8s/gridiron-backup-gcp-config.yaml`'s `config.json`.
`gridiron-backup-gcp-config.yaml` also carries a fourth placeholder,
`IMPERSONATED_SA_EMAIL`, in its `service_account_impersonation_url` —
substitute `${SA_EMAIL}` from the script above (this document never
spells that address out as a literal email-shaped value — see its own
split form higher up this section). Fill in all four before applying
either manifest — the placeholders fail closed (Google rejects an
audience that does not resolve to a real provider, or an impersonation
target that is not a real service account) rather than succeeding
silently against the wrong pool or principal.

### S3-compatible storage (Backblaze B2, Cloudflare R2, ...)

Not the owner's chosen target, kept here for a future operator who picks
one instead. Gridiron does not ship a built-in S3-compatible uploader:
doing that safely needs either a new SDK dependency or a hand-rolled
SigV4 signer. Point `BACKUP_OFFHOST_DIR` at a local directory, then run
[`rclone`](https://rclone.org/) as a sidecar container (not present in
this deployment today) to mirror that directory to your bucket:

```sh
# Example sidecar command, run on a schedule (cron or a Kubernetes
# CronJob) against the same volume BACKUP_OFFHOST_DIR names. `copy`, not
# `sync`: BACKUP_OFFHOST_KEEP's local rotation is much shorter than a
# real cloud retention policy should be, and `sync` would delete a
# bucket object the moment its local copy rotated out.
rclone copy /mnt/gridiron-offhost b2:your-bucket/gridiron-backups \
  --min-age 5m
```

`--min-age 5m` skips a file still mid-copy from the directory sink above.
`rclone` supports Backblaze B2, Cloudflare R2, and most other
S3-compatible providers with the same `rclone copy` command; only the
remote name in its config file changes. Pair this with a bucket-side
lifecycle/retention rule, the same way the Google Cloud Storage section
above does — `rclone` itself should never be the retention mechanism.

### Other targets

| Option | Needs | Extra moving part? |
| --- | --- | --- |
| Google Cloud Storage | See above — already the owner's chosen, implemented target. | No — native upload, no sidecar. |
| Hetzner Storage Box (or any SMB/SSHFS mount) | An SMB/SSHFS-capable CSI driver or `hostPath`-style mount in the cluster, and the Box's own credentials (stored as a Secret, never committed). `BACKUP_OFFHOST_DIR` points at the mount path. | No. |
| A second PVC / NFS mount | A second StorageClass or NFS server backed by different physical storage than the primary `gridiron-2000-data` PVC — the whole point is surviving the loss of that volume. | No. |
| S3-compatible bucket (Backblaze B2, Cloudflare R2, ...) | A bucket, an access key/secret key pair (a `rclone.conf`, stored as a Secret, never committed), and an ordinary PVC for `BACKUP_OFFHOST_DIR` to write into before rclone copies it out. | Yes — an `rclone` sidecar, not present in this deployment today. |

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

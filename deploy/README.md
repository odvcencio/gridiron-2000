# Deploy — league.json and the ConfigMap

This directory (`deploy/`) is tracked in the public repository. This
project's own league identity — name, teams, divisions, draft date, and
invite copy — is not: it lives only in a gitignored file and a Kubernetes
`ConfigMap`, never in a committed manifest.

## Release image policy

Builds carry a release identifier, source SHA, and UTC build timestamp in
both OCI labels and `/api/health`. Publish a dated/SHA tag, then pin each
Deployment to the pushed digest before rollout. Do not deploy `:latest` or
rely on a mutable tag:

```bash
# Future release-pin step: update only the image field in both manifests to
# the newly pushed digest, preserving COMMISSIONER_INSTANCE_ID,
# COMMISSIONER_HQ_PEERS, and all Secret/ConfigMap references.
# Apply SK first, then flagship, only after both HQ Secret patches succeed:
kubectl apply -f deploy/k8s/sk/deployment.yaml
kubectl -n stablekernel rollout status deployment/gridiron-2000-sk --timeout=5m
kubectl apply -f deploy/k8s/deployment.yaml
kubectl -n gridiron rollout status deployment/gridiron-2000 --timeout=5m
```

This is a future release-pin step; this document does not claim a new build or
rollout and intentionally contains no current release digest. Before either
Deployment changes, record each old revision and exact image reference, then
follow [the launch checklist's SK-first canary gate](../docs/launch-checklist.md).
After the second (flagship) roll, compare the live `gitSHA` and `appVersion`
in both `/api/health` responses with the release record. The two instances
intentionally share a binary and relay but keep separate `LEAGUE_FILE`
ConfigMaps and state volumes.

The release health gate accepts `fantasyPoolMode` `live` or `cache`, but
only when `fantasyPoolError` is empty and
`fantasyPoolPlayers >= fantasyRosterCapacity`. A `cache` result is not an
error when those conditions hold.

## Existing-instance release controls

For a release that enables Commissioner HQ, generate one newly generated,
independent `COMMISSIONER_HQ_TOKEN` with at least 256 bits and install its
identical value in both existing application Secrets
(`gridiron-2000-secrets` in `gridiron` and
`gridiron-2000-sk-secrets` in `stablekernel`) before either Deployment
rolls. Use the no-display, one-patch-file workflow in
[the launch checklist](../docs/launch-checklist.md#102-install-the-new-commissioner-hq-token-before-either-roll).
Never print or read (including fetch, echo, or log) the token value, and never
reuse `DATA_API_TOKEN` or `SESSION_SECRET`.

Record old revisions and image digests before any manifest apply. Roll
Stable Kernel first and wait for its health, redirect, and read-only smoke
gates. Only then roll the flagship and smoke both instances. During the
first SK canary, the flagship peer card may be unavailable because it is
still on the old image; that is expected until the second roll. After the
flagship roll, both peer cards must be available.

Rollback uses the exact revisions captured before the change:

```bash
kubectl -n stablekernel rollout undo deployment/gridiron-2000-sk \
  --to-revision=<recorded-sk-before-revision>
kubectl -n gridiron rollout undo deployment/gridiron-2000 \
  --to-revision=<recorded-flagship-before-revision>
```

Run `rollout status` after each undo. Roll back on a rollout timeout,
unready pod, failed health predicate, insufficient player pool, broken
redirect, failed read-only smoke, or (after both rolls) an unavailable
Commissioner HQ peer card. Do not guess a digest in a rollback command or
bypass the manifest source of truth with `kubectl set image`.

## Why a ConfigMap, not a file in this repo

`deploy/k8s/deployment.yaml` mounts a `ConfigMap` named
`gridiron-2000-league-config` at `/etc/gridiron/league.json` and sets
`LEAGUE_FILE=/etc/gridiron/league.json`. The manifest references the
`ConfigMap` by name only — it never embeds the real values, because
`deploy/` is a tracked directory and anything committed here ships in the
public repo.

The real file lives at `deploy/local/league.json`, which is gitignored
(see `.gitignore`'s `deploy/local/` entry). It is never committed.

## Creating the ConfigMap

From the repository root, with `deploy/local/league.json` populated:

```bash
kubectl create configmap gridiron-2000-league-config \
  --from-file=league.json=deploy/local/league.json \
  --namespace gridiron \
  --dry-run=client -o yaml | kubectl apply -f -
```

The `--dry-run=client -o yaml | kubectl apply -f -` form makes the command
idempotent: re-run it after editing `deploy/local/league.json` to update
the live `ConfigMap`, then roll the deployment:

```bash
kubectl rollout restart deployment/gridiron-2000 --namespace gridiron
```

## Verifying

```bash
curl -s https://gridiron.draco.quest/api/health | jq '.leagueConfig'
# "file:/etc/gridiron/league.json"
```

`internal/league/config.go`'s `LoadConfig` validates the file at boot and
refuses to start on a bad config (`league config: ...` in the pod logs) —
a bad edit never silently falls back to the neutral built-in default.

## Starting from scratch

Copy `config/league.json.example` (the neutral, public template) to
`deploy/local/league.json` and edit it with this deployment's real values,
then run the `kubectl create configmap` command above.

## Shared Tank01 relay (statrelay)

Every Tank01 request this app makes is league-agnostic: the player list,
ADP, weekly and preseason games, and box scores are the same URLs whatever
league is asking. Two league deployments — the flagship `gridiron-2000`
instance and the live Stable Kernel instance — would otherwise each
hold their own RapidAPI key and pay for the same calls twice. `statrelay`
(`cmd/statrelay`, `deploy/k8s/statrelay.yaml`) is a small caching relay
that sits between every league instance and RapidAPI, so both instances
share one metered upstream quota.

### Topology

- One `statrelay` Deployment runs in the `gridiron` namespace, holding the
  real `TANK01_API_KEY` in its own `statrelay-secrets` Secret (see
  `deploy/k8s/statrelay-secret.example.yaml`). League manifests and secret
  examples do not provision that key. A legacy Secret may retain an unused
  copy until an explicit secret-maintenance operation removes it.
- Every league instance sets `TANK01_BASE_URL` to the relay's in-cluster
  Service address:

  ```
  TANK01_BASE_URL=http://statrelay.gridiron.svc.cluster.local
  ```

  `internal/fantasy`'s client (`tank01.go`) sends every Tank01 request to
  that base URL instead of building `https://<TANK01_HOST>/...` itself,
  and stops requiring `TANK01_API_KEY` locally — the relay injects the
  real key upstream (see `internal/fantasy/model.go`'s `ConfigFromEnv` and
  `Enabled`).
- When one operator signs in with multiple permitted Google accounts, set the
  canonical address in `COMMISSIONER_EMAILS` and map each alternate
  explicitly with `IDENTITY_ALIASES=alias=canonical`. The raw alias still
  must pass the league's independent domain/allowlist/invite gate. Internal
  member keys, co-manager bindings, boards, Pick'em, Blitz, notification
  preferences, OAuth sessions, and audit attribution then resolve to the
  canonical address.

  The paired Gridiron deployments use this one-way identity direction:

  ```dotenv
  COMMISSIONER_EMAILS=commissioner@example.com
  IDENTITY_ALIASES=commissioner.alias@example.org=commissioner@example.com
  ```

  `LEAGUE_ALLOWED_EMAILS`, stored invitations, and
  `membership.allowed_domain` still evaluate the raw Google provider email.
  List or invite each account that should pass admission; aliasing alone never
  admits an account.
- The identity startup migration is idempotent and fails closed on conflicting
  seats, roles, or user-owned values. It leaves raw invite entries unchanged
  because they are admission policy, not identity ownership. Apply the mapping
  to both paired league Secrets only after a preflight passes; review startup
  health/logs for migration errors before proceeding.
- Before changing an alias mapping, run the read-only doctor against an offline
  copy of the app's rolling SQLite backup (never the live SQLite file):

  ```bash
  IDENTITY_ALIASES='commissioner.alias@example.org=commissioner@example.com' \
    go run ./cmd/identitydoctor -sqlite-snapshot /secure/offline/league.db.bak
  ```

  A legacy JSON snapshot is also accepted with `-snapshot`. Pass exactly one
  snapshot flag. SQLite snapshots open with `mode=ro`, `immutable=1`, and
  `query_only`; copy the rolling backup away from the app before inspection.
  The command runs the exact startup reconciliation on a deep in-memory clone.
  Exit `0` means the projection is safe; exit `2` means it failed closed. Its
  JSON contains only aggregate before/after counts, `wouldChange`, and a bounded
  conflict category (`seat`, `role`, `co_manager`, `board`, `pickem`, `blitz`,
  `notification`, `identity_state`, or `snapshot_schema`). A snapshot schema
  newer than this binary supports fails closed. The doctor prints no emails,
  team IDs, player IDs, preference names, secret values, or raw migration
  errors, and it has no persistence path. Keep the snapshot private and remove
  it using the same operator procedure that created it.
- `statrelay` caches each upstream response by request path and query, so
  two league instances requesting the identical Tank01 URL within its TTL
  window collapse to one upstream call. Concurrent identical requests
  collapse further, into one in-flight upstream call (singleflight). See
  `cmd/statrelay/relay.go`'s `ttlTable` for the per-endpoint cache
  lifetimes, each derived from and documented against this app's own
  refresh cadences.
- The cache also persists to the `statrelay-data` PVC, so it survives a
  pod restart without a burst of refetches.

### Deploying statrelay

The relay is an existing shared dependency, not part of this application
release. The checked-in `deploy/k8s/statrelay.yaml` still uses
`harbor.draco.quest/orchard/gridiron-2000-statrelay:latest`, and its image
provenance/digest pinning is future cleanup. Do not rebuild, retag, repin, or
roll `statrelay` while applying the app release; do not turn that future
cleanup into a prerequisite for the SK-first canary.

```bash
kubectl apply -f deploy/k8s/statrelay.yaml
# Copy deploy/k8s/statrelay-secret.example.yaml, fill in the real key,
# and apply the copy (never the example file itself) before the pod can
# reach RapidAPI:
kubectl apply -f deploy/local/statrelay-secret.yaml
```

The apply commands above are first-install/bootstrap only. A future,
separate relay cleanup must build from a recorded source commit, publish an
immutable tag and digest with provenance, update the relay Deployment, and
roll it under its own acceptance and rollback plan. This app release records
the relay dependency and leaves that cleanup untouched.

After this app release is accepted, removal of the stale flagship
`TANK01_API_KEY` is an explicit, separate secret-maintenance operation. Do
not combine it with the rollout or remove the key from `statrelay-secrets`:
only that relay Secret owns the real upstream key. When the separate window is
approved, remove the old flagship key with the cluster's secret-management
workflow (for a Kubernetes JSON Secret, the targeted operation is):

```bash
kubectl -n gridiron patch secret/gridiron-2000-secrets --type=json \
  --patch='[{"op":"remove","path":"/data/TANK01_API_KEY"}]'
```

Never print or fetch any Secret value while checking or performing this
maintenance.

### Local development (no relay)

`TANK01_BASE_URL` is unset by default. With no relay running, a local
checkout keeps working exactly as before: `internal/fantasy`'s client
builds `https://<TANK01_HOST>/...` directly and requires its own
`TANK01_API_KEY`, or falls back to the offline pool with no key at all.
Nothing about direct mode changed — see `tank01_test.go`'s
`TestTank01ClientDirectModeIsByteIdentical`.

## Federated Commissioner HQ

`/commissioner` composes a read-only fleet view while each league keeps its
own database, sessions, OAuth callback, CSRF boundary, and commissioner
actions. Configure an instance ID and explicit peer pairs with
`COMMISSIONER_INSTANCE_ID` and `COMMISSIONER_HQ_PEERS`. Each peer entry uses
`id=service-origin|public-origin`; the service origin is reachable only by
the server-side federation reader, while the public origin is the exact
browser-facing URL shown on an unavailable card and used for cross-league
links. Each peer serves only the typed, PII-free
`/api/commissioner/v2/summary` contract, and its response must identify the
peer and return the configured normalized public origin.

Generate one newly generated, independent `COMMISSIONER_HQ_TOKEN` of at least
256 bits and place the identical value in both participating existing
application Secrets *before either Deployment rolls*. Do not reuse
`DATA_API_TOKEN`, `SESSION_SECRET`, or a value copied from another Secret.
Never print, echo, log, or fetch the token value; use the shared opaque patch
file described in the launch checklist. The federation token cannot mutate
league state, and the HQ never forwards browser cookies or Google identities.
Cross-league buttons are ordinary links to the owning host's existing
`/admin` and `/draft` surfaces, where that host repeats its own session,
commissioner, and CSRF checks.

Peer URLs are deployment wiring, not league rules, so they deliberately stay
out of `league.json`. Startup rejects missing tokens, duplicate/self IDs,
missing service/public pairs, credentials, paths, queries, fragments, unsafe
origins, and more than eight peers. Peer reads are concurrent,
redirect-disabled, size-bounded, and time-bounded; an unavailable league
becomes one truthful degraded card rather than failing the whole HQ. During
the SK-first canary, the flagship card may be unavailable until the second
roll; after the flagship rolls, both peer cards are required for acceptance.

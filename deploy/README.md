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
kubectl -n gridiron set image deployment/gridiron-2000 \
  gridiron-2000=harbor.draco.quest/orchard/gridiron-2000@sha256:<digest>
kubectl -n stablekernel set image deployment/gridiron-2000-sk \
  gridiron-2000=harbor.draco.quest/orchard/gridiron-2000@sha256:<digest>
```

After rollout, compare the live `gitSHA` and `appVersion` in `/api/health`
with the release record. The two instances intentionally share a binary and
relay but keep separate `LEAGUE_FILE` ConfigMaps and state volumes.

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

```bash
kubectl apply -f deploy/k8s/statrelay.yaml
# Copy deploy/k8s/statrelay-secret.example.yaml, fill in the real key,
# and apply the copy (never the example file itself) before the pod can
# reach RapidAPI:
kubectl apply -f deploy/local/statrelay-secret.yaml
```

Build and push its image from the repository root with
`deploy/statrelay.Dockerfile` (a separate, smaller image from the main
app's `Dockerfile` — statrelay is a dependency-free stdlib binary with no
GoSX asset build step):

```bash
docker build -f deploy/statrelay.Dockerfile -t harbor.draco.quest/orchard/gridiron-2000-statrelay:latest .
docker push harbor.draco.quest/orchard/gridiron-2000-statrelay:latest
```

Then point each league Deployment at the relay with `TANK01_BASE_URL` and
roll it. After verification, remove any obsolete `TANK01_API_KEY` from a
league Secret during an explicit secret-maintenance operation; only
`statrelay-secrets` needs it.

### Local development (no relay)

`TANK01_BASE_URL` is unset by default. With no relay running, a local
checkout keeps working exactly as before: `internal/fantasy`'s client
builds `https://<TANK01_HOST>/...` directly and requires its own
`TANK01_API_KEY`, or falls back to the offline pool with no key at all.
Nothing about direct mode changed — see `tank01_test.go`'s
`TestTank01ClientDirectModeIsByteIdentical`.

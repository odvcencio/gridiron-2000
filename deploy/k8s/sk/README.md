# Stable Kernel (SK) instance — deploy/k8s/sk

A second deployment of the same digest-pinned Gridiron image, its own
namespace, its own config, its own SQLite state — the
platform's isolation thesis: one codebase, two leagues, neither affects the
other. See `deploy/README.md` for the shared conventions this set follows
(ConfigMap-not-committed pattern, the statrelay topology).

## What is here

| File | Mirrors | Differs |
|---|---|---|
| `namespace.yaml` | — (new) | namespace `stablekernel`, not `gridiron` |
| `pvc.yaml` | `../pvc.yaml` | name, namespace |
| `deployment.yaml` | `../deployment.yaml` | namespace, resource names, ConfigMap/Secret refs, env (see the file's own comments — no redundant `DRAFT_AT`/`DRAFT_TZ`/`SEASON_START_AT`/`SCORING_FORMAT`, `TANK01_BASE_URL` set) |
| `service.yaml` | `../service.yaml` | name, namespace |
| `ingress.yaml` | `../ingress.yaml` | host, namespace, resource names |
| `http-redirect.yaml` | `../http-redirect.yaml` | host, namespace, middleware ref |
| `security-headers.yaml` | `../security-headers.yaml` | namespace, labels |
| `secret.example.yaml` | `../secret.example.yaml` | namespace, no `TANK01_API_KEY` (points at the shared relay instead) |

## Live hostname

`sk.gridiron.draco.quest` is live with public DNS and its own certificate.
The tracked `http-redirect.yaml` is also applied: plain HTTP is resolved to
HTTPS in the live Stable Kernel namespace. Verify that redirect during every
release canary; it is not an outstanding DNS or manifest task.
If the hostname changes, update `ingress.yaml`,
`http-redirect.yaml`, `secret.example.yaml`'s `GOOGLE_REDIRECT_URL`, and
`league-sk.json`'s `league.url` together.

## Deploy order

```bash
kubectl apply -f deploy/k8s/sk/namespace.yaml

# regcred does not cross namespace boundaries — create SK's own copy
# (docs/launch-checklist.md's "Create the pull secret" step, same command,
# -n stablekernel):
kubectl create secret docker-registry regcred \
  --docker-server=harbor.draco.quest \
  --docker-username=<user> --docker-password=<password> \
  --namespace stablekernel

kubectl apply -f deploy/k8s/sk/pvc.yaml

# league-sk.json is gitignored (deploy/local/) — copy config/league.json.example,
# edit with SK's real values (see the worked example this branch ships at
# deploy/local/league-sk.json in a local checkout), then:
kubectl create configmap gridiron-2000-sk-league-config \
  --from-file=league.json=deploy/local/league-sk.json \
  --namespace stablekernel \
  --dry-run=client -o yaml | kubectl apply -f -

cp deploy/k8s/sk/secret.example.yaml /tmp/gridiron-2000-sk-secret.yaml
# edit /tmp/gridiron-2000-sk-secret.yaml with real values, then:
kubectl apply -f /tmp/gridiron-2000-sk-secret.yaml
rm /tmp/gridiron-2000-sk-secret.yaml

kubectl apply -f deploy/k8s/sk/deployment.yaml
kubectl apply -f deploy/k8s/sk/service.yaml
kubectl apply -f deploy/k8s/sk/security-headers.yaml
kubectl apply -f deploy/k8s/sk/ingress.yaml
kubectl apply -f deploy/k8s/sk/http-redirect.yaml
```

The commands above are first-install/bootstrap order. For an existing
application release, use the strict SK-first canary order in
[`docs/launch-checklist.md`](../../../docs/launch-checklist.md): record both
old Deployment revisions and image digests, install one newly generated
independent 256-bit `COMMISSIONER_HQ_TOKEN` in both existing application
Secrets before either roll using the launch checklist's no-display patch
workflow; never print or read (including fetch, echo, or log) the token value.
Only after both Secret patches succeed, apply the future digest-pinned
`deploy/k8s/sk/deployment.yaml` and smoke SK; then apply
`deploy/k8s/deployment.yaml` and smoke both instances. These manifests remain
the source of truth for `COMMISSIONER_INSTANCE_ID` and
`COMMISSIONER_HQ_PEERS`; do not use `kubectl set image`. Never roll the two
Deployments concurrently.

During the first SK roll, the flagship peer card may be unavailable until the
second (flagship) roll because the flagship is still on its old image. That is
expected canary state. After the second roll, both Commissioner HQ peer cards
must be available. A passing health response accepts `fantasyPoolMode`
`live` or `cache` when `fantasyPoolError` is empty and
`fantasyPoolPlayers >= fantasyRosterCapacity`.

## statrelay dependency

SK's `deployment.yaml` sets `TANK01_BASE_URL=http://statrelay.gridiron.svc.cluster.local`,
the shared relay's fully-qualified in-cluster DNS name. This form —
`<service>.<namespace>.svc.cluster.local` — resolves from any namespace in
the cluster, including `stablekernel`, so no extra DNS/networking setup is
needed here; `statrelay` itself must already be running in the `gridiron`
namespace (`deploy/k8s/statrelay.yaml`, `deploy/README.md`'s "Shared Tank01
relay" section). SK's own secret carries no `TANK01_API_KEY` — only
`statrelay-secrets` (in `gridiron`) needs the real RapidAPI key.

## Changing the draft date after launch

`deployment.yaml` deliberately omits the redundant `DRAFT_AT`/`DRAFT_TZ`/
`SEASON_START_AT`/`SCORING_FORMAT` env vars the flagship's manifest still
carries (see that file's comment for why: env wins over the config file, so
leaving them set would mean a ConfigMap-only edit silently does nothing).
For SK, moving the draft date is exactly:

```bash
# edit deploy/local/league-sk.json's draft.at, then:
kubectl create configmap gridiron-2000-sk-league-config \
  --from-file=league.json=deploy/local/league-sk.json \
  --namespace stablekernel \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart deployment/gridiron-2000-sk --namespace stablekernel
```

Claimed seats, members, team names, and badges survive the restart. The
scheduled time remains informational: the draft stays closed until a
commissioner explicitly starts it.

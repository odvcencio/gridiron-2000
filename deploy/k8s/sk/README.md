# Stable Kernel (SK) instance — deploy/k8s/sk

A second deployment of the same `harbor.draco.quest/orchard/gridiron-2000:latest`
image, its own namespace, its own config, its own SQLite state — the
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
| `secret.example.yaml` | `../secret.example.yaml` | namespace, no `TANK01_API_KEY` (points at the shared relay instead) |

## DNS requirement — do not assume this exists

`sk.gridiron.draco.quest` needs a public DNS **A or CNAME record** pointed
at the cluster's ingress, the same way `gridiron.draco.quest` already does
for the flagship (`docs/launch-checklist.md`). This has NOT been created as
part of this work. The host itself is a placeholder pending the owner's
final choice of subdomain — if it changes, update `ingress.yaml`,
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
kubectl apply -f deploy/k8s/sk/ingress.yaml
kubectl apply -f deploy/k8s/sk/http-redirect.yaml
```

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

See the top-level task report for the verified restart-safety evidence
(claimed seats/members/team names/badges survive; the pick-clock's self-arm
gate reads the new date cleanly either direction, forward or backward, as
long as the draft has not already started).

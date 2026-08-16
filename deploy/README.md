# Deploy — league.json and the ConfigMap

This directory (`deploy/`) is tracked in the public repository. This
project's own league identity — name, teams, divisions, draft date, and
invite copy — is not: it lives only in a gitignored file and a Kubernetes
`ConfigMap`, never in a committed manifest.

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

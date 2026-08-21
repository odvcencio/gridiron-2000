# Launch checklist

This runbook takes GRIDIRON 2000 from a built image to a live production
deployment at `gridiron.draco.quest`. Follow the steps in order. Each step
lists the exact command to run.

## Deployment status (2026-08-21)

Both league instances are live with valid certificates and working Google
OAuth:

- `https://gridiron.draco.quest` — GRIDIRON 2000
- `https://sk.gridiron.draco.quest` — STABLE KERNEL LEAGUE

Both load their league-specific ConfigMap and report a live player pool.
Stable Kernel already uses the shared `statrelay`; this release adds the same
relay URL to the flagship Deployment. The flagship's existing application
Secret still has an obsolete `TANK01_API_KEY` key from the direct-client
topology. It is ignored after the relay rollout and should be removed during
the next explicit secret-maintenance window; no secret value is required or
changed by this release.

The tracked Stable Kernel HTTP redirect was absent from the live namespace at
the last audit. Step 5 applies it so both plain-HTTP hostnames redirect to
HTTPS. Run the complete step 10 smoke test after every release.

## Before you start

Confirm you have:

- Docker, `kubectl`, and `openssl` on your machine.
- Push access to the `harbor.draco.quest/orchard` registry.
- `kubectl` context pointed at the target cluster.
- Access to the Google Cloud Console project for this app's OAuth client.
- A RapidAPI account for the Tank01 NFL API.

## 1. Build and push the image

Every release gets a human-readable date plus the source commit's short SHA.
The deployment is then pinned to the image digest; the tag is only a lookup
handle and `latest` is never used by the manifests.

```bash
RELEASE="release-$(date -u +%Y.%m.%d)-$(git rev-parse --short=7 HEAD)"
GIT_SHA="$(git rev-parse HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
IMAGE="harbor.draco.quest/orchard/gridiron-2000:${RELEASE}"

docker build \
  --build-arg APP_VERSION="${RELEASE}" \
  --build-arg GIT_SHA="${GIT_SHA}" \
  --build-arg BUILD_DATE="${BUILD_DATE}" \
  -t "${IMAGE}" .
docker login harbor.draco.quest
docker push "${IMAGE}"
```

Record the pushed digest before changing either Deployment:

```bash
docker image inspect --format='{{index .RepoDigests 0}}' "${IMAGE}"
```

The `/api/health` response exposes `appVersion`, `gitSHA`, `buildDate`, and
`frameworkVersion` separately. This makes an old image or a source/image
branch mismatch visible without reading registry internals.

## 2. Create the namespace

```
kubectl apply -f deploy/k8s/namespace.yaml
```

## 3. Create the registry pull secret

Every namespace on this cluster that pulls from Harbor holds its own copy
of a `regcred` secret. Create one in the `gridiron` namespace. Replace the
username and password with your own Harbor robot account credentials.

```
kubectl create secret docker-registry regcred \
  --namespace gridiron \
  --docker-server=harbor.draco.quest \
  --docker-username=<harbor-robot-account> \
  --docker-password=<harbor-robot-account-token>
```

## 4. Create the application secret

Generate a session secret.

```
openssl rand -base64 48
```

Copy `deploy/k8s/secret.example.yaml` to a local, untracked file and fill
in every value: the session secret you just generated, the Google OAuth
client ID and secret (see step 7), the league's allowed manager emails
(see step 8), and a random data-API export token. Do not put a Tank01 key
in a league Secret: both instances use the shared `statrelay` Service, and
only `statrelay-secrets` owns the upstream key.

```
cp deploy/k8s/secret.example.yaml /tmp/gridiron-2000-secret.yaml
```

Edit `/tmp/gridiron-2000-secret.yaml`, then apply it and delete the local
copy.

```
kubectl apply -f /tmp/gridiron-2000-secret.yaml
rm /tmp/gridiron-2000-secret.yaml
```

Never commit the filled-in secret file.

## 5. Apply the remaining manifests

```
kubectl apply -f deploy/k8s/pvc.yaml
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml
kubectl apply -f deploy/k8s/security-headers.yaml
kubectl apply -f deploy/k8s/ingress.yaml
kubectl apply -f deploy/k8s/http-redirect.yaml
```

For Stable Kernel, apply the matching files under `deploy/k8s/sk/`, including
`sk/http-redirect.yaml` and `sk/security-headers.yaml`. The HTTPS ingress
references the namespaced middleware, so apply that middleware before or
with the ingress rollout.

Confirm the pod reaches the `Ready` state.

```
kubectl get pods -n gridiron -w
```

## 6. Create the DNS record

Add a DNS `A` or `CNAME` record for `gridiron.draco.quest` that points to
the cluster's public ingress address. Check the current address with:

```
kubectl get ingress -n gridiron gridiron-2000
```

Wait for the record to propagate, then confirm the TLS certificate issues.

```
kubectl get certificate -n gridiron
```

## 7. Register the Google OAuth redirect URI

1. Open the Google Cloud Console for this app's OAuth client.
2. Add this exact production redirect URI to the client's authorized
   redirect URIs list:
   ```
   https://gridiron.draco.quest/auth/google/callback
   ```
3. Save the client. Do not remove the existing local development URI.

## 8. Set the league's allowed managers

Two paths work; use either or both:

1. **Runtime invites (no restart).** Sign in as a commissioner
   (`COMMISSIONER_EMAILS`), open `/admin`, and add each manager's email in
   the Invites panel. Invites persist in league state.
2. **Environment allowlist.** Edit `LEAGUE_ALLOWED_EMAILS` in the applied
   secret (comma-separated, no spaces), then restart:
   ```
   kubectl apply -f /tmp/gridiron-2000-secret.yaml
   kubectl rollout restart deployment/gridiron-2000 -n gridiron
   ```

The commissioner console also releases seats and resets the draft or the
whole league (type RESET to confirm).

## 9. Configure the shared Tank01 relay

1. Create or sign in to a RapidAPI account.
2. Subscribe to "Tank01 NFL Live In-Game Real Time Statistics" on the free
   tier (1,000 requests per month, no card required).
3. Copy the API key RapidAPI issues you.
4. Store the key only in `statrelay-secrets` (see
   `deploy/k8s/statrelay-secret.example.yaml`), then restart the relay.
   Do not add it to either league's Secret.
5. Confirm both Deployments set
   `TANK01_BASE_URL=http://statrelay.gridiron.svc.cluster.local` and that
   `/api/health` reports `"fantasyPoolMode":"live"` within a
   minute. The app spends five requests per sync and syncs every six
   hours, so the free tier covers normal operation.

## 10. Pre-draft smoke test

Run every check below before the league relies on the deployment. Do not
skip the demo-mode pick rehearsal.

1. **Health endpoint.** Confirm the API reports a healthy, ready state.
   ```
   curl -s https://gridiron.draco.quest/api/health
   ```
   Check the response for `"ok":true`.
2. **Login.** Open `https://gridiron.draco.quest/login` in a browser. Sign
   in with a Google account from the allowed manager list. Confirm the app
   redirects you to the home page signed in.
3. **Draft room loads.** Open `https://gridiron.draco.quest/draft`. Confirm
   the page lists the draft order, the player pool, and the pick tape.
4. **Explicit draft start rehearsal.** Set `DEMO_MODE=true` in a staging
   copy of the secret, or run the image locally with `DEMO_MODE=true`.
   Confirm the room remains closed before and after the scheduled window
   until a commissioner types `START`. Start it intentionally, submit one
   pick, and confirm the pick tape and on-the-clock team advance.

## Notes on client assets

The image builder runs `gosx build --dev .` with the CLI version pinned to
the go.mod module version. The image therefore serves the hashed runtime
and island assets, and the draft-room island hydrates in production. Every
user flow also works without JavaScript through plain HTML forms. There is
no bespoke application JavaScript file: the big-board and blitz pool
searches, the live matchup scores, the signal wire feed, and the presence
heartbeat all run on gosx's own declarative runtime primitives
(`data-gosx-filter`, `data-gosx-live-*`, `data-gosx-region*`, and
`data-gosx-heartbeat`).

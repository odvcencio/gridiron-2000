# Launch checklist

This runbook takes GRIDIRON 2000 from a built image to a live production
deployment at `gridiron.draco.quest`. Follow the steps in order. Each step
lists the exact command to run.

## Deployment status (2026-08-16)

Steps 1 through 6 are complete. The app is live at
`https://gridiron.draco.quest` with a valid certificate. The wildcard
`*.draco.quest` DNS record already covered the hostname. The applied
secret holds a generated `SESSION_SECRET` and `DATA_API_TOKEN`; the
Google OAuth values, the manager allowlist, and the Tank01 key are still
empty, so the app runs in OAuth setup mode. To finish:

1. Complete step 7 (Google OAuth redirect URI).
2. Patch the secret with the real values, then restart:
   ```
   kubectl patch secret gridiron-2000-secrets -n gridiron --type merge -p \
     '{"stringData":{"GOOGLE_CLIENT_ID":"...","GOOGLE_CLIENT_SECRET":"...","LEAGUE_ALLOWED_EMAILS":"a@x.com,b@x.com","TANK01_API_KEY":"..."}}'
   kubectl rollout restart deployment/gridiron-2000 -n gridiron
   ```
3. Run the step 10 smoke test.

## Before you start

Confirm you have:

- Docker, `kubectl`, and `openssl` on your machine.
- Push access to the `harbor.draco.quest/orchard` registry.
- `kubectl` context pointed at the target cluster.
- Access to the Google Cloud Console project for this app's OAuth client.
- A RapidAPI account for the Tank01 NFL API.

## 1. Build and push the image

Build the image from the repository root.

```
docker build -t harbor.draco.quest/orchard/gridiron-2000:launch-2026-08-16 .
```

Tag the image `latest` as well, so the default deployment manifest resolves.

```
docker tag harbor.draco.quest/orchard/gridiron-2000:launch-2026-08-16 \
  harbor.draco.quest/orchard/gridiron-2000:latest
```

Log in to Harbor, then push both tags. Pushing is a manual operator step;
this checklist does not automate it.

```
docker login harbor.draco.quest
docker push harbor.draco.quest/orchard/gridiron-2000:launch-2026-08-16
docker push harbor.draco.quest/orchard/gridiron-2000:latest
```

Use a dated or commit-sha tag, not only `latest`, for every future release.
A fixed tag lets you roll back with one `kubectl set image` command.

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
(see step 8), a random data-API export token, and the Tank01 API key (see
step 9).

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
kubectl apply -f deploy/k8s/ingress.yaml
```

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

1. Collect the Google account email for each of the eight league managers.
2. Edit the `LEAGUE_ALLOWED_EMAILS` value in your applied secret. Use a
   comma-separated list with no spaces.
3. Apply the updated secret, then restart the deployment so the new pod
   picks up the change.
   ```
   kubectl apply -f /tmp/gridiron-2000-secret.yaml
   kubectl rollout restart deployment/gridiron-2000 -n gridiron
   ```

## 9. Subscribe to the Tank01 NFL API

1. Create or sign in to a RapidAPI account.
2. Subscribe to "Tank01 NFL Live In-Game Real Time Statistics" on the free
   tier (1,000 requests per month, no card required).
3. Copy the API key RapidAPI issues you.
4. Set `TANK01_API_KEY` in your applied secret to that key, then apply the
   secret and restart the deployment as in step 8.
5. Confirm `/api/health` reports `"fantasyPoolMode":"live"` within a
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
4. **Rehearsal pick in demo mode.** Set `DEMO_MODE=true` in a staging copy
   of the secret, or run the image locally with `DEMO_MODE=true`, before
   the real draft date. Submit one draft pick. Confirm the pick appears in
   the pick tape and the on-the-clock team advances.

## Notes on client assets

The image builder runs `gosx build --dev .` with the CLI version pinned to
the go.mod module version. The image therefore serves the hashed runtime
and island assets, and the draft-room island hydrates in production. Every
user flow also works without JavaScript through plain HTML forms. The
position filters and the pool search additionally have a plain JavaScript
fallback in `public/gridiron.js`.

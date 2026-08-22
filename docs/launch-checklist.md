# Release and first-install runbook

This runbook separates first-install/bootstrap work from a routine immutable
release. Hostnames and resource names below describe this repository's two
reference deployments; operators of another installation must substitute
their own values. It intentionally records no current image, revision,
certificate, Secret, or rollout status—capture those facts at execution time.

This file governs image release, canary, acceptance, and rollback. Use the
[season operations handbook](season-operations.md) for league configuration,
manager onboarding, draft night, weekly locks, waivers, trades, week close,
degraded-data decisions, and Commissioner HQ operating boundaries.

For an existing installation, skip steps 2–9. Build and pin the image in step
1, record rollback state in 10.1, provision or intentionally rotate the shared
Commissioner HQ token only when required by 10.2, then canary Stable Kernel
before the flagship as described in 10.3–10.4.

## Before you start

Confirm you have:

- Docker, `kubectl`, `curl`, `jq`, and `openssl` on your machine.
- Push access to the `harbor.draco.quest/orchard` registry.
- `kubectl` context pointed at the target cluster.
- Access to the Google Cloud Console project for this app's OAuth client.
- A RapidAPI account for the Tank01 NFL API.

## 1. Build and push an immutable image

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
branch mismatch visible without reading registry internals. Do not copy a
digest from a manifest into this runbook as a new release digest.

## First-install boundary for steps 2–9

Steps 2–9 are first-install/bootstrap-only instructions, including step 5's
manifest applies. They are not an existing-release rollout path. For the two
already-provisioned Deployments, skip every step from 2 through 9; do not
recreate namespaces, pull Secrets, application Secrets, ConfigMaps, DNS, OAuth
settings, manager settings, relay settings, or apply step 5's manifests as a
shortcut. After any explicitly required HQ provisioning or rotation in step
10.2, the only release path is the sequential, digest-pinned-manifest apply in
steps 10.3 and 10.4.

## 2. First-install/bootstrap only: create the namespace

```
kubectl apply -f deploy/k8s/namespace.yaml
```

## 3. First-install/bootstrap only: create the registry pull secret

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

## 4. First-install/bootstrap only: create the application secret

These namespace/bootstrap commands are for a first installation only. For
the existing two Deployments, do not replace either application Secret with a
template and do not restart from this section; use the release gate in step 10
below.

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

## 5. First-install/bootstrap only: apply the remaining manifests

This step is for a first install and must not be used as an existing-release
rollout. Existing releases use only the new digest-pinned Deployment
manifests in steps 10.3 and 10.4, after any explicitly required step 10.2
credential work.

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

## 6. First-install/bootstrap only: create the DNS record

Add a DNS `A` or `CNAME` record for `gridiron.draco.quest` that points to
the cluster's public ingress address. Check the current address with:

```
kubectl get ingress -n gridiron gridiron-2000
```

Wait for the record to propagate, then confirm the TLS certificate issues.

```
kubectl get certificate -n gridiron
```

## 7. First-install/bootstrap only: register the Google OAuth redirect URI

1. Open the Google Cloud Console for this app's OAuth client.
2. Add this exact production redirect URI to the client's authorized
   redirect URIs list:
   ```
   https://gridiron.draco.quest/auth/google/callback
   ```
3. Save the client. Do not remove the existing local development URI.

## 8. First-install/bootstrap only: set the league's allowed managers

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

### Commissioner identities and explicit aliases

`COMMISSIONER_EMAILS` is independent from `LEAGUE_ALLOWED_EMAILS`: it grants
the named canonical Google identities access to `/admin`, Commissioner HQ,
and commissioner-only controls. Values are comma-separated, case-insensitive,
and trimmed.

When one person has multiple permitted Google identities, keep the canonical
identity in `COMMISSIONER_EMAILS` and add a one-way mapping in
`IDENTITY_ALIASES`:

```dotenv
COMMISSIONER_EMAILS=oscar@m31labs.dev
IDENTITY_ALIASES=oscar.villavicencio@stablekernel.com=oscar@m31labs.dev
```

The alias is checked against the raw league domain/allowlist/invite policy
first. The mapping then unifies seat ownership, co-manager bindings, Big
Board, Pick'em, Blitz, notification preferences, sessions, and audit
attribution. Do not use chained mappings or map two canonical identities
together; the process fails closed on those configurations.

On first boot after enabling a mapping, the store idempotently migrates
internal alias-keyed records. It merges only compatible duplicate records and
refuses startup on conflicting seats, roles, picks, or preferences. Admission
invite records remain raw emails by design. Review `StartupError` and the
state database before retrying a conflict; no live data mutation is performed
by the application until the migration passes.

After changing the applied list, roll each Deployment through the normal
SK-first release gate. A Secret update alone does not alter an already-running
pod's environment, and a live identity check should use `/commissioner` after
the new pod is Ready. Do not use `kubectl set env` because it bypasses the
tracked Secret workflow.

## 9. First-install/bootstrap only: configure the shared Tank01 relay

1. Create or sign in to a RapidAPI account.
2. Subscribe to "Tank01 NFL Live In-Game Real Time Statistics" on the free
   tier (1,000 requests per month, no card required).
3. Copy the API key RapidAPI issues you.
4. Store the key only in `statrelay-secrets` (see
   `deploy/k8s/statrelay-secret.example.yaml`), then restart the relay.
   Do not add it to either league's Secret.
5. Confirm both Deployments set
   `TANK01_BASE_URL=http://statrelay.gridiron.svc.cluster.local`. A healthy
   cache is acceptable for release acceptance: `fantasyPoolMode` may be
   `live` or `cache`, provided `fantasyPoolError` is empty and
   `fantasyPoolPlayers >= fantasyRosterCapacity`. The app spends five
   requests per sync and syncs every six hours, so the free tier covers normal
   operation.

## 10. Existing-instance release gate

Use this sequence for the already-provisioned `gridiron-2000` and
`gridiron-2000-sk` Deployments. Never roll both at once. The order is
strictly Stable Kernel (SK) canary first, then flagship:

1. record both old revisions and image digests;
2. when first enabling HQ or intentionally rotating its credential, install
   one newly generated independent 256-bit token into both application
   Secrets before either Deployment rolls; otherwise leave it untouched;
3. apply the new digest-pinned SK Deployment manifest and wait for its
   canary gates;
4. apply the new digest-pinned flagship Deployment manifest only after SK
   passes;
5. smoke both instances and verify both Commissioner HQ peer cards.

### 10.1 Record the rollback point before either Deployment rolls

Create an operator-owned release record outside the repository. These
commands print only Deployment metadata and image references, never Secret
values:

```bash
RECORD_DIR="/tmp/gridiron-release-record-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "${RECORD_DIR}"

kubectl -n stablekernel get deployment/gridiron-2000-sk \
  -o jsonpath='name={.metadata.name} revision={.metadata.annotations.deployment\.kubernetes\.io/revision} image={.spec.template.spec.containers[?(@.name=="gridiron-2000")].image}{"\n"}' \
  | tee "${RECORD_DIR}/sk-before.txt"
kubectl -n stablekernel rollout history deployment/gridiron-2000-sk \
  | tee -a "${RECORD_DIR}/sk-before.txt"

kubectl -n gridiron get deployment/gridiron-2000 \
  -o jsonpath='name={.metadata.name} revision={.metadata.annotations.deployment\.kubernetes\.io/revision} image={.spec.template.spec.containers[?(@.name=="gridiron-2000")].image}{"\n"}' \
  | tee "${RECORD_DIR}/flagship-before.txt"
kubectl -n gridiron rollout history deployment/gridiron-2000 \
  | tee -a "${RECORD_DIR}/flagship-before.txt"
```

Keep the exact old revision numbers and image digests. Do not guess them and
do not substitute the current manifest digest for the new release digest.

### 10.2 Provision or intentionally rotate the Commissioner HQ token

Run this step only when enabling Commissioner HQ for the first time or when a
credential rotation is an explicit release objective. A routine application
release does not rotate or inspect the existing token; continue to 10.3 when
both deployments are already configured for HQ.

Generate exactly one new independent 32-byte (256-bit) token and use the same
opaque patch file for both existing Secrets. This workflow never prints,
echoes, logs, or fetches the token value; do not add a `kubectl get secret`
or JSONPath command that reads it:

```bash
umask 077
TOKEN_FILE="$(mktemp)"
PATCH_FILE="$(mktemp)"
trap 'rm -f "${TOKEN_FILE}" "${PATCH_FILE}"' EXIT

openssl rand -hex 32 > "${TOKEN_FILE}"
jq -n --rawfile token "${TOKEN_FILE}" \
  '{stringData:{COMMISSIONER_HQ_TOKEN:($token|rtrimstr("\n"))}}' \
  > "${PATCH_FILE}"

kubectl -n gridiron patch secret/gridiron-2000-secrets \
  --type=merge --patch-file="${PATCH_FILE}"
kubectl -n stablekernel patch secret/gridiron-2000-sk-secrets \
  --type=merge --patch-file="${PATCH_FILE}"
```

The identical patch file is the equality check. Do not reuse
`DATA_API_TOKEN`, `SESSION_SECRET`, or any token copied from a Secret.
When this step is required, both patches must succeed before either Deployment
manifest is applied or any rollout command runs.

### 10.3 Apply the new digest-pinned SK Deployment as the canary

Step 1 and the release manifest commit must prepare these two repository
Deployment manifests with the new immutable image digest before this gate,
without applying either one yet:

- `deploy/k8s/sk/deployment.yaml` — SK canary, applied first.
- `deploy/k8s/deployment.yaml` — flagship, applied second.

Change only each manifest's application image to the new
`harbor.draco.quest/orchard/gridiron-2000@sha256:<new-release-digest>`.
Preserve the manifest as the source of truth, including
`COMMISSIONER_INSTANCE_ID`, `COMMISSIONER_HQ_PEERS`, `TANK01_BASE_URL`,
and the existing Secret/ConfigMap references. Do not use `kubectl set image`
for this release path.

After any required step 10.2 patches succeed, apply the SK Deployment manifest
and wait for its canary rollout:

```bash
kubectl apply -f deploy/k8s/sk/deployment.yaml
kubectl -n stablekernel rollout status deployment/gridiron-2000-sk \
  --timeout=5m
```

Run the SK health and redirect checks in step 11. During this first roll, SK's
Commissioner HQ page may show the flagship peer card as unavailable because
the flagship is still on the old image. That card can remain unavailable until
the second roll; it is expected canary state, not an SK canary failure.

### 10.4 Apply the flagship Deployment manifest only after SK passes

```bash
kubectl apply -f deploy/k8s/deployment.yaml
kubectl -n gridiron rollout status deployment/gridiron-2000 \
  --timeout=5m
```

After this second roll, rerun the health, redirect, login, draft-room, and
Commissioner HQ checks for both hosts. Both peer cards must be available
before acceptance; the temporary first-roll exception no longer applies.

### 10.5 Rollback criteria and commands

Stop the sequence and roll back if either rollout times out, its pod is not
Ready, health is not `ok`, the pool mode is neither `live` nor `cache`,
`fantasyPoolError` is non-empty, the player count is below
`fantasyRosterCapacity`, the resolved SK redirect breaks, or either app
cannot complete its read-only smoke checks. After the second roll, an
unavailable HQ peer card is also a rollback failure.

Use the exact old revision numbers and image references captured in step
10.1; placeholders below are deliberately not current digests:

```bash
# SK canary failed before the flagship rolled:
kubectl -n stablekernel rollout undo deployment/gridiron-2000-sk \
  --to-revision=<recorded-sk-before-revision>
kubectl -n stablekernel rollout status deployment/gridiron-2000-sk \
  --timeout=5m

# Flagship or final acceptance failed after both rolls:
kubectl -n gridiron rollout undo deployment/gridiron-2000 \
  --to-revision=<recorded-flagship-before-revision>
kubectl -n gridiron rollout status deployment/gridiron-2000 \
  --timeout=5m
kubectl -n stablekernel rollout undo deployment/gridiron-2000-sk \
  --to-revision=<recorded-sk-before-revision>
kubectl -n stablekernel rollout status deployment/gridiron-2000-sk \
  --timeout=5m
```

If a recorded revision is unavailable, restore the exact old image digest in
the corresponding Deployment manifest from the release record and apply that
manifest. Keep the newly installed HQ token in both Secrets during an image
rollback; never print or fetch it. Do not bypass the manifest source of truth
with `kubectl set image`.

## 11. Pre-draft smoke test

Run every check below independently against both league hosts. This is a
read-only release smoke: it must never POST draft start, click a Start
control, submit a pick, or mutate seats, members, or league state.

1. **Health endpoint on both instances.** A passing response has `ok: true`,
   `fantasyPoolMode` equal to `live` or `cache`, an empty
   `fantasyPoolError`, and `fantasyPoolPlayers >= fantasyRosterCapacity`:
   ```bash
   set -euo pipefail
   check_health() {
     local label="$1" url="$2" body
     if ! body="$(curl --fail-with-body -sS "${url}/api/health")"; then
       printf '%s: curl failed\n' "${label}" >&2
       return 1
     fi
     if ! printf '%s' "${body}" | jq -e '
         .ok == true and
         (.fantasyPoolMode == "live" or .fantasyPoolMode == "cache") and
         (.fantasyPoolError // "") == "" and
         (.fantasyPoolPlayers >= .fantasyRosterCapacity)
       ' >/dev/null; then
       printf '%s: health predicate failed\n' "${label}" >&2
       return 1
     fi
     printf '%s: health OK\n' "${label}"
   }
   check_health flagship https://gridiron.draco.quest
   check_health stable-kernel https://sk.gridiron.draco.quest
   ```
   `set -euo pipefail` makes a failure on either host fail this block
   immediately; the second host cannot mask a first-host failure.
2. **HTTP redirects on both instances.** With redirects disabled, confirm
   `http://gridiron.draco.quest/` and
   `http://sk.gridiron.draco.quest/` each return a permanent redirect whose
   `Location` is the matching HTTPS host. The SK result verifies the
   tracked/live-resolved `sk/http-redirect.yaml` wiring:
   ```bash
   set -euo pipefail
   check_redirect() {
     local host="$1" headers
     if ! headers="$(curl -sS -D - -o /dev/null "http://${host}/" | tr -d '\r')"; then
       printf '%s: curl failed\n' "${host}" >&2
       return 1
     fi
     if ! printf '%s\n' "${headers}" | grep -Eiq '^HTTP/[0-9.]+ 30(1|8) '; then
       printf '%s: missing permanent redirect\n' "${host}" >&2
       return 1
     fi
     if ! printf '%s\n' "${headers}" | grep -Eiq "^location: https://${host}/?$"; then
       printf '%s: wrong redirect Location\n' "${host}" >&2
       return 1
     fi
     printf '%s: redirect OK\n' "${host}"
   }
   check_redirect gridiron.draco.quest
   check_redirect sk.gridiron.draco.quest
   ```
   The same `set -euo pipefail` fail-fast rule means either host's redirect
   failure makes this block nonzero; a later success cannot mask it.
3. **Login on both instances.** Open each `/login` URL in a browser, sign in
   with an allowed manager account, and confirm that account returns to that
   host's home page. Do not carry a session or callback URL between hosts.
4. **Draft room on both instances.** Open each `/draft` URL and confirm the
   draft order, player pool, pick tape, and closed/ready state render.
5. **Commissioner HQ after the second roll.** Open `/commissioner` on both
   hosts. Confirm the local card and the peer card are available, show the
   expected release metadata, and contain no PII. A peer-unavailable card is
   tolerated only during the SK-first canary window described in step 10.3.
6. **No draft mutation.** Do not run a draft-start request, click
   `START`, submit a pick, or use a demo-mode rehearsal against either
   production host. Test those mutations only in a separate staging
   environment and outside this release gate.

## 12. Separate post-acceptance secret maintenance

Only after both instances pass the release smoke, and in a separately
approved secret-maintenance window, remove the stale flagship
`TANK01_API_KEY` if it is still present. This is not part of the image
rollout and does not apply to `statrelay-secrets`, which remains the sole
owner of the real upstream key. The targeted Kubernetes JSON Secret operation
is:

```bash
kubectl -n gridiron patch secret/gridiron-2000-secrets --type=json \
  --patch='[{"op":"remove","path":"/data/TANK01_API_KEY"}]'
```

Never print or fetch any Secret value while performing this maintenance.

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

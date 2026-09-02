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

Run this source-integrity preflight from the checkout that will produce the
image. It requires a clean local `main` whose commit is exactly the fetched
`origin/main`; a feature branch, detached checkout, dirty tree, or locally
generated artifact must stop the release before any image is built or pushed.

```bash
set -euo pipefail
test "$(git branch --show-current)" = "main"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
git fetch --quiet origin main
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
```

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

Build and push the relay image from the same commit with its own Dockerfile
(the relay deployment pulls `:latest` with `imagePullPolicy: Always`, so a
`kubectl rollout restart deployment/statrelay` picks the new image up):

```bash
docker build -f cmd/statrelay/Dockerfile \
  -t "harbor.draco.quest/orchard/gridiron-2000-statrelay:${RELEASE}" \
  -t harbor.draco.quest/orchard/gridiron-2000-statrelay:latest .
docker push "harbor.draco.quest/orchard/gridiron-2000-statrelay:${RELEASE}"
docker push harbor.draco.quest/orchard/gridiron-2000-statrelay:latest
```

Record the pushed digest before changing either Deployment:

```bash
docker image inspect --format='{{index .RepoDigests 0}}' "${IMAGE}"
```

The `/api/health` response exposes `appVersion`, `gitSHA`, `buildDate`, and
`frameworkVersion` separately, plus the PII-free `stateSchema` object with
the logical `persistedVersion`/`supportedVersion`, physical
`persistedDatabaseVersion`/`supportedDatabaseVersion`, and `compatible`.
This makes an old image or a source/image branch mismatch visible without
reading registry internals. Do not copy a digest from a manifest into this
runbook as a new release digest.

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
whole league. Destructive reset confirmations are distinct: type `RESET
DRAFT` for the draft-scoped reset or `RESET LEAGUE` for the full league reset.
The full reset returns roster and seat topology to the league config defaults
while retaining franchise name overrides, invites, scoring, announcements, and
notification preferences.

### Commissioner identities and explicit aliases

`COMMISSIONER_EMAILS` is independent from `LEAGUE_ALLOWED_EMAILS`: it grants
the named canonical Google identities access to `/admin`, Commissioner HQ,
and commissioner-only controls. Values are comma-separated, case-insensitive,
and trimmed.

When one person has multiple permitted Google identities, keep the canonical
identity in `COMMISSIONER_EMAILS` and add a one-way mapping in
`IDENTITY_ALIASES`:

```dotenv
COMMISSIONER_EMAILS=commissioner@example.com
IDENTITY_ALIASES=commissioner.alias@example.org=commissioner@example.com
```

The configured commissioner's explicit alias is admitted by the narrow
commissioner exception; unrelated aliases are checked against the raw league
domain/allowlist/invite policy first. The mapping then unifies seat ownership,
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

**2026 status:** the Stable Kernel league did not form for 2026 (see
[section 13](#13-regular-season-live-scoring-rollout)), so `gridiron-2000-sk`
is not a live second instance this season and flagship is the only live
instance. Steps 10.3 and 10.4's SK canary are skipped for a 2026 release;
apply only `deploy/k8s/deployment.yaml` and run flagship's own authenticated
acceptance (step 11.2, read against one instance). This section's full
two-instance sequence remains the documented path for the season SK is
provisioned again.

Use this sequence for the already-provisioned `gridiron-2000` and
`gridiron-2000-sk` Deployments. Never roll both at once. The order is
strictly Stable Kernel (SK) canary first, then flagship:

1. record both old revisions and image digests;
2. when first enabling HQ or intentionally rotating its credential, install
   one newly generated independent 256-bit token into both application
   Secrets before either Deployment rolls; otherwise leave it untouched;
3. apply the new digest-pinned SK Deployment manifest and wait for its
   rollout;
4. complete the authenticated SK canary gate in step 11.1. Health and
   redirect checks alone are not sufficient: an allowed manager must verify
   login continuity plus read-only Team, Board, and Draft truth, and an
   authenticated commissioner must verify the local HQ card and the exact
   candidate release metadata. The old flagship peer may be unavailable or
   still report its previous release during this canary gate;
5. apply the new digest-pinned flagship Deployment manifest only after the
   complete SK gate passes;
6. complete the bilateral post-flagship gate in step 11.2. Both instances
   must pass authenticated manager and commissioner acceptance, and both HQ
   peers must be available and show the candidate release metadata.

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

Also record the state-schema evidence independently for each instance. The
the logical persisted value is the live store's authoritative
`kv.schema_version` marker and the physical persisted value is SQLite's
`PRAGMA user_version`; neither is the normalized in-memory
`PersistedState.SchemaVersion` or inferred from a Deployment revision:

```bash
curl --fail-with-body -sS https://sk.gridiron.draco.quest/api/health \
  | jq '{appVersion,gitSHA,buildDate,frameworkVersion,stateSchema}' \
  > "${RECORD_DIR}/sk-before-health.txt"
curl --fail-with-body -sS https://gridiron.draco.quest/api/health \
  | jq '{appVersion,gitSHA,buildDate,frameworkVersion,stateSchema}' \
  > "${RECORD_DIR}/flagship-before-health.txt"
```

Before either manifest is applied, adjudicate the candidate against those
records. A candidate must support at least both persisted versions it will
read: compare logical `persistedVersion` to `supportedVersion` and physical
`persistedDatabaseVersion` to `supportedDatabaseVersion`; require
`compatible: true`. If the candidate advances either storage schema, record a
separately tested, schema-compatible fallback digest and its release metadata
for each rollout (digest, app version, source SHA, build timestamp, and both
supported schema versions). The previous revision is not presumed compatible
merely because Kubernetes can address it.

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

Run the complete authenticated canary gate in step 11.1. During this first
roll, SK's Commissioner HQ page may show the flagship peer card as unavailable
or may show the flagship's previous release metadata because the flagship is
still on the old image. That peer state can remain until the second roll; the
SK local card, authenticated manager journey, and candidate release metadata
must still pass.

### 10.4 Apply the flagship Deployment manifest only after SK passes

```bash
kubectl apply -f deploy/k8s/deployment.yaml
kubectl -n gridiron rollout status deployment/gridiron-2000 \
  --timeout=5m
```

After this second roll, run the complete bilateral acceptance gate in step
11.2 for both hosts. Both authenticated manager journeys and commissioner
checks must pass, both HQ peer cards must be available, and each local card
must show the exact candidate release metadata before the release is accepted.

### 10.5 Rollback criteria and commands

Stop the sequence and roll back if either rollout times out, its pod is not
Ready, health is not `ok`, the pool mode is neither `live` nor `cache`,
`fantasyPoolError` is non-empty, the player count is below
`fantasyRosterCapacity`, the resolved SK redirect breaks, or either app
cannot complete its read-only smoke checks. After the second roll, an
unavailable HQ peer card is also a rollback failure.

Rollback is a schema-aware adjudication, not an image-only undo. Before
running any command below, compare the target revision or tested fallback's
recorded logical `stateSchema.supportedVersion` with the live instance's
`stateSchema.persistedVersion`, and its physical
`stateSchema.supportedDatabaseVersion` with
`stateSchema.persistedDatabaseVersion`. A target is eligible only when it
supports both persisted versions, reports `compatible: true`, and its
metadata was tested for this release. Do not run `kubectl rollout undo` to an
incompatible binary. If the previous revision is too old, roll forward the
candidate or apply the exact tested compatible fallback digest from the
release record; do not invent a backup, promise old-binary compatibility, or
assume an off-node backup exists. When a release advances the logical
schema (`internal/league/sqlmigrate.go`'s migration chain), the previous
revision's binary refuses the migrated database outright (`errSchemaTooNew`
in `internal/league/sqlstore.go`) rather than reading it incorrectly; rolling
back from that release goes through the release record's pre-roll snapshot
and `cmd/leaguerestore` (see [Backup and restore](backup-restore.md)), not
`kubectl rollout undo` alone. `release-2026.09.02-ee12ed7-wave6` (logical
schema 10 to 11, adding the commissioner event ledger; digest recorded in
`deploy/k8s/deployment.yaml`) is the first flagship release this rule
applies to.

Use the exact old revision numbers and image references captured in step
10.1; placeholders below are deliberately not current digests:

```bash
# SK canary failed before the flagship rolled, and the recorded target passed
# the schema compatibility adjudication above:
kubectl -n stablekernel rollout undo deployment/gridiron-2000-sk \
  --to-revision=<recorded-sk-before-revision>
kubectl -n stablekernel rollout status deployment/gridiron-2000-sk \
  --timeout=5m

# Flagship or final acceptance failed after both rolls; adjudicate both
# targets independently before running either undo:
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

## 11. Authenticated acceptance and pre-draft smoke test

Release acceptance has two ordered gates. The SK canary gate below must pass
before the flagship Deployment is applied; the bilateral gate must pass after
the flagship rollout. Health and redirect checks are necessary transport
evidence, but they are not sufficient release acceptance.

Every acceptance action is read-only. Do not POST or submit any production
mutation during either gate. In particular, do not start the draft, make a
pick, change ready/autopick or presence, claim or release a seat, invite or
remove a member, rename a team, upload an image, edit a Big Board, change a
lineup, submit a waiver, trade, Pick'em, or Blitz action, or alter any
commissioner setting. If a flow cannot be checked without a mutation, stop
and test that flow in a separate staging or rehearsal environment instead.
Use a fresh authenticated browser session per host; never carry a cookie,
OAuth callback, or return URL from one league instance to the other.

### 11.1 SK canary acceptance before flagship

Run the shared health and redirect checks below for Stable Kernel only, then
complete both authenticated acceptance roles on the SK host:

1. An allowed manager signs in from a deep link and confirms login continuity
   returns to the same SK host. The manager then opens Team, Board, and Draft
   and verifies the instance's truthful identity, roster-capacity/empty
   pre-draft state, Big Board controls, draft order, player pool, pick tape,
   and closed/ready state. Do not save, toggle, claim, or start anything.
2. An authenticated commissioner opens Commissioner HQ on SK and confirms the
   local card is available, contains no PII, and reports the exact candidate
   release metadata: release/app version, source Git SHA, build timestamp,
   and framework version. Separately verify the immutable image digest with
   read-only Deployment metadata against the operator release record; the HQ
   card is not a digest source. The flagship peer may be unavailable or may
   still show its previous release during this gate; that temporary peer state
   is the only canary exception.

Do not apply the flagship manifest until both authenticated SK checks and all
shared SK transport checks pass.

### 11.2 Bilateral post-flagship acceptance

After the flagship rollout is Ready, run the shared checks for both hosts and
repeat both authenticated acceptance roles independently on each host:

1. An allowed manager completes login continuity and the read-only Team, Board,
   and Draft truth checks on flagship and SK. Each session must remain on its
   own host and show that instance's league configuration and state.
2. An authenticated commissioner opens Commissioner HQ on each host. Each
   local card and its peer card must be available, contain no PII, and show
   the exact candidate release metadata. Separately verify that both
   Deployment image references resolve to the exact candidate immutable
   digest. Both peers must be reachable and agree on the candidate release
   identity before acceptance is recorded.

The first-roll peer exception ends when the flagship rollout begins. An
unavailable peer, stale candidate metadata, or cross-host session continuity
failure is a final-gate failure and requires rollback or investigation before
the release is accepted.

### 11.3 Shared pre-draft smoke checks

Run the checks below only against the host scope required by 11.1 or 11.2.
They provide the repeatable health, redirect, and page-level evidence used by
those gates.

1. **Health endpoint for the requested host set.** A passing response has `ok: true`,
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
   # SK canary gate (11.1):
   check_health stable-kernel https://sk.gridiron.draco.quest

   # Bilateral final gate (11.2):
   check_health flagship https://gridiron.draco.quest
   check_health stable-kernel https://sk.gridiron.draco.quest
   ```
   `set -euo pipefail` makes a failure on either host fail this block
   immediately; the second host cannot mask a first-host failure.
2. **HTTP redirects for the requested host set.** With redirects disabled,
   confirm each requested host returns a permanent redirect whose
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
   # SK canary gate (11.1):
   check_redirect sk.gridiron.draco.quest

   # Bilateral final gate (11.2):
   check_redirect gridiron.draco.quest
   check_redirect sk.gridiron.draco.quest
   ```
   The same `set -euo pipefail` fail-fast rule means either host's redirect
   failure makes this block nonzero; a later success cannot mask it.
3. **Allowed-manager session continuity.** For every host in the active gate,
   open a deep link such as `/draft?week=1`, sign in with an allowed manager,
   and confirm the sanitized return lands on that same host. Then inspect
   `/team`, `/board`, and `/draft` as described in the gate above.
4. **Commissioner session and metadata.** For every host in the active gate,
   open `/commissioner` as the authenticated commissioner and compare the
   local and peer cards with the exact candidate release record. Verify the
   four visible build fields there; verify the immutable digest separately
   from read-only Deployment metadata. The local candidate must be exact in
   11.1; both local and peer candidates must be exact and available in 11.2.
   Do not accept a generic healthy response as a substitute for the visible
   release metadata check.
5. **No mutation or peer exception outside the canary.** The only tolerated
   unavailable peer is the old flagship peer during SK canary acceptance in
   11.1. Any final-gate peer outage or any attempted production mutation
   fails acceptance. Test mutations only in a separate staging environment.

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

## 13. Regular-season live scoring rollout

Regular-season live scoring (`internal/livescore`) ships behind the
`LIVE_SCORING_ENABLED` kill switch, default `false`. See
[Game day](season-operations.md#game-day) in the season operations handbook
for the four states, precedence, and the kill-switch procedure this section's
drill exercises. Perform these steps in order; each step is a separate,
explicit action, never a batch.

1. **Verified Ultra quota and the layered budgets.** The RapidAPI Tank01
   Ultra listing (the plan ceiling; no higher tier) caps at 15,000
   requests/day, soft-limited — RapidAPI bills overage at $0.01/request
   instead of blocking, and returns no `429` on overage, so the app's own
   429 circuit breaker never fires there; `STATRELAY_DAILY_BUDGET` is the
   real wallet guard. The polling design (season-operations.md's Game day
   section) replaces the old blanket per-game poll: a scoreboard tick
   (`LIVE_SCOREBOARD_INTERVAL`, default `10s`, only while a game is inside
   its own poll window) resolves Tank01 IDs; GC-2b's adaptive cadence then
   fetches each in-progress game's box at `LIVE_BOX_FAST` (default `20s`)
   while a relevant possession is actively known, `LIVE_BOX_BASELINE`
   (default `30s`) otherwise (including every game before possession is
   verified), or at most once (its first sighting only) for a game where
   neither team fields a single league starter this week; a Signal Wire
   trigger fetches a named
   team's game at once regardless of either adaptive-cadence backoff,
   bounded to one triggered fetch per game per 10s. The tracked defaults
   are: `LIVE_DAILY_BUDGET=9000` per app instance (box-score fetches
   only — a games-list call is never charged against it) and
   `STATRELAY_DAILY_BUDGET=13000` on the shared relay
   (`deploy/k8s/statrelay.yaml`), both real values against the verified
   quota, not placeholders. `LIVE_POLL_INTERVAL` is the deprecated alias
   for `LIVE_SCOREBOARD_INTERVAL`; no tracked manifest sets it.
2. **Deploy `statrelay` first**, to the `gridiron` namespace. Confirm the
   `X-Statrelay-Budget-Remaining` response header on a manual `curl` against
   any relayed endpoint — the header appears only once a budget is set.
   Rebuild and roll `statrelay` whenever `cmd/statrelay/relay.go`'s `ttlTable`
   changes (for example, adding or changing the `/getNFLScoresOnly` prefix
   rule); an unmatched path silently falls back to `defaultTTL` (6 hours),
   which is stale for a live-scoring endpoint.
3. **Deploy the app image to flagship** with `LIVE_SCORING_ENABLED=false`.
   The Stable Kernel league did not form for 2026, so flagship is the only
   live instance; its own canary is temporal, not a second instance — step 5
   enables it for the Thursday Night Football window first, watched closely,
   before the full Sunday slate. Confirm `/api/health` and that the Matchups
   status line reads `LEDGER`.
4. **Rehearse the replay locally first** (`LIVE_SCORING_ENABLED=true
   LIVE_REPLAY_FIXTURE=<dir> LIVE_REPLAY_STEP=1s go run .`, watch `/matchups`
   in a browser), then **on flagship** with `LIVE_REPLAY_FIXTURE` mounted from
   a ConfigMap, `LIVE_SCORING_ENABLED=true`, and
   `LIVE_REPLAY_ALLOW_PRODUCTION=true` (replay mode refuses to start under a
   production `APP_ENV` without this explicit override, because it replaces
   the league schedule with the replay's one game) for 15 minutes, outside
   any real game window. Remove the fixture and the override afterward and
   confirm the status line returns to `LEDGER`.
5. **TNF window as the canary, then the kill-switch drill.** On the first
   live regular-season Thursday Night Football kickoff, flip
   `LIVE_SCORING_ENABLED=true` on flagship 30 minutes before kickoff. This
   Thursday window is flagship's own canary — a bounded, watched enablement
   before the full Sunday slate — standing in for the Stable Kernel canary
   the design originally assumed, since that second instance did not form
   for 2026. Record the live `gameStatusCode` from one relay response to
   confirm the status-code rule (`"2"` final, `"1"` in progress, `"0"`/`""`
   pre-game, any other code with a non-empty `currentPeriod` treated as in
   progress). One hour after kickoff, run the kill-switch drill on flagship:
   set the flag to `false`, roll the pod,
   confirm `LEDGER` on the status line within 60 s, set it back to `true`,
   confirm `LIVE` within 60 s. Log the drill below. The status line reads
   `LEDGER`, not `PAUSED · disabled`, because the flag is read only at
   process start: the rolled pod's poller has never ticked, so it has no
   in-progress game in memory to pause on. See
   [Kill-switch procedure](season-operations.md#kill-switch-procedure) for
   the full explanation and the harness evidence (`TestSimGameDayTimeline`).
7. **Watch the relay budget header** on the first live Sunday, at kickoff,
   mid-afternoon, and Sunday Night Football kickoff.

- **Known display lag:** an already-open Matchups page keeps showing `LIVE`
  until it next redraws. The `observe` function in `app/matchups/live.go`
  broadcasts a live update only when the poller's version number moves.
  The clock-driven window-close correction never moves that version. The
  page still self-corrects at its next re-render, bounded by the feed
  cache's 45-second limit.

### Kill-switch drill log

| Date | Kickoff (matchup) | Flag off at | `LEDGER` confirmed at | Flag on at | `LIVE` confirmed at | Operator |
| --- | --- | --- | --- | --- | --- | --- |
| 2026-08-30 | _Bounded rehearsal, flagship (`release-2026.08.30-c655472`), no live kickoff_¹ | rollout² | not checked (boot log only)² | rollout² | not checked² | — |
| _2026-09-10 (DAL@PHI, 20:20 EDT)_ | | | | | | |

¹ This drill ran outside a live kickoff, in the reverse order of the columns above: flag on, then flag off. It confirmed boot logging on both sides of the flag.

² Flag on, rollout: the enabled poller logged nothing at boot. That silent gap is the finding this commit fixes, with a new `livescore: poller enabled (...)` boot line. Flag off, rollout: within seconds, the log confirmed `livescore: LIVE_SCORING_ENABLED is not true; the live poller stays off`. The kill switch itself already worked. The drill did not re-run against a live game, so it did not check the status line directly. Per the corrected [Kill-switch procedure](season-operations.md#kill-switch-procedure), a boot-time-disabled poller has no in-progress game history to pause on. The expected status-line state is therefore `LEDGER`, not `PAUSED · disabled`.

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

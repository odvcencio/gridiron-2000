# League configuration reference

`league.json` is the public, operator-owned description of one Gridiron league. It controls league identity, teams, draft meeting, roster shape, membership posture, waivers, and trades. It does not contain credentials, invitations, commissioner identities, OAuth secrets, or live league state.

Start with [`config/league.json.example`](../config/league.json.example). The loader accepts strict JSON only: comments and unknown fields are rejected. Restart the application after changing the file.

Validate each instance before creating its ConfigMap or rolling its pod:

```sh
go run ./cmd/leaguecheck --file deploy/local/league.json
go run ./cmd/leaguecheck --file deploy/local/league-sk.json --format json
```

`leaguecheck` uses the same strict loader and the seven supported field environment overrides as the application, but it does not start the server or open the league state database. The `--file` path is authoritative: `LEAGUE_FILE`, `GOSX_APP_ROOT`, `DATA_FILE`, and the current working directory cannot redirect it. A successful text report shows the resolved public identity, season, draft meeting and timezone, team count, roster capacity, membership posture, waivers, and trade policy. JSON output is intended for fleet automation. An invalid file exits nonzero before a deployment changes.

Library callers that need an isolated preflight can use `league.LoadConfigFile(path)`. It reads exactly the supplied path, applies strict decoding, preset resolution, and validation, returns nonfatal warnings, and does not read process environment. `league.LoadConfigFileWithEnvOverrides(path)` is the explicit-path variant for tools such as `leaguecheck` that intentionally retain the seven documented field overrides.

## File lookup and precedence

The first existing config wins, in this order:

1. `LEAGUE_FILE`, when set;
2. `./league.json`;
3. `./config/league.json`;
4. `league.json` beside `DATA_FILE`.

If none exists, Gridiron starts with a neutral reference configuration whose dates are deliberately far in the future. Precedence is built-in defaults, then the selected JSON file, then the limited environment overrides listed below. The active source is visible in `/api/health` and commissioner diagnostics.

## Top-level fields

| Field | Required / allowed value | Meaning |
| --- | --- | --- |
| `version` | exactly `1` | Config schema version. A different version fails startup. |
| `league` | required object | Public identity and calendar settings. |
| `teams` | 4–14 entries, even count | Stable franchise identities and display properties. |
| `draft` | required object | Scheduled meeting, number of rounds, and format label. The schedule never starts the draft; a commissioner starts it intentionally. |
| `season_start_at` | RFC3339 timestamp | Calendar boundary used when deriving the season phase before persisted phase transitions exist. |
| `scoring_format` | `half_ppr`, `ppr`, or `standard` | Selects the base reception rule and provider projection/ADP mode. The complete scoring table is shown at `/scoring`. |
| `copy` | object; strings may be empty | Optional public copy overrides. Empty values use neutral generated copy, except an empty venue omits the venue line. |
| `membership` | optional object | Domain-wide admission posture. Individual runtime invitations and the deployment allowlist remain separate. |
| `roster` | optional object | A named preset or an explicit starter/bench shape, plus optional reserve, IR, and position limits. |
| `waivers` | optional object | Waiver ordering/budget and processing window. An omitted block uses defaults. |
| `trades` | optional object | Trade deadline, veto authority, and review window. An omitted block uses defaults. |

## `league`

| Field | Rule |
| --- | --- |
| `name` | Required, non-empty public league name. |
| `short_code` | 1–5 characters. |
| `tagline` | Public landing-page supporting copy; may be empty. |
| `mode_label` | Public format label such as `DYNASTY` or `REDRAFT`. It is presentation, not a hidden rules switch. |
| `url` | Canonical public base URL used in application links. |
| `timezone` | Valid IANA timezone such as `America/New_York`. |
| `season` | Integer from 2020 through 2100. |

## `teams[]`

Every team object supports:

| Field | Rule |
| --- | --- |
| `id` | Permanent lowercase key matching `[a-z0-9-]+`; unique across the league. Do not change it after state refers to the team. |
| `name` | 1–40 characters. |
| `abbreviation` | 2–4 characters; unique case-insensitively. |
| `division` | Either set a division on every team or on none. Every named division needs at least two teams. |
| `tone` | Empty for automatic assignment, or one of `blue`, `cyan`, `gold`, `lime`, `magenta`, `orange`, `pink`, `violet`. |

Leagues above 12 teams are valid but emit a player-pool depth warning; `deep-league` is the compact preset intended for that shape.

## `draft`

| Field | Rule |
| --- | --- |
| `at` | RFC3339 timestamp for the draft meeting. It is a reminder/window only; it never starts pick one. |
| `rounds` | 1–30 and exactly equal to the draftable roster total: starters + bench + reserve. IR is excluded because it is an in-season stash, not a draft slot. |
| `format_label` | Optional public description of the draft format. |

## `copy`

All four fields are strings:

- `hero_kicker`: landing-page kicker; empty derives neutral league-shape copy.
- `footer_line`: site/email footer; empty derives neutral copy.
- `venue_line`: invite venue detail; empty omits the row or clause.
- `invite_blurb`: invite opening paragraph; empty derives neutral invite copy.

## `membership`

`membership.allowed_domain` is either empty or a bare domain such as `stablekernel.com`—never an email address and never prefixed with `@`. The runtime exposes one effective posture to admission and public copy.

Public labels and details expose only this posture mode; they never include the configured domain or invitation identities.

- **OPEN AFTER SIGN-IN** — the domain is empty and both `LEAGUE_ALLOWED_EMAILS` and stored invitations are empty. Any authenticated Google account may enter while setup is open.
- **DOMAIN OR INVITE** — a domain is configured. A raw Google email at that domain is admitted; an explicit environment or stored invitation is an additive path. A configured domain with no invitations still rejects outsiders.
- **INVITE-ONLY** — no domain is configured and at least one explicit environment or stored invitation exists. Only invited identities enter.

Existing persisted members remain admitted if a domain or invitation list later changes. Commissioner admission is a narrow authority exception: the canonical identities listed in `COMMISSIONER_EMAILS` and their explicit `IDENTITY_ALIASES` are admitted so a commissioner is not locked out by a colleague-domain gate. The alias does not grant unrelated commissioner or league authorization to other identities.

## `roster`

Choose exactly one base shape:

- `preset`: one of `standard` (15 draft slots), `superflex` (15), `gridiron-house` (17), or `deep-league` (14); or
- `slots` plus `bench`: an explicit shape. Valid starter keys are `QB`, `RB`, `WR`, `TE`, `FLEX`, `SUPERFLEX`, `DST`, `K`, and `P`. Each count is 0–4, at least one starter is required, and `bench` is 0–10.

Do not combine `preset` with `slots` or `bench`. An omitted roster block resolves to `gridiron-house`, so portable new configs should name their intended preset explicitly.

These optional fields may accompany either base shape:

- `reserve`: map of real player positions (`QB`, `RB`, `WR`, `TE`, `DST`, `K`, `P`) to 0–4 draftable reserve slots. Reserve counts toward `draft.rounds`.
- `ir`: 0–10 injury-gated, in-season stash slots. IR does not count toward `draft.rounds`.
- `limits`: map of real player positions to a maximum of 1–20 held players across starters, bench, and reserve. IR occupants are exempt. An absent entry is unlimited.

## `waivers`

| Field | Rule |
| --- | --- |
| `mode` | `perf-priority` or `faab`. |
| `season_weight_pct` | 0–100. Controls the season-performance contribution where performance priority is used. |
| `faab_budget` | 1–1000. |
| `clear_days` | 0–7. |
| `process_time` | Local league time in 24-hour `HH:MM` form. |

## `trades`

| Field | Rule |
| --- | --- |
| `deadline` | RFC3339 timestamp or an empty string for no configured deadline. |
| `veto` | `commissioner`, `vote`, `both`, or `none`. |
| `review_hours` | 1–72. |

## Supported environment overrides

Only these seven environment values override public JSON fields:

| Environment variable | Overrides |
| --- | --- |
| `APP_NAME` | `league.name` |
| `DRAFT_TZ` | `league.timezone` |
| `LEAGUE_URL` | `league.url` |
| `SCORING_FORMAT` | `scoring_format` |
| `DRAFT_AT` | `draft.at` |
| `SEASON_START_AT` | `season_start_at` |
| `NFL_SEASON` | `league.season` |

Empty or unset values are no-ops. A non-empty malformed `DRAFT_AT` or
`SEASON_START_AT`, or a nonnumeric/out-of-range `NFL_SEASON`, fails startup
with an error naming the environment variable; Gridiron never silently keeps
the prior JSON/default value after an invalid override.

Other environment variables configure private identity policy, credentials, providers, storage, or runtime behavior; they are not part of the public `league.json` schema. See [`.env.example`](../.env.example) and the README configuration map.

## Private Commissioner HQ v1 provider

Each league binary can expose its signed, read-only commissioner summary on a
dedicated internal listener. The provider is disabled only when all of
`COMMISSIONER_HQ_LEAGUE_ID`, `COMMISSIONER_HQ_PROVIDER_KEY_ID`,
`COMMISSIONER_HQ_PROVIDER_SECRET`, `COMMISSIONER_HQ_PROVIDER_SECRET_FILE`, and
`COMMISSIONER_HQ_PROVIDER_ADDR` are absent. Setting any one opts in and startup
then requires a complete configuration:

- an explicit `COMMISSIONER_INSTANCE_ID` and `COMMISSIONER_HQ_LEAGUE_ID`;
- `COMMISSIONER_HQ_PROVIDER_KEY_ID` plus exactly one of the provider secret or
  an absolute provider-secret-file path; and
- an optional numeric bind address, defaulting to `:8091`.

Secret-file bytes are read exactly—no newline or whitespace trimming—and are
bounded to 32–4096 bytes by the HMAC credential contract. Never reuse the
legacy `COMMISSIONER_HQ_TOKEN`; it protects a different protocol. A configured
invalid release SHA/build timestamp, incomplete identity, unusable secret, or
failed private bind aborts startup. `/api/health` reports only whether this
provider is configured and listening; it exposes no address, key ID, secret,
or file path.

The v1 path is served only on the private listener. The public application
router, Service, and Ingress must not mount or proxy it. Per-instance internal
Services and restrictive NetworkPolicies belong to the declarative fleet
topology once its HQ-host relationship is explicit; do not compensate with an
allow-all rule on port 8091.

## Commissioner HQ v1 connection registry

The HQ consumer core reads one strict versioned JSON registry from the explicit
absolute `COMMISSIONER_HQ_V1_REGISTRY_FILE` path. If the variable is absent, v1
fleet hosting is disabled. Start with
[`config/commissioner-hq-v1.example.json`](../config/commissioner-hq-v1.example.json).
The registry is operator-owned topology, not league state: it contains stable
connection/league IDs, display metadata, fixed order, reviewed provider and
public origins, capabilities, canonical links, and one environment-variable or
absolute-file secret reference. It must never contain a secret value, email,
member identity, invitation, or raw upstream response.

Loading is bounded and fail-closed for malformed JSON, unknown fields, unsafe
origins/links, duplicate key/order/league identity, unsupported versions, and
more than 64 connections. An enabled connection whose referenced secret is
temporarily missing becomes a safe `misconfigured` row without preventing
other leagues from loading. Disabled hosting and disabled connections validate
safe topology but never read credentials or make provider requests.

The consumer keeps only a process-local last-success cache. Collection uses a
two-second per-provider timeout, three-second aggregate deadline, and at most
eight concurrent provider calls while preserving configured row order. A valid
success is `connected/live` and replaces only that connection's cache. A later
failure retains the exact prior provider snapshot as `stale` for at most 24
hours; after that, facts are `unavailable/not_reported`. Provider data quality
(`healthy`, `degraded`, or `not_reported`) remains independent of transport and
freshness. Restarts intentionally begin with an empty cache.

The source-only retry core fetches exactly one known connection and uses the
same timeout, classification, cache, and attempt-generation rules. The later
commissioner browser API owns session authorization, CSRF, per-commissioner
rate limiting, envelopes, and `Retry-After`; those concerns are deliberately
not guessed inside the identity-free fleet collector.

## Fleet document and generated publication

cmd/fleetgen accepts an explicit fleet.json; it does not discover a fleet from
the current directory or from environment variables. Each league_config_path is
resolved relative to the fleet document, preflighted by the canonical league
loader, and copied byte-for-byte into the generated ConfigMap. The
privacy-safe starting point is config/fleet.json.example, whose
league.json.example reference resolves beside it.

The fleet schema has version, an immutable image in
repository@sha256:<64 lowercase hex> form, a relay statrelay_origin, an
ingress_class, a certificate_issuer, and one or more instances. Every instance
supplies a lowercase id, Kubernetes namespace and resource_prefix, an HTTPS
public_origin, a fleet-relative league_config_path, a positive pvc_storage
quantity, and a required `commissioner_hq` value. `commissioner_hq: null` is a
nonparticipant; an object is a participant and supplies the explicit
`league_id`, nonnegative `order`, safe-token `accent`, safe-token `key_id`, and
boolean `host` fields. Participant `league_id`, `order`, and `key_id` values are
unique across the fleet. If any participant exists exactly one is `host: true`;
with no participants there are no hosts. A fleet definition has no credentials,
Secret values, email addresses, member identities, DNS records, or OAuth client
material.

The participant set is the Commissioner HQ v1 registry. Fleetgen orders registry
connections by their declared order and uses the instance ID as the registry
connection key. The single host is the only browser consumer: it receives the
read-only registry ConfigMap at `/etc/gridiron-hq/registry.json` and the
`COMMISSIONER_HQ_V1_REGISTRY_FILE` setting. Participants receive a private
named 8091 provider port, a dedicated ClusterIP Service, and a NetworkPolicy
that allows provider traffic only from the host namespace label and pod `app`
label. Public Services keep port 80 targeting the named application port 8080,
and public Ingresses route only to that Service port.

Each participant Secret example owns its
`COMMISSIONER_HQ_PROVIDER_SECRET`. The host client Secret example has one
distinct `COMMISSIONER_HQ_V1_SECRET_<INSTANCE>` placeholder per registry
connection; fill each with exactly the corresponding provider Secret value.
These are read-only/scoped HMAC pairs, not legacy bearer tokens, Tank01 keys,
sessions, OAuth credentials, or browser identities. Rotate a provider/client
pair deliberately. The generated registry contains only origins, canonical
capabilities and links, key IDs, and `secret_env` references—never secret
values or provider credentials.

Use fleetgen render --file <fleet.json> --out <directory> only after the
document and every league source validate. The publisher writes a complete
deterministic bundle behind a fixed .fleetgen-owner marker and replaces a
non-empty directory only when that exact marker is already present. Review
operator-checklist.md and each instance's generated Secret example before
provisioning anything. The checklist prints the exact callback
<public_origin>/auth/google/callback for every instance.

fleetgen check --file <fleet.json> --out <directory> compiles the same
in-memory expected publication and is strictly read-only. A clean check
returns zero; drift reports sorted expected-missing, changed, and
unexpected-existing paths. The output is generated and owned; existing
hand-authored production manifests under deploy/k8s/** are not implicitly
adopted by this tooling.

For first installation, author the league and run leaguecheck, render/check and
review the fleet bundle, provision Secrets/DNS/OAuth, then apply in the
reviewed order. For an existing immutable release, build once, pass the SK
canary gate, and roll the identical recorded image digest through the flagship
and remaining fleet. The shared statrelay is the sole Tank01 key owner.
Generated local-path PVCs are node-local ReadWriteOnce storage; check the
StorageClass reclaim policy and arrange backups separately, without assuming HA
or backups from the bundle.

## Validate before rollout

Use the same checks as the application build:

```bash
go test ./internal/league -run 'Test.*Config'
go test ./...
gosx check app/guide/page.gsx
gosx build --prod .
```

Startup fails closed on invalid config. `/guide` projects only public config facts—league name/mode, team and roster capacity, draft meeting/timezone, membership posture, and player-pool target math—and never exposes member, invite, seat, pick, or board state.

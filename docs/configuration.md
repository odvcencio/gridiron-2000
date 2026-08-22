# League configuration reference

`league.json` is the public, operator-owned description of one Gridiron league. It controls league identity, teams, draft meeting, roster shape, membership posture, waivers, and trades. It does not contain credentials, invitations, commissioner identities, OAuth secrets, or live league state.

Start with [`config/league.json.example`](../config/league.json.example). The loader accepts strict JSON only: comments and unknown fields are rejected. Restart the application after changing the file.

Validate each instance before creating its ConfigMap or rolling its pod:

```sh
go run ./cmd/leaguecheck --file deploy/local/league.json
go run ./cmd/leaguecheck --file deploy/local/league-sk.json --format json
```

`leaguecheck` uses the same strict loader and supported environment overrides as the application, but it does not start the server or open the league state database. A successful text report shows the resolved public identity, season, draft meeting and timezone, team count, roster capacity, membership posture, waivers, and trade policy. JSON output is intended for fleet automation. An invalid file exits nonzero before a deployment changes.

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

`membership.allowed_domain` is either empty or a bare domain such as `stablekernel.com`—never an email address and never prefixed with `@`. When set, a Google identity at that domain may enter without an individual invite. Runtime invitations and `LEAGUE_ALLOWED_EMAILS` continue to work alongside it. Non-domain identities still depend on those runtime controls, except that an installation with both lists empty is deliberately open during setup and admits any authenticated Google identity.

An empty or omitted value means only that there is no domain gate; public config cannot reveal the effective runtime posture. Individual invitations or `LEAGUE_ALLOWED_EMAILS` may restrict admission. When both are empty, the initial-setup behavior is open to any authenticated Google identity. Commissioner authority is separate and still comes from `COMMISSIONER_EMAILS`; identity merging is separate and comes from `IDENTITY_ALIASES`.

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

## Validate before rollout

Use the same checks as the application build:

```bash
go test ./internal/league -run 'Test.*Config'
go test ./...
gosx check app/guide/page.gsx
gosx build --prod .
```

Startup fails closed on invalid config. `/guide` projects only public config facts—league name/mode, team and roster capacity, draft meeting/timezone, membership posture, and player-pool target math—and never exposes member, invite, seat, pick, or board state.

# GRIDIRON 2000

A private, self-hostable fantasy-football league room built with GoSX. It uses Google OAuth for league identity, keeps draft and data state on the machine you operate, listens to a commissioner-curated public signal wire, and mirrors open NFL datasets. An optional Tank01 connection adds live ADP, projections, and fantasy news to the draft room; multi-instance deployments can route every league through one shared relay so the upstream key and request budget have one owner.

Every league-specific fact — name, team count, divisions, draft date, and invite copy — lives in `league.json` (see `config/league.json.example`), not in the code. A fresh checkout with no `league.json` runs a neutral, clearly-placeholder reference league; copy the example file, edit it, and restart to run your own. `DRAFT_AT` and `DRAFT_TZ` still override the file's draft date/timezone for a quick change without touching the file.

## What is included

- Neo-Retro “Stadium OS, year 2000” league HQ, matchup simulator, team terminal, draft room, Signal Wire, and mobile layouts.
- Google OAuth with PKCE, encrypted HTTP-only sessions, CSRF protection, an optional email allowlist, and a configurable seat count (4 to 14 teams; see `config/league.json.example`).
- Persistent manager assignments, draft readiness, and snake-draft picks in the league database, `data/league.db` (SQLite, write-ahead logging, one transaction per mutation). A pre-existing `data/league-state.json` imports itself into the database on the first start; the file survives as `data/league-state.json.imported`.
- A no-key RSS/Atom mesh with ESPN NFL, CBS Sports NFL, r/fantasyfootball, and Mastodon hashtag feeds enabled by default, plus optional curated Bluesky Jetstream identities.
- A signed-in league form for tips, shared news, and human-entered market sightings—including observations from PrizePicks—without account automation or scraping.
- Editable Arbiter classification and provenance rules, exact-link clustering, corroboration counts, conditional feed requests, source health, and a metadata-only audit journal.
- An atomic local mirror of nflverse schedules, weekly injury reports, corrected weekly player statistics, team-week defense/special-teams box scores, and play-by-play, under the CC-BY-4.0 license.
- A draft pool service with a swappable provider seam: an embedded 182-player offline pool with approximate ranks, or a live Tank01 pool with ADP, projections, bye weeks, injuries, and news.
- A draft room at `/draft` with four tabs — Pool (available players), Big Board (the seat's private queue, with a no-JS up/down reorder form), Picks (round-by-round history), and Teams (every roster grid) — plus a seat-level Big Board at `/board`: the primary and co-manager share one private order, and the draft room/autopick surface that team's top available targets on the clock.
- A commissioner console at `/admin` (`COMMISSIONER_EMAILS`): a person-attributed commissioner event ledger, regular-season schedule generation, week close, runtime invites, seat release, league announcements, and typed-confirmation draft or league resets.
- One-click league backup: a commissioner-downloadable snapshot archive from `/admin`, nightly local snapshots, and an offline `cmd/leaguerestore` restore path — see [Backup and restore](docs/backup-restore.md).
- `/matchups` shows one live status line per matchup: the state chip reads `LIVE`, `PAUSED`, `FINAL`, or `LEDGER`, next to a freshness clause naming when the source was last checked — see [Live scoring and cost tiers](#live-scoring-and-cost-tiers).
- A `/players` waiver desk: browse the pool, file claims, and bid FAAB (Free Agent Acquisition Budget) or use priority order, with private per-team receipts and a commissioner view of every outcome.
- `/pickem`: an independent against-the-spread game with per-game locks, season standings, and a weekly leaderboard.
- `/locker`: the Locker Room, a league-wide post feed for commissioner notes and manager activity.
- `/settings`: per-manager notification preferences for delivery channels and category.
- Honest empty states: seats show `UNCLAIMED` until a manager signs in, records start `0–0`, and rosters stay empty until picks are made.
- Same-origin league APIs plus token-protected JSON, NDJSON, and CSV exports for future applications.
- A complete demo experience while Google credentials and trusted social sources are being configured.
- A public /guide for managers arriving from another fantasy provider, with a five-minute start, commissioner checklist, draft controls, data states, and a manual migration checklist.
- An explicit [season operations handbook](docs/season-operations.md) for draft night, weekly lineup locks, waivers, trades, week close, degraded data, and fleet-scale commissioner operations.

The seat-scoped board ownership, transfer/detach behavior, legacy per-account
migration, rollback, and attribution follow-up are versioned in
[Decision 0001](docs/decisions/0001-seat-scoped-big-board.md).

There are no Sleeper, Genius Sports, sportsbook, PrizePicks, or NFL+ account integrations. No sports-data API key is required: without one the draft room runs on the embedded offline pool.

## How the app treats you

Every page in Gridiron follows the same product contract:

- A value shows its true state — `LIVE`, `CACHE`, `STALE`, `DEGRADED`, `OFFLINE`, or `AWAITING_RELEASE` — never a guessed or hidden number.
- Plain language comes first; a term links to its definition instead of assuming you already know it.
- A disabled control names the reason next to it.
- A displayed time is league-local, names its zone, and adds a relative phrase such as "in 3 hours."
- An action returns you to the page, filter, and position you started from.
- A destructive action — a reset, a drop, a trade decision — asks for a typed or checked confirmation before it runs.
- Every workflow works with JavaScript off, through plain HTML forms.
- Every page is usable on a phone.

## Run locally

Requirements: Go 1.26 and GoSX v0.53.10.

```bash
cp .env.example .env
go run .
```

Open [http://localhost:8080](http://localhost:8080), then visit `/wire` for source health and league submissions. On first start, the public feeds and 2026 schedule synchronize immediately. The 2026 injury and player-stat files correctly report `awaiting_release` until nflverse publishes them.

Useful checks:

```bash
go test ./...
arbiter check internal/wire/signal_rules.arb
gosx check app/wire/page.gsx
gosx build --dev .
```

## Configure your league

Copy `config/league.json.example` to `league.json` (or `config/league.json`) and edit the public league configuration. [`docs/configuration.md`](docs/configuration.md) documents every supported field, validation boundary, membership posture, file lookup rule, and environment override. The example remains strict, valid JSON; comments belong in the documentation, not in the config file.

Then create a Google OAuth client with application type **Web application**, and register the exact callback URI:

```text
http://localhost:8080/auth/google/callback
```

Set the credentials and one email per seat:

```dotenv
GOOGLE_CLIENT_ID=your-client-id
GOOGLE_CLIENT_SECRET=your-client-secret
GOOGLE_REDIRECT_URL=http://localhost:8080/auth/google/callback
LEAGUE_ALLOWED_EMAILS=alex@example.com,maya@example.com
COMMISSIONER_EMAILS=commissioner@example.com
# Optional explicit identity merge; a configured commissioner alias is admitted across the membership gate.
IDENTITY_ALIASES=alias@example.com=commissioner@example.com
DEMO_MODE=false
```

An authenticated manager claims the first open seat. The allowlist should contain one account per configured team before exposing the app outside your home network. Production needs HTTPS, a strong random `SESSION_SECRET`, and the production callback URL registered with Google.

## Run with Docker Compose

Self-hosters who want a running league without a Kubernetes cluster can use [`deploy/compose/compose.yaml`](deploy/compose/compose.yaml). It runs the app container, a data volume, and a Caddy sidecar for automatic HTTPS. An HTTP-only profile also runs a domain-free local trial. Follow [`docs/quickstart.md`](docs/quickstart.md) for the full ten-minute walkthrough, including OAuth client registration and first sign-in.

## Author and publish a fleet

Multi-instance operators can keep public topology in a strict fleet document.
Start with `config/fleet.json.example`, keep each `league_config_path` relative
to that document, and use the explicit compiler and publisher:

    go run ./cmd/leaguecheck --file config/league.json.example
    go run ./cmd/fleetgen render --file config/fleet.json --out deploy/generated
    go run ./cmd/fleetgen check --file config/fleet.json --out deploy/generated

First-install order: author and validate the league; render and check the
fleet bundle; review every generated manifest and Secret example; provision
real Secrets, DNS, and OAuth registrations using the checklist's printed
callbacks; then apply the reviewed resources. Neither the fleet document nor
the generated bundle contains a Secret value or a member identity.

For an existing immutable release, build and publish one image once, pin and
record its digest, pass the Stable Kernel (SK) canary acceptance gates, then
roll that identical digest through the flagship and remaining fleet instances
in the recorded order. `fleetgen check` verifies the reviewed bundle; it never
applies resources.

The shared `statrelay` remains the sole owner of the real `TANK01_API_KEY`;
fleet-generated Secrets receive only a relay URL. Generated local-path PVCs
are node-local ReadWriteOnce storage with no HA guarantee. Gridiron takes its
own local nightly and on-demand snapshots (see
[Backup and restore](docs/backup-restore.md)) but never copies them off the
node; plan an off-host copy yourself. `fleetgen adopt` runs a read-only,
secret-free preflight against an existing hand-authored fleet before any
operator applies a resource. See [`deploy/README.md`](deploy/README.md) for
the complete fleet-authoring, adoption-preflight, and Commissioner HQ v1
topology reference.

### One person, multiple Google identities

Use `IDENTITY_ALIASES` when the same commissioner has more than one
permitted Google email:

```dotenv
COMMISSIONER_EMAILS=commissioner@example.com
IDENTITY_ALIASES=commissioner.alias@example.org=commissioner@example.com
```

Mappings are explicit, one-way, and fail closed on malformed, chained, or
ambiguous entries. A configured commissioner's alias is admitted by this
narrow exception; unrelated identities still need raw domain, allowlist, or
invite policy. After admission, the canonical identity owns commissioner
authorization, seat/team ownership, co-manager bindings, and every
per-account feature — Big Board, Pick'em, Blitz, notification preferences,
sessions, and audit attribution. See the membership section of
[`docs/configuration.md`](docs/configuration.md) for the full admission-order
table.

## Assemble the free Signal Wire

The built-in source file starts with four complementary channels:

| Channel | Role | Default trust tier |
| --- | --- | --- |
| ESPN NFL + CBS Sports NFL RSS | Publisher headlines | `PUBLISHER` |
| r/fantasyfootball New + Mastodon #fantasyfootball | Broad crowd radar | `COMMUNITY` |
| Curated Bluesky handles/DIDs | Reporter, team, and weather alerts | `CURATED SOCIAL` |
| Signed-in league submissions | Tips, linked reporting, TV or market observations | `COMMUNITY`, `COMMUNITY LINK`, or `MARKET WATCH` |

RSS/Atom feeds poll every two minutes using ETag and Last-Modified validators. Copy [`config/wire-sources.example.json`](config/wire-sources.example.json), edit it, and point the app at it to replace the defaults:

```dotenv
WIRE_SOURCES_FILE=/absolute/path/to/wire-sources.json
```

For event-driven social alerts, choose a small set of public Bluesky accounts: official teams, domain-verified reporters, local beat writers, and weather specialists. Add handles or stable DIDs as comma-separated values:

```dotenv
BLUESKY_HANDLES=reporter.example,team.example
BLUESKY_DIDS=did:plc:example
```

Handles resolve to permanent DIDs at startup. The server asks Jetstream only for `app.bsky.feed.post` records from those DIDs; it does not ingest the full public network. Relevant posts usually reach the server as events within seconds, and the browser checks the private wire every 20 seconds.

Managers can submit what they see on TV, a local site, social media, or a projections platform. A market sighting from PrizePicks is stored only as the manager's summary, source label, optional link, and timestamp. The server never signs into PrizePicks, captures its cookies, or polls an undocumented endpoint.

Classification rules live in [`internal/wire/signal_rules.arb`](internal/wire/signal_rules.arb). Override them without changing the repository:

```dotenv
WIRE_RULES_FILE=/absolute/path/to/league-rules.arb
```

Provenance policy lives in [`internal/wire/trust_rules.arb`](internal/wire/trust_rules.arb) and can be overridden with `WIRE_TRUST_RULES_FILE`. Publisher, social, crowd, and market evidence receive different visible tiers and weights. Exact canonical source links cluster together and show how many independent source records corroborated them. Every result remains provisional: it can alert the league, but it cannot add points, change a roster, or settle a dispute.

## Fantasy draft pool and shared relay

The draft room always works. Without an upstream connection it uses an embedded offline pool of 182 players with approximate ranks. With Tank01 available it syncs live data and labels the pool `live`; the last good atomic cache remains usable as `cache` when a later refresh fails.

For a one-process local development server, connect directly:

1. Create a RapidAPI account and subscribe to "Tank01 NFL Live In-Game Real Time Statistics". The free tier (1,000 requests per month, no card) is enough: one sync costs five requests, and the app syncs every six hours (about 600 per month at the default interval — set `FANTASY_SYNC_INTERVAL=8h` to stay under 1,000, or the $10/month Pro tier removes the concern).
2. Set `TANK01_API_KEY` in the local `.env` and restart. This direct-key path is for a standalone development installation, not a shared multi-instance topology.
3. Confirm `fantasyPoolMode` reports `live` at `/api/health`.

For Kubernetes or any host running more than one league, deploy one `statrelay`, put `TANK01_API_KEY` only in `statrelay-secrets`, and set every league application to:

```dotenv
TANK01_BASE_URL=http://statrelay.gridiron.svc.cluster.local
```

Do not copy the upstream key into a league Secret. The relay owns authentication, caching, and quota sharing; each league process consumes its Tank01-compatible envelope. The tracked flagship and Stable Kernel Deployment manifests are a two-instance example of this N-instance contract.

One sync fetches the player list, ADP, weekly projections, fantasy news, and team bye weeks, then writes an atomic cache under `data/fantasy/`. Between syncs, and across restarts, the last good pool serves from that cache. `SCORING_FORMAT` (half_ppr, ppr, standard) selects the ADP type and projection scoring.

The same vendor publishes NBA, MLB, NHL, and WNBA APIs with the same envelope. Point `TANK01_HOST` at another Tank01 host to reuse this client when those seasons start.

## Open statistics mirror

The app uses five nflverse release assets:

- `games.csv` for schedules and game-level scores, checked every five minutes with HTTP validators.
- `injuries_2026.csv` for weekly report and practice statuses, checked every 15 minutes.
- `stats_player_week_2026.csv` for corrected weekly player statistics and fantasy fields, checked every six hours.
- `stats_team_week_2026.csv` for each team's defense/special-teams box score, checked every six hours.
- `play_by_play_2026.csv.gz` for per-play detail (punts, in particular), checked every six hours.

Downloads go to an owner-only temporary file, are size-limited and parsed, then replace the prior cache atomically. A manifest retains row counts, SHA-256 hashes, ETags, timestamps, source URLs, and license information. An unavailable preseason player asset is a waiting state, not an application error.

This is intentionally a two-speed system:

```text
publisher + crowd feeds  -> minute-scale provisional alerts -> private Signal Wire
curated social + members -> seconds/human provisional alerts -> private Signal Wire
open nflverse files      -> slower corrected ledgers         -> scoring/reconciliation source
```

The first layer provides the “something just happened” experience without paying a real-time vendor. The second provides reusable structured history and powers schedule-backed fantasy matchups. When `LIVE_SCORING_ENABLED=true` (the default is `false`), regular-season live scoring adds a third layer: `internal/livescore` fetches Tank01 through `statrelay` in three tiers — a games-list scoreboard tick every `LIVE_SCOREBOARD_INTERVAL`, a per-game box-score baseline every `LIVE_BOX_BASELINE`, and an out-of-band box fetch the Signal Wire triggers for a `Touchdown`/`Turnover`/`BigPlay`/`KickingPlay` signal naming a team with a game in progress (bounded to one triggered fetch per game per 10 seconds; this only changes fetch timing, never a stat or a score) — and overlays the result onto the mirrored ledger, player by player: the live row wins while that player's game is in progress, the mirrored ledger row wins once the game is final or whenever live has no data for that player. The nflverse file stays the close-week truth regardless: during an open week, matchup totals are provisional calculations from the current effective lineups and the best available source (live or mirrored); this is **not** official scoring. When the commissioner closes a week, Gridiron records the matchup results and pins every team's effective starters for that week. Later drops, trades, or roster-shape edits cannot rewrite that closed result; a repeated close is an idempotent no-op. A posted final score never changes once a week is closed, even if a later source correction disagrees. The commissioner close remains explicit even when the NFL schedule and corrected-stat freshness checks say the week is ready.

See [Live scoring and cost tiers](#live-scoring-and-cost-tiers) for the three cost tiers this feature runs under.

## Live scoring and cost tiers

Three cost tiers cover regular-season live scoring. The live feed only ever affects an open week's freshness; it never decides a closed week's score. Full cadence, budget, and status-line detail live in [Game day](docs/season-operations.md#game-day).

| Tier | Cost | Cadence | Monthly/daily budget |
| --- | --- | --- | --- |
| Offline (no Tank01 key) | $0 | No live poll; matchups read the mirrored nflverse ledger only (`LIVE_SCORING_ENABLED=false`, the default). | n/a |
| `LIVE_PROFILE=free` (Tank01 free tier) | $0 | Scoreboard every 30 minutes, box scores every 6 hours baseline/fast, about a live score every 30 minutes on a game day. | About 780 requests/month, under the free tier's 1,000/month hard limit. |
| Ultra (default `LIVE_PROFILE`) | About $25/month for the whole league | Scoreboard every 10 seconds, box scores every 20–30 seconds. | `LIVE_DAILY_BUDGET=9000` fetches/day per instance, against the verified 15,000/day soft-limited Ultra quota. |

Every league instance behind one shared `statrelay` (see [Fantasy draft pool and shared relay](#fantasy-draft-pool-and-shared-relay)) draws on that relay's own `STATRELAY_DAILY_BUDGET` and cache, so several free-tier or Ultra-tier leagues do not each meter their own quota against the same Tank01 key.

## Private storage and exports

```text
data/
  league.db                    authoritative league state (SQLite)
  league.db.bak                rolling snapshot, written before every draft pick
  league-state.json.imported   the JSON state file the database was built from
  backups/
    gridiron-snapshot-*.tar.gz   nightly local snapshots, rotated to BACKUP_KEEP (default 7)
  signal-wire/
    state.json                 current, rewriteable signal excerpts
    events/YYYY-MM-DD.ndjson   metadata-only audit journal
  open-stats/
    games.csv
    injuries_2026.csv
    stats_player_week_2026.csv
    manifest.json
```

Files are created with owner-only permissions. The Signal Wire honors Bluesky deletions by clearing the post text and CID from current state. Feed and manager entries retain explicit provenance; the journal preserves only derived metadata needed to audit classification.

A commissioner can also download a complete, restorable backup archive
on demand from `/admin` (League configuration), and Gridiron saves the same
archive locally every night. Off-host copying remains the operator's job. See
[Backup and restore](docs/backup-restore.md) for exactly what an archive
contains and how to restore one with `cmd/leaguerestore`.

League-session endpoints:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/data/status` | Signal and open-data health |
| `GET /api/wire/status` | Signal listener state and counters |
| `GET /api/wire/events?limit=50&category=injury` | Recent classified signals |

Reusable exports require `Authorization: Bearer <DATA_API_TOKEN>`:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/data/games?week=1` | Normalized schedule/game records |
| `GET /api/data/player-week?week=1&player_id=...` | Normalized weekly player ledger |
| `GET /api/data/injuries?week=1&team=BUF` | Normalized injury/practice reports |
| `GET /api/data/fantasy-pool` | Normalized draft pool with ADP and projections |
| `GET /api/data/signal-export.ndjson` | Current non-deleted signal records |
| `GET /api/data/schedules.csv` | Mirrored nflverse schedule CSV |
| `GET /api/data/player-stats.csv` | Mirrored nflverse player-stat CSV |
| `GET /api/data/injuries.csv` | Mirrored nflverse injury CSV |

CORS is intentionally disabled. Keep the bearer token server-side in any later application.

## Configuration map

| Variable | Default | Purpose |
| --- | --- | --- |
| `DRAFT_AT` | `2099-01-01T00:00:00Z` | Scheduled draft meeting/window as RFC3339; only the commissioner’s **Start draft** action begins pick one |
| `DRAFT_TZ` | `America/New_York` | Timezone for displayed clock times |
| `PICK_CLOCK` | `120` (seconds; also accepts a Go duration such as `90s`) | Overrides `draft.pick_clock_seconds`; clamped to 10–600 seconds |
| `DRAFT_LIVE_MODE` | `target` | `fallback` restores the pre-GoSX-v0.53.10 refetch-and-swap draft-room wiring; only `target` and `fallback` are meaningful |
| `GOSX_APP_ROOT` | current working directory | Overrides the app-root path `LEAGUE_FILE` lookup and GoSX's own asset resolution use |
| `COMMISSIONER_EMAILS` | empty | Canonical accounts allowed into `/admin` |
| `IDENTITY_ALIASES` | empty | Explicit `alias=canonical` mappings; a configured commissioner's aliases are admitted by the narrow commissioner exception, while unrelated identities still need raw policy |
| `COMMISSIONER_INSTANCE_ID` | `local` | Stable ID for this isolated league in Commissioner HQ |
| `COMMISSIONER_HQ_PEERS` | empty | Comma-separated `id=service-origin\|public-origin` entries for every other league this instance should display; fleet size is not capped |
| `COMMISSIONER_HQ_CONCURRENCY` | `8` | Simultaneous peer-summary reads, from 1–64; bounds resource use without limiting fleet size |
| `COMMISSIONER_HQ_TIMEOUT` | `1.5s` | Per-peer read timeout, greater than zero and at most 10 seconds |
| `COMMISSIONER_HQ_LEAGUE_ID` | unset | Stable expected league ID for the private v1 summary provider; setting it opts into the all-or-nothing provider configuration |
| `COMMISSIONER_HQ_PROVIDER_KEY_ID` | unset | Opaque HMAC key ID for the private v1 provider; never reuse the legacy `COMMISSIONER_HQ_TOKEN` |
| `COMMISSIONER_HQ_TOKEN` | empty | Legacy shared bearer token for the older peer-summary Commissioner HQ protocol; the v1 provider above uses `COMMISSIONER_HQ_PROVIDER_KEY_ID`/`COMMISSIONER_HQ_PROVIDER_SECRET` instead, never this value |
| `COMMISSIONER_HQ_PROVIDER_SECRET` / `COMMISSIONER_HQ_PROVIDER_SECRET_FILE` | unset | Exactly one 32–4096 byte HMAC secret source; the file path must be absolute and bytes are not trimmed |
| `COMMISSIONER_HQ_PROVIDER_ADDR` | `:8091` when configured | Private numeric bind address for the v1 provider; this listener must not be routed by the public Service or Ingress |
| `COMMISSIONER_HQ_V1_REGISTRY_FILE` | unset | Absolute path to the strict v1 HQ-host connection registry; absence disables v1 fleet hosting, and the registry stores secret references rather than secret bytes |
| `APP_IMAGE_DIGEST` | empty | Optional immutable `sha256:` release digest reported to HQ; empty is represented as unknown rather than guessed |
| `TANK01_API_KEY` | empty | Direct upstream credential for a standalone/local process; in the tracked Kubernetes topology only `statrelay-secrets` owns it |
| `TANK01_BASE_URL` | empty | Override the provider base URL; point every Kubernetes league at the shared `statrelay` Service |
| `TANK01_HOST` | Tank01 NFL host | Swap for another Tank01 sport later |
| `BLITZ_DAILY_BUDGET` | `300` | Preseason Blitz's own daily cap on relayed Tank01 calls |
| `BLITZ_POLL_INTERVAL` | `180s` | Preseason Blitz scoreboard poll interval |
| `LIVE_SCORING_ENABLED` | `false` | Kill switch for regular-season live scoring; `internal/livescore` only polls in-progress box scores when this is exactly `true`. Exception: with `LIVE_REPLAY_FIXTURE` set, the poller runs unless this is exactly `false` — still gated by `LIVE_REPLAY_ALLOW_PRODUCTION` outside a local `APP_ENV` |
| `LIVE_SCOREBOARD_INTERVAL` | `10s` (floor `5s`) | How often each instance fetches one games-list call, only while a game is inside its own poll window; `LIVE_POLL_INTERVAL` is the deprecated alias (a startup log line names the mapping when used) |
| `LIVE_BOX_BASELINE` | `30s` | The flat/fallback cadence for an in-progress game's box score: used whenever GC-2b's adaptive cadence is not promoting the game to `LIVE_BOX_FAST` (unknown possession, a clock-stopped break, an unchanged-payload backoff) — see `LIVE_BOX_FAST` |
| `LIVE_BOX_FAST` | `20s` (floor `10s`) | GC-2b's fast-tier cadence: used only for a game whose currently known possession is itself relevant (the possessing team fields a league offensive starter, or the defending team's DST is started). A game where neither team fields a single league starter at all is fetched at most once (its first sighting), never repeatedly at either cadence — the shared scoreboard call keeps its score/period/final state current enough for free after that |
| `LIVE_MAX_INFLIGHT` | `4` | Maximum concurrent in-progress game fetches per instance |
| `LIVE_DAILY_BUDGET` | `9000` | Per-instance daily cap on live box-score fetches (a games-list call is never charged against it); sized against the verified Ultra quota (15,000 requests/day, soft-limited) with headroom held back for `STATRELAY_DAILY_BUDGET` |
| `LIVE_REPLAY_FIXTURE` | empty | Directory of a recorded game's play-by-play; when set, live scoring replays it instead of polling Tank01, and replaces the league schedule with the replay's one game |
| `LIVE_REPLAY_STEP` | `2s` | Wall-clock interval between replayed play-by-play frames |
| `LIVE_REPLAY_ALLOW_PRODUCTION` | `false` | Required alongside `LIVE_REPLAY_FIXTURE` to run a replay under a non-local `APP_ENV`; refused otherwise, since replay mode replaces the real schedule |
| `STATRELAY_DAILY_BUDGET` | `0` (unlimited) | `statrelay`'s own daily cap on relayed upstream Tank01 calls, shared by every league instance behind it; the tracked manifest sets `13000` against the verified Ultra quota — the real wallet guard, since RapidAPI bills overage instead of returning a 429 |
| `STATRELAY_BOX_LIVE_TTL` | `10s` | `statrelay`'s in-progress `getNFLBoxScore` cache TTL, aligned with `LIVE_SCOREBOARD_INTERVAL`'s default |
| `STATRELAY_SCOREBOARD_TTL` | `10s` | `statrelay`'s `getNFLGamesForWeek` cache TTL for the live regular-season query only; the preseason Blitz query keeps its own 24h TTL |
| `SCORING_FORMAT` | `half_ppr` | ADP type and projection scoring |
| `FANTASY_SYNC_INTERVAL` | `6h` | Pool refresh interval |
| `FANTASY_ROOT` | `data/fantasy` | Pool cache directory |
| `FANTASY_POOL_LIMIT` | scaled default: `teams × roster spots × 2.5`, clamped to `200–800` | Optional maximum pool size override |
| `AVATAR_ROOT` | `data/avatars` | Immutable avatar-object target; must remain strictly below `AVATAR_DURABLE_ROOT` |
| `AVATAR_DURABLE_ROOT` | `data` (`/app/data` in the container) | Pre-existing PVC/storage anchor. Avatar writes never create or fsync outside this directory; custom roots require an existing matching anchor |
| `AVATAR_DEFAULTS_ROOT` | `public/avatars/defaults` | Commissioner-supplied default tone badges; see [Default team badges](docs/avatar-default-badges.md) |
| `AVATAR_MOTIFS_ROOT` | `public/avatars/motifs` | Source art for generated badge motifs |
| `DATA_FILE` | `data/league-state.json` | Names the data directory, and the JSON state file to import once. The league database is `league.db` beside it |
| `WIRE_ENABLED` | `true` | Enable the public signal listener |
| `WIRE_FEEDS_ENABLED` | `true` | Enable the RSS/Atom source mesh |
| `WIRE_SOURCES_FILE` | embedded defaults | Replacement feed-source JSON |
| `WIRE_FEED_INTERVAL` | `2m` | Public-feed polling interval |
| `BLUESKY_HANDLES` / `BLUESKY_DIDS` | empty | Commissioner-approved identities |
| `WIRE_ROOT` | `data/signal-wire` | Private signal store |
| `WIRE_RECENT_LIMIT` | `1000` | Current signal window |
| `WIRE_REPLAY_WINDOW` | `24h` | Maximum timestamp-cursor replay window |
| `OPEN_STATS_ENABLED` | `true` | Enable nflverse mirroring |
| `NFL_SEASON` | current year | Season retained in normalized views |
| `OPEN_STATS_ROOT` | `data/open-stats` | Private open-data cache |
| `OPEN_STATS_SCHEDULE_INTERVAL` | `5m` | Schedule check interval |
| `OPEN_STATS_PLAYER_INTERVAL` | `6h` | Player-ledger check interval |
| `OPEN_STATS_PLAYER_PREV_INTERVAL` | `24h` | Previous-season player-ledger check interval |
| `OPEN_STATS_INJURY_INTERVAL` | `15m` | Injury-report check interval |
| `OPEN_STATS_TEAM_STATS_INTERVAL` | `6h` | Team-week defense/special-teams check interval |
| `OPEN_STATS_PBP_INTERVAL` | `6h` | Play-by-play check interval |
| `OPEN_STATS_MAX_DOWNLOAD_MB` | `128` | Per-download safety limit |
| `NFLVERSE_SCHEDULE_URL` / `NFLVERSE_PLAYER_STATS_URL` / `NFLVERSE_PLAYER_STATS_PREV_URL` / `NFLVERSE_INJURY_URL` / `NFLVERSE_TEAM_STATS_URL` / `NFLVERSE_PBP_URL` | nflverse's GitHub release assets | Point one or more assets at a league-controlled mirror |
| `DATA_API_TOKEN` | empty | Enables protected exports |

See [`.env.example`](.env.example) for every endpoint and reconnect override.

## Project shape

```text
app/                  GoSX routes, actions, and Signal Wire UI
internal/league/      seats, draft store, fixtures, and view models
internal/fantasy/     Tank01 client, pool sync, offline fallback pool
internal/wire/        RSS/Atom + Jetstream ingestion, trust policy, clustering, journal, and redaction
internal/openstats/   nflverse sync, parser, manifest, and normalized queries
public/               Neo-Retro visual system and browser enhancers
deploy/k8s/           single-replica Kubernetes manifests
docs/                 configuration, season operations, source policy, data contract, and release runbooks
```

The SQLite/WAL state store is deliberate for one private league and one application process. Keep one writer per league database. Running N leagues means running N isolated league instances and databases; Commissioner HQ federates read-only summaries and does not turn them into one shared transactional store.

## Documentation

Every file under `docs/`, indexed at [`docs/README.md`](docs/README.md):

| Document | Covers |
| --- | --- |
| [`docs/quickstart.md`](docs/quickstart.md) | Ten-minute Docker Compose deployment walkthrough |
| [`docs/configuration.md`](docs/configuration.md) | Every `league.json` field, boot states, and environment override |
| [`docs/season-operations.md`](docs/season-operations.md) | Draft night through week close, live scoring, and degraded-data operations |
| [`docs/launch-checklist.md`](docs/launch-checklist.md) | Kubernetes release, canary, and rollback runbook |
| [`docs/backup-restore.md`](docs/backup-restore.md) | Backup archive contents and the offline restore procedure |
| [`docs/data-pipeline.md`](docs/data-pipeline.md) | Signal Wire and open-stats mirror architecture |
| [`docs/sources.md`](docs/sources.md) | The accepted source mesh and PrizePicks/market-data policy |
| [`docs/design-spec.md`](docs/design-spec.md) | Visual-system tokens and the accessibility baseline |
| [`docs/avatar-default-badges.md`](docs/avatar-default-badges.md) | Default team-badge naming convention and fallback chain |
| [`docs/qa-1-acceptance-matrix.md`](docs/qa-1-acceptance-matrix.md) | The bounded QA-1 server-render acceptance matrix |
| [`docs/px1_manager-handbook.md`](docs/px1_manager-handbook.md) | Five-minute manager orientation and data-state guidance |
| [`docs/px1_commissioner-handbook.md`](docs/px1_commissioner-handbook.md) | Commissioner safe-operating loop and recovery links |
| [`docs/px1_operator-help-projection.md`](docs/px1_operator-help-projection.md) | How to verify the public `/help` corpus is safe to publish |
| [`docs/px1_help_corpus.md`](docs/px1_help_corpus.md) | The `/help` corpus contract: topics, search, and recovery guidance |
| [`docs/px1_glossary.md`](docs/px1_glossary.md) | A projection of the in-app glossary |
| [`docs/px1_concept-transition.md`](docs/px1_concept-transition.md) | A vocabulary map for managers migrating from another platform |
| [`docs/decisions/0001-seat-scoped-big-board.md`](docs/decisions/0001-seat-scoped-big-board.md) | Seat-scoped Big Board ownership decision record |

## Upstream references

- [Bluesky Firehose guide](https://docs.bsky.app/docs/advanced-guides/firehose)
- [Bluesky Jetstream design and tradeoffs](https://docs.bsky.app/blog/jetstream)
- [ESPN RSS information](https://www.espn.com/espn/news/story?page=rssinfo)
- [Mastodon RSS discovery](https://docs.joinmastodon.org/user/network/)
- [nflverse data repository and CC-BY-4.0 license](https://github.com/nflverse/nflverse-data)
- [nflverse schedule data documentation](https://nflreadr.nflverse.com/articles/nflverse_data_schedule.html)
- [nflverse injury-report documentation](https://nflreadr.nflverse.com/reference/load_injuries.html)
- [PrizePicks terms of service](https://www.prizepicks.com/help-center/terms-of-service)
- [Google OAuth for web-server applications](https://developers.google.com/identity/protocols/oauth2/web-server)

Read [`docs/sources.md`](docs/sources.md) before adding another upstream. The design deliberately rejects credential scraping and “free tier” services that can later hold the league’s history hostage.

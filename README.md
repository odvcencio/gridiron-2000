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
- An atomic local mirror of nflverse schedules, weekly injury reports, and corrected weekly player statistics under the CC-BY-4.0 license.
- A draft pool service with a swappable provider seam: an embedded 182-player offline pool with approximate ranks, or a live Tank01 pool with ADP, projections, bye weeks, injuries, and news.
- A seat-level Big Board at `/board`: the primary and co-manager share one private order, and the draft room/autopick surface that team's top available targets on the clock.
- A commissioner console at `/admin` (`COMMISSIONER_EMAILS`): runtime invites, seat release, and typed-confirmation draft or league resets.
- Honest empty states: seats show `UNCLAIMED` until a manager signs in, records start `0–0`, and rosters stay empty until picks are made.
- Same-origin league APIs plus token-protected JSON, NDJSON, and CSV exports for future applications.
- A complete demo experience while Google credentials and trusted social sources are being configured.
- A public /guide for managers arriving from another fantasy provider, with a five-minute start, commissioner checklist, draft controls, data states, and a manual migration checklist.
- An explicit [season operations handbook](docs/season-operations.md) for draft night, weekly lineup locks, waivers, trades, week close, degraded data, and fleet-scale commissioner operations.

There are no Sleeper, Genius Sports, sportsbook, PrizePicks, or NFL+ account integrations. No sports-data API key is required: without one the draft room runs on the embedded offline pool.

## Run locally

Requirements: Go 1.26 and GoSX v0.53.4.

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

### One person, multiple Google identities

Use `IDENTITY_ALIASES` when the same commissioner has more than one
permitted Google email:

```dotenv
COMMISSIONER_EMAILS=commissioner@example.com
IDENTITY_ALIASES=commissioner.alias@example.org=commissioner@example.com
```

Mappings are explicit, one-way, and fail closed on malformed, chained, or
ambiguous entries. Admission evaluates a provider email in this order:
existing persisted membership; the canonical identities in
`COMMISSIONER_EMAILS` and their explicit aliases; a raw configured domain;
a raw environment or stored invitation; and finally the open-after-sign-in
fallback only when both the domain and invitation sources are empty. This
keeps a colleague-domain gate truthful while ensuring a configured
commissioner—such as the commissioner's `m31labs.dev` identity and its explicit
Stable Kernel alias—can still sign in. Alias canonicalization is not a
general bypass for unrelated authorization. After admission, the canonical
identity is used for commissioner authorization, seat/team ownership,
co-manager bindings, Big Board, Pick'em, Blitz, notification preferences,
sessions, and audit attribution. On startup, existing internal state is
migrated idempotently; conflicting seats, roles, picks, or preferences stop
startup for review. Invite entries themselves remain raw policy records.

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

The app uses three nflverse release assets:

- `games.csv` for schedules and game-level scores, checked every five minutes with HTTP validators.
- `injuries_2026.csv` for weekly report and practice statuses, checked every 15 minutes.
- `stats_player_week_2026.csv` for corrected weekly player statistics and fantasy fields, checked every six hours.

Downloads go to an owner-only temporary file, are size-limited and parsed, then replace the prior cache atomically. A manifest retains row counts, SHA-256 hashes, ETags, timestamps, source URLs, and license information. An unavailable preseason player asset is a waiting state, not an application error.

This is intentionally a two-speed system:

```text
publisher + crowd feeds  -> minute-scale provisional alerts -> private Signal Wire
curated social + members -> seconds/human provisional alerts -> private Signal Wire
open nflverse files      -> slower corrected ledgers         -> scoring/reconciliation source
```

The first layer provides the “something just happened” experience without paying a real-time vendor. The second provides reusable structured history and powers schedule-backed fantasy matchups. During an open week, matchup totals are provisional calculations from the current effective lineups and mirrored weekly player statistics; this is **not** official, play-by-play, sub-minute scoring. When the commissioner closes a week, Gridiron records the matchup results and pins every team's effective starters for that week. Later drops, trades, or roster-shape edits cannot rewrite that closed result; a repeated close is an idempotent no-op. The commissioner close remains explicit even when the NFL schedule and corrected-stat freshness checks say the week is ready.

## Private storage and exports

```text
data/
  league.db                    authoritative league state (SQLite)
  league.db.bak                rolling snapshot, written before every draft pick
  league-state.json.imported   the JSON state file the database was built from
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
| `COMMISSIONER_EMAILS` | empty | Canonical accounts allowed into `/admin` |
| `IDENTITY_ALIASES` | empty | Explicit `alias=canonical` mappings; a configured commissioner's aliases are admitted by the narrow commissioner exception, while unrelated identities still need raw policy |
| `COMMISSIONER_INSTANCE_ID` | `local` | Stable ID for this isolated league in Commissioner HQ |
| `COMMISSIONER_HQ_PEERS` | empty | Comma-separated `id=service-origin\|public-origin` entries for every other league this instance should display; fleet size is not capped |
| `COMMISSIONER_HQ_CONCURRENCY` | `8` | Simultaneous peer-summary reads, from 1–64; bounds resource use without limiting fleet size |
| `COMMISSIONER_HQ_TIMEOUT` | `1.5s` | Per-peer read timeout, greater than zero and at most 10 seconds |
| `TANK01_API_KEY` | empty | Direct upstream credential for a standalone/local process; in the tracked Kubernetes topology only `statrelay-secrets` owns it |
| `TANK01_BASE_URL` | empty | Override the provider base URL; point every Kubernetes league at the shared `statrelay` Service |
| `TANK01_HOST` | Tank01 NFL host | Swap for another Tank01 sport later |
| `SCORING_FORMAT` | `half_ppr` | ADP type and projection scoring |
| `FANTASY_SYNC_INTERVAL` | `6h` | Pool refresh interval |
| `FANTASY_ROOT` | `data/fantasy` | Pool cache directory |
| `FANTASY_POOL_LIMIT` | scaled default: `teams × roster spots × 2.5`, clamped to `200–800` | Optional maximum pool size override |
| `AVATAR_ROOT` | `data/avatars` | Immutable avatar-object target; must remain strictly below `AVATAR_DURABLE_ROOT` |
| `AVATAR_DURABLE_ROOT` | `data` (`/app/data` in the container) | Pre-existing PVC/storage anchor. Avatar writes never create or fsync outside this directory; custom roots require an existing matching anchor |
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
| `OPEN_STATS_INJURY_INTERVAL` | `15m` | Injury-report check interval |
| `OPEN_STATS_MAX_DOWNLOAD_MB` | `128` | Per-download safety limit |
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

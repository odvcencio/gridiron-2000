# Owner-operated football data pipeline

GRIDIRON 2000 separates discovery from authority. Publisher feeds, community feeds, curated social events, and league sightings provide provisional awareness; open structured files provide slower, corrected facts. Everything is normalized into storage operated by the league, and no paid sports-data account is required.

## Data flow

```text
ESPN / CBS RSS       Reddit / Mastodon feeds       curated Bluesky Jetstream
       \                       |                            /
        -> conditional fetch / allowlisted event ingestion
        -> local Arbiter relevance + provenance policies
        -> canonical-link clustering + corroboration count
        -> rewriteable current-state excerpts
        -> metadata-only daily audit journal
        -> authenticated league UI / token-protected export

signed-in manager form
  -> CSRF + field validation
  -> tip / shared-news / market evidence tier
  -> same classifier, clustering, storage, and UI path
  -> submitted URLs are recorded but never fetched

nflverse GitHub release assets
  -> conditional HTTP request (ETag / Last-Modified)
  -> owner-only, size-limited temporary file
  -> schema validation + season filter
  -> atomic CSV replacement + manifest
  -> normalized schedule, injury, and player-week queries
```

The lanes are deliberately not merged into one certainty level. A headline, post, community tip, or projection movement is always `provisional`; only a corrected structured record or commissioner ruling can become a scoring fact.

## Signal contract

Four RSS/Atom sources are enabled by default. The feed reader polls in parallel every two minutes, advertises only XML syndication media types, sends a descriptive user agent, honors HTTP validators, limits downloads, rejects non-HTTPS remote URLs, and ignores items older than 72 hours. A source-health record tracks its state, accepted/ignored counts, last check, last published item, and safe error text.

The optional Bluesky listener resolves configured handles to stable DIDs, then requests only `app.bsky.feed.post` commits for those identities. This avoids operating a full-network keyword scraper. A persistent cursor is supplied when reconnecting, bounded by `WIRE_REPLAY_WINDOW` for timestamp-style cursors.

The manager form accepts `community`, `submitted_news`, or `market` evidence. It stores a source label, optional HTTP(S) link, 480-character summary, reporter display name, and local timestamps. It never opens the submitted link, which keeps the application from becoming a crawler or server-side request proxy.

Classification is implemented by [`internal/wire/signal_rules.arb`](../internal/wire/signal_rules.arb). Built-in categories cover scoring plays, injuries, practice participation, inactive/availability states, role changes, roster moves, turnovers, kicking, weather, big plays, shared news, community tips, and market sightings. Noise is counted but not retained as text.

Provenance weighting is separately governed by [`internal/wire/trust_rules.arb`](../internal/wire/trust_rules.arb):

| Evidence | Tier | Weight |
| --- | --- | ---: |
| Corrected/official record | `CORRECTED RECORD` | 1.00 |
| Publisher feed | `PUBLISHER` | 0.88 |
| Curated social identity | `CURATED SOCIAL` | 0.76 |
| Community feed/tip | `COMMUNITY` | 0.62 |
| Manager-shared news | `COMMUNITY LINK` | 0.62 |
| Human market observation | `MARKET WATCH` | 0.40 |

The displayed confidence is the classifier match multiplied by provenance weight. It is a sorting/triage aid, not a calibrated truth score. Signals sharing the same canonical URL cluster together; the highest-confidence record represents the cluster, and distinct source URIs produce its corroboration count.

## Signal storage

```text
data/signal-wire/
  state.json
  events/
    2026-09-13.ndjson
```

Current state stores stable IDs, source and evidence type, source URI/URL, source or reporter name, short text, hashes, category, classification/trust rules, confidence, cluster ID, provisional flag, and timestamps. Replayed feed items or Bluesky commits with the same content ID do not inflate counts. Updated content replaces current state.

`state.json` is atomically replaceable and contains current excerpts. Daily NDJSON lines contain no post/article text, allowing a later Bluesky deletion to clear source text while retaining proof that a classifier decision occurred. Files and directories use owner-only permissions.

The journal is an audit trail, not a redistributed news or social archive. Future applications should consume `/api/data/signal-export.ndjson`, which exposes only current non-deleted records behind the private data token.

## Open-data mirror contract

The nflverse schedule asset is checked every five minutes, injury reports every 15 minutes, and the current-season weekly player-stat asset every six hours. HTTP 304 retains the cache. HTTP 404 becomes `awaiting_release`, which is expected before a season file exists. Other transport or schema failures preserve the last known-good CSV and record an error in the manifest.

A successful download must:

1. stay below `OPEN_STATS_MAX_DOWNLOAD_MB`;
2. reach disk in an owner-only temporary file;
3. fsync successfully;
4. contain required CSV columns;
5. parse into the configured season;
6. atomically replace the prior cache;
7. persist row count, byte count, SHA-256, HTTP validators, timestamps, URL, and license.

The injury view retains report/practice status and primary/secondary injuries by player, team, week, and season type. The player-week view retains identifiers, teams, game/week, passing, rushing, receiving, fumbles lost, and standard/PPR fantasy fields. Original CC-BY CSVs remain beside normalized in-memory views for future models and transparent reprocessing.

## Access boundaries

Signal status, recent events, and sighting submission require a signed-in league session in production. Demo mode permits local preview and a synthetic demo commissioner. Form actions use the existing encrypted session and CSRF middleware. Reusable schedules, injury reports, player ledgers, signal NDJSON, and raw CSV downloads require a constant-time-checked bearer token from `DATA_API_TOKEN`. CORS is not enabled.

Status responses omit local filesystem paths and secrets. The browser never receives the export token. The app stores no NFL+, PrizePicks, sportsbook, or publisher credentials.

## Freshness and failure modes

- Publisher/community feeds poll every two minutes; a publisher may still delay, edit, or remove an item.
- Jetstream is event-driven but has no league-specific service-level guarantee.
- The browser checks the private Signal Wire every 20 seconds while visible.
- A manager or reporter can be wrong; a relevant signal is never a scoring event.
- nflverse corrected data follows its own release cadence and may receive later corrections.
- A restart retains feed content IDs and the durable Bluesky cursor, preventing duplicate archive entries.
- A disconnected source records an error while existing local records remain readable.
- A malformed open-data replacement cannot overwrite the last known-good cache.

This provides useful minute-scale awareness, occasional second-scale social alerts, and reusable history. It does not claim vendor-grade official play-by-play or guaranteed sub-minute fantasy scoring.

## Scaling boundary

The filesystem implementation is appropriate for one eight-person league and one process. Before horizontal scaling, elect one ingestion leader, move state to transactional storage, constrain signal IDs with a unique key, and put exports behind a separate authenticated service. Keep discovery and corrected authority separate even if the storage changes.

## Attribution

The nflverse cache and normalized views retain CC-BY-4.0 attribution in status responses and CSV headers. See the [nflverse data repository](https://github.com/nflverse/nflverse-data). Publisher/community signals retain their source links; Bluesky events retain AT URI, DID, and public post URL; upstream social deletions are honored locally.

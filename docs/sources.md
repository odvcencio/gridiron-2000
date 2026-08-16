# Free-source policy

The league owns its application, configuration, derived indexes, and local copies. “Owner-operated” does not mean pretending upstream facts appeared from nowhere: every GRIDIRON 2000 signal carries its source, evidence type, trust rule, timestamps, and provisional status.

## Accepted source mesh

| Source | Cost / access | Freshness | Allowed role | Trust tier |
| --- | --- | --- | --- | --- |
| ESPN NFL RSS | Public publisher feed | Polled every two minutes | Relevant NFL headline alerts | `PUBLISHER` |
| CBS Sports NFL RSS | Public publisher feed | Polled every two minutes | Relevant NFL headline alerts | `PUBLISHER` |
| r/fantasyfootball Atom | Public community feed | Polled every two minutes | Broad crowd radar | `COMMUNITY` |
| Mastodon hashtag/account RSS | Public community feeds | Polled every two minutes | Broad or curated social radar | `COMMUNITY` or `CURATED SOCIAL` |
| Bluesky Jetstream | Public WebSocket; no account or API key | Event-driven; UI checks every 20 seconds | Alerts from commissioner-approved team, reporter, and weather identities | `CURATED SOCIAL` |
| League-manager sighting | Signed-in local form | Human speed | TV observations, local reports, shared articles, or market movement | `COMMUNITY`, `COMMUNITY LINK`, or `MARKET WATCH` |
| nflverse schedules | Public CC-BY-4.0 release | Checked every five minutes | Schedule and game-level records | Corrected-data lane |
| nflverse injuries | Public CC-BY-4.0 release | Checked every 15 minutes | Weekly report and practice statuses | Corrected-data lane |
| nflverse player stats | Public CC-BY-4.0 release | Checked every six hours | Corrected reusable player-week facts and fantasy fields | Corrected-data lane |
| Commissioner ruling | Local human decision | On demand | Resolve a remaining scoring/data dispute | Final league authority |

The two publisher feeds complement the two crowd feeds: publishers are narrower and more accountable; community feeds are faster at surfacing beat-report fragments, role changes, and anomalies. One source is never treated as proof. Exact canonical links are grouped, independent records are counted, and the strongest provenance becomes the displayed representative.

## Runtime source control

The default list is embedded in [`internal/wire/default_sources.json`](../internal/wire/default_sources.json). For a commissioner-owned list, copy [`config/wire-sources.example.json`](../config/wire-sources.example.json), edit it, and set `WIRE_SOURCES_FILE` to its absolute path. Only HTTPS RSS or Atom sources are accepted, with a maximum of 32 enabled feeds and four MiB per response by default.

For each source:

1. Prefer an official publisher feed, team account, or identity linked from a recognized employer/domain.
2. Use local beat writers for teams represented on league rosters instead of ingesting a whole social network.
3. Add team and player-specific Mastodon tags or accounts only when their signal-to-noise ratio is useful.
4. Keep community feeds as leads; open the linked source before changing a lineup.
5. Review the list each season and remove feeds that fail, drift topics, or become promotional spam.
6. Treat source confidence as provenance, never as probability that a claim is true.

Bluesky handles are convenience inputs. At startup they resolve to stable DIDs, which are the identities sent to Jetstream. RSS/Atom requests use ETag and Last-Modified when the publisher provides them.

## PrizePicks and similar platforms

PrizePicks can be useful as a **human observation surface**: a manager may notice that a player projection moved, select “Market sighting,” write a short summary, and optionally attach the page URL. That entry receives the lowest named trust tier and stays provisional.

The server does not sign into PrizePicks, reuse an NFL+ or PrizePicks cookie, call an undocumented endpoint, run a browser bot, or scrape an account page. PrizePicks’ published terms describe the service as personal/noncommercial and identify unauthorized scripts as improper conduct, so an automated adapter is intentionally outside this project. The same boundary applies to sportsbook and consumer fantasy accounts.

## Deliberate exclusions

- **NFL+ session polling:** consumer authentication is not a supported statistics API; no password, token, or browser cookie is requested or stored.
- **Automated PrizePicks/sportsbook collection:** market information enters only when a league manager reports what they saw.
- **Enterprise trials and quota traps:** no dormant paid-data SDK is present.
- **Whole-network social scraping:** Bluesky uses a small DID allowlist, while Mastodon/Reddit use explicit public feeds.
- **Social-derived scoring:** classifiers and corroboration counts never award points or silently rewrite structured records.
- **Unattributed redistribution:** the app retains short working excerpts for this private league and preserves links/provenance; it is not a news-republication service.

## Replacement seams

`WIRE_SOURCES_FILE` swaps the feed mesh without code changes. `BLUESKY_JETSTREAM_URL` can point to another compatible Jetstream instance. All three `NFLVERSE_*_URL` variables can point to a league-controlled mirror. These changes preserve the local schemas and API contracts, so later applications can reuse the archive without inheriting one vendor.

References: [ESPN RSS information](https://www.espn.com/espn/news/story?page=rssinfo), [Mastodon RSS feeds](https://docs.joinmastodon.org/user/network/), [Bluesky Firehose](https://docs.bsky.app/docs/advanced-guides/firehose), [Jetstream tradeoffs](https://docs.bsky.app/blog/jetstream), [nflverse data](https://github.com/nflverse/nflverse-data), [nflverse injury reports](https://nflreadr.nflverse.com/reference/load_injuries.html), and [PrizePicks terms](https://www.prizepicks.com/help-center/terms-of-service).

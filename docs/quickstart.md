# Run your league in ten minutes

This guide takes you from nothing to a running Gridiron 2000 league,
using Docker Compose. It covers a local trial with no domain, and a real
deployment with your own domain and automatic HTTPS.

For Kubernetes fleets, use [`deploy/k8s/`](../deploy/k8s) and
[`deploy/README.md`](../deploy/README.md) instead. That path is for
operators running several leagues behind one shared relay. This guide is
for one commissioner running one league.

## What Gridiron does not do

- No ads, and no third-party trackers.
- No scraping of other providers. The signal wire reads public RSS/Atom
  feeds and, optionally, Bluesky handles you name yourself.
- No data hostage. Your league lives in one SQLite file on your own
  volume. Export it at any time with the JSON, NDJSON, and CSV endpoints
  under `DATA_API_TOKEN` (see the root [`README.md`](../README.md)).

## Prerequisites

Pick one of these two paths before you start:

- **Local trial**: a machine with Docker installed. No domain needed.
- **Real deployment**: a VPS or home server with Docker installed, a
  public IP address, and a domain whose DNS A/AAAA record points at that
  server.

Either way, install [Docker Engine and the Compose plugin](https://docs.docker.com/engine/install/) first. Check both are present:

```bash
docker --version
docker compose version
```

## 1. Get the compose files

Clone the repository, then move into the compose directory:

```bash
git clone https://github.com/odvcencio/gridiron-2000.git
cd gridiron-2000/deploy/compose
```

Copy the example settings file and open it in an editor:

```bash
cp .env.example .env
```

`deploy/compose/.env.example` holds the minimal first-run settings, with
plain-language comments. For the complete variable reference, see
[`docs/configuration.md`](configuration.md) and the root
[`.env.example`](../.env.example).

## 2. Generate a session secret

Every deployment needs a random `SESSION_SECRET`. Generate one and paste
it into `.env`:

```bash
openssl rand -base64 48
```

## 3. Decide: trial now, or a real domain

### Option A: try it now, no domain

Leave `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` empty in `.env`, and
start the localhost profile:

```bash
docker compose --profile localhost up -d
```

Open [http://localhost:8080](http://localhost:8080). With no Google
credentials configured, the app runs in **demo mode**: every visitor gets
an open session with commissioner powers, and there is no sign-in wall.
That is expected, and it is fine for a private trial on your own
machine. Demo mode is never fine for anything another person can reach.
Finish option B before you invite real managers.

Skip to [step 5](#5-first-sign-in-and-seat-claiming) once the app is up.

### Option B: run it for real, with your own domain

Point your domain's DNS A (and AAAA, if you have an IPv6 address) record
at your server first, then edit `.env`:

```dotenv
DOMAIN=league.example.com
APP_ENV=production
DEMO_MODE=false
GOOGLE_REDIRECT_URL=https://league.example.com/auth/google/callback
COMMISSIONER_EMAILS=commissioner@example.com
LEAGUE_ALLOWED_EMAILS=alex@example.com,maya@example.com
```

Setting `APP_ENV=production` disables demo mode outright, even if
`GOOGLE_CLIENT_ID` stays empty. Finish step 4 first, or sign-in will not
work at all once you start this profile:

```bash
docker compose --profile domain up -d
```

Caddy requests and renews your Let's Encrypt certificate automatically.
Ports 80 and 443 must be open on your server's firewall.

## 4. Register a Google OAuth client

1. Open the [Google Cloud Console credentials page](https://console.cloud.google.com/apis/credentials).
2. Create an OAuth client with application type **Web application**.
3. Add exactly one authorized redirect URI, matching the path you chose in step 3:
   - Local trial: `http://localhost:8080/auth/google/callback`
   - Real domain: `https://league.example.com/auth/google/callback`
4. Copy the client ID and client secret into `GOOGLE_CLIENT_ID` and
   `GOOGLE_CLIENT_SECRET` in `.env`.
5. Restart the stack so the new values take effect:

   ```bash
   docker compose --profile domain up -d
   ```

## 5. First sign-in and seat claiming

1. Open the app in a browser.
2. Sign in with a Google account listed in `LEAGUE_ALLOWED_EMAILS` or
   `COMMISSIONER_EMAILS`.
3. Follow the **Claim your seat** prompt to pick a team name and badge.
4. Repeat sign-in and seat claiming for each manager.

The commissioner account reaches `/admin` for runtime invites, seat
release, and draft or league resets.

## Where to go next

- [`docs/configuration.md`](configuration.md) — every `league.json`
  field, including teams, draft date, roster shape, and waivers.
- [`docs/season-operations.md`](season-operations.md) — draft night,
  weekly lineup locks, waivers, trades, and week close.
- `/guide` inside the running app — a five-minute manager orientation
  and migration checklist for managers arriving from another provider.

## Building from source instead

Self-hosters who do not want to pull the published image can build it
from this checkout. Set a plain local tag in `.env` first — Docker
refuses to tag a build with the published digest:

```dotenv
GRIDIRON_IMAGE=gridiron-2000:local
```

Then build before starting either profile:

```bash
docker compose build app
docker compose --profile localhost up -d
```

## Optional: a shared Tank01 relay

Running more than one league on the same host? A shared `statrelay`
service lets every league share one Tank01 API key and quota, instead of
each league metering its own. Add these two lines to `.env`, then start
the relay profile alongside whichever profile you use above:

```dotenv
TANK01_API_KEY=your-rapidapi-key
TANK01_BASE_URL=http://statrelay:8090
```

```bash
docker compose --profile relay --profile domain up -d
```

Without a relay, the draft room still works: it falls back to an
embedded offline player pool with approximate ranks.

## Other ways to run it

- **Fly.io**: [`deploy/fly/fly.toml`](../deploy/fly/fly.toml) is a
  starting template — a data volume, a single instance, and forced
  HTTPS. Edit the `app` name and `GOOGLE_REDIRECT_URL`, then follow the
  `fly launch` / `fly secrets set` / `fly deploy` commands in the file's
  own comments. This repository does not create a Fly account or deploy
  anything for you.
- **Railway**: Railway builds directly from this repository's
  `Dockerfile`. No extra template file is needed. In the Railway
  dashboard:

  1. Create a new project from this repository.
  2. Set the same `.env` variables from step 3 as service variables.
  3. Attach a persistent volume at `/app/data`.
  4. Set the service's public port to `8080`.

  Railway issues HTTPS on its own domain automatically. Point a custom
  domain at it from the Railway dashboard if you want one.

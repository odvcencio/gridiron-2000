# Contributing to Gridiron 2000

Gridiron 2000 is a private, self-hostable fantasy-football league app built
with GoSX. This guide covers local development. For running a league in
production, see [`docs/quickstart.md`](docs/quickstart.md).

## Run it locally

Requirements: Go 1.26 and GoSX v0.53.11-0.20260903011141-48af3189fe1f
(see [`go.mod`](go.mod)).

```bash
cp .env.example .env
go run .
```

Open [http://localhost:8080](http://localhost:8080). With no Google OAuth
credentials configured, the app runs in demo mode: every visitor gets an
open session with commissioner powers.

Two environment variables matter most for local work:

- `DEMO_MODE` — set to `false` to require real sign-in, or leave unset for
  the open demo session. See the root
  [`README.md`](README.md#run-locally) for the full local `.env` example.
- `LEAGUE_FILE` — points the strict config loader at a `league.json` other
  than the default lookup path. See
  [`docs/configuration.md`](docs/configuration.md) for the complete file
  lookup order and every supported field.

For the full ten-minute self-host walkthrough, including Docker Compose
and Google OAuth client registration, follow
[`docs/quickstart.md`](docs/quickstart.md).

## Build the client runtime

The app template runtime (`app/*.gsx`) compiles through the pinned GoSX
CLI, not `go build` alone. Install the pinned version and build a dev
bundle before running browser tests:

```bash
go install m31labs.dev/gosx/cmd/gosx@v0.53.11-0.20260903011141-48af3189fe1f
gosx build --dev .
```

Remove the build output when you are done, so it does not linger in the
checkout (`dist/` is already git-ignored):

```bash
rm -rf dist
```

## Test

Run the full suite:

```bash
go test ./...
```

Most tests need no browser and no built client runtime. A smaller set of
browser tests drives headed Chrome or Chromium through `chromedp` (see
`chromePath` in
[`sim_browser_test.go`](sim_browser_test.go)). They:

- Skip automatically when no `google-chrome`, `google-chrome-stable`,
  `chromium`, or `chromium-browser` binary is on `PATH`.
- Skip automatically when `dist/build.json` is missing, unless
  `GOSX_APP_ROOT` is set in your own shell environment — in that case a
  missing build fails loudly instead of skipping, since that combination
  means a release gate ran without building the client first.
- Skip under `go test -short ./...`, along with the longer simulated-draft
  scenarios (see `wave6_browser_helpers_test.go` and the other
  `*_browser_test.go` files).

Run `gosx build --dev .` first (see above) if you want the browser suite
to run rather than skip.

Other useful checks, from the root [`README.md`](README.md#run-locally):

```bash
arbiter check internal/wire/signal_rules.arb
gosx check app/wire/page.gsx
gofmt -l .
go vet ./...
```

## The test harness

Local and CI tests reach a running instance through a harness-only route
surface, mounted only when the process starts with `GRIDIRON_TEST_AUTH=1`
and a local `APP_ENV` (see `AppConfig.validate` in
[`app_build.go`](app_build.go); this combination is refused outside a
local environment, so a leaked flag cannot open a live league). The
harness adds:

- `/test/signin` — signs a browser in as a named test manager.
- `/test/clock` — overrides the process-wide league clock for a
  deterministic test.
- `/test/draft` and `/test/live` — read draft and live-poller state.

Every harness route answers `GET` only and is rejected outside a loopback
request (see `testRoutesLoopbackOnly` in
[`test_routes.go`](test_routes.go)). See
[`cmd/gridiron-sim`](cmd/gridiron-sim) for a rehearsal-draft client built
on this same surface.

## The product experience contract

Gridiron 2000's `app/` pages hold to a small set of rules, enforced by
render and contract tests throughout `app/`:

- **Truthful state.** A page never claims a fact the data does not
  support: an unpublished draft date says so plainly instead of showing a
  placeholder as if it were real, and a live indicator reflects the
  poller's authoritative state, not an optimistic guess.
- **Plain language first.** Manager- and commissioner-facing copy uses
  league nouns, not Go field names or implementation detail.
- **Adjacent disabled reasons.** A disabled control carries its reason
  next to it (for example "Already first" or "Locked"), not only in a
  title attribute.
- **League-local time.** Every stored instant renders in the league's own
  configured timezone, with a relative label, not a hard-coded zone.
- **Return-path preservation.** A sign-in, action, or form submission
  returns the visitor to the page they started from, validated against
  `internal/navigation`'s same-origin allow-list.

## Prose standard

Write commit messages, this file, and any other documentation prose in
[ASD-STE100](https://www.asd-ste100.org/) style: active voice, the
imperative mood for instructions, and one meaning per word. Keep a
procedural sentence at or under 20 words, and a descriptive sentence at
or under 25.

## Commit hygiene

- Use a conventional subject: `type(scope): Subject`, for example
  `fix(draft): Guard commissioner controls against stale actions`.
- Do not add a `Co-Authored-By` line or a "Generated with" attribution
  trailer.
- Keep a commit's description to why the change exists, not a restatement
  of the diff.

## Where decisions live

Durable product and architecture decisions are versioned under
[`docs/decisions/`](docs/decisions/), for example
[Decision 0001](docs/decisions/0001-seat-scoped-big-board.md) on
seat-scoped Big Board ownership. Record a decision there when a change
affects behavior a future contributor could reasonably assume differently.

# Contributing to Gridiron 2000

Gridiron 2000 is a private, self-hostable fantasy-football league app built
with GoSX. This guide covers local development. For running a league in
production, see [`docs/quickstart.md`](docs/quickstart.md).

## Run it locally

Requirements: Go 1.26 and GoSX v0.57.1
(see [`go.mod`](go.mod)).

```bash
cp .env.example .env
go run .
```

Open [http://localhost:8080](http://localhost:8080). `.env.example` ships
`DEMO_MODE=false`, so this first run lands on the tokenized `/setup`
wizard, not an open session.

Two environment variables matter most for local work:

- `DEMO_MODE` — an explicit, local-only opt-in for an open session with
  commissioner powers against a configured reference league. Set it to
  `true` together with a local `APP_ENV` (`development`, `local`, or
  `test`; `.env.example`'s default) and `LEAGUE_FILE=config/league.json.example`.
  `DEMO_MODE=true` alone does not bypass first-boot `/setup`, and it is
  refused outside a local `APP_ENV`. See the root
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
go install m31labs.dev/gosx/cmd/gosx@v0.57.1
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
scripts/go-check.sh test -count=1
scripts/go-check.sh vet
```

The harness derives its sorted package set from tracked `.go` files and calls
`go list` once per directory, including underscore-prefixed route packages
that Go's recursive wildcard pattern omits. Test and vet flags after the
subcommand are forwarded to the corresponding Go tool.

Most tests need no browser and no built client runtime, and `scripts/go-check.sh
test -count=1` above already covers all of them. A separate set of browser
tests drives headed Chrome or Chromium through `chromedp` (see `chromePath`
in [`sim_browser_test.go`](sim_browser_test.go)). They sit behind the `e2e`
build tag (`//go:build e2e`, every `*_browser_test.go` file plus its
chromedp-only helpers), so the command above never builds or runs them —
run them explicitly instead:

```bash
scripts/go-check.sh test -tags e2e -count=1
```

With that tag set, the browser suite still:

- Skips automatically when no `google-chrome`, `google-chrome-stable`,
  `chromium`, or `chromium-browser` binary is on `PATH`.
- Skips automatically when `dist/build.json` is missing, unless
  `GOSX_APP_ROOT` is set in your own shell environment — in that case a
  missing build fails loudly instead of skipping, since that combination
  means a release gate ran without building the client first.
- Shortens the longer simulated-draft scenarios under `-short` (see
  `wave6_browser_helpers_test.go` and the other `*_browser_test.go` files).

Run `gosx build --dev .` first (see above) if you want the browser suite
to run rather than skip.

Other useful checks, from the root [`README.md`](README.md#run-locally):

```bash
arbiter check internal/wire/signal_rules.arb
gosx check app/wire/page.gsx
gofmt -l .
scripts/go-check.sh vet
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

Write plainly: lead with the point, use common words and the active voice, keep each term consistent, back claims with evidence (numbers, links, test output), and say what you did not verify. M31 agents: see decision 0012 and the `writing-plainly` skill in hypha://m31labs/hyphae.

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

# Security Policy

## Supported versions

Gridiron 2000 supports the latest release only. Self-hosters should track
the current image or checkout; see [`CHANGELOG.md`](CHANGELOG.md) for the
release history.

## Reporting a vulnerability

Report a vulnerability through a private
[GitHub security advisory](https://github.com/odvcencio/gridiron-2000/security/advisories/new)
on this repository. Do not open a public issue for an unpatched
vulnerability. This project has no separate public security contact
address at this time.

## What the app does for you

Verified from the code, not a claim about any deployment you run
yourself:

- **Content Security Policy.** `script-src 'self' 'nonce-{nonce}'
  'strict-dynamic' 'wasm-unsafe-eval'`, with `object-src 'none'` and
  `frame-ancestors 'none'` (see `gridironSecurityPolicy` in
  [`main.go`](main.go)).
- **CSRF protection.** Every unsafe-method request needs a matching CSRF
  token (`session.Manager.Protect`); a rejection shows a plain
  "session expired" page instead of a bare `403`.
- **Encrypted, HTTP-only sessions.** Session state lives in an encrypted,
  HTTP-only cookie, keyed by `SESSION_SECRET`.
- **Google OAuth with PKCE.** Sign-in is Google OAuth with PKCE
  (proof key for code exchange); there is no local-password path.
- **Secrets stay in configuration, not code.** Credentials come from
  environment variables or, for a Kubernetes fleet, `Secret` objects (see
  [`deploy/k8s/`](deploy/k8s)). A shared-relay deployment keeps
  `TANK01_API_KEY` only in the `statrelay` Secret; no league instance's
  own namespace holds it.
- **A commissioner audit trail.** Every admin mutation is recorded as an
  append-only, person-attributed `CommissionerEvent` (see
  `internal/league/model.go`), naming who acted, not only what changed.
- **No caching of authenticated content.** Authenticated pages and live
  fragments answer with `Cache-Control: private, no-store`.

## The test harness

A harness-only route surface (`/test/signin`, `/test/clock`,
`/test/draft`, `/test/live`) exists for local and CI evidence. It mounts
only when the process starts with `GRIDIRON_TEST_AUTH=1` **and** a local
`APP_ENV`; that combination is refused outside a local environment (see
`AppConfig.validate` in [`app_build.go`](app_build.go)), and every
harness route also rejects a non-loopback request (see
`testRoutesLoopbackOnly` in [`test_routes.go`](test_routes.go)). A
production deployment sets neither flag.

## Scope

This policy covers the Gridiron 2000 application in this repository. It
does not cover Google's OAuth infrastructure, the Tank01/RapidAPI
upstream, or a third-party host you deploy to.

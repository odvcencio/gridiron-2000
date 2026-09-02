# Changelog

All notable changes to Gridiron 2000 are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

No changes yet.

## [release-2026.09.02-ee12ed7-wave6] — 2026-09-02

Scope: a cumulative wave 1-6 UX and accessibility audit pass across every
league page, plus a free-tier live-scoring profile. Rolling to the flagship
now.

### Added
- A four-slot mobile bottom tab bar (Home, Team, Matchups, More) for signed-in managers.
- A shared rail attention chip and a login seat meter, both readable as text, not color alone.
- A commissioner event audit trail that attributes every admin mutation to the person who acted.
- A free-tier `LIVE_PROFILE=free` bundle sized for Tank01's free quota.
- ARIA table semantics on the starter comparison grids, for screen-reader users.

### Changed
- Review-confirm gates now cover clearing the Big Board, removing a Locker Room post, and declining a trade, matching the existing drop and trade-accept gates.
- Danger-zone reset copy uses league nouns instead of Go field names, and states plainly that only a backup restores the data.
- Every league page carries a plain sentence-case heading naming the page, replacing a slogan.
- `relativeTime` now reads a still-future instant as "in N minutes" instead of "just now".
- Sign-out on `/login` is a native page navigation again, so the signed-out state always shows.

### Fixed
- A completed draft's live pick counter no longer exceeds the final pick.
- A CSRF rejection now shows a plain "session expired" recovery page instead of a bare 403.
- A blank team-name submission shows a plain-language error instead of silently resetting the name.
- Team abbreviation normalization was restored for LAR/WSH/JAC, fixing their kickoff clock and waiver lock.
- The attention chip no longer names trades or Pick'em games to a signed-out demo-league visitor.

## [release-2026.09.01-3f78a44-adoption-wave] — 2026-09-01

Scope: the first-boot setup wizard, backup and restore, and a one-command
self-host deploy path.

### Added
- A first-boot setup wizard: draft engine, HTTP surface, review-then-commit flow, and hybrid restart.
- A boot state machine with three truthful outcomes: configured, setup, or a fail-closed operator-error page.
- Tier 0 invite-link sign-in.
- One-click league backup, nightly snapshots, and an offline restore path.
- A one-command self-host deploy for Docker Compose and Fly.io.

### Changed
- The session cookie's max age rose to 180 days.
- `/api/health` reports the configured boot state.

## [release-2026.09.01-460d658-gap-closure-wave1] — 2026-09-01

Scope: the three-layer live-scoring engine, the draft war room, the sim and
replay harness, and gap-closure fixes across scoring, waivers, and punters.

### Added
- A three-layer live-scoring design: a scoreboard tick gates a change-triggered box-score fetch, with a wire-signal trigger for fast plays.
- A draft war room: shell panes, a tape cursor, queue reorder, round-grouped pick history, and a CSV ledger export.
- The Locker Room league board.
- House rank VORP ranking beside market ADP, and an autopick guard against scarce-position starvation.
- Punter rankings from the league's embedded prior-season rescoring.
- A harness-only `/test/*` route surface and a draft rehearsal command for browser evidence.

### Fixed
- Four gap-closure items in scoring, projection, and image sizing.
- The waiver penalty floor, deferral expiry, and force-run wiring.
- Matchup scores and team lines now read honestly instead of guessing.
- The live poller's shutdown, ETag matching, and health reporting.

### Docs
- Documented the three-layer live-scoring design and the verified Tank01 quota.

## [release-2026.08.29-7fdd84f] — 2026-08-29

Scope: read-only commissioner live-operations dashboards, the postseason
bracket, and a public help center.

### Added
- Read-only live-operations dashboards for admins and commissioners, plus a scoped Trade Desk refresh region.
- A persisted postseason bracket lifecycle.
- A versioned help center with a searchable topic corpus.
- The QA-1 acceptance matrix harness for release evidence.

### Fixed
- The draft live room is event-driven instead of polling.
- Open seats are excluded from draft readiness status.
- Console and matchup layouts no longer overflow their container.

## [release-2026.08.25-36719b3] — 2026-08-25

Scope: avatar upload limits.

### Changed
- Raised the avatar upload size limit, resolution, and concurrency controls.

## [release-2026.08.25-82c8d8c] / [release-2026.08.25-90c8935] — 2026-08-25

Scope: two release names for the same commit. Draft readiness, roster
capacity, and admission-gate hardening.

### Added
- Explicit `SetReady` draft-seat readiness with claim validation.
- A roster-capacity breakdown by zone in the player pool.
- Native reorder controls for the seat board, preserving query context.

### Fixed
- Removed a false claim of automatic dynasty roster rollover.
- Waiver claims no longer resolve before their filed time during historical catch-up.
- Cross-week roster mutations are locked under store authority.

## [release-2026.08.24-ea3dcae] — 2026-08-24

Scope: the Commissioner HQ v1 federation and the fleet configuration
compiler.

### Added
- Commissioner HQ v1: a signed, bounded fleet-registry provider and consumer.
- A strict fleet configuration compiler (`fleetconfig`) with origin and image validation.
- `fleetgen`, which publishes deterministic, reviewable fleet bundles.

## [release-2026.08.24-7586755] — 2026-08-24

Scope: release provenance evidence, commissioner lineup intervention,
truthful live-state indicators, and a broad confirmation-gate pass.

### Added
- Schema-aware release compatibility evidence, shown as release provenance on commissioner fleet cards.
- Commissioner lineup intervention and authoritative starter ledgers on matchup scores.
- Private waiver receipts with claim reordering, and trade history with failure receipts.
- A stage-aware action center on the home dashboard and team terminal.
- A 44px mobile touch-target baseline across content controls.

### Changed
- Destructive admin actions now require explicit typed confirmation.
- Live indicators reflect authoritative state instead of an optimistic guess.
- Notification delivery status is reported accurately instead of assumed.

### Fixed
- Pick'em now treats an unavailable market as a void pick, not a broken submission.
- The live week API is protected behind league access.
- Scoring rejects non-finite point values and reverts a failed write.
- Trades close to new offers at the deadline but stay open to execute an accepted one.

## [release-2026.08.23-3d72967-lifecycle-ready] — 2026-08-23

Scope: draft lifecycle completion.

### Fixed
- The draft lifecycle now completes correctly and enforces roster-safe picks.

## [release-2026.08.23-e8cfcc4-responsive-sweep] — 2026-08-23

Scope: responsive layout fixes.

### Fixed
- Team and Pick'em layouts no longer overflow on narrow viewports.

## [release-2026.08.23-9178977-mobile-ready] — 2026-08-23

Scope: mobile masthead fix.

### Fixed
- The masthead no longer overflows beside the navigation rail.

## [release-2026.08.23-7e68ba5-mobile-ready] — 2026-08-23

Scope: draft check-in.

### Added
- A commissioner control to explicitly set a seat's draft readiness.

### Changed
- Clearer draft check-in guidance and state styling.

## [release-2026.08.22-91378de-season-ready] — 2026-08-22

Scope: draft order and commissioner switching.

### Added
- A commissioner league switcher in the admin console.

### Fixed
- The draft clock applies a NOT SEEN cap for an unresponsive seat.
- The draft order and schedule publish exactly once.

## [release-2026.08.22-3def224-season-ready] — 2026-08-22

Scope: the first tagged release. The core league app: draft room with a
live pick clock and autopick, scoring and playoffs, Pick'Em, waivers,
trades, notifications, and the commissioner console.

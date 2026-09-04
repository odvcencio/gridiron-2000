# Changelog

All notable changes to Gridiron 2000 are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed
- The Help Center glossary shows its 75 terms, definitions, and topic links instead of 75 blank cards linking to a 404.
- The Help Center's "coming from another app" table shows its nine rows instead of nine blank rows.

## [release-2026.09.04-b96bb85-gosx0552] — 2026-09-04

Scope: GoSX v0.55.2 adoption, plus a phone-first pass over every route (anonymous, manager, commissioner)
in the seated, live-draft, and season states, plus the stylesheet and font
delivery path. Rolled as revision 104; no schema change.

### Changed
- The framework is GoSX v0.55.2. Comments inside `.gsx` markup compile away on the released line instead of a maintenance prerelease, and soft navigation reconciles the body in place instead of replacing it.
- The three type families are self-hosted under `public/fonts` and preloaded; the stylesheet no longer imports fonts.googleapis.com, and the Content Security Policy drops both Google font hosts.
- The server serves `styles.css` with its source comments stripped and its font URLs content-addressed (about 39 KB gzipped instead of 135 KB); the `?v=` hash now covers the served bytes.
- The anonymous header reads "Guide" and "Sign in" on one row at phone width, and public pages no longer reserve space for a fixed bar they never render, so the landing's sign-in action sits in the first phone viewport.
- Mastheads on phones lead with less space above the eyebrow, drop the second-eyebrow reserve, and set their lede at body size, so each route's first state card lands in the first viewport.

### Fixed
- The browser harness drops one known chromedp log line about `@starting-style` telemetry, measures compact density at desktop width where the toggle applies, and reports where a replay-score stall happened.
- Draft-room pool rows at phone width show the full player name instead of three characters: the rank chip carries only the active-sort rank and the detail line moves under the name.
- The phone action bar shows on the same tier as the tab bar, so tablet widths between 609 px and 899 px no longer reserve 56 px of empty bottom padding.
- Compact density keeps the 13 px small-text floor on touch widths.
- The pre-draft room title reads "opens TBD" instead of "opens TBD ·".
- The trade desk's veto policy sentence reads at text size instead of scorebug size.
- The matchup status chip hides until a live state has text, instead of rendering an empty pill in the preseason.
- The dead `--rail-breakpoint` token is gone.

## [release-2026.09.03-1828a8e-sweep6] — 2026-09-03

Scope: the sixth sweep release — the re-audit residue on the manager loop and
the console. Rolled as revision 103; no schema change.

### Fixed
- /board's header row shares the row tracks and no longer widens the page on desktop.
- /matchups starter meta lines end cleanly on desktop; the win-probability bar is an accessible meter; the featured card's manager names wrap instead of clipping at 1440.
- /pickem's "Back to current week" sits on its own row within the phone viewport.
- /team's phone action bar no longer covers the stat strip on first paint; a DST row shows its house rank.
- /draft/results names the league in its masthead; /settings says "On · sends once email is configured".


## [release-2026.09.03-38f998b-sweep5] — 2026-09-03

Scope: the fifth sweep release — the draft room's residue from the re-audit.
Rolled as revision 102; no schema change.

### Fixed
- The draft room's pool search hides non-matching rows and reports the true count, and works as a plain form without JavaScript.
- The phone countdown shows the full time to the draft; the notice banner keeps one line with a Details disclosure.
- Before the draft starts, the live region, the grid's next-pick cell, and the commissioner drawer all say the draft has not started.
- The rank shows on phones as a chip before the name; the two ranks are separated; the RK header describes the active sort.
- Draft grid team headers ellipsize; the commissioner drawer reads in plain words.
- /players shows five rows per phone screen with the injury note in the stat tip; news icons are 44 px targets on every surface; /board rows share one height.
- A stylesheet merge that dropped a closing brace is guarded by a brace-balance test.


## [release-2026.09.03-577fd49-sweep4] — 2026-09-03

Scope: the fourth sweep release — the manager loop surfaces from the comb
audits, plus the draft room's pre-draft checklist as a disclosure that
never collapses the pane grid. Rolled as revision 100; no schema change.

### Fixed
- /trades partner chips and headings use the league's real team names.
- /matchups shows each side's projection and win probability before kickoff instead of dashes; the ledger reads in plain words with one freshness clause.
- /team rows carry the news tip and house rank like the other pool surfaces, one schedule line per row, and the VIEW MATCHUP button sits under the stat strip instead of inside its scroller.
- Action Center chips read "On track" / "Needs you"; the trade veto policy is a sentence; trade composer options meet the 44 px floor; trade sections number sequentially.
- The draft room's pre-draft checklist is a closed-by-default disclosure whose open state overlays the panes; the pane grid keeps one geometry in both states at every width.
- Personal names removed from two source comments (privacy contract).


## [release-2026.09.03-4cf0542-sweep3] — 2026-09-03

Scope: the third sweep release — the commissioner and results surfaces from
the comb audits. Rolled as revision 97; no schema change.

### Fixed
- /draft/results renders the full app shell and the league's identity for signed-in members instead of the anonymous bar with a blank masthead; an unknown `?team=` code says so.
- /admin: pending invites exclude people who already hold a seat; the draft date and seat presence read as words, not raw values; the invite preview wraps instead of widening the page; the draft-night runbook marks each step done, next, or later from the league's real state.
- /admin and /commissioner report one pool-coverage figure; the commissioner page names each seat's team.
- /help: the mapping table keeps its headers on phones, the source hash is short, and topic mastheads wrap.
- Anonymous header links meet the 44 px floor; a failed avatar image no longer paints its alt text over its neighbours.


## [release-2026.09.03-e1baaa1-sweep2] — 2026-09-03

Scope: the second sweep release, from the fine-toothed-comb audits run on
faithful copies of the live league. Rolled as revision 96; no schema change.

### Fixed
- /activity's team filter lists the league's real team names, and an unknown team code says so instead of filtering silently.
- The attention chip counts only the pick'em games the viewer has not called, so it no longer shows "1 URGENT" to a manager who has picked every game.
- /scoring's jump strip is a single opaque row on phones and at most two rows on desktop; /pickem's Prev and Next stay pinned at the strip's edges.
- /blitz no longer prints empty champion labels; /settings says when email delivery is not configured; /wire says "player stat ledger" and its status dot renders.
- /players collapses its filter rail to one row on phones (seven rows visible instead of two) and, with /board, gains column headers and a legend for RK and H.
- The desktop rail fits every link at 1440×900 and 1280×800; the home status line and the rail footer wrap instead of clipping.
- Player and pool counts pluralize correctly.


## [release-2026.09.03-19e370f-sweep1] — 2026-09-03

Scope: the first release of the whole-repo sweep before the Sunday draft,
verified on faithful copies of the live league. Rolled as revision 95; no
schema change.

### Fixed
- Draft room: the pool is a real table with aligned headers and a fixed info column; the pre-draft checklist no longer hides the pool; position chips include punters and reflect the active filter; VS ADP is hidden before pick 1; the pool orders by house rank (superflex value) by default with an ADP toggle; the phone pill no longer overflows or shrinks the page; the collapsed rail no longer overprints the command bar; pre-draft copy says the draft has not started and shows the start control on phones.
- The player pool is frozen while a draft is in progress, and a resync can never drop a rostered player.
- Team defenses have projections and house ranks; manual picks apply the same scarcity guard as autopick; the projection request sends the league's scoring values.
- /players and /activity render one region for the page and the 4-second refresh, so the drop confirmation survives; the matchup stat query pages past the 1,000-row cap.
- Big Board rows no longer collapse player names beside the news icon.
- Simulator: an existing-seats rehearsal mode and a punter fallback in the bot.
- Code health: dead symbols removed, comment drift fixed, `.claude/` ignored, `/favicon.ico` served, /admin and the session-expired page have headings.


## [release-2026.09.03-eaa98d1-draftweek] — 2026-09-03

Scope: draft-week fixes from the commissioner's own use of the live site.
Rolled to the flagship as revision 94; no schema change.

### Fixed
- Punters are in the live draft pool again: the pool keeps a per-position floor (teams × slots + 4) that the ADP cut cannot remove, so a roster with a P slot can always fill it.
- A custom team avatar stored with a writable file mode no longer returns 404: the read path repairs the mode after a hash match, and a boot sweep repairs the whole store.
- A player's news headline no longer stretches Big Board, player pool, and draft rows: the row detail stays one line and the headline opens from a newspaper icon with its own detail panel.
- A managed save keeps your scroll position: runtime requests drop the section anchor from the redirect, native form posts keep it.
- Developer comments inside `.gsx` markup no longer render as page text (GoSX v0.53.11, pinned by pseudo-version).
- Two tests that depended on the wall clock or on an incidental element count are deterministic.


## [release-2026.09.02-1d2b7e4-wave7] — 2026-09-02

Scope: draft results and roster clarity, plus the mobile pass across every
page. Rolled to the flagship as revision 93; no schema change.

### Added
- `/draft/results`: the draft by team (viewer first), by round, and as a grid with sticky headers; vs-ADP on every pick; a CSV link; a home card and a nav entry after the draft completes.
- Draft round and pick (`R3 · P28`) on /players rows, the activity feed, and every /team roster row; a "Your draft class" callout on /team.
- /team: bench grouped by position with sticky headers, a positional depth line, visible FLEX eligibility, kickoff and bye on every row before lock.
- A phone action bar that keeps each page's primary verb under the thumb (/team submits SET BEST LINEUP from it); a web app manifest and home-screen icons.

### Changed
- The 44 px touch floor and 16 px inputs now key on `pointer: coarse` and `hover: none`, so landscape phones keep them; press feedback on touch; toasts anchor above the tab bar.
- The draft room's command bar collapses to a 56 px "on the clock" pill on phones with a bottom sheet for sound, League, autopick, and force-pick; the Draft grid tab is reachable on phones with "Jump to my picks".
- /matchups shows scores first on game day with a sticky week strip; /players, /activity, and /board keep search and filters sticky; /wire collapses long feeds; long documents and the console get sticky section strips.
- `viewport-fit=cover` with safe-area padding on every fixed bar; `100dvh`; the scanline overlay is off on touch devices.

### Fixed
- Sticky round and team headers on both draft grids no longer drift off-screen.
- /players no longer shows a finished draft's clock panel above the pool; the notice stack collapses on phones.
- Keyboard hints (`enterkeyhint`, `inputmode`) and email autocomplete on forms; empty `title` attributes removed; the sign-in page's heading is in the first viewport.


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

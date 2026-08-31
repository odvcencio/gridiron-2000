# Season operations handbook

This handbook describes how a Gridiron league moves from configuration to draft night and through a regular-season week. It is for commissioners and managers. It describes the product's current behavior; it is not a promise that Gridiron copies another provider's workflow.

Use these sources of truth in this order:

1. The live `/scoring` page for league rules, roster shape, waiver mode, trade policy, schedule, and locks.
2. `/admin` for commissioner-owned operational state and explicit mutations.
3. `/commissioner` for a read-only view across configured league instances.
4. `/api/health` for build, provider, open-data, and state-schema diagnostics.
5. This handbook for the operating sequence and recovery decisions.

If this handbook and the live league disagree, the live configured state wins.

For the short, searchable answers use the [Help center](/help). Managers can
start with the [manager projection](manager-handbook.md); commissioners can
start with the [commissioner projection](commissioner-handbook.md). The
interactive topics link back here for restart, correction, and week-close
recovery detail.

## State vocabulary

Gridiron exposes state instead of hiding it behind generic success copy.

| State | Meaning | What to do |
| --- | --- | --- |
| `UNCLAIMED` | No manager owns that franchise seat. | The invited manager claims it at `/join`, or the commissioner removes it before draft-order randomization. |
| `NOT READY` | A claimed seat has not checked in for the draft. | Build the Big Board, confirm the draft tab, then mark ready. Readiness never starts the draft. |
| `SCHEDULED` | A draft meeting exists, but the commissioner has not started it. | Wait for the commissioner to type `START`. |
| `LIVE` | The draft or current data source is actively available, depending on context. | Read the adjacent label; draft-live and provider-live are separate states. |
| `CACHE` | The last successful player-pool snapshot is serving after a later refresh could not replace it. | Drafting may continue if capacity is covered and the commissioner accepts the snapshot age. |
| `OFFLINE` | The embedded fallback player pool is serving approximate ranks. | Use for rehearsal or proceed only after the commissioner explicitly accepts the limitation. |
| `AWAITING_RELEASE` | An upstream season file does not exist yet. | Wait for the publisher; do not interpret it as a zero or an application failure. |
| `READY TO CLOSE` | Every known real game is final and the player ledger satisfies the close gate. | The commissioner may perform the normal week close. |
| `FINAL` | A week has been closed and its effective starters/results are pinned. | Later roster movement cannot rewrite that result. Repeating close is a no-op. |

## Before managers arrive

The commissioner performs this once for each isolated league instance.

1. Configure `league.json`. Confirm league name, mode, timezone, season, team IDs, roster shape, draft rounds, waiver mode, and trade policy. Team IDs are durable; do not rename IDs after state refers to them.
2. Open `/scoring` and read the rendered rules as a manager would. This catches a valid configuration that still expresses the wrong league.
3. Confirm Google OAuth, COMMISSIONER_EMAILS, admission policy, and any IDENTITY_ALIASES. An identity alias unifies one person's internal ownership; a configured commissioner's explicit alias is admitted by the narrow commissioner exception, while unrelated aliases still need raw league policy.
4. Confirm `/api/health` reports the expected league configuration source, release identity, player-pool mode, player count, roster capacity, no pool error, and a `stateSchema` object whose logical `persistedVersion` is no greater than `supportedVersion`, whose physical `persistedDatabaseVersion` is no greater than `supportedDatabaseVersion`, and whose `compatible` flag is true.
5. Add manager invitations. A domain-gated league admits identities in the configured domain; use explicit invitations for permitted people outside it.
6. Ask every manager to claim the intended franchise before draft order is finalized.
7. At the draft-order milestone, one draw publishes both the final order and the default 14-week regular-season schedule, then sends one notification batch. Use the separate schedule control beforehand only when the league needs a custom span or first NFL week.

For a multi-instance Kubernetes topology, every league can use the shared `statrelay`. Only `statrelay-secrets` owns `TANK01_API_KEY`; each league sets `TANK01_BASE_URL` to the relay Service. The tracked flagship and Stable Kernel manifests are the current two-instance example, not a fleet-size limit.

## Manager five-minute setup

1. Follow the invite link and sign in with the invited Google identity.
2. Claim the correct franchise at `/join`. Signing in and owning a team seat are separate states.
3. Open `/scoring` and read the active league's roster, scoring, waivers, trades, and lock rules.
4. Open `/team`; set the franchise name and visual identity if the league permits it.
5. Build a ranked, deep Big Board at `/board`. Primary and co-manager share the seat-level board; the lifecycle, legacy migration, and rollback contract is [Decision 0001](decisions/0001-seat-scoped-big-board.md).
6. Open `/draft`, choose the truthful ready and autopick states, and leave the room available on draft night.

A custom PNG or JPEG up to 10 MB becomes the franchise identity. The image is center-cropped and resized to a 512×512 PNG with metadata removed, and any claimed stock badge is released so another team may claim it.

## Draft-night runbook

The scheduled time never starts the draft.

### Commissioner

Follow this sequence from the owning league's `/admin` or `/draft` page. Do not
operate one league from another league's Commissioner HQ card.

1. **Verify the meeting, not just the calendar invite.** Read the rendered draft
   date, local time, timezone, mode, round count, and pick duration. Ask one
   manager to confirm that `/draft` shows the same meeting. A scheduled meeting
   is inert: reaching or passing that timestamp does not start or arm the draft.
2. **Resolve the seat map.** About an hour before the meeting, resolve every
   unclaimed seat. If the league will contract, drop those seats before the
   order is randomized. Commissioner-delegated `AUTO` is available only for a
   claimed seat; an unclaimed franchise is not a substitute manager.
3. **Classify each claimed seat without collapsing three different facts.**
   `HERE`/`IDLE`/`AWAY`/`NOT SEEN` is live presence, `READY` is the manager's
   deliberate check-in, and `AUTO` is persisted pick authority. Presence never
   silently enables AUTO, and READY never starts the room. For a manager known
   to be absent, confirm the manager's intent, turn on commissioner-delegated
   AUTO for that claimed seat, and tell the room that its first available Big
   Board player will be used before best available.
4. **Clear every Board gap.** The commissioner sees each claimed team's Board
   count and whether it is empty, never its private ranking order. Ask the
   primary or co-manager to add targets. A deliberate empty Board is allowed
   only when the league accepts authoritative best-available ranking as that
   seat's complete fallback.
5. **Verify provider capacity and truth.** The player pool must contain at least
   `teams × draft rounds` distinct eligible players. `LIVE` is preferred. A
   labeled last-good cache may be accepted when its player count covers that
   capacity and its age/error are understood. `OFFLINE`, `UNAVAILABLE`, an
   unexplained provider error, or an undersized pool blocks a real draft.
6. **Verify order and schedule together.** The published order must contain each
   active team exactly once. Confirm round one top-to-bottom and round two in
   reverse for the snake. The initial draw publishes the regular-season
   schedule in the same operation; inspect the first-week pairings, team count,
   configured span, and any odd-team bye before proceeding. Starting the draft
   locks the order. Do not redraw merely because a manager dislikes a result.
7. **Read the readiness row aloud.** Name every claimed seat that is not ready,
   not present, on AUTO, or has a Board gap. Resolve or explicitly accept each
   exception. This is the final reversible checkpoint.
8. **Start intentionally.** Only when pick one should begin immediately, type
   `START` into the rendered start control and submit once. The scheduled time,
   manager readiness, browser presence, and AUTO settings cannot perform this
   mutation.
9. **Require one exclusive clock state.** The room must show exactly one of:
   `NOT RUNNING` before explicit start or after terminal completion; `PAUSED`
   with persisted remaining seconds; or `RUNNING` with one authoritative
   deadline. Conflicting labels are a stop condition: refresh before acting.
10. **Pause before resolving a room dispute.** Pause freezes the remaining
    seconds and survives refresh/restart. It does not erase the on-clock seat or
    prohibit an intentional manual/forced selection. Resume once; it arms a new
    deadline from the persisted remaining duration. Do not press resume twice.
11. **Extend only the running pick currently shown.** Extension is unavailable
    when paused, unarmed, or complete. It adds the entered seconds to the current
    deadline within the product's clock bounds. The form is bound to the exact
    pick and deadline on screen; if another client acts first, the submission is
    stale and must make no change.
12. **Force the current pick only as an explicit ruling.** Open the destructive
    control, read the named consequence, type `FORCE CURRENT PICK`, and submit
    once. This immediately selects the first still-available player on that
    seat's Big Board, otherwise authoritative best available, and advances the
    draft even while paused. Missing, mistyped, stale, or repeated submissions
    must fail without consuming a player.
13. **Undo only the exact last pick displayed.** Type `UNDO` after explaining the
    correction to the room. Undo returns that player to availability, reopens the
    same draft slot, and re-arms its clock when running. During a pause, the room
    remains paused. A stale or repeated undo must not remove an earlier pick.
14. **Verify terminal completion.** After the final configured slot, confirm the
    pick count equals `teams × draft rounds`, the clock reads `NOT RUNNING`, no
    deadline or on-clock action remains, and every team's `/team` roster contains
    the expected slot count. Then check the pick tape and transaction/activity
    record before dismissing managers.

#### Draft refresh and restart recovery

Use this recovery sequence; never repair draft state by editing SQLite.

1. **After a browser refresh:** read the pick number, on-clock team, exclusive
   clock state, deadline/remaining time, and latest pick again before submitting.
   A managed action may update only its page region; a native form may redirect
   through a full page. Both must render the same persisted result.
2. **After a stale or double response:** stop. The server rejects the old
   current-pick/previous-pick token before mutation. Do not change the typed value
   and resubmit the same page. Reload, explain what another client changed, and
   decide again from the newly rendered state.
3. **After a process restart with a paused clock:** `PAUSED` and its persisted
   remaining seconds stay unchanged. Presence may temporarily read `NOT SEEN`;
   wait for manager heartbeats instead of converting that observation into AUTO.
4. **After a process restart with a future running deadline:** the stored future
   deadline remains authoritative. The restart does not add time.
5. **After a process restart past an expired running deadline:** Gridiron grants
   one bounded `RestartGrace` deadline—30 seconds or the configured pick duration,
   whichever is smaller—rather than immediately punishing the on-clock manager.
   Confirm that new deadline, then pause if the room still needs reconciliation.
   A restart never starts a draft that was not explicitly started.
6. **After any restart:** allow the two-minute boot presence guard to receive
   heartbeats before interpreting `NOT SEEN`; it prevents a restart from
   shortening every manager's clock at once. Verify pool health and the latest
   pick again, then resume normal operation from the persisted state.

### Manager

1. Keep `/draft` open and enable sound through a normal page interaction.
2. Watch the on-clock team, remaining time, pick tape, and player availability.
3. Pick from the available player pool. LIVE is fresh from the source; CACHED, STALE, or DEGRADED is a labeled last-good snapshot that remains usable. OFFLINE or UNAVAILABLE must be resolved before a real draft starts. If the clock expires, Gridiron uses the first available Big Board player and then best available rank.
4. Use `/team` to verify the roster as picks accumulate.

Do not rehearse start, pick, undo, or reset actions against either live production league. Use a disposable demo state or a separate staging instance.

## Weekly operating rhythm

### Before the first kickoff

- Managers set starters at `/team` and resolve empty or invalid lineup slots.
- A player's slot locks when that player's game begins. Other players whose games have not started remain editable.
- Managers review injuries and source freshness. An unpublished injury file is `AWAITING_RELEASE`, not evidence that nobody is injured.
- Commissioners review `/commissioner` for attention items across every configured league, then follow the owning league's link to act.

### During games

- Matchup totals are provisional calculations from effective lineups and the best available source: the live box-score overlay when `LIVE_SCORING_ENABLED=true` and a player's game is in progress, otherwise the mirrored player ledger. Neither is official, play-by-play scoring — see [Game day](#game-day) for the exact precedence and states.
- Signal Wire reports are provisional and may inform a manager; they never mutate a lineup, roster, or score.
- A rostered player whose game has started cannot be dropped until the week closes.

### Waivers and free agency

- A true free agent can be added immediately while roster and position limits permit it.
- A dropped player enters waivers for the configured clear window.
- A player whose game has started is unavailable for immediate addition and follows the displayed waiver timing.
- Claims resolve on the configured daily processing clock. Performance-priority leagues use the displayed order; FAAB leagues use bids and the configured budget.
- A claim is revalidated when it resolves. A stale claim cannot bypass roster capacity, position limits, current ownership, or budget.
- A manager may hold open claims up to the roster size at once. Cancel a claim to file another past that limit.
- A manager may file at most 60 claims per rolling hour. Filing faster is refused with a clear message. The count lives in memory per running instance; a restart resets it.
- An injured-reserve occupant may be named as a claim's drop, but it frees no roster spot: IR already sits outside the roster cap, so the claim still needs an open spot for the add.
- A claim on a player who leaves the pool during a run stays open and deferred. It resolves once the player returns to the pool, or the manager cancels it. The manager sees one notice on the first deferred run. A claim deferred across 3 consecutive runs, spanning at least 48 hours, expires automatically, with a final notice naming the reason.
- A beaten FAAB claim reports only that another team won the player, never the winning bid amount. A manager sees their own bid at all times.
- The commissioner sees every team's waiver receipts, not only their own team's. The commissioner may also force an out-of-cycle run from `/admin` when a run is stuck or overdue.

### Punter rankings

The live Tank01 feed carries no punter ADP and no punter projections. Gridiron ranks punters from the league's own 2025 season, rescored under the league's punting rules and embedded in the app.

- The player pool shows a `P##` label for each ranked punter, for example `P01` for the top punter by projected points per game.
- A punter absent from the embedded data shows `—`, the same dash every other unranked player shows.
- Market ADP never covers a punter, so the `P##` label is the only rank a punter ever carries.
- A punter with fewer than 8 games shows no rank.
- A punter's 2025 line has no games floor. A punter below the rank floor can still show a line and `—` for rank.
- The pool sync keeps a limited number of players. A punter cut by that limit shows no rank.

### House rank

Market ADP ranks players for a generic fantasy market, not the league's own roster shape. House rank re-ranks the same pool by replacement value under the league's actual starter demand.

- House rank measures VORP (value over replacement player). VORP is a player's per-game projection minus the projected per-game points of the best player left at that position after demand is filled.
- Demand comes from the active roster preset: each position's own starter slot count, times the team count.
- The league fills FLEX and SUPERFLEX slots one at a time. Each open slot goes to the eligible position whose next player projects highest.
- A position whose pool runs out inside its own demand gets a replacement level of zero. Every remaining player there then carries a very high VORP.
- A tied VORP breaks first by market ADP, then by name. A missing ADP sorts last.
- The player pool shows an `H##` label beside the market rank, for example `H01` for the top house-ranked player.
- A zero-projection player carries no house rank and shows no `H##` label.
- Autopick selects from house order. The player pool and the draft-room board still sort by market ADP.
- The house-rank model counts starter demand only. It carries no bench-depth term.

### Trades

- Only a manager with a franchise seat can compose an offer.
- Only claimed franchises are valid trade partners.
- The configured deadline, review duration, and veto authority on `/scoring` are authoritative.
- Players locked by an active game remain locked until the week closes; accepting an offer does not bypass that lock.

### Closing the week

1. In `/admin`, select the schedule week and read the readiness reason.
2. Normal close becomes available after every known real game is final and the player-stat dataset satisfies the freshness gate.
3. Close the ready week. Gridiron records matchup results and pins each team's effective starters for that week.
4. Confirm the week reads `FINAL`, standings advance, and the next schedule week becomes current.
5. Repeating close is idempotent: it makes no lineup or scoring change.

`Force close week N` is an exception path. It requires the exact typed confirmation and deliberately bypasses advisory readiness. Before forcing it, record why the upstream schedule or ledger cannot satisfy the normal gate and accept that the current mirrored inputs become the closed result.

## Game day

Regular-season live scoring (`internal/livescore`) is gated by `LIVE_SCORING_ENABLED`, defaulting to `false`. When on, each instance polls in-progress Tank01 box scores through `statrelay` every `LIVE_POLL_INTERVAL` (default `5s`, up to `LIVE_MAX_INFLIGHT` concurrent game fetches, capped at `LIVE_DAILY_BUDGET` fetches per instance per day) and overlays them onto the mirrored nflverse ledger. The overlay never changes which source is *authoritative for a closed week* — only how an *open* week's provisional total is computed while games are in progress.

### The four states

| State | Meaning | Matchups status line |
| --- | --- | --- |
| `LIVE` | The poller has a healthy, in-progress signal for at least one starter's game. | `Live box scores · checked N s ago` |
| `FINAL` | The poller marked a starter's game final, but the mirrored weekly ledger has not posted that player's corrected stats yet. | `Final box scores · weekly ledger pending` |
| `LEDGER` | No live signal is authoritative right now — pre-kickoff, the week is closed, or every relevant stat already sits in the mirrored nflverse file. | `Weekly ledger (nflverse)` |
| `PAUSED` | The live poller itself is degraded (the relay returned 429, the daily budget is exhausted, or repeated relay/listing failures) while a starter's game the poller has already recorded as in progress. The kill switch alone cannot produce this state: flipping it restarts the process, and the restarted poller has no game history to pause on — see [Kill-switch procedure](#kill-switch-procedure). | `Live box scores paused · <reason>` |

### Precedence

1. A live row wins while that player's game is in progress and the poller itself is healthy (not degraded).
2. A ledger row wins once the game is final.
3. A ledger row wins whenever live has no data for that player (Tank01 omits a player from the box score until their first recorded stat; a starter with no live row yet and no ledger row either still renders an honest `0.0` once the game is known to have started, or a dash before kickoff or during a known poller outage — never an implicit, unlabeled zero).
4. Once the commissioner closes a week, the posted final score is always authoritative and never changes, regardless of any later live or ledger correction; the mismatch (if any) is called out beside the posted total, not silently absorbed.

### Tank01 game-status code rule

Live scoring classifies a game from Tank01's `gameStatusCode`:

- `"2"` — final.
- `"1"` — in progress.
- `"0"` or `""` — pre-game.
- Any other code paired with a non-empty `currentPeriod` — treated as in progress (a code Gridiron does not otherwise recognize, but the game has clearly started).

### Kill-switch procedure

`LIVE_SCORING_ENABLED` is read once, at process start (`internal/livescore`'s `Poller`). Nothing re-reads it while the process runs. So "flipping the flag" always means a pod roll, never a live in-process toggle, and the new pod's poller always starts with an empty box-score history — it has never ticked, so it has never seen any game as in progress.

That has a direct consequence for the state you will observe. `PAUSED` (the precedence above) requires the *current* poller to already hold an in-progress game in its own memory; a pod that just booted with the flag off never reaches that condition, no matter how far into a game the drill runs. **The drill's correct, expected result is `LEDGER`, not `PAUSED · disabled`.** `LEDGER` here still proves the kill switch works — the poller is off (`Health().Enabled == false`), degraded, and correctly reports `disabled` — it is only the *status-line label* that differs from a same-process runtime toggle, because this product has no such toggle. Confirmed on the harness: `TestSimGameDayTimeline` (`sim_gameday_test.go`) restarts a live-in-progress child with the flag off and observes `LEDGER` every time; see [Replay harness evidence](#replay-harness-evidence) below.

1. Confirm at least one starter's game is currently in progress (the status line already reads `LIVE`).
2. Set the flag to `false` on flagship (`LIVE_SCORING_ENABLED=false` on its ConfigMap/Deployment) and roll the pod. Flagship is the only live instance for 2026 (the Stable Kernel league did not form).
3. Within 60 seconds, confirm the Matchups status line reads `LEDGER` (`Weekly ledger (nflverse)`) and `/api/health`-adjacent poller diagnostics (or an operator route, once one exists) show the poller disabled.
4. To resume, set `LIVE_SCORING_ENABLED=true` and roll again; confirm the state chip reads `LIVE` within 60 seconds.
5. Log the drill (date, times, and confirmed state transitions) in `docs/launch-checklist.md`. The Sep 10 2026 TNF drill (DAL@PHI, kickoff 20:20 EDT) is the first scheduled run: at 21:00 EDT (one hour after kickoff, a game in progress), set the flag to `false` on flagship, confirm `LEDGER` within 60 s, set it back to `true`, and confirm `LIVE` within 60 s.

### Replay harness evidence

`go test . -run 'TestSimReplay' -race -count=1 -timeout 900s` replays the BAL@BUF 2025 play-by-play behind a fake relay end to end (poller, overlay, fingerprint, hub, and browser unchanged) and proves the p95 goal — a point change reaches `/matchups` within 10 s of Tank01 ingestion — without touching production or an upstream vendor. Measured on the harness lane (`gridiron-2000-livescore-20260829`, 2026-08-30, `go1.26.0`, under `-race` and concurrent load from other sessions on the same host):

| Scenario | Measured wall time | Budget |
| --- | --- | --- |
| `TestSimReplayScoresFlowThroughOverlayFingerprintAndHub` | 27.4 s | ≤ 30 s |
| `TestSimReplayWindowClosesFiveHoursAfterKickoff` | 33.4 s (45.0 s including test-binary build), after replacing two fixed 6 s sleeps with `waitForInWindow` polling (15 s cap each) | ≤ 45 s |
| `TestBrowserReplayScoreReachesMatchupsWithinTenSeconds` | 42.0 s (per-change latency 0.6 s – 2.6 s, all ≤ 10 s) | ≤ 45 s |
| `TestBrowserMatchupsFitsPhoneWidthAndExpandsScorebugs` | 26.6 s | ≤ 30 s |
| Whole root run (`go test . -count=1`) | 130.8 s (2:11.5), under concurrent host load from other sessions | "under two minutes" (Goal 6) |

The whole-root-run figure above was measured with several other agents' work running concurrently on the same host (background builds and an unrelated project's test suite); no quiet-host baseline has been recorded yet. Re-run on an otherwise idle host before treating a Goal 6 regression as real.

`go test . -run 'TestSimGameDayTimeline' -count=1 -timeout 900s` walks the whole Sep 10 2026 timeline end to end in one scenario, using the same replay fixture as one continuous game: boot with the flag off (`LEDGER`, poller off) — restart with the flag on 30 minutes before kickoff (enabled, idle, zero box-score fetches while the window is closed) — advance to 5 minutes before kickoff (the window opens, frames flow, `LIVE`) — restart with the flag off an hour after kickoff, a game in progress (the kill-switch drill: `LEDGER`, not `PAUSED · disabled` — see the finding in [Kill-switch procedure](#kill-switch-procedure) above) — restart with the flag on (`LIVE` resumes) — advance past kickoff+5h (the window closes). Measured wall time: 41.1 s (`gridiron-2000-gameday-20260830`, 2026-08-30, `go1.26.0`).

`perf-budget.json`'s `league` profile (`/`, `/matchups`, `/team`, `/login`) caps `js_total_kb` at 90 on a gzip transfer-byte basis. A signed-out `/` stays at the pre-existing inline navigation enhancer alone (24 KB gzip) — the bootstrap hub bundle Task 6 added loads only for seated pages. A seated page (the GoSX bootstrap runtime plus the scores-live hub chunk) measures 77 KB gzip (`bootstrap-runtime` 40 KB + `bootstrap-feature-hubs` 14 KB + the 24 KB floor), comfortably inside the 90 KB cap.

## Fleet-scale commissioner operations

`/commissioner` is a fleet readout, not a multi-tenant database.

- It shows each configured instance's release, runtime, seats, readiness, draft, schedule, week-close, player pool, open-data state, and attention items.
- A fleet may contain any number of isolated league instances. Each instance lists the peers it should display; use a full peer mesh when every HQ should show the complete fleet.
- It intentionally excludes manager identities and other league PII.
- It does not share browser sessions or grant cross-host mutation authority.
- It does not merge memberships, rosters, drafts, transactions, or SQLite state.
- Every action link opens the league that owns the state. Confirm the hostname before submitting a commissioner action.

An unavailable peer card means the local league cannot obtain a current authenticated summary from that peer. It does not mean the remote database is empty. Open the remote host directly and check `/api/health` before changing either deployment.

## Degraded-data decisions

Use the least destructive response that preserves an honest league record.

| Observation | Decision |
| --- | --- |
| Pool is `CACHE`, error is empty, and player count covers roster capacity. | Normal operation may continue; tell managers the snapshot is cached. |
| Pool is `OFFLINE`. | Rehearsal remains available. Do not imply live ADP, injuries, projections, or news. |
| Player count is below roster capacity. | Do not start the draft. Restore the provider/relay or deliberately reduce the league shape before any pick. |
| Schedule or stats are `AWAITING_RELEASE`. | Wait. This is expected before the publisher releases the season asset. |
| Schedule, stats, or injuries are `ERROR`. | Inspect source status and application logs. Keep an already-final week unchanged. |
| Week close says games are unfinished or stats are stale. | Use normal close later. Force close only as a recorded commissioner ruling. |
| Commissioner HQ peer is unavailable. | Operate the reachable league independently and diagnose the peer; do not infer peer state from the local cache. |

`/healthz` answers whether the process can serve. `/api/health` is the richer configuration and dependency snapshot, including the PII-free `stateSchema` release evidence. Provider degradation should remain visible even when the process itself is alive.

## Corrections and recovery

- Draft mistake, stale action, browser refresh, or process restart: follow the
  exact [draft refresh and restart recovery](#draft-refresh-and-restart-recovery)
  sequence. Use typed last-pick undo for the exact latest pick; do not edit
  SQLite by hand.
- Wrong team claim: the commissioner releases the seat; the manager then claims the correct open seat.
- Wrong invited identity: correct the invitation or configured alias. Do not create an alias merely to bypass admission policy.
- Incorrect open week lineup: change only players whose games have not locked.
- Incorrect final week: stop and make a commissioner ruling before attempting any repair. A final week is intentionally immutable through normal manager flows.
- Source outage: preserve the last good cache and diagnose the relay or upstream. Do not replace a known snapshot with fabricated zeros.
- Release failure: follow `docs/launch-checklist.md`, canary Stable Kernel first, and adjudicate both recorded logical and physical state-schema bounds before using any prior revision. If an old binary cannot read either persisted marker, roll forward or use the separately tested compatible fallback digest; an image-only undo is forbidden.

## Current year-one boundary

Gridiron records the regular-season phase and final matchup results. The
postseason boundary is after the last regular-season week closes and standings
are final. In the playoffs phase, the commissioner previews the deterministic
bracket generated from that final standings snapshot, checks the displayed
qualification/seeding/tie-break explanations, and publishes the exact preview.
Publish is idempotent for the same preview ID; a retry must not create a second
bracket. Weekly advancement accepts only final authoritative result records,
never partial, stale, degraded, or unavailable scores. A correction is a
separate explicit, confirmed, reasoned, audited operation; do not edit the
SQLite bracket row by hand.

### Dynasty season boundary

DYNASTY describes the league's long-term format intent; it is not evidence that this release can roll state into a new season. Year one starts with the configured draft and a new season record. Automated multi-season roster rollover is not available yet. Before any future season, the commissioner must announce and manage keepers, carryover rosters, picks, credits, and other exceptions manually, then record the ruling in the league announcement or season record. Managers should not assume that a roster, history, or identity will carry forward automatically.

## Commissioner acceptance checklist

Before calling a league season-ready, verify:

- [ ] The live `/scoring` page matches the intended format and roster.
- [ ] Every manager can authenticate, and every intended franchise is claimed.
- [ ] Draft order, readiness, and player-pool capacity are visible.
- [ ] Every claimed seat exposes separate presence, readiness, AUTO, and Board-gap truth; unclaimed seats cannot receive commissioner AUTO.
- [ ] The commissioner can start only through explicit `START` confirmation.
- [ ] Pause, resume, extend, force-current-pick, undo, refresh, stale/double submission, restart grace, and terminal completion have been rehearsed on disposable state.
- [ ] The regular-season schedule is generated and its seed is recorded.
- [ ] Managers understand per-player lineup locks, waiver mode, and trade review.
- [ ] The normal week-close gate explains unfinished or stale inputs.
- [ ] `/commissioner` shows every expected instance without PII.
- [ ] `/api/health` exposes the expected release, provider, and compatible state-schema evidence.
- [ ] Every co-located league uses the shared relay rather than duplicating upstream credentials.
- [ ] The release rollback point is recorded before either deployment changes.

For deployment mechanics, immutable image provenance, SK-first canary order, and rollback commands, use [`launch-checklist.md`](launch-checklist.md). For the public manager-oriented introduction, use `/guide`.

# Season operations handbook

This handbook describes how a Gridiron league moves from configuration to draft night and through a regular-season week. It is for commissioners and managers. It describes the product's current behavior; it is not a promise that Gridiron copies another provider's workflow.

Use these sources of truth in this order:

1. The live `/scoring` page for league rules, roster shape, waiver mode, trade policy, schedule, and locks.
2. `/admin` for commissioner-owned operational state and explicit mutations.
3. `/commissioner` for a read-only view across configured league instances.
4. `/api/health` for build, provider, and open-data diagnostics.
5. This handbook for the operating sequence and recovery decisions.

If this handbook and the live league disagree, the live configured state wins.

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
4. Confirm `/api/health` reports the expected league configuration source, release identity, player-pool mode, player count, roster capacity, and no pool error.
5. Add manager invitations. A domain-gated league admits identities in the configured domain; use explicit invitations for permitted people outside it.
6. Ask every manager to claim the intended franchise before draft order is finalized.
7. At the draft-order milestone, one draw publishes both the final order and the default 14-week regular-season schedule, then sends one notification batch. Use the separate schedule control beforehand only when the league needs a custom span or first NFL week.

For a multi-instance Kubernetes topology, every league can use the shared `statrelay`. Only `statrelay-secrets` owns `TANK01_API_KEY`; each league sets `TANK01_BASE_URL` to the relay Service. The tracked flagship and Stable Kernel manifests are the current two-instance example, not a fleet-size limit.

## Manager five-minute setup

1. Follow the invite link and sign in with the invited Google identity.
2. Claim the correct franchise at `/join`. Signing in and owning a team seat are separate states.
3. Open `/scoring` and read the active league's roster, scoring, waivers, trades, and lock rules.
4. Open `/team`; set the franchise name and visual identity if the league permits it.
5. Build a ranked, deep Big Board at `/board`. Primary and co-manager share the seat-level board.
6. Open `/draft`, choose the truthful ready and autopick states, and leave the room available on draft night.

A custom PNG or JPEG becomes the franchise identity and releases any claimed stock badge so another team may claim it.

## Draft-night runbook

The scheduled time never starts the draft.

### Commissioner

1. About an hour before the meeting, resolve unclaimed seats. If the league will contract, drop those seats before randomizing the order.
2. Confirm the player pool covers `teams × draft rounds`. Treat target coverage as planning headroom; roster capacity is the hard draft requirement.
3. Draw or confirm the draft order. The initial draw runs six shuffle passes, publishes only the final result plus the regular-season schedule, and sends one manager notification batch. Starting the draft locks the order.
4. Review claimed and ready counts. A manager may be present but intentionally leave autopick off; ready and autopick are separate controls.
5. At the meeting time, confirm the room verbally. Type `START` in `/admin` or `/draft` only when pick one should begin immediately.
6. Use pause, resume, extend, or forced autopick when the room needs intervention. These controls change persisted draft-clock state.
7. If the most recent pick was a mistake, use the typed `UNDO` control. It reopens that draft slot and re-arms its clock.

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

- Matchup totals are provisional calculations from effective lineups and the mirrored player ledger. They are not sub-minute official scoring.
- Signal Wire reports are provisional and may inform a manager; they never mutate a lineup, roster, or score.
- A rostered player whose game has started cannot be dropped until the week closes.

### Waivers and free agency

- A true free agent can be added immediately while roster and position limits permit it.
- A dropped player enters waivers for the configured clear window.
- A player whose game has started is unavailable for immediate addition and follows the displayed waiver timing.
- Claims resolve on the configured daily processing clock. Performance-priority leagues use the displayed order; FAAB leagues use bids and the configured budget.
- A claim is revalidated when it resolves. A stale claim cannot bypass roster capacity, position limits, current ownership, or budget.

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

`/healthz` answers whether the process can serve. `/api/health` is the richer configuration and dependency snapshot. Provider degradation should remain visible even when the process itself is alive.

## Corrections and recovery

- Draft mistake: use the typed last-pick undo before proceeding. Do not edit SQLite by hand.
- Wrong team claim: the commissioner releases the seat; the manager then claims the correct open seat.
- Wrong invited identity: correct the invitation or configured alias. Do not create an alias merely to bypass admission policy.
- Incorrect open week lineup: change only players whose games have not locked.
- Incorrect final week: stop and make a commissioner ruling before attempting any repair. A final week is intentionally immutable through normal manager flows.
- Source outage: preserve the last good cache and diagnose the relay or upstream. Do not replace a known snapshot with fabricated zeros.
- Release failure: follow `docs/launch-checklist.md`, canary Stable Kernel first, and use the recorded prior deployment revisions or image digests.

## Current year-one boundary

Gridiron records the regular-season phase and final matchup results. The correct playoff boundary is after the last regular-season week closes and standings are final. The bracket engine exists, but automated commissioner seeding is not wired into the product yet; closing the final week advances the recorded phase without inventing a bracket. The commissioner must communicate the year-one postseason plan outside the unavailable control and avoid copy that implies seeding already occurred.

## Commissioner acceptance checklist

Before calling a league season-ready, verify:

- [ ] The live `/scoring` page matches the intended format and roster.
- [ ] Every manager can authenticate, and every intended franchise is claimed.
- [ ] Draft order, readiness, and player-pool capacity are visible.
- [ ] The commissioner can start only through explicit `START` confirmation.
- [ ] The regular-season schedule is generated and its seed is recorded.
- [ ] Managers understand per-player lineup locks, waiver mode, and trade review.
- [ ] The normal week-close gate explains unfinished or stale inputs.
- [ ] `/commissioner` shows every expected instance without PII.
- [ ] `/api/health` exposes the expected release and provider state.
- [ ] Every co-located league uses the shared relay rather than duplicating upstream credentials.
- [ ] The release rollback point is recorded before either deployment changes.

For deployment mechanics, immutable image provenance, SK-first canary order, and rollback commands, use [`launch-checklist.md`](launch-checklist.md). For the public manager-oriented introduction, use `/guide`.

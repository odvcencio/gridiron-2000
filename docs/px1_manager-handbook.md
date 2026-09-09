# Manager help projection

This is the manager-facing companion to [`/help`](../help) and the existing
[`app/guide`](../app/guide) page. It answers the common first-session path
without inventing a second source of truth.

## Five-minute path

1. Sign in with the identity admitted by this league and confirm the correct
   membership state at [`/help/identity-admission-and-membership`](../help/identity-admission-and-membership).
2. Claim or confirm the intended team seat at `/join`; sign-in and seat
   ownership are separate predicates.
3. Read the current `/scoring` configuration for roster slots, scoring,
   waivers, trades, schedule, and locks.
4. Build the team seat's private Big Board at
   [`/help/big-board-and-autopick`](../help/big-board-and-autopick). The
   primary manager and co-manager use the same durable order, and AUTO consumes
   that shared order before the best-available fallback.
5. Read the displayed draft or lineup state before acting. Use the owning
   route's current deadline, capability, freshness, and permission label.

## What to do when a page is not straightforward

- `loading`: wait for the current read/action; do not duplicate a mutation.
- `empty` or `no-results`: distinguish a valid empty collection from a filter
  that matches nothing; clear filters without calling the source empty.
- `pending`: keep the submitted context and wait for the displayed review or
  processor boundary; do not resubmit blindly.
- `locked` or `disabled`: read the reason and owning deadline; a lock is not a
  transient network error.
- `stale`, `degraded`, `offline`, or `unavailable`: read the last-success
  label and source capability. Do not convert an approximate snapshot into a
  final score or claim.
- `failed` or `permission-denied`: preserve context, use the recovery link,
  and ask the commissioner when the operation is authority-gated. If a
  transport response leaves the outcome unknown, refresh/reread the owning
  route and activity before retrying; do not replay the stale submission.

If a refresh or another manager changes the page, reread the persisted object,
current actor, and current state before retrying. The server's latest result
wins over an old browser view.

## Runtime precedence

The live league page owns mutable values: dates, rules, phase, clocks,
lineup/waiver/trade boundaries, feature capability, source freshness, and
last-success time. This handbook supplies navigation and safe interpretation;
it does not freeze those values.

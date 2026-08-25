# Commissioner help projection

Use the [`commissioner-operations`](../help/commissioner-operations) topic as
the short decision card, then follow the detailed
[`season-operations.md`](season-operations.md) runbook. Commissioner HQ is a
read-only cross-league projection; mutations belong to the owning league's
`/admin` or `/draft` route.

## Safe operating loop

1. Read the owning league, mode, normalized phase, current source state, and
   last-success label.
2. Confirm the actor and capability on the rendered control. Commissioner
   authority does not replace membership or team-seat identity.
3. Read the exact current object and deadline immediately before submitting.
4. Use the product's typed confirmation for consequential actions; submit
   once and reread the persisted result.
5. Record a concise commissioner note for a correction, force, or exception.
6. If the result is stale or conflicting, stop and recover from the owning
   route; never edit SQLite or infer state from a browser toast.

## Recovery links

- Draft readiness/order/clock: [`/help/draft-order-readiness-and-clock`](../help/draft-order-readiness-and-clock)
- Commissioner operations: [`/help/commissioner-operations`](../help/commissioner-operations)
- Activity and notes: [`/help/activity-and-commissioner-notes`](../help/activity-and-commissioner-notes)
- Data/freshness: [`/help/data-state-and-freshness`](../help/data-state-and-freshness)
- Detailed restart and week-close procedure: [`season-operations.md`](season-operations.md)

Start, pause, force, undo, close, waiver processing, and trade boundaries are
runtime-owned. This document intentionally describes the safety contract and
navigation, not a calendar or an unchangeable rule value.

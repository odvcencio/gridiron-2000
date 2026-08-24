# Decision 0001: one private Big Board per franchise seat

- Status: Accepted
- Version: 1.0
- Date: 2026-08-24

## Decision

The Big Board is one private, durable order per franchise seat. The primary
manager and co-manager both read and write that same order, and AUTO consumes
it before falling back to the best available player. The canonical owner key
is the normalized primary-manager identity for the seat. A commissioner may
see only seat-level readiness, presence, and board gap/count signals; private
rankings remain unavailable unless the commissioner is also a member of that
seat.

## Lifecycle rules

- Detaching a co-manager removes that person's access only. The seat board is
  retained under its canonical primary owner key for the next authorized
  operator.
- A primary transfer or identity migration carries the board to the new
  canonical owner in the same transaction as the seat change. No partial
  transfer may leave picks and AUTO pointing at different board keys.
- When importing legacy per-account boards, prefer the existing primary's
  order. If only the co-manager has a legacy order, promote that order to the
  seat key. If both differ, preserve the primary order and append co-manager
  entries that are not already present, keeping first appearance order.
- Every migration is idempotent and records the source keys and resulting
  order in the migration/audit record. A failed migration rolls back the
  member/key change and leaves both source boards untouched; retrying after
  correction is safe.
- Concurrent edits use the store's serialized mutation path. The last
  committed move/add/remove wins, and a successful action reports the updated
  order to the acting manager. Attribution/activity work remains a follow-up:
  record which authorized operator submitted each change without exposing
  the private ranking to commissioner summaries.

## Related product contract

The manager-facing operating handbook links here from its Big Board setup
step. UI actions must preserve the discovery query, position, page, and
board-pool return target across native and managed submissions.

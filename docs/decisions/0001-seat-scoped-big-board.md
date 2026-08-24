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
- Releasing a claimed seat folds the primary board first and any legacy
  co-manager board second into a unique, first-appearance order under the
  deterministic internal key `seat:<teamID>`. The primary and co-manager
  member records and their personal board keys are removed in that same
  mutation; pending co-invites and readiness are cleared as part of seat
  release.
- Reclaiming an open seat promotes its escrow order into the claimant's
  normalized primary identity key. Any existing claimant personal entries
  append after escrow entries, with duplicates removed by first appearance;
  the escrow key is deleted in the same mutation.
- Binding a co-manager folds that person's legacy personal board after the
  existing primary order, removes duplicate player IDs, stores the result
  under the normalized primary identity key, and deletes the co-manager key.
- Release, reclaim, and co-manager binding retries are idempotent. A
  persistence failure restores the exact pre-mutation state and does not
  publish a partial board or membership transition.
- A lifecycle merge is rejected before any state change when its distinct
  entries would exceed the 100-player board limit. Entries are never
  truncated; remove entries from one source board and retry the operation.
- Concurrent edits use the store's serialized mutation path. The last
  committed move/add/remove wins, and a successful action reports the updated
  order to the acting manager.

## Related product contract

The manager-facing operating handbook links here from its Big Board setup
step. UI actions must preserve the discovery query, position, page, and
board-pool return target across native and managed submissions.

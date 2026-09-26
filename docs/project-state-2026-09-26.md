# Project state — 2026-09-26

## Snapshot

- Repository: `odvcencio/gridiron-2000`; active source is `main` at `bda59bdb` (`bump(deps): bump gosx to v0.57.1`). The isolated worktree began clean and matched `origin/main`.
- No `AGENTS.md` is present. README and `docs/` describe a self-hosted, one-league-per-process fantasy-football platform. The current code includes Google sign-in, SQLite-backed league state, draft and Big Board, waiver/trade/lineup flows, live and mirrored scoring, Pick'em, postseason lifecycle, Commissioner HQ, backup/restore, and optional notifications.
- There are no open GitHub issues or pull requests. The latest merged PR is #71 (GoSX v0.57.1). No production deployment was performed or checked here.

## What works

- Main contains the latest security/operations hardening, scoring fixes, Board layout work, and GoSX v0.57.1 upgrade. Recent required CI jobs passed on the v0.57.1 PR.
- The September 26 scheduled CI run passed its non-short simulation suite. Its other jobs were skipped because they only run for push/PR events; this is not a failed required CI run.
- GoSX free/Ultra live-scoring profiles and the free-tier request arithmetic are already implemented and documented in `.env.example`, README, configuration, and season operations. The old Hyphae gap-wave plan still marks these complete items unchecked.

## Incomplete or stale

- The canonical Hyphae platform roadmap remains authoritative, but its last status receipt is dated September 8 and does not include later September merges. Its open completion gates include PX-1 human comprehension/owner review, full QA-1 release evidence, SY-1, NT-1, and PS-1. The code has substantial HQ, notification, and postseason support; do not treat source-level coverage alone as proof of the full acceptance gates.
- The roadmap says to complete postseason/bracket acceptance before the end of NFL Week 10. Treat that as the next product milestone: confirm current configured league rules, run restart/correction/tie/bye/degraded-source evidence, and record an owner-reviewed acceptance receipt.
- Stable Kernel was cancelled for the 2026 season and is offline. Do not present it as a live canary or deploy it. Its tracked configuration remains a future-instance template.
- `docs/px1_comprehension-gate.md` remains explicitly **OPEN — NOT RUN** and requires real participant responses and owner review. Agents should not fabricate this evidence.

## CI finding and current change

Two September 24 CI runs failed in the non-required browser job at `TestBrowserBoardRowDragReorderHasNoScrollJump`; the test timed out before its reorder request entered a pending state. CI installed GoSX CLI v0.56.3 and bypassed the version check, while `go.mod` pins v0.57.1. This change makes CI derive the CLI version from `go.mod` and adds a drift contract. With the exact v0.57.1 CLI and built runtime, the focused board test passed three runs, and the full e2e-tagged suite passed. One earlier local e2e run had a separate admin roster-correction assertion failure; its targeted test and a later full-suite rerun passed. The hosted PR CI run remains the final check for the runner environment.

## Recommended next work

1. Close the QA-1 browser evidence gap: confirm the CLI-aligned browser CI run is stable, then fix any remaining reproducible browser failures rather than widening global timeouts.
2. Finish PS-1 against the actual configured league: validate qualification/tiebreak rules and the documented restart, correction, tie, bye, and unavailable-source cases; record the receipt without deploying.
3. Close PX-1's human comprehension gate with participant evidence and owner review. Keep the offline Stable Kernel decision intact.

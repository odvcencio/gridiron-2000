# QA-1 acceptance matrix

QA-1 is a bounded, hermetic server/render acceptance pass for the product
truth dimensions that are easy to regress independently:

| Dimension | Values | Rows |
| --- | --- | ---: |
| Format | dynasty, redraft | 2 |
| Identity | anonymous, pending, seatless-open, seatless-full, primary, co-manager, commissioner overlay | 7 |
| Lifecycle | pre-draft, draft, preseason, regular, postseason, complete, unknown | 7 |
| Source posture | healthy, stale, degraded, offline, validation, recovery | 6 |

The canonical runner is `TestQA1AcceptanceMatrix` and executes all 588
Cartesian rows. It composes the existing `PublicEntryView`, commissioner
phase precedence, and source-health normalization contracts. Each identity
fixture is built with a `testing.T.TempDir` Store; the rows do not use the
process-wide league singleton, network calls, or persistent shared state.

Run the complete bounded evidence pass from the repository root:

```sh
scripts/qa1-acceptance-matrix.sh
```

The script also runs the existing GoSX render fixtures for public entry,
landing/home, matchup, and source-health copy. Those are server-render
contracts; this harness does not claim desktop/mobile browser evidence and
does not replace browser QA.

Focused commands are useful when diagnosing one dimension:

```sh
go test ./internal/league -run '^TestQA1AcceptanceMatrix$' -count=1
go test ./app/login -run '^TestPublicEntryRenderMatrixKeepsActionsTruthful$' -count=1
```

The matrix is intentionally test-only evidence tooling. It does not alter
league state, lifecycle transitions, source adapters, postseason consumers,
fleet/HQ product code, release metadata, or deployment behavior.

## Active draft-pool browser receipt — 2026-09-08

This supplemental receipt records a bounded active-draft check against an
isolated simulated league. The draft was still open (not completed), and the
run made no production or persistent league-state changes. Headless Chromium
used a coarse primary pointer and genuine CDP touch start/move/end gestures.
These measurements were captured from the isolated `9e100c0476ba746376ec3a25e76fa5862c68fca6`
test build (`9e100c0`), not from the later `cb21969` release artifact.
The pool body was swiped vertically, the position-chip rail was swiped
horizontally when it overflowed, and the search and position filters were
exercised after the touch checks. Values below are the observed scroll
positions and the pool's `scrollHeight/clientHeight` or chip-rail
`scrollWidth/clientWidth` geometry.

| Viewport | Pool body vertical pan | Position-chip rail | Search result | Position filter | Page horizontal overflow |
| --- | ---: | ---: | ---: | ---: | --- |
| 320×844 | 0 → 253 px (3263/249) | 0 → 136 px (408/272) | 50 → 1 | 1 active chip | None |
| 390×844 | 0 → 348 px (3269/322) | 0 → 67 px (408/341) | 50 → 1 | 1 active chip | None |
| 844×390 landscape | 0 → 72 px (2845/113) | Fits (415/415) | 50 → 1 | 1 active chip | None |
| 768×1024 tablet | 0 → 653 px (3428/548) | Fits (413/413) | 50 → 1 | 1 active chip | None |

The active pool retained `overflow-y: auto` and real scroll distance at every
size. No reproducible active-pool geometry or interaction defect was found;
no production CSS or application change was required by this receipt.

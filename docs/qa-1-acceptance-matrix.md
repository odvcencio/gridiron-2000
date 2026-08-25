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

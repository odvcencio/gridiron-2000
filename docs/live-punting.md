# Live punting scoring

Punting uses the configured league rules; live ingestion does not change those
rules or substitute all punting yards for the 40-or-more-yard-punts rule.

Tank01 box scores provide per-player `Punting` aggregates: `punts`, `puntYds`,
`puntLong`, `puntsin20`, and `puntTouchBacks`. The live parser retains these
rows, including a valid zero-punt row, and the existing score overlay joins
them to the punter's player identity.

- Inside-20 and touchback counts feed their actual scoring rules.
- A longest punt of at least 50 yards confirms one 50+ bonus. Aggregates do
  not prove the number of additional 50+ punts.
- With exactly one punt and matching total/longest yardage, the individual
  distance is known, so a punt of at least 40 yards earns its yardage points.
- With multiple punts, total yardage cannot prove which punts passed the
  40-yard gate. The parser does not score that aggregate as qualifying yards.
- Coffin-corner, inside-the-5, blocked-punt attribution, and full distance
  bonuses settle through the existing weekly play-by-play adapter.

During games, the starter disclosure labels aggregate-based punting as
provisional. A missing punter row is **pending**, not a confirmed 0.0, and
does not qualify for the win estimator's known-scoreless-live-player fallback.
No additional polling endpoint or request cadence is introduced.

For a verified one-punt box score of 51 yards, the default yardage rate
(`0.02`) plus one 50+ bonus (`1`) contributes 2.02 points, displayed as 2.0.
Commissioner-edited point values are resolved by the normal scoring engine.

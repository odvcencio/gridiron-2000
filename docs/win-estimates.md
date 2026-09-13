# Matchup wheel estimates

The wheel shows an **estimated** win chance, not betting odds or a calibrated
prediction. The matchup page's initial HTML and live updates use the same model.

Each starter contributes actual scored points plus their matching-week projection
multiplied by remaining game time. Remaining time uses quarter midpoints (87.5%,
62.5%, 37.5%, 12.5%); overtime currently retains 12.5%. Final players contribute
only actual points and no remaining uncertainty.

For unfinished players, the initial standard-deviation prior is the larger of
6 fantasy points and 65% of the player's projected points. Remaining variance is
that squared deviation multiplied by remaining time. Team variances are summed;
the projected score difference is evaluated against a normal distribution with
the combined variance. This assumes independent scoring increments and players.
It does not model quarterback/receiver correlation, opponent correlation, injury
hazards, or exact-clock play volume. The prior is deliberately explicit, **not
fitted or validated against historical outcomes**.

Before a verified final, displayed estimates stay between 1% and 99%. Verified
finals with known scores show WON, LOST, or TIED instead of probabilities. Missing
required projections, incomplete in-game scoring joins, degraded matchups, or
completed games awaiting authoritative finality show an unavailable estimate.
Finished players do not require an old projection to remain available.

Next accuracy milestone: retain pregame projection snapshots and evaluate forecast
errors and probability calibration out of sample (including Brier score), then
fit position/scoring-format variance and correlations. Until that evaluation,
describe this as a time-aware approximation, not a measured accuracy improvement.

This model currently powers matchup wheels. The Team terminal's separate pregame
comparison still uses the original fixed-gap estimate and should be consolidated
before treating estimates across all product surfaces as interchangeable.

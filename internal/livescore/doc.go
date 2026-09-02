// Package livescore polls Tank01 for in-progress NFL games and overlays the
// results onto the league's stat lines. A three-layer design bounds the
// request cost against the RapidAPI daily quota: a cheap scoreboard tick
// (score, clock, period, and possession for every game on the slate) gates a
// change-triggered box-score fetch, and a narrow wire-signal trigger forces
// an off-cycle fetch for one game.
//
// Poller is disabled by default (LIVE_SCORING_ENABLED) and every cadence and
// budget value comes from environment configuration, since the RapidAPI
// tier, not the code, decides what the deployment can afford.
package livescore

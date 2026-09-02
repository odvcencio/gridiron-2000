// Package fantasy owns the normalized NFL draft pool: player identity,
// position, team, ADP (average draft position), and season projection.
// Service syncs the pool from the Tank01 API on an interval when a key is
// configured, and serves the last good pool from disk between syncs; without
// a key it serves an embedded offline pool so a draft always has players.
//
// The package also parses live box scores and scoreboards for
// internal/livescore, and enriches punters from the league's own embedded
// prior-season rescoring, since the live Tank01 feed carries no punter
// projections.
package fantasy

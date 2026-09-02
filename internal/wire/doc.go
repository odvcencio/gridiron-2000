// Package wire ingests public Bluesky posts and RSS/Atom feeds over the
// Jetstream firehose, classifies each post into a bounded Signal category
// (for example touchdown, turnover, big play, or kicking play), and scores
// source trust before publishing a Signal to the league's news readout.
//
// A Signal is deliberately provisional, not a fantasy scoring event: it
// stays unconfirmed until a structured stats dataset agrees. A narrow subset
// of fast, fantasy-relevant categories can also trigger an out-of-band
// internal/livescore box-score fetch ahead of the next scoreboard tick.
package wire

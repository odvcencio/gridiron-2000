// Package openstats mirrors free, openly licensed nflverse datasets
// (schedules, player stats, injuries, team stats, and play-by-play; CC-BY-4.0,
// attributed to nflverse) to local disk on a configurable interval, so the
// league app never depends on nflverse's uptime during a live request.
//
// Service downloads each dataset, verifies it with a SHA-256 checksum and
// ETag, and exposes a DatasetStatus per dataset plus an overall Status for
// operator visibility into freshness and download health.
package openstats

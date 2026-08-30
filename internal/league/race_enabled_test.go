//go:build race

package league

// raceDetectorEnabled is true only in a `go test -race` binary. Its own
// per-memory-access instrumentation legitimately inflates a timing-based
// test's wall-clock cost well past what the same code costs un-instrumented
// (draft_history_test.go's TestDraftHistoryStaysFastAtFullDraftPickCount
// measured 4-6ms under -race against under 1ms without it, on the same
// machine) — a tight threshold meant to catch a real algorithmic
// regression must not also fail on race-detector overhead alone.
const raceDetectorEnabled = true

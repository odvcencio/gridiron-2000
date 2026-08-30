package replay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
)

// defaultStep is Serve's step fallback when the caller passes a
// non-positive duration.
const defaultStep = 2 * time.Second

// Server replays one Game's frames behind a fake Tank01 relay: an
// httptest.Server that answers getNFLBoxScore and getNFLGamesForWeek the
// way the real relay does, advancing one frame per step of wall time
// since Serve was called. The real poller, overlay, fingerprint, hub, and
// browser run unchanged against it (Task 9).
type Server struct {
	game    *Game
	frames  []Frame
	step    time.Duration
	now     func() time.Time
	start   time.Time
	eastern *time.Location
	http    *httptest.Server

	mu     sync.Mutex
	served map[int]time.Time // frame index -> first time it was served
	last   int               // highest frame index served so far
}

// Serve starts a fake relay for game, advancing one frame every step of
// wall time as measured by now. start is fixed at now() when Serve is
// called, so the replay's kickoff is always "now," never the fixture's
// own original kickoff.
func Serve(game *Game, step time.Duration, now func() time.Time) *Server {
	if now == nil {
		now = time.Now
	}
	if step <= 0 {
		step = defaultStep
	}
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		eastern = time.UTC
	}
	s := &Server{
		game:    game,
		frames:  game.Frames(),
		step:    step,
		now:     now,
		start:   now(),
		eastern: eastern,
		served:  map[int]time.Time{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/getNFLBoxScore", s.handleBoxScore)
	mux.HandleFunc("/getNFLGamesForWeek", s.handleGamesForWeek)
	mux.HandleFunc("/", s.handleNotFound)
	s.http = httptest.NewServer(mux)
	return s
}

// URL is the fake relay's base URL, suitable for fantasy.NewBoxScoreClient.
func (s *Server) URL() string { return s.http.URL }

// Close stops the underlying httptest.Server. Safe to register on
// AppRuntime.closers and call exactly once.
func (s *Server) Close() { s.http.Close() }

// Start is the replay's kickoff instant: now() at the moment Serve ran.
func (s *Server) Start() time.Time { return s.start }

// Step is the wall-clock duration between served frames.
func (s *Server) Step() time.Duration { return s.step }

// FrameCount is the total number of frames this replay serves, including
// the pre-game and final frames.
func (s *Server) FrameCount() int { return len(s.frames) }

// ServedIndex is the highest frame index served so far (0 before the
// first request).
func (s *Server) ServedIndex() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// ServedAt is the first time frame index was served, or the zero time if
// it never has been.
func (s *Server) ServedAt(index int) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.served[index]
}

// ScheduleSource adapts this replay to the league schedule: one game, ID
// "replay-<Tank01 ID>" so it never collides with a real schedule row,
// kickoff pinned to Start, teams normalized to nflverse abbreviations, and
// Final once the real final frame has actually been served at least once
// (not merely reached by elapsed time — a caller that has not fetched it
// yet must not see the game as over).
func (s *Server) ScheduleSource() league.ScheduleSource {
	return func() []league.GameInfo {
		final := s.ServedIndex() >= len(s.frames)-1
		return []league.GameInfo{{
			ID:      "replay-" + s.game.ID,
			Week:    1,
			Kickoff: s.Start(),
			Away:    livescore.NormalizeTeam(s.game.Away),
			Home:    livescore.NormalizeTeam(s.game.Home),
			Final:   final,
		}}
	}
}

// indexAt is the frame index a request at instant now should receive:
// one frame per step of elapsed time since Start, clamped to the last
// (final) frame once the replay has run its course, and never negative
// for a request that somehow arrives before Start.
func (s *Server) indexAt(now time.Time) int {
	elapsed := now.Sub(s.start)
	if elapsed < 0 {
		elapsed = 0
	}
	index := int(elapsed / s.step)
	if max := len(s.frames) - 1; index > max {
		index = max
	}
	return index
}

// recordServed tracks the first time index was served and the highest
// index served so far, both read back by ServedAt/ServedIndex.
func (s *Server) recordServed(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index > s.last {
		s.last = index
	}
	if _, ok := s.served[index]; !ok {
		s.served[index] = s.now()
	}
}

// handleBoxScore answers getNFLBoxScore for this replay's own game only:
// a request naming a different (or missing) gameID gets a plain 404,
// never this game's frame, and is not counted toward ServedIndex/ServedAt
// (round-2 review of commit 698ec54, finding 7).
func (s *Server) handleBoxScore(w http.ResponseWriter, r *http.Request) {
	if gameID := r.URL.Query().Get("gameID"); gameID != s.game.ID {
		s.handleNotFound(w, r)
		return
	}
	index := s.indexAt(s.now())
	s.recordServed(index)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.frames[index].Body)
}

// statusAt mirrors the status/statusCode Frames itself assigned to frame
// index, so the week listing's gameStatus/gameStatusCode track the box
// score it is listing.
func (s *Server) statusAt(index int) (status, code string) {
	switch {
	case index <= 0:
		return "Scheduled", "0"
	case index >= len(s.frames)-1:
		return "Completed", "2"
	default:
		return "Live - In Progress", "1"
	}
}

func (s *Server) handleGamesForWeek(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	status, statusCode := s.statusAt(s.indexAt(now))
	listing := map[string]any{
		"gameID": s.game.ID,
		// gameDate is the replay's start date, never the fixture's own
		// gameDate: the poller matches a schedule game to a Tank01
		// listing by kickoff date, and this replay's schedule row
		// (ScheduleSource) carries Start as its kickoff, so the listing
		// must agree with that, not with whatever date the recorded game
		// actually happened on (round-1 finding 1).
		"gameWeek":       "Week 1",
		"away":           s.game.Away,
		"home":           s.game.Home,
		"gameDate":       s.start.In(s.eastern).Format("20060102"),
		"gameTime_epoch": s.start.Unix(),
		"gameStatus":     status,
		"gameStatusCode": statusCode,
	}
	writeEnvelope(w, []any{listing})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"statusCode":404}`))
}

func writeEnvelope(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	raw, err := json.Marshal(struct {
		StatusCode int `json:"statusCode"`
		Body       any `json:"body"`
	}{StatusCode: 200, Body: body})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(raw)
}

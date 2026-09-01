package league

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Playoff lifecycle values are persisted with the bracket.  A preview is
// commissioner-only working state; published is the one bracket truth that
// downstream readers may consume.
const (
	PlayoffStatusPreview   = "preview"
	PlayoffStatusPublished = "published"

	PlayoffPublishConfirmation    = "PUBLISH PLAYOFF BRACKET"
	PlayoffCorrectionConfirmation = "CORRECT PLAYOFF BRACKET"
)

// PlayoffProvenance identifies the immutable standings snapshot from which a
// bracket was built.  SourceState and Authoritative are deliberately explicit
// so a cached, partial, or degraded feed can never be mistaken for final
// standings.  CapturedAt is supplied by the caller; rules and dates remain
// runtime-owned rather than being invented by the bracket engine.
type PlayoffProvenance struct {
	SnapshotID             string    `json:"snapshotId"`
	Source                 string    `json:"source"`
	SourceState            string    `json:"sourceState"`
	CapturedAt             time.Time `json:"capturedAt"`
	FinalWeek              int       `json:"finalWeek"`
	RegularSeasonStartWeek int       `json:"regularSeasonStartWeek"`
	TiebreakOrder          []string  `json:"tiebreakOrder"`
	Authoritative          bool      `json:"authoritative"`
}

// PlayoffResultProvenance records the source quality attached to a final
// playoff result.  It is kept on the matchup so a public bracket can explain
// why an advancement was accepted without consulting a transient feed.
type PlayoffResultProvenance struct {
	Source        string    `json:"source"`
	SourceState   string    `json:"sourceState"`
	ObservedAt    time.Time `json:"observedAt"`
	Authoritative bool      `json:"authoritative"`
}

// PlayoffAuditEntry is the public, PII-free audit shape for preview, publish,
// advancement, and manual correction transitions.  Callers should pass a
// role or internal actor label, not an email address.
type PlayoffAuditEntry struct {
	Action               string    `json:"action"`
	Actor                string    `json:"actor"`
	At                   time.Time `json:"at"`
	Reason               string    `json:"reason,omitempty"`
	PreviewID            string    `json:"previewId,omitempty"`
	MatchupID            string    `json:"matchupId,omitempty"`
	PreviousWinnerTeamID string    `json:"previousWinnerTeamId,omitempty"`
	WinnerTeamID         string    `json:"winnerTeamId,omitempty"`
	Revision             int       `json:"revision"`
}

// PlayoffCorrection is the explicit confirmation boundary for a commissioner
// correction.  Only a published bracket may be corrected, and the confirmation
// phrase is intentionally not inferred from a button or request method.
type PlayoffCorrection struct {
	MatchupID      string
	WinnerTeamID   string
	HomeScore      float64
	AwayScore      float64
	ScoresProvided bool
	Actor          string
	Reason         string
	Confirmation   string
	At             time.Time
}

// ValidatePostseasonConfig validates fields that are safe to check while
// loading league.json.  Season-bound checks (the bracket's final week) happen
// when a final schedule is available in ValidatePlayoffConfig.
func ValidatePostseasonConfig(cfg PlayoffConfig, leagueTeamCount, divisionCount int) error {
	if cfg.TeamCount == 0 {
		if cfg.StartWeek != 0 || cfg.RoundLengthWeeks != 0 || cfg.DivisionWinnersFirst || cfg.Reseed || cfg.Consolation || cfg.ToiletBowl || cfg.Byes != 0 || cfg.Qualification != "" || len(cfg.TiebreakOrder) != 0 {
			return fmt.Errorf("postseason.teamCount must be set when postseason rules are configured")
		}
		return nil
	}
	if cfg.TeamCount < 2 || cfg.TeamCount > 8 {
		return fmt.Errorf("postseason.teamCount must be between 2 and 8")
	}
	if leagueTeamCount > 0 && cfg.TeamCount > leagueTeamCount {
		return fmt.Errorf("postseason.teamCount (%d) exceeds the league size (%d)", cfg.TeamCount, leagueTeamCount)
	}
	if cfg.StartWeek < 1 {
		return fmt.Errorf("postseason.startWeek must be at least 1")
	}
	if cfg.RoundLengthWeeks != 1 && cfg.RoundLengthWeeks != 2 {
		return fmt.Errorf("postseason.roundLengthWeeks must be 1 or 2")
	}
	if cfg.DivisionWinnersFirst && divisionCount == 0 {
		return fmt.Errorf("postseason.divisionWinnersFirst requires divisions to exist")
	}
	if cfg.DivisionWinnersFirst && divisionCount > 0 && cfg.TeamCount < divisionCount {
		return fmt.Errorf("postseason.divisionWinnersFirst requires teamCount (%d) >= division count (%d)", cfg.TeamCount, divisionCount)
	}
	switch cfg.Qualification {
	case "", "top-record", "division-winners-wildcards":
	default:
		return fmt.Errorf("postseason.qualification must be one of top-record, division-winners-wildcards")
	}
	if cfg.Qualification == "division-winners-wildcards" && divisionCount == 0 {
		return fmt.Errorf("postseason.qualification requires divisions to exist")
	}
	if cfg.Byes < 0 || cfg.Byes > cfg.TeamCount {
		return fmt.Errorf("postseason.byes must be between 0 and %d", cfg.TeamCount)
	}
	expectedByes := nextPow2(cfg.TeamCount) - cfg.TeamCount
	if cfg.Byes != 0 && cfg.Byes != expectedByes {
		return fmt.Errorf("postseason.byes must equal %d for %d playoff teams", expectedByes, cfg.TeamCount)
	}
	if len(cfg.TiebreakOrder) > 0 {
		if err := ValidateTiebreakChain(cfg.TiebreakOrder); err != nil {
			return fmt.Errorf("postseason.tiebreakOrder: %w", err)
		}
	}
	return nil
}

// NewPlayoffProvenance hashes a deterministic final standings snapshot.  The
// hash is independent of input order, while the captured timestamp and source
// remain visible metadata on the persisted bracket.
func NewPlayoffProvenance(standings []Standing, finalWeek int, capturedAt time.Time, tiebreakOrder []string) (PlayoffProvenance, error) {
	return NewPlayoffProvenanceFromSource(standings, finalWeek, 1, capturedAt, "regular-season-final", "final", true, tiebreakOrder)
}

// NewPlayoffProvenanceFromSource is the explicit constructor used by callers
// that have a named authoritative source or a non-default regular-season
// start week.  It refuses source states that are not final.
func NewPlayoffProvenanceFromSource(standings []Standing, finalWeek, regularSeasonStartWeek int, capturedAt time.Time, source, sourceState string, authoritative bool, tiebreakOrder []string) (PlayoffProvenance, error) {
	if finalWeek < 1 {
		return PlayoffProvenance{}, fmt.Errorf("postseason final week must be at least 1")
	}
	if regularSeasonStartWeek < 1 || regularSeasonStartWeek > finalWeek {
		return PlayoffProvenance{}, fmt.Errorf("postseason regular-season start week must be between 1 and final week")
	}
	if capturedAt.IsZero() {
		return PlayoffProvenance{}, fmt.Errorf("postseason provenance requires capturedAt")
	}
	if strings.TrimSpace(source) == "" {
		return PlayoffProvenance{}, fmt.Errorf("postseason provenance requires source")
	}
	if strings.ToLower(strings.TrimSpace(sourceState)) != "final" || !authoritative {
		return PlayoffProvenance{}, fmt.Errorf("postseason provenance requires an authoritative final source")
	}
	order := append([]string(nil), tiebreakOrder...)
	if len(order) == 0 {
		order = append([]string(nil), DefaultTiebreakChain...)
	}
	if err := ValidateTiebreakChain(order); err != nil {
		return PlayoffProvenance{}, fmt.Errorf("postseason provenance tiebreakOrder: %w", err)
	}
	snapshotID, err := playoffStandingsSnapshotID(standings, finalWeek)
	if err != nil {
		return PlayoffProvenance{}, err
	}
	return PlayoffProvenance{
		SnapshotID:             snapshotID,
		Source:                 strings.TrimSpace(source),
		SourceState:            "final",
		CapturedAt:             capturedAt.UTC(),
		FinalWeek:              finalWeek,
		RegularSeasonStartWeek: regularSeasonStartWeek,
		TiebreakOrder:          order,
		Authoritative:          true,
	}, nil
}

type playoffStandingsEnvelope struct {
	FinalWeek int        `json:"finalWeek"`
	Standings []Standing `json:"standings"`
}

func playoffStandingsSnapshotID(standings []Standing, finalWeek int) (string, error) {
	ordered := append([]Standing(nil), standings...)
	seenTeams := make(map[string]struct{}, len(ordered))
	for _, standing := range ordered {
		if strings.TrimSpace(standing.TeamID) == "" {
			return "", fmt.Errorf("postseason standings contain an empty team id")
		}
		if _, exists := seenTeams[standing.TeamID]; exists {
			return "", fmt.Errorf("postseason standings contain duplicate team %q", standing.TeamID)
		}
		seenTeams[standing.TeamID] = struct{}{}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Rank != ordered[j].Rank {
			return ordered[i].Rank < ordered[j].Rank
		}
		return ordered[i].TeamID < ordered[j].TeamID
	})
	raw, err := json.Marshal(playoffStandingsEnvelope{FinalWeek: finalWeek, Standings: ordered})
	if err != nil {
		return "", fmt.Errorf("postseason standings snapshot: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// BuildPlayoffPreview is the pure commissioner-preview boundary. It builds
// one deterministic bracket and stamps it as preview; persistence and publish
// are intentionally separate Store operations.
func BuildPlayoffPreview(standings []Standing, divisions map[string]string, cfg PlayoffConfig, provenance PlayoffProvenance) (PlayoffState, error) {
	if cfg.TeamCount < 2 {
		return PlayoffState{}, fmt.Errorf("postseason is disabled or has no playoff teams")
	}
	if err := validatePlayoffProvenance(provenance); err != nil {
		return PlayoffState{}, err
	}
	if expected, err := playoffStandingsSnapshotID(standings, provenance.FinalWeek); err != nil {
		return PlayoffState{}, err
	} else if provenance.SnapshotID != expected {
		return PlayoffState{}, fmt.Errorf("postseason provenance snapshot does not match final standings")
	}
	divisionCount := 0
	seenDivisions := map[string]struct{}{}
	for _, division := range divisions {
		if division == "" {
			continue
		}
		if _, ok := seenDivisions[division]; !ok {
			seenDivisions[division] = struct{}{}
			divisionCount++
		}
	}
	if err := ValidatePlayoffConfig(cfg, len(standings), divisionCount, provenance.RegularSeasonStartWeek, provenance.FinalWeek); err != nil {
		return PlayoffState{}, err
	}
	if cfg.Qualification == "division-winners-wildcards" {
		cfg.DivisionWinnersFirst = true
	}
	if len(cfg.TiebreakOrder) == 0 {
		cfg.TiebreakOrder = append([]string(nil), provenance.TiebreakOrder...)
	}
	state, err := GeneratePlayoffState(standings, divisions, cfg)
	if err != nil {
		return PlayoffState{}, err
	}
	state.Status = PlayoffStatusPreview
	state.PreviewID = playoffPreviewID(provenance.SnapshotID, cfg)
	state.Revision = 1
	state.Provenance = clonePlayoffProvenance(provenance)
	state.PreviewedAt = provenance.CapturedAt.UTC()
	return state, nil
}

func playoffPreviewID(snapshotID string, cfg PlayoffConfig) string {
	raw, _ := json.Marshal(struct {
		SnapshotID string
		Config     PlayoffConfig
	}{snapshotID, cfg})
	digest := sha256.Sum256(raw)
	return "preview-" + hex.EncodeToString(digest[:])
}

func clonePlayoffProvenance(in PlayoffProvenance) PlayoffProvenance {
	in.TiebreakOrder = append([]string(nil), in.TiebreakOrder...)
	return in
}

func standingSeedExplanation(standing Standing) string {
	if standing.DecidedBy != "" {
		return "final standings tie-break: " + standing.DecidedBy
	}
	return "final standings record"
}

func validatePlayoffProvenance(provenance PlayoffProvenance) error {
	if strings.TrimSpace(provenance.SnapshotID) == "" {
		return fmt.Errorf("postseason provenance requires snapshotId")
	}
	if strings.TrimSpace(provenance.Source) == "" {
		return fmt.Errorf("postseason provenance requires source")
	}
	if strings.ToLower(strings.TrimSpace(provenance.SourceState)) != "final" {
		return fmt.Errorf("postseason provenance source must be final")
	}
	if !provenance.Authoritative {
		return fmt.Errorf("postseason provenance must be authoritative")
	}
	if provenance.CapturedAt.IsZero() {
		return fmt.Errorf("postseason provenance requires capturedAt")
	}
	if provenance.FinalWeek < 1 || provenance.RegularSeasonStartWeek < 1 || provenance.RegularSeasonStartWeek > provenance.FinalWeek {
		return fmt.Errorf("postseason provenance has invalid season weeks")
	}
	if err := ValidateTiebreakChain(provenance.TiebreakOrder); err != nil {
		return fmt.Errorf("postseason provenance tiebreakOrder: %w", err)
	}
	return nil
}

func effectivePlayoffStatus(state PlayoffState) string {
	if state.Status != "" {
		return state.Status
	}
	// A pre-PS-1 state with a non-empty bracket was already the deployed
	// bracket truth. Keep it readable after migration, while requiring new
	// mutations to use the explicit preview/published lifecycle.
	if len(state.Seeds) > 0 || len(state.Matchups) > 0 || state.ChampionTeamID != "" || state.RunnerUpTeamID != "" || state.ToiletTeamID != "" {
		return PlayoffStatusPublished
	}
	return ""
}

func playoffNow(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}

func playoffWinnerExplanation(m PlayoffMatchup, res PlayoffRoundResult) string {
	if m.HomeScore != m.AwayScore {
		return fmt.Sprintf("round score decides: %.2f over %.2f", m.HomeScore, m.AwayScore)
	}
	if res.HomeBestWeek != res.AwayBestWeek {
		return fmt.Sprintf("tied round score; best single week decides: %.2f over %.2f", res.HomeBestWeek, res.AwayBestWeek)
	}
	return fmt.Sprintf("tied round score and best week; seed decides: %d over %d", m.HomeSeed, m.AwaySeed)
}

func playoffResultProvenance(res PlayoffRoundResult) *PlayoffResultProvenance {
	if res.Source == "" && res.SourceState == "" && res.ObservedAt.IsZero() && !res.Authoritative {
		return nil
	}
	return &PlayoffResultProvenance{
		Source:        res.Source,
		SourceState:   strings.ToLower(strings.TrimSpace(res.SourceState)),
		ObservedAt:    res.ObservedAt.UTC(),
		Authoritative: res.Authoritative,
	}
}

// ValidateAuthoritativePlayoffRound rejects partial, degraded, unavailable,
// or non-final result sets before the pure advancement engine is called.
func ValidateAuthoritativePlayoffRound(state PlayoffState, results []PlayoffRoundResult) error {
	if len(results) == 0 {
		return fmt.Errorf("no authoritative playoff results supplied")
	}
	byID := make(map[string]PlayoffMatchup, len(state.Matchups))
	maxRound := map[string]int{}
	for _, matchup := range state.Matchups {
		byID[matchup.ID] = matchup
		if matchup.Round > maxRound[matchup.Bracket] {
			maxRound[matchup.Bracket] = matchup.Round
		}
	}
	active := map[string]struct{}{}
	for _, matchup := range state.Matchups {
		if matchup.Round == maxRound[matchup.Bracket] && !matchup.Final {
			active[matchup.ID] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	for _, result := range results {
		if strings.TrimSpace(result.MatchupID) == "" {
			return fmt.Errorf("playoff result is missing matchupId")
		}
		if _, duplicate := seen[result.MatchupID]; duplicate {
			return fmt.Errorf("duplicate playoff result %q", result.MatchupID)
		}
		seen[result.MatchupID] = struct{}{}
		matchup, ok := byID[result.MatchupID]
		if !ok {
			return fmt.Errorf("unknown playoff matchup %q", result.MatchupID)
		}
		if !result.Final || !result.Authoritative || strings.ToLower(strings.TrimSpace(result.SourceState)) != "final" {
			return fmt.Errorf("playoff result %q is partial or degraded; only authoritative final results may advance", result.MatchupID)
		}
		if strings.TrimSpace(result.Source) == "" || result.ObservedAt.IsZero() {
			return fmt.Errorf("playoff result %q is missing source provenance", result.MatchupID)
		}
		if math.IsNaN(result.HomeScore) || math.IsNaN(result.AwayScore) || math.IsInf(result.HomeScore, 0) || math.IsInf(result.AwayScore, 0) {
			return fmt.Errorf("playoff result %q has a non-finite score", result.MatchupID)
		}
		if matchup.Final {
			if matchup.HomeScore != result.HomeScore || matchup.AwayScore != result.AwayScore {
				return fmt.Errorf("playoff result %q conflicts with its persisted final score", result.MatchupID)
			}
			continue
		}
		if matchup.HomeTeamID == "" || matchup.AwayTeamID == "" {
			return fmt.Errorf("matchup %q has no opponent to score", result.MatchupID)
		}
		if _, ok := active[result.MatchupID]; !ok {
			return fmt.Errorf("playoff result %q is not in the active round", result.MatchupID)
		}
	}
	for matchupID := range active {
		if _, ok := seen[matchupID]; !ok {
			return fmt.Errorf("partial playoff results: missing matchup %q", matchupID)
		}
	}
	return nil
}

// AdvanceAuthoritativePlayoffRound is the strict pure advancement boundary.
// AdvancePlayoffRound remains available for legacy fixtures, but persisted
// production advancement should use this function or the Store method below.
func AdvanceAuthoritativePlayoffRound(state PlayoffState, results []PlayoffRoundResult) (PlayoffState, error) {
	if err := ValidateAuthoritativePlayoffRound(state, results); err != nil {
		return PlayoffState{}, err
	}
	return AdvancePlayoffRound(state, results)
}

// PlayoffTruth returns a cloned, authoritative bracket read model. A caller
// cannot mutate Store state through the returned pointer. Legacy brackets are
// read as published, but cannot pass the new mutation boundaries without a
// fresh preview.
func (s *Store) PlayoffTruth() *PlayoffState {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.Playoffs == nil {
		return nil
	}
	out := clonePlayoffState(s.state.Playoffs)
	if out.Status == "" {
		out.Status = effectivePlayoffStatus(*out)
	}
	return out
}

func playoffAudit(action, actor string, at time.Time, revision int) PlayoffAuditEntry {
	return PlayoffAuditEntry{Action: action, Actor: strings.TrimSpace(actor), At: playoffNow(at), Revision: revision}
}

func (s *Store) SetPlayoffPreview(preview PlayoffState, actor string, at time.Time) error {
	if preview.Status != PlayoffStatusPreview {
		return fmt.Errorf("playoff preview must have status %q", PlayoffStatusPreview)
	}
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("playoff preview requires an actor")
	}
	if preview.PreviewID == "" {
		return fmt.Errorf("playoff preview requires previewId")
	}
	if err := validatePlayoffProvenance(preview.Provenance); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if current := s.state.Playoffs; current != nil {
		status := effectivePlayoffStatus(*current)
		if status == PlayoffStatusPublished {
			return fmt.Errorf("a published playoff bracket cannot be replaced without an explicit correction")
		}
		if status == PlayoffStatusPreview && current.PreviewID == preview.PreviewID {
			return nil
		}
	}
	previous := cloneState(s.state)
	previousDirty := s.dirty
	candidate := clonePlayoffState(&preview)
	if previous.Playoffs != nil && previous.Playoffs.Revision >= candidate.Revision {
		candidate.Revision = previous.Playoffs.Revision + 1
	}
	audit := playoffAudit("preview", actor, at, candidate.Revision)
	audit.PreviewID = candidate.PreviewID
	audit.Reason = "commissioner preview"
	candidate.Audit = append(append([]PlayoffAuditEntry(nil), candidate.Audit...), audit)
	s.state.Playoffs = candidate
	if err := s.persistLocked(colPlayoffs); err != nil {
		if persistDispositionOf(err) == persistNotCommitted {
			s.state = previous
			s.dirty = previousDirty
		}
		return err
	}
	return nil
}

// PublishPlayoffPreview is idempotent for the same preview ID. It requires an
// explicit confirmation phrase and an actor, so a GET/retry cannot publish a
// preview accidentally.
func (s *Store) PublishPlayoffPreview(previewID, confirmation, actor string, at time.Time) (PlayoffState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return PlayoffState{}, err
	}
	if strings.TrimSpace(previewID) == "" {
		return PlayoffState{}, fmt.Errorf("playoff publish requires previewId")
	}
	if strings.TrimSpace(confirmation) != PlayoffPublishConfirmation {
		return PlayoffState{}, fmt.Errorf("playoff publish requires explicit confirmation")
	}
	if strings.TrimSpace(actor) == "" {
		return PlayoffState{}, fmt.Errorf("playoff publish requires an actor")
	}
	if s.state.Playoffs == nil {
		return PlayoffState{}, fmt.Errorf("playoff preview does not exist")
	}
	current := s.state.Playoffs
	if effectivePlayoffStatus(*current) == PlayoffStatusPublished {
		if current.PreviewID == previewID {
			return *clonePlayoffState(current), nil
		}
		return PlayoffState{}, fmt.Errorf("a different playoff bracket is already published")
	}
	if current.Status != PlayoffStatusPreview || current.PreviewID != previewID {
		return PlayoffState{}, fmt.Errorf("commissioner preview must precede publish")
	}
	if err := validatePlayoffProvenance(current.Provenance); err != nil {
		return PlayoffState{}, err
	}
	previous := cloneState(s.state)
	previousDirty := s.dirty
	candidate := clonePlayoffState(current)
	candidate.Status = PlayoffStatusPublished
	candidate.PublishedAt = playoffNow(at)
	candidate.Revision++
	audit := playoffAudit("publish", actor, candidate.PublishedAt, candidate.Revision)
	audit.PreviewID = previewID
	audit.Reason = "commissioner published preview"
	candidate.Audit = append(append([]PlayoffAuditEntry(nil), candidate.Audit...), audit)
	s.state.Playoffs = candidate
	if err := s.persistLocked(colPlayoffs); err != nil {
		if persistDispositionOf(err) == persistNotCommitted {
			s.state = previous
			s.dirty = previousDirty
		}
		return PlayoffState{}, err
	}
	return *clonePlayoffState(candidate), nil
}

// AdvancePublishedPlayoffRound persists only fully authoritative final
// results. A partial result set is rejected before any state mutation.
func (s *Store) AdvancePublishedPlayoffRound(results []PlayoffRoundResult) (PlayoffState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return PlayoffState{}, err
	}
	if s.state.Playoffs == nil || s.state.Playoffs.Status != PlayoffStatusPublished {
		return PlayoffState{}, fmt.Errorf("published playoff bracket is required before advancement")
	}
	if err := ValidateAuthoritativePlayoffRound(*s.state.Playoffs, results); err != nil {
		return PlayoffState{}, err
	}
	next, err := AdvanceAuthoritativePlayoffRound(*s.state.Playoffs, results)
	if err != nil {
		return PlayoffState{}, err
	}
	if reflect.DeepEqual(next.Matchups, s.state.Playoffs.Matchups) && next.ChampionTeamID == s.state.Playoffs.ChampionTeamID && next.RunnerUpTeamID == s.state.Playoffs.RunnerUpTeamID && next.ToiletTeamID == s.state.Playoffs.ToiletTeamID {
		return *clonePlayoffState(s.state.Playoffs), nil
	}
	previous := cloneState(s.state)
	previousDirty := s.dirty
	next.Revision = s.state.Playoffs.Revision + 1
	at := time.Time{}
	for _, result := range results {
		if result.ObservedAt.After(at) {
			at = result.ObservedAt
		}
	}
	audit := playoffAudit("advance", "system", at, next.Revision)
	audit.Reason = "authoritative final results"
	next.Audit = append(append([]PlayoffAuditEntry(nil), s.state.Playoffs.Audit...), audit)
	s.state.Playoffs = &next
	collections := []collectionID{colPlayoffs}
	if next.ChampionTeamID != "" && s.state.Phase != PhaseSeasonComplete {
		s.state.Phase = PhaseSeasonComplete
		collections = append(collections, colScalars)
	}
	if err := s.persistLocked(collections...); err != nil {
		if persistDispositionOf(err) == persistNotCommitted {
			s.state = previous
			s.dirty = previousDirty
		}
		return PlayoffState{}, err
	}
	return *clonePlayoffState(&next), nil
}

// CorrectPublishedPlayoff applies only a terminal-matchup correction. Earlier
// rounds require a new preview because downstream participants would need to
// be recomputed from the corrected result; refusing that case preserves one
// authoritative bracket truth instead of publishing a stale descendant.
func (s *Store) CorrectPublishedPlayoff(correction PlayoffCorrection) (PlayoffState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return PlayoffState{}, err
	}
	if s.state.Playoffs == nil || s.state.Playoffs.Status != PlayoffStatusPublished {
		return PlayoffState{}, fmt.Errorf("published playoff bracket is required before correction")
	}
	if strings.TrimSpace(correction.Confirmation) != PlayoffCorrectionConfirmation {
		return PlayoffState{}, fmt.Errorf("playoff correction requires explicit confirmation")
	}
	if strings.TrimSpace(correction.Actor) == "" || strings.TrimSpace(correction.Reason) == "" {
		return PlayoffState{}, fmt.Errorf("playoff correction requires actor and reason")
	}
	if strings.TrimSpace(correction.MatchupID) == "" || strings.TrimSpace(correction.WinnerTeamID) == "" {
		return PlayoffState{}, fmt.Errorf("playoff correction requires matchupId and winnerTeamId")
	}
	index := -1
	for i, matchup := range s.state.Playoffs.Matchups {
		if matchup.ID == correction.MatchupID {
			index = i
			break
		}
	}
	if index < 0 {
		return PlayoffState{}, fmt.Errorf("unknown playoff matchup %q", correction.MatchupID)
	}
	matchup := s.state.Playoffs.Matchups[index]
	if !matchup.Final {
		return PlayoffState{}, fmt.Errorf("playoff correction requires a final matchup")
	}
	if correction.WinnerTeamID != matchup.HomeTeamID && correction.WinnerTeamID != matchup.AwayTeamID {
		return PlayoffState{}, fmt.Errorf("playoff correction winner must be a matchup team")
	}
	if matchup.Round < bracketTotalRounds(s.state.Playoffs, matchup.Bracket) {
		return PlayoffState{}, fmt.Errorf("earlier-round correction requires a new playoff preview")
	}
	if matchup.Bracket != "championship" && matchup.Bracket != "toilet" {
		return PlayoffState{}, fmt.Errorf("playoff correction is not supported for bracket %q", matchup.Bracket)
	}
	if correction.ScoresProvided {
		if math.IsNaN(correction.HomeScore) || math.IsNaN(correction.AwayScore) || math.IsInf(correction.HomeScore, 0) || math.IsInf(correction.AwayScore, 0) || correction.HomeScore < 0 || correction.AwayScore < 0 {
			return PlayoffState{}, fmt.Errorf("playoff correction has an invalid score")
		}
	}
	if matchup.WinnerTeamID == correction.WinnerTeamID && (!correction.ScoresProvided || (matchup.HomeScore == correction.HomeScore && matchup.AwayScore == correction.AwayScore)) {
		return *clonePlayoffState(s.state.Playoffs), nil
	}
	previous := cloneState(s.state)
	previousDirty := s.dirty
	candidate := clonePlayoffState(s.state.Playoffs)
	target := &candidate.Matchups[index]
	previousWinner := target.WinnerTeamID
	target.WinnerTeamID = correction.WinnerTeamID
	if correction.ScoresProvided {
		target.HomeScore = correction.HomeScore
		target.AwayScore = correction.AwayScore
	}
	target.ResultProvenance = &PlayoffResultProvenance{
		Source:        "commissioner-correction",
		SourceState:   "final",
		ObservedAt:    playoffNow(correction.At),
		Authoritative: true,
	}
	target.TieBreakExplanation = "manual correction: " + strings.TrimSpace(correction.Reason)
	if matchup.Bracket == "championship" {
		loser := target.AwayTeamID
		if target.WinnerTeamID == target.AwayTeamID {
			loser = target.HomeTeamID
		}
		candidate.ChampionTeamID = target.WinnerTeamID
		candidate.RunnerUpTeamID = loser
	} else if matchup.Bracket == "toilet" {
		loser := target.AwayTeamID
		if target.WinnerTeamID == target.AwayTeamID {
			loser = target.HomeTeamID
		}
		candidate.ToiletTeamID = loser
	}
	candidate.Revision++
	audit := playoffAudit("correction", correction.Actor, correction.At, candidate.Revision)
	audit.Reason = strings.TrimSpace(correction.Reason)
	audit.MatchupID = correction.MatchupID
	audit.PreviousWinnerTeamID = previousWinner
	audit.WinnerTeamID = correction.WinnerTeamID
	candidate.Audit = append(append([]PlayoffAuditEntry(nil), candidate.Audit...), audit)
	s.state.Playoffs = candidate
	if err := s.persistLocked(colPlayoffs); err != nil {
		if persistDispositionOf(err) == persistNotCommitted {
			s.state = previous
			s.dirty = previousDirty
		}
		return PlayoffState{}, err
	}
	return *clonePlayoffState(candidate), nil
}

// AdminPreviewPlayoffs derives one final standings snapshot from the persisted
// closed schedule, then stores the commissioner preview. The commissioner
// console owns the action; every read consumer uses PlayoffTruth.
func (s *Service) AdminPreviewPlayoffs(r *http.Request, at time.Time) (PlayoffState, error) {
	if err := s.requireCommissioner(r); err != nil {
		return PlayoffState{}, err
	}
	state := s.store.Snapshot()
	if state.Phase != PhasePlayoffs {
		return PlayoffState{}, fmt.Errorf("playoff preview requires the playoffs phase")
	}
	if !scheduleHasAllFinalWeeks(state.Schedule) {
		return PlayoffState{}, fmt.Errorf("playoff preview requires every regular-season week to be final")
	}
	if s.cfg.Postseason.TeamCount < 2 {
		return PlayoffState{}, fmt.Errorf("postseason rules are not configured")
	}
	teamIDs := make([]string, 0, len(s.Teams()))
	divisions := make(map[string]string, len(s.Teams()))
	for _, team := range s.Teams() {
		teamIDs = append(teamIDs, team.ID)
		divisions[team.ID] = team.Division
	}
	finalWeek := 0
	startWeek := 0
	for _, week := range state.Schedule.Weeks {
		if startWeek == 0 || week.Week < startWeek {
			startWeek = week.Week
		}
		if week.Week > finalWeek {
			finalWeek = week.Week
		}
	}
	order := append([]string(nil), s.cfg.Postseason.TiebreakOrder...)
	standings := ComputeStandings(*state.Schedule, teamIDs, TiebreakInputs{Chain: order})
	provenance, err := NewPlayoffProvenanceFromSource(standings, finalWeek, startWeek, playoffNow(at), "regular-season-final", "final", true, order)
	if err != nil {
		return PlayoffState{}, err
	}
	preview, err := BuildPlayoffPreview(standings, divisions, s.cfg.Postseason, provenance)
	if err != nil {
		return PlayoffState{}, err
	}
	if err := s.store.SetPlayoffPreview(preview, "commissioner", at); err != nil {
		return PlayoffState{}, err
	}
	truth := s.store.PlayoffTruth()
	if truth == nil {
		return PlayoffState{}, fmt.Errorf("playoff preview was not persisted")
	}
	if _, err := s.RecordCommissionerEvent(r, "playoff.preview", "previewed the playoff bracket", CommissionerEventRefs{}); err != nil {
		log.Printf("commissioner event: playoff.preview: %v", err)
	}
	return *truth, nil
}

func (s *Service) AdminPublishPlayoffs(r *http.Request, previewID, confirmation string, at time.Time) (PlayoffState, error) {
	if err := s.requireCommissioner(r); err != nil {
		return PlayoffState{}, err
	}
	published, err := s.store.PublishPlayoffPreview(previewID, confirmation, "commissioner", at)
	if err != nil {
		return PlayoffState{}, err
	}
	s.notifyPlayoffUpdate(s.store.Snapshot(), published, "published", at)
	if _, err := s.RecordCommissionerEvent(r, "playoff.publish", "published the playoff bracket", CommissionerEventRefs{}); err != nil {
		log.Printf("commissioner event: playoff.publish: %v", err)
	}
	return published, nil
}

func (s *Service) AdminAdvancePlayoffs(r *http.Request, results []PlayoffRoundResult) (PlayoffState, error) {
	if err := s.requireCommissioner(r); err != nil {
		return PlayoffState{}, err
	}
	advanced, err := s.store.AdvancePublishedPlayoffRound(results)
	if err != nil {
		return PlayoffState{}, err
	}
	s.notifyPlayoffUpdate(s.store.Snapshot(), advanced, "advanced", s.clock())
	if _, err := s.RecordCommissionerEvent(r, "playoff.advance", "advanced the playoff bracket", CommissionerEventRefs{}); err != nil {
		log.Printf("commissioner event: playoff.advance: %v", err)
	}
	return advanced, nil
}

func (s *Service) AdminCorrectPlayoff(r *http.Request, correction PlayoffCorrection) (PlayoffState, error) {
	if err := s.requireCommissioner(r); err != nil {
		return PlayoffState{}, err
	}
	correction.Actor = "commissioner"
	corrected, err := s.store.CorrectPublishedPlayoff(correction)
	if err != nil {
		return PlayoffState{}, err
	}
	s.notifyPlayoffUpdate(s.store.Snapshot(), corrected, "corrected", correction.At)
	if _, err := s.RecordCommissionerEvent(r, "playoff.correct", "corrected the playoff bracket", CommissionerEventRefs{}); err != nil {
		log.Printf("commissioner event: playoff.correct: %v", err)
	}
	return corrected, nil
}
